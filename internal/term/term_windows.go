//go:build windows

package term

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"

	"github.com/cxykevin/alcoh/internal/input"
)

// winTerm 是 Windows 平台的 Terminal 实现。
// 目标终端：Windows Terminal（WT）。WT 支持完整 VT（1049 alternate screen、
// 真 24-bit SGR、CSI 输入序列、UTF-8 强制），仅需正确设置 Console Mode。
type winTerm struct {
	stdin, stdout *os.File
	inHandle      windows.Handle
	outHandle     windows.Handle
	origInMode    uint32
	origOutMode   uint32
	rawMode       bool

	evCh   chan Event
	stopCh chan struct{}
	parser *input.Parser

	lastW, lastH int
}

// Open 返回 Windows 终端的 Terminal 实现。
func Open() (Terminal, error) {
	in, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return nil, err
	}
	out, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return nil, err
	}
	return &winTerm{
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		inHandle:  in,
		outHandle: out,
	}, nil
}

func (t *winTerm) Size() (int, int) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(t.outHandle, &info); err != nil {
		return 80, 24
	}
	// 可见窗口（srWindow）即单元格尺寸；不用 Size（含滚动缓冲）或 MaximumWindowSize。
	w := int(info.Window.Right - info.Window.Left + 1)
	h := int(info.Window.Bottom - info.Window.Top + 1)
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

func (t *winTerm) EnterRaw() error {
	if t.rawMode {
		return nil
	}
	// ---- 输入模式 ----
	var inMode uint32
	if err := windows.GetConsoleMode(t.inHandle, &inMode); err != nil {
		return errors.New("stdin is not a console: " + err.Error())
	}
	t.origInMode = inMode
	// 关闭回显/行缓冲/系统处理（Ctrl+C 变字节 0x03 走解析器）
	inMode &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT
	// 打开 VT 输入（WT 把按键转为 CSI 序列，Unix 风格解析器直接复用）
	inMode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	// 关闭 quick-edit（否则鼠标点击被用于文本选择）；启用鼠标输入以便 VT 上报滚轮
	inMode = (inMode &^ windows.ENABLE_QUICK_EDIT_MODE) | windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_MOUSE_INPUT
	if err := windows.SetConsoleMode(t.inHandle, inMode); err != nil {
		return err
	}

	// ---- 输出模式 ----
	var outMode uint32
	if err := windows.GetConsoleMode(t.outHandle, &outMode); err != nil {
		return err
	}
	t.origOutMode = outMode
	// 打开 VT 处理（1049/SGR/光标移动）
	outMode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.ENABLE_PROCESSED_OUTPUT
	if err := windows.SetConsoleMode(t.outHandle, outMode); err != nil {
		return err
	}

	t.rawMode = true
	t.Write([]byte("\x1b[?1049h\x1b[2J\x1b[H" + hideCursorSeq + mouseEnableSeq + bracketedPasteEnableSeq))

	t.evCh = make(chan Event, 64)
	t.stopCh = make(chan struct{})
	t.parser = input.NewParser(t.stdin)
	t.lastW, t.lastH = t.Size()

	go t.readLoop()
	go t.pollLoop()
	return nil
}

func (t *winTerm) ExitRaw() error {
	if !t.rawMode {
		return nil
	}
	close(t.stopCh)
	windows.SetConsoleMode(t.inHandle, t.origInMode)
	windows.SetConsoleMode(t.outHandle, t.origOutMode)
	t.rawMode = false
	t.Write([]byte(bracketedPasteDisableSeq + mouseDisableSeq + "\x1b[0m\x1b[?25h\x1b[?1049l"))
	return nil
}

func (t *winTerm) Events() <-chan Event { return t.evCh }

func (t *winTerm) Write(p []byte) error {
	_, err := t.stdout.Write(p)
	return err
}

func (t *winTerm) CopyToClipboard(text string) error {
	_, err := t.stdout.Write(osc52Clipboard(text))
	return err
}

func (t *winTerm) readLoop() {
	for {
		ev, err := t.parser.Next()
		if err != nil {
			select {
			case t.evCh <- Event{Kind: EventQuit}:
			default:
			}
			return
		}
		// 每次事件顺带检查一次尺寸（无 SIGWINCH，作为低成本检测）
		t.checkResize()
		select {
		case t.evCh <- eventFromInput(ev):
		case <-t.stopCh:
			return
		}
	}
}

// pollLoop 每 250ms 轮询尺寸变化（Windows 无 SIGWINCH）。
func (t *winTerm) pollLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.checkResize()
		case <-t.stopCh:
			return
		}
	}
}

func (t *winTerm) checkResize() {
	w, h := t.Size()
	if w != t.lastW || h != t.lastH {
		t.lastW, t.lastH = w, h
		select {
		case t.evCh <- Event{Kind: EventResize, W: w, H: h}:
		default:
		}
	}
}
