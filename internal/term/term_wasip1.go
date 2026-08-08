//go:build wasip1

package term

import (
	"io"
	"os"
	"strconv"

	"github.com/cxykevin/alcoh/internal/input"
)

// wasip1Term 是 GOOS=wasip1 GOARCH=wasm 的降级 Terminal 实现。
// WASI 无 termios/ioctl/信号：无 raw mode、无尺寸查询。
// 仅当 host 在 raw/cbreak 的 pty 下运行模块时，字节流才是可用的按键序列。
type wasip1Term struct {
	stdin  io.Reader
	stdout io.Writer

	evCh   chan Event
	stopCh chan struct{}
	parser *input.Parser
	w, h   int
}

// Open 返回 wasip1 降级终端。尺寸默认 80×24，可经 ALCOH_COLS/ALCOH_ROWS 覆盖。
func Open() (Terminal, error) {
	w, h := 80, 24
	if v, err := strconv.Atoi(os.Getenv("ALCOH_COLS")); err == nil && v > 0 {
		w = v
	}
	if v, err := strconv.Atoi(os.Getenv("ALCOH_ROWS")); err == nil && v > 0 {
		h = v
	}
	return &wasip1Term{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		evCh:   make(chan Event, 64),
		w:      w,
		h:      h,
	}, nil
}

func (t *wasip1Term) Size() (int, int) { return t.w, t.h }

func (t *wasip1Term) EnterRaw() error {
	if t.stopCh != nil {
		return nil
	}
	// WASI 无 raw mode：直接进入 alternate screen（若 host 支持 VT）；一并请求鼠标上报。
	t.Write([]byte("\x1b[?1049h\x1b[2J\x1b[H" + hideCursorSeq + mouseEnableSeq))
	t.stopCh = make(chan struct{})
	t.parser = input.NewParser(t.stdin)
	go t.readLoop()
	return nil
}

func (t *wasip1Term) ExitRaw() error {
	if t.stopCh == nil {
		return nil
	}
	close(t.stopCh)
	t.Write([]byte(mouseDisableSeq + "\x1b[0m\x1b[?25h\x1b[?1049l"))
	return nil
}

func (t *wasip1Term) Events() <-chan Event { return t.evCh }

func (t *wasip1Term) Write(p []byte) error {
	_, err := t.stdout.Write(p)
	return err
}

func (t *wasip1Term) CopyToClipboard(text string) error {
	_, err := t.stdout.Write(osc52Clipboard(text))
	return err
}

func (t *wasip1Term) readLoop() {
	for {
		ev, err := t.parser.Next()
		if err != nil {
			// stdin 非终端时可能立即 EOF，避免刷屏
			select {
			case t.evCh <- Event{Kind: EventQuit}:
			default:
			}
			return
		}
		select {
		case t.evCh <- eventFromInput(ev):
		case <-t.stopCh:
			return
		}
	}
}
