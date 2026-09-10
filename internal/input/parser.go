// Package input 定义按键事件类型与转义序列解析器（纯逻辑，平台无关）。
package input

import (
	"bufio"
	"errors"
	"io"
	"time"
	"unicode/utf8"
)

// ErrClosed 表示输入流已关闭。
var ErrClosed = errors.New("input closed")

// escapeWindow 是 ESC 之后等待序列后续字节的最长等待窗口。
// 终端把 Alt+X 等组合键编码为 ESC 前缀，而独立的 Esc 键后面不跟任何字节，
// 两者只能靠时限区分：窗口内没有后续字节即判定为独立 Esc 键
// （与 readline/vim 的 keyseq-timeout 同一思路）。测试可临时放宽该窗口。
var escapeWindow = 50 * time.Millisecond

// deadlineReader 是支持读超时的输入源（终端、管道等 *os.File）。
// 不支持时解析器退回"缓冲区为空即独立 Esc"的即时判定，保持既有语义。
type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// Parser 把字节流解析为 KeyEvent。
type Parser struct {
	rd  *bufio.Reader
	src io.Reader
}

// NewParser 创建从 r 读取的解析器。
func NewParser(r io.Reader) *Parser {
	return &Parser{rd: bufio.NewReaderSize(r, 256), src: r}
}

// Next 阻塞返回下一个输入事件（按键或鼠标）。输入流关闭时返回 ErrClosed。
func (p *Parser) Next() (Event, error) {
	for {
		b, err := p.rd.ReadByte()
		if err != nil {
			if err == io.EOF {
				return Event{}, ErrClosed
			}
			return Event{}, err
		}
		switch {
		case b == 0x1b:
			return p.handleEscape()
		case b == 0x0d || b == 0x0a:
			return KeyEventOf(SimpleKey(KeyEnter)), nil
		case b == 0x09:
			return KeyEventOf(SimpleKey(KeyTab)), nil
		case b == 0x7f || b == 0x08:
			return KeyEventOf(SimpleKey(KeyBackspace)), nil
		case b < 0x20:
			return KeyEventOf(p.ctrlKey(b)), nil
		default:
			r, err := p.readRune(b)
			if err != nil {
				return Event{}, err
			}
			return KeyEventOf(RuneKey(r, ModNone)), nil
		}
	}
}

// handleEscape 处理 ESC 前缀后的字节。
// 独立的 Esc 键与"Esc 前缀的序列/Alt 组合键"共享同一个 0x1b 字节，只能靠
// 后续字节是否在 escapeWindow 内到达来区分：超时则判定为独立 Esc 键。
func (p *Parser) handleEscape() (Event, error) {
	if !p.escapeHasContinuation() {
		return KeyEventOf(SimpleKey(KeyEsc)), nil
	}
	b, err := p.rd.ReadByte()
	if err != nil {
		if err == io.EOF {
			return KeyEventOf(SimpleKey(KeyEsc)), nil
		}
		return Event{}, err
	}
	switch b {
	case '[':
		return p.parseCSI()
	case 'O':
		ke, err := p.parseSS3()
		return KeyEventOf(ke), err
	case 0x1b:
		// 连续的 Esc 字节：终端会把快速连按合并进同一读批次。第一个字节按
		// 独立 Esc 键处理，第二个退回缓冲，由下一次 Next 解析成第二次 Esc，
		// 避免整批被当成一次 Alt+Esc 而丢失（打断快捷键必须逐次生效）。
		if err := p.rd.UnreadByte(); err != nil {
			return Event{}, err
		}
		return KeyEventOf(SimpleKey(KeyEsc)), nil
	default:
		r, err := p.readRune(b)
		if err != nil {
			return Event{}, err
		}
		return KeyEventOf(RuneKey(r, ModAlt)), nil
	}
}

// escapeHasContinuation 报告 ESC 字节之后是否还有后续字节。缓冲区中已有字节
// 时立即返回；否则最多等待 escapeWindow，让被终端拆包送达的序列/组合键字节
// 赶上。输入源不支持读超时（如测试中的 bytes.Reader）时按无后续字节处理。
func (p *Parser) escapeHasContinuation() bool {
	if p.rd.Buffered() > 0 {
		return true
	}
	src, ok := p.src.(deadlineReader)
	if !ok {
		return false
	}
	if err := src.SetReadDeadline(time.Now().Add(escapeWindow)); err != nil {
		return false
	}
	defer func() { _ = src.SetReadDeadline(time.Time{}) }()
	if _, err := p.rd.Peek(1); err != nil {
		// 超时或读错误：没有后续字节，按独立 Esc 键处理。
		return false
	}
	return p.rd.Buffered() > 0
}

