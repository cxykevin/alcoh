package input

import (
	"io"
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

// chunkReader 按分片投递字节并实现读超时接口：模拟终端把一条序列拆成多个
// 读批次送达。它不依赖平台是否支持真实超时，因此在 Windows 上同样可运行。
type chunkReader struct {
	chunks [][]byte
	index  int
	// deadlines 记录每次 SetReadDeadline 的入参：解析器会在窗口结束后清空
	// 期限，因此只记录调用轨迹才能断言窗口确实被设置过。
	deadlines []time.Time
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(p, chunk), nil
}

func (r *chunkReader) SetReadDeadline(t time.Time) error {
	r.deadlines = append(r.deadlines, t)
	return nil
}

// windowRequested 报告解析器是否设置过非零读超时窗口。
func (r *chunkReader) windowRequested() bool {
	for _, d := range r.deadlines {
		if !d.IsZero() {
			return true
		}
	}
	return false
}

// TestParserSplitEscapeSequence 验证被终端拆包送达的序列仍按序列解析：
// 先到的 ESC 必须在窗口内等到后续字节，不能提前判定为独立 Esc。
func TestParserSplitEscapeSequence(t *testing.T) {
	old := escapeWindow
	escapeWindow = 2 * time.Second
	defer func() { escapeWindow = old }()

	src := &chunkReader{chunks: [][]byte{[]byte("\x1b"), []byte("[A")}}
	p := NewParser(src)
	if got := nextKey(t, p); got != SimpleKey(KeyUp) {
		t.Fatalf("split CSI = %+v, want Up", got)
	}
	if !src.windowRequested() {
		t.Fatal("等待序列后续字节时必须设置读超时窗口")
	}
}

// splitReader 与 chunkReader 投递方式相同，但不实现读超时接口：
// 对应 Windows 控制台 / os.Pipe 这类不支持 SetReadDeadline 的输入源。
type splitReader struct {
	chunks [][]byte
	index  int
}

func (r *splitReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(p, chunk), nil
}

// TestParserNoDeadlineReaderFallsBack 验证输入源不支持读超时时保持既有语义：
// 没有后续字节可等，ESC 立即判定为独立 Esc，紧随其后的字节按普通按键解析。
func TestParserNoDeadlineReaderFallsBack(t *testing.T) {
	src := &splitReader{chunks: [][]byte{[]byte("\x1b"), []byte("[A")}}
	p := NewParser(src)
	want := []KeyEvent{SimpleKey(KeyEsc), RuneKey('[', ModNone), RuneKey('A', ModNone)}
	for i, w := range want {
		if got := nextKey(t, p); got != w {
			t.Fatalf("event %d = %+v, want %+v", i, got, w)
		}
	}
}

// TestParserStandaloneEscapeTimesOut 验证没有后续字节时 ESC 在窗口后判定为
// 独立 Esc 键，且超时不会损坏解析器（后续按键仍可解析）。
// 读超时依赖平台支持：不支持时该用例跳过，回退语义由上一个用例覆盖。
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
	if err := rd.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Skipf("输入源不支持读超时，跳过: %v", err)
	}
	_ = rd.SetReadDeadline(time.Time{})

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
