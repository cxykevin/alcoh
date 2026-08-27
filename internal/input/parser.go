// Package input 定义按键事件类型与转义序列解析器（纯逻辑，平台无关）。
package input

import (
	"bufio"
	"errors"
	"io"
	"unicode/utf8"
)

// ErrClosed 表示输入流已关闭。
var ErrClosed = errors.New("input closed")

// Parser 把字节流解析为 KeyEvent。
type Parser struct {
	rd *bufio.Reader
}

// NewParser 创建从 r 读取的解析器。
func NewParser(r io.Reader) *Parser {
	return &Parser{rd: bufio.NewReaderSize(r, 256)}
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
// 这是原始的无超时行为：缓冲中没有后续字节时，立即视为独立 Esc。
func (p *Parser) handleEscape() (Event, error) {
	if p.rd.Buffered() == 0 {
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
	default:
		r, err := p.readRune(b)
		if err != nil {
			return Event{}, err
		}
		return KeyEventOf(RuneKey(r, ModAlt)), nil
	}
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