// ctrlKey 把控制字节映射为 Ctrl 组合键。
func (p *Parser) ctrlKey(b byte) KeyEvent {
	switch b {
	case 0x00:
		return RuneKey(' ', ModCtrl)
	case 0x1b:
		return SimpleKey(KeyEsc)
	}
	if b >= 0x01 && b <= 0x1a {
		return RuneKey(rune('a'+b-1), ModCtrl)
	}
	chars := []rune{'\\', ']', '^', '_'}
	idx := int(b) - 0x1c
	if idx >= 0 && idx < len(chars) {
		return RuneKey(chars[idx], ModCtrl)
	}
	return RuneKey(rune(b), ModCtrl)
}

// readRune 从首字节 b 开始读取一个完整 UTF-8 rune。
func (p *Parser) readRune(b byte) (rune, error) {
	need := utf8LenByLead(b)
	if need == 0 {
		return utf8.RuneError, nil
	}
	if need == 1 {
		return rune(b), nil
	}
	buf := make([]byte, need)
	buf[0] = b
	for i := 1; i < need; i++ {
		c, err := p.rd.ReadByte()
		if err != nil {
			return 0, err
		}
		buf[i] = c
	}
	r, _ := utf8.DecodeRune(buf)
	return r, nil
}

func utf8LenByLead(b byte) int {
	switch {
	case b < 0x80:
		return 1
	case b >= 0xc0 && b < 0xe0:
		return 2
	case b >= 0xe0 && b < 0xf0:
		return 3
	case b >= 0xf0 && b < 0xf8:
		return 4
	}
	return 0
}

// modifierFromParam 把 CSI modifier 数值映射为 Mod。
func modifierFromParam(n int) Mod {
	if n <= 1 {
		return ModNone
	}
	base := n - 1
	var m Mod
	if base&1 != 0 {
		m |= ModShift
	}
	if base&2 != 0 {
		m |= ModAlt
	}
	if base&4 != 0 {
		m |= ModCtrl
	}
	return m
}

func (p *Parser) parseCSI() (Event, error) {
	var params []int
	num := 0
	hasNum := false
	sgrMouse := false
	for {
		c, err := p.rd.ReadByte()
		if err != nil {
			return Event{}, err
		}
		switch {
		case c >= '0' && c <= '9':
			num = num*10 + int(c-'0')
			hasNum = true
		case c == ';':
			params = append(params, num)
			num = 0
			hasNum = false
		case c == '<':
			sgrMouse = true
		case c == '?' || c == '>':
		default:
			if hasNum {
				params = append(params, num)
			}
			if sgrMouse && (c == 'M' || c == 'm') {
				return MouseEventOf(decodeSGRMouse(params, c == 'M')), nil
			}
			if c == '~' && len(params) > 0 && params[0] == 200 {
				return p.parseBracketedPaste()
			}
			ke, err := p.mapCSI(params, c)
			return KeyEventOf(ke), err
		}
	}
}

// decodeSGRMouse 解析 `ESC[<Cb;Cx;Cy(M|m)` 三段参数。
// Cb 位定义（xterm）：低两位为按键索引（0=Left,1=Middle,2=Right,3=release-legacy）；
// bit 5 (32)=motion；bit 6 (64)=wheel（+按键索引=方向）；
// bit 2 (4)=Shift, bit 3 (8)=Meta/Alt, bit 4 (16)=Ctrl。
func decodeSGRMouse(params []int, press bool) MouseEvent {
	cb, x, y := 0, 0, 0
	if len(params) >= 1 {
		cb = params[0]
	}
	if len(params) >= 2 {
		x = params[1]
	}
	if len(params) >= 3 {
		y = params[2]
	}
	ev := MouseEvent{X: x, Y: y}
	if cb&4 != 0 {
		ev.Mod |= ModShift
	}
	if cb&8 != 0 {
		ev.Mod |= ModAlt
	}
	if cb&16 != 0 {
		ev.Mod |= ModCtrl
	}
	motion := cb&32 != 0
	wheel := cb&64 != 0
	buttonBits := cb & 3
	extraBits := cb & 128
	switch {
	case wheel:
		// 滚轮事件始终视为 press（终端不发 release）。
		switch buttonBits {
		case 0:
			ev.Button = MouseWheelUp
		case 1:
			ev.Button = MouseWheelDown
		case 2:
			ev.Button = MouseWheelLeft
		case 3:
			ev.Button = MouseWheelRight
		}
		ev.Action = MousePress
	default:
		switch buttonBits {
		case 0:
			ev.Button = MouseLeft
		case 1:
			ev.Button = MouseMiddle
		case 2:
			ev.Button = MouseRight
		case 3:
			ev.Button = MouseNone
		}
		if extraBits != 0 {
			// bit 7 (128) 与按键位一起标记扩展按键；这里忽略具体索引，退回 MouseNone。
			ev.Button = MouseNone
		}
		if motion {
			ev.Action = MouseMove
		} else if press {
			ev.Action = MousePress
		} else {
			ev.Action = MouseRelease
		}
	}
	return ev
}

