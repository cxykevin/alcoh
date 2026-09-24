//go:build windows

package term

import (
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/cxykevin/alcoh/internal/input"
)

// spuriousReadBackoff 是控制台"伪 EOF"后重新读取前的退避，避免极端情况下空转。
const spuriousReadBackoff = 10 * time.Millisecond

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

	// lastW/lastH 由 readLoop 与 pollLoop 两个 goroutine 经 checkResize 读写，
	// 用 mutex 串行化（否则既是数据竞争，也可能丢掉尺寸事件）。
	mu           sync.Mutex
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
	t.mu.Lock()
	t.lastW, t.lastH = t.Size()
	t.mu.Unlock()

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

// maxSpuriousReadRetries 是连续"伪 EOF"重试上限：超过则认为输入流真的坏了，
// 交给主循环正常退出（而不是留在没有输入的界面里，让用户只能关窗口）。
const maxSpuriousReadRetries = 100

func (t *winTerm) readLoop() {
	spurious := 0
	for {
		ev, err := t.parser.Next()
		if err != nil {
			if spurious < maxSpuriousReadRetries && t.consoleAlive() {
				// 控制台仍然可用：这是一次"伪 EOF"，不是输入源关闭。
				//
				// Windows 控制台上 Go 的 ReadConsole 会在若干情况下返回 0 字节，
				// 而 os.Stdin 的 ZeroReadIsEOF 会把它转换成 io.EOF：
				//   - 按下 Ctrl+Z：Go 的 readConsole 把 0x1A 当作控制台 EOF
				//     标记直接吃掉（internal/poll/fd_windows.go）；
				//   - ReadConsole 返回 0 字符，或输入缓冲被 flush（输入积压时
				//     控制台会丢弃/清空缓冲）。
				// 这些都不代表输入源关闭，句柄仍可读：退避后继续读即可恢复。
				spurious++
				select {
				case <-time.After(spuriousReadBackoff):
				case <-t.stopCh:
					return
				}
				continue
			}
			// 输入确实没了（控制台关闭/句柄失效，或持续伪 EOF）：可靠地投递
			// 退出事件。绝不能用非阻塞 default 分支丢弃——一旦丢弃而 readLoop
			// 已经退出，主循环会带着永不产生事件的终端继续运行：按键、鼠标、
			// Ctrl+C 全部失效，只能关窗口，即用户报告的"卡死"。
			select {
			case t.evCh <- Event{Kind: EventQuit}:
			case <-t.stopCh:
			}
			return
		}
		spurious = 0
		// 每次事件顺带检查一次尺寸（无 SIGWINCH，作为低成本检测）
		t.checkResize()
		select {
		case t.evCh <- eventFromInput(ev):
		case <-t.stopCh:
			return
		}
	}
}

// consoleAlive 报告输入句柄是否还是可用控制台。句柄失效即输入源真正关闭。
func (t *winTerm) consoleAlive() bool {
	var mode uint32
	return windows.GetConsoleMode(t.inHandle, &mode) == nil
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
	t.mu.Lock()
	w, h := t.Size()
	if w == t.lastW && h == t.lastH {
		t.mu.Unlock()
		return
	}
	t.lastW, t.lastH = w, h
	t.mu.Unlock()
	// 尺寸事件必须送到：一旦丢弃，缓冲区尺寸就永远停留在旧值（终端与帧缓冲
	// 不一致，画面错位/残留，看起来像卡死）。这里阻塞等待事件循环消费，
	// 退出时由 stopCh 解除。
	select {
	case t.evCh <- Event{Kind: EventResize, W: w, H: h}:
	case <-t.stopCh:
	}
}
