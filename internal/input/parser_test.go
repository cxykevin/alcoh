package input

import (
	"bytes"
	"testing"
)

// feedParser 用给定字节序列喂给解析器。
func feedParser(data []byte) *Parser {
	return NewParser(bytes.NewReader(data))
}

func nextKey(t *testing.T, p *Parser) KeyEvent {
	t.Helper()
	ev, err := p.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if ev.Kind != EventTypeKey {
		t.Fatalf("expected key event, got %+v", ev)
	}
	return ev.Key
}

func TestParserBasics(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want KeyEvent
	}{
		{"ascii", []byte("a"), RuneKey('a', ModNone)},
		{"utf8 cjk", []byte("中"), RuneKey('中', ModNone)},
		{"enter", []byte("\r"), SimpleKey(KeyEnter)},
		{"tab", []byte("\t"), SimpleKey(KeyTab)},
		{"backspace", []byte{0x7f}, SimpleKey(KeyBackspace)},
		{"ctrl-c", []byte{0x03}, RuneKey('c', ModCtrl)},
		{"ctrl-a", []byte{0x01}, RuneKey('a', ModCtrl)},
		{"ctrl-space", []byte{0x00}, RuneKey(' ', ModCtrl)},
		{"up", []byte("\x1b[A"), SimpleKey(KeyUp)},
		{"down", []byte("\x1b[B"), SimpleKey(KeyDown)},
		{"right", []byte("\x1b[C"), SimpleKey(KeyRight)},
		{"left", []byte("\x1b[D"), SimpleKey(KeyLeft)},
		{"home-csi", []byte("\x1b[H"), SimpleKey(KeyHome)},
		{"end-csi", []byte("\x1b[F"), SimpleKey(KeyEnd)},
		{"ctrl-up", []byte("\x1b[1;5A"), KeyEvent{Type: KeyUp, Mod: ModCtrl}},
		{"shift-tab", []byte("\x1b[Z"), KeyEvent{Type: KeyTab, Mod: ModShift}},
		{"home-tilde", []byte("\x1b[1~"), SimpleKey(KeyHome)},
		{"end-tilde", []byte("\x1b[4~"), SimpleKey(KeyEnd)},
		{"pageup", []byte("\x1b[5~"), SimpleKey(KeyPageUp)},
		{"pagedown", []byte("\x1b[6~"), SimpleKey(KeyPageDown)},
		{"delete", []byte("\x1b[3~"), SimpleKey(KeyDelete)},
		{"insert", []byte("\x1b[2~"), SimpleKey(KeyInsert)},
		{"f1-ss3", []byte("\x1bOP"), SimpleKey(KeyF1)},
		{"f4-ss3", []byte("\x1bOS"), SimpleKey(KeyF4)},
		{"f5", []byte("\x1b[15~"), SimpleKey(KeyF5)},
		{"alt-x", []byte("\x1bx"), RuneKey('x', ModAlt)},
		{"alt-utf8", []byte("\x1b中"), RuneKey('中', ModAlt)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := feedParser(tt.data)
			got := nextKey(t, p)
			if got != tt.want {
				t.Errorf("Next() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParserEscSequence(t *testing.T) {
	// 序列形式的输入：\x1b[A 应立即解析为 Up（不能误判为孤立 Esc）
	p := feedParser([]byte("\x1b[A"))
	ev := nextKey(t, p)
	if ev.Type != KeyUp {
		t.Errorf("expected Up, got %+v", ev)
	}
}

func TestParserMultipleKeys(t *testing.T) {
	p := feedParser([]byte("ab\x1b[B"))
	want := []KeyEvent{
		RuneKey('a', ModNone),
		RuneKey('b', ModNone),
		SimpleKey(KeyDown),
	}
	for i, w := range want {
		got := nextKey(t, p)
		if got != w {
			t.Errorf("key %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestParserCtrlRunes(t *testing.T) {
	p := feedParser([]byte{0x0e, 0x11, 0x15})
	want := []KeyEvent{
		RuneKey('n', ModCtrl),
		RuneKey('q', ModCtrl),
		RuneKey('u', ModCtrl),
	}
	for i, w := range want {
		got := nextKey(t, p)
		if got != w {
			t.Errorf("ctrl %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestParserSGRMouseWheel(t *testing.T) {
	// 滚轮上：`ESC[<64;12;7M`
	p := feedParser([]byte("\x1b[<64;12;7M"))
	ev, err := p.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != EventTypeMouse {
		t.Fatalf("expected mouse event, got %+v", ev)
	}
	if ev.Mouse.Button != MouseWheelUp || ev.Mouse.Action != MousePress {
		t.Fatalf("mouse = %+v, want wheel-up press", ev.Mouse)
	}
	if ev.Mouse.X != 12 || ev.Mouse.Y != 7 {
		t.Fatalf("coords = (%d,%d), want (12,7)", ev.Mouse.X, ev.Mouse.Y)
	}
}

func TestParserSGRMouseWheelDown(t *testing.T) {
	p := feedParser([]byte("\x1b[<65;3;9M"))
	ev, err := p.Next()
	if err != nil || ev.Kind != EventTypeMouse || ev.Mouse.Button != MouseWheelDown {
		t.Fatalf("event = %+v, err = %v", ev, err)
	}
}

func TestParserSGRMouseLeftPressRelease(t *testing.T) {
	p := feedParser([]byte("\x1b[<0;5;5M\x1b[<0;5;5m"))
	press, err := p.Next()
	if err != nil || press.Kind != EventTypeMouse || press.Mouse.Button != MouseLeft || press.Mouse.Action != MousePress {
		t.Fatalf("press = %+v, err = %v", press, err)
	}
	release, err := p.Next()
	if err != nil || release.Kind != EventTypeMouse || release.Mouse.Action != MouseRelease {
		t.Fatalf("release = %+v, err = %v", release, err)
	}
}

func TestParserSGRMouseModifiers(t *testing.T) {
	// Ctrl+Shift + wheel up: 64 + 4 + 16 = 84
	p := feedParser([]byte("\x1b[<84;1;1M"))
	ev, err := p.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Mouse.Button != MouseWheelUp {
		t.Fatalf("button = %v", ev.Mouse.Button)
	}
	if ev.Mouse.Mod&(ModCtrl|ModShift) != ModCtrl|ModShift {
		t.Fatalf("mod = %v", ev.Mouse.Mod)
	}
}

func TestParserKeyThenMouseInterleave(t *testing.T) {
	p := feedParser([]byte("a\x1b[<64;1;1Mb"))
	first := nextKey(t, p)
	if first != RuneKey('a', ModNone) {
		t.Fatalf("first = %+v", first)
	}
	middle, err := p.Next()
	if err != nil || middle.Kind != EventTypeMouse || middle.Mouse.Button != MouseWheelUp {
		t.Fatalf("middle = %+v, err = %v", middle, err)
	}
	last := nextKey(t, p)
	if last != RuneKey('b', ModNone) {
		t.Fatalf("last = %+v", last)
	}
}