func (p *Parser) parseBracketedPaste() (Event, error) {
	const end = "\x1b[201~"
	var text []byte
	for {
		b, err := p.rd.ReadByte()
		if err != nil {
			return Event{}, err
		}
		text = append(text, b)
		if len(text) >= len(end) && string(text[len(text)-len(end):]) == end {
			text = text[:len(text)-len(end)]
			return KeyEventOf(KeyEvent{Type: KeyPaste, Text: string(text)}), nil
		}
	}
}

func (p *Parser) mapCSI(params []int, final byte) (KeyEvent, error) {
	mod := ModNone
	if len(params) >= 2 {
		mod = modifierFromParam(params[1])
	}
	first := 0
	if len(params) >= 1 {
		first = params[0]
	}
	switch final {
	case 'A':
		return KeyEvent{Type: KeyUp, Mod: mod}, nil
	case 'B':
		return KeyEvent{Type: KeyDown, Mod: mod}, nil
	case 'C':
		return KeyEvent{Type: KeyRight, Mod: mod}, nil
	case 'D':
		return KeyEvent{Type: KeyLeft, Mod: mod}, nil
	case 'H':
		return KeyEvent{Type: KeyHome, Mod: mod}, nil
	case 'F':
		return KeyEvent{Type: KeyEnd, Mod: mod}, nil
	case 'Z':
		return KeyEvent{Type: KeyTab, Mod: mod | ModShift}, nil
	case '~':
		switch first {
		case 1, 7:
			return KeyEvent{Type: KeyHome, Mod: mod}, nil
		case 2:
			return KeyEvent{Type: KeyInsert, Mod: mod}, nil
		case 3:
			return KeyEvent{Type: KeyDelete, Mod: mod}, nil
		case 4, 8:
			return KeyEvent{Type: KeyEnd, Mod: mod}, nil
		case 5:
			return KeyEvent{Type: KeyPageUp, Mod: mod}, nil
		case 6:
			return KeyEvent{Type: KeyPageDown, Mod: mod}, nil
		case 15:
			return KeyEvent{Type: KeyF5, Mod: mod}, nil
		case 17:
			return KeyEvent{Type: KeyF6, Mod: mod}, nil
		case 18:
			return KeyEvent{Type: KeyF7, Mod: mod}, nil
		case 19:
			return KeyEvent{Type: KeyF8, Mod: mod}, nil
		case 20:
			return KeyEvent{Type: KeyF9, Mod: mod}, nil
		case 21:
			return KeyEvent{Type: KeyF10, Mod: mod}, nil
		case 23:
			return KeyEvent{Type: KeyF11, Mod: mod}, nil
		case 24:
			return KeyEvent{Type: KeyF12, Mod: mod}, nil
		}
	}
	return SimpleKey(KeyEsc), nil
}

func (p *Parser) parseSS3() (KeyEvent, error) {
	c, err := p.rd.ReadByte()
	if err != nil {
		return KeyEvent{}, err
	}
	switch c {
	case 'A':
		return SimpleKey(KeyUp), nil
	case 'B':
		return SimpleKey(KeyDown), nil
	case 'C':
		return SimpleKey(KeyRight), nil
	case 'D':
		return SimpleKey(KeyLeft), nil
	case 'H':
		return SimpleKey(KeyHome), nil
	case 'F':
		return SimpleKey(KeyEnd), nil
	case 'P':
		return SimpleKey(KeyF1), nil
	case 'Q':
		return SimpleKey(KeyF2), nil
	case 'R':
		return SimpleKey(KeyF3), nil
	case 'S':
		return SimpleKey(KeyF4), nil
	}
	return SimpleKey(KeyEsc), nil
}
