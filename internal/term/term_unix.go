//go:build unix

package term

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/cxykevin/alcoh/internal/input"
)

// unixTerm 是 unix 平台的 Terminal 实现。
type unixTerm struct {
	stdin  *os.File
	stdout *os.File

	parser  *input.Parser
	evCh    chan Event
	sigCh   chan os.Signal
	stopCh  chan struct{}
	raw     *unix.Termios
	rawMode bool
}

// Open 返回当前终端的 Terminal 实现。
func Open() (Terminal, error) {
	return &unixTerm{
		stdin:  os.Stdin,
		stdout: os.Stdout,
	}, nil
}

func (t *unixTerm) Size() (int, int) {
	fd := int(t.stdout.Fd())
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 80, 24
	}
	w, h := int(ws.Col), int(ws.Row)
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

func (t *unixTerm) EnterRaw() error {
	if t.rawMode {
		return nil
	}
	fd := int(t.stdin.Fd())
	if !isTTY(fd) {
		return errors.New("stdin is not a terminal")
	}
	ts, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return err
	}
	saved := *ts
	raw := *ts
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return err
	}
	t.raw = &saved
	t.rawMode = true

	// 进入 alternate screen、隐藏光标、启用鼠标按键与滚轮上报
	t.Write([]byte("\x1b[?1049h\x1b[2J\x1b[H" + hideCursorSeq + mouseEnableSeq + bracketedPasteEnableSeq))

	// 启动事件采集
	t.evCh = make(chan Event, 64)
	t.stopCh = make(chan struct{})
	t.sigCh = make(chan os.Signal, 4)
	signal.Notify(t.sigCh, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM)

	t.parser = input.NewParser(t.stdin)
	go t.readLoop()
	go t.signalLoop()
	return nil
}

func (t *unixTerm) ExitRaw() error {
	if !t.rawMode {
		return nil
	}
	close(t.stopCh)
	signal.Stop(t.sigCh)
	if t.raw != nil {
		unix.IoctlSetTermios(int(t.stdin.Fd()), ioctlWriteTermios, t.raw)
		t.raw = nil
	}
	t.rawMode = false
	// 关闭鼠标上报、退出 alternate screen、复位样式、显示光标
	t.Write([]byte(bracketedPasteDisableSeq + mouseDisableSeq + "\x1b[0m\x1b[?25h\x1b[?1049l"))
	return nil
}

func (t *unixTerm) Events() <-chan Event { return t.evCh }

func (t *unixTerm) Write(p []byte) error {
	_, err := t.stdout.Write(p)
	return err
}

func (t *unixTerm) CopyToClipboard(text string) error {
	_, err := t.stdout.Write(osc52Clipboard(text))
	return err
}

func (t *unixTerm) readLoop() {
	for {
		ev, err := t.parser.Next()
		if err != nil {
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

func (t *unixTerm) signalLoop() {
	for {
		select {
		case sig := <-t.sigCh:
			switch sig {
			case syscall.SIGWINCH:
				w, h := t.Size()
				select {
				case t.evCh <- Event{Kind: EventResize, W: w, H: h}:
				default:
				}
			case syscall.SIGINT, syscall.SIGTERM:
				select {
				case t.evCh <- Event{Kind: EventQuit}:
				default:
				}
			}
		case <-t.stopCh:
			return
		}
	}
}

func isTTY(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	return err == nil
}

// PromptSecret 在终端上读取一行敏感输入（关闭回显，退格仍由行规程处理）。
// 提示语写到 stderr，避免污染 stdout（dump / one-shot 场景）。
// stdin 不是终端（管道/脚本）时按普通行读取，不做回显控制。
func PromptSecret(prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(os.Stderr, prompt)
	}
	fd := int(os.Stdin.Fd())
	restore := func() {}
	if isTTY(fd) {
		ts, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
		if err != nil {
			return "", err
		}
		saved := *ts
		noEcho := *ts
		noEcho.Lflag &^= unix.ECHO
		if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &noEcho); err != nil {
			return "", err
		}
		restore = func() { _ = unix.IoctlSetTermios(fd, ioctlWriteTermios, &saved) }
	}
	line, err := readSecretLine(os.Stdin)
	restore()
	if prompt != "" {
		// 不回显时用户看不到自己按下的回车，补一个换行。
		fmt.Fprintln(os.Stderr)
	}
	return line, err
}
