package input

import (
	"os"
	"testing"
	"time"
)

// TestParserCoalescedEscape 验证同一读批次里的多个 Esc 字节逐个还原成 Esc 键，
// 而不是被当成一次 Alt+Esc 吞掉（终端快速连按 Esc 时的真实字节形态）。
func TestParserCoalescedEscape(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []KeyEvent
	}{
		{"single", "\x1b", []KeyEvent{SimpleKey(KeyEsc)}},
		{"double", "\x1b\x1b", []KeyEvent{SimpleKey(KeyEsc), SimpleKey(KeyEsc)}},
		{"triple", "\x1b\x1b\x1b", []KeyEvent{SimpleKey(KeyEsc), SimpleKey(KeyEsc), SimpleKey(KeyEsc)}},
		// 连按两次 Esc 后紧接着又按了 a：第一次 Esc 仍必须还原成 Esc 键，
		// 第二个 Esc 与其后的 a 按终端语义继续解析为 Alt+a。
		{"double-then-rune", "\x1b\x1ba", []KeyEvent{SimpleKey(KeyEsc), RuneKey('a', ModAlt)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := feedParser([]byte(tt.data))
			for i, want := range tt.want {
				got := nextKey(t, p)
				if got != want {
					t.Fatalf("event %d = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

// TestParserSplitEscapeSequence 验证被终端拆包送达的序列仍按序列解析：
// 先到的 ESC 在窗口内等到后续字节，不能提前判定为独立 Esc。
func TestParserSplitEscapeSequence(t *testing.T) {
	old := escapeWindow
	escapeWindow = 2 * time.Second
	defer func() { escapeWindow = old }()

	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer rd.Close()
	defer wr.Close()

	p := NewParser(rd)
	if _, err := wr.WriteString("\x1b"); err != nil {
		t.Fatalf("write: %v", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = wr.WriteString("[A")
	}()
	if got := nextKey(t, p); got != SimpleKey(KeyUp) {
		t.Fatalf("split CSI = %+v, want Up", got)
	}
}

// TestParserStandaloneEscapeTimesOut 验证没有后续字节时 ESC 在窗口后判定为
// 独立 Esc 键，且超时不会损坏解析器（后续按键仍可解析）。
func TestParserStandaloneEscapeTimesOut(t *testing.T) {
	old := escapeWindow
	escapeWindow = 30 * time.Millisecond
	defer func() { escapeWindow = old }()

	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer rd.Close()
	defer wr.Close()

	p := NewParser(rd)
	if _, err := wr.WriteString("\x1b"); err != nil {
		t.Fatalf("write: %v", err)
	}
	start := time.Now()
	if got := nextKey(t, p); got != SimpleKey(KeyEsc) {
		t.Fatalf("standalone esc = %+v, want Esc", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("standalone esc waited %v, want about one window", elapsed)
	}
	// 超时读取后解析器必须仍然可用。
	if _, err := wr.WriteString("a"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := nextKey(t, p); got != RuneKey('a', ModNone) {
		t.Fatalf("after timeout = %+v, want rune a", got)
	}
}
