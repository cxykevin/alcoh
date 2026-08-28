package renderer

import (
	"strconv"
	"unicode/utf8"
)

// Screen is a dependency-free VT screen model for terminal previews.
type Screen struct {
	buf            *Buffer
	x, y           int
	savedX, savedY int
	style          Style
	state          byte
	seq            []byte
	pending        []byte
	title          string
	cursorVisible  bool
	main, alt      *Buffer
}

const (
	vtGround byte = iota
	vtEsc
	vtCSI
	vtOSC
)

func NewScreen(w, h int) *Screen {
	b := NewBuffer(w, h)
	return &Screen{buf: b, main: b, style: DefaultStyle(), cursorVisible: true}
}
func (s *Screen) Buffer() *Buffer { return s.buf }
func (s *Screen) Snapshot() Buffer {
	b := NewBuffer(s.buf.W, s.buf.H)
	copy(b.Cells, s.buf.Cells)
	return *b
}
func (s *Screen) Cursor() (int, int)  { return s.x, s.y }
func (s *Screen) CursorVisible() bool { return s.cursorVisible }
func (s *Screen) Title() string       { return s.title }
func (s *Screen) Reset() {
	s.buf = NewBuffer(s.buf.W, s.buf.H)
	s.main = s.buf
	s.alt = nil
	s.x, s.y, s.savedX, s.savedY = 0, 0, 0, 0
	s.style = DefaultStyle()
	s.state = vtGround
	s.seq = nil
	s.pending = nil
}

// Feed consumes output incrementally, including chunks split inside escapes.
func (s *Screen) Feed(in []byte) int {
	p := append(s.pending, in...)
	s.pending = nil
	i := 0
	for i < len(p) {
		b := p[i]
		if s.state == vtOSC {
			if b == 7 {
				s.finishOSC()
				s.state = vtGround
			} else if b == 27 && i+1 < len(p) && p[i+1] == 92 {
				s.finishOSC()
				s.state = vtGround
				i++
			} else {
				s.appendSeq(b)
			}
			i++
			continue
		}
		if s.state == vtCSI {
			if b >= 0x40 && b <= 0x7e {
				s.handleCSI(b)
				s.seq = nil
				s.state = vtGround
			} else {
				s.appendSeq(b)
			}
			i++
			continue
		}
		if s.state == vtEsc {
			s.state = vtGround
			switch b {
			case 91:
				s.state = vtCSI
			case 93:
				s.state = vtOSC
			case 55:
				s.savedX, s.savedY = s.x, s.y
			case 56:
				s.x, s.y = s.savedX, s.savedY
			case 99:
				s.Reset()
			case 68:
				s.lineFeed()
			case 77:
				s.reverseIndex()
			case 69:
				s.lineFeed()
				s.x = 0
			}
			i++
			continue
		}
		if b == 27 {
			s.state = vtEsc
			i++
			continue
		}
		if b < 32 {
			s.control(b)
			i++
			continue
		}
		r, n := utf8.DecodeRune(p[i:])
		if r == utf8.RuneError && n == 1 && p[i] >= 128 {
			if len(p)-i < 4 {
				s.pending = append(s.pending, p[i:]...)
				break
			}
		}
		s.put(r)
		i += n
	}
	return len(p) - len(s.pending)
}
func (s *Screen) appendSeq(b byte) {
	if len(s.seq) < 4096 {
		s.seq = append(s.seq, b)
	}
}
func (s *Screen) control(b byte) {
	switch b {
	case 8:
		if s.x > 0 {
			s.x--
		}
	case 9:
		s.x = (s.x/4 + 1) * 4
	case 10, 11, 12:
		s.lineFeed()
	case 13:
		s.x = 0
	}
}
func (s *Screen) put(r rune) {
	if s.buf.W == 0 || s.buf.H == 0 {
		return
	}
	w := RuneWidth(r)
	if w == 0 {
		return
	}
	if s.x >= s.buf.W {
		s.x = 0
		s.lineFeed()
	}
	if w == 2 && s.x == s.buf.W-1 {
		s.x = 0
		s.lineFeed()
	}
	s.buf.Set(s.x, s.y, Cell{R: r, Style: s.style, Width: w})
	s.x += w
}
func (s *Screen) lineFeed() {
	if s.buf.H == 0 {
		s.x, s.y = 0, 0
		return
	}
	if s.y >= s.buf.H-1 {
		s.y = s.buf.H - 1
		s.scroll(1)
	} else if s.y < 0 {
		s.y = 0
	} else {
		s.y++
	}
}
func (s *Screen) reverseIndex() {
	if s.buf.H == 0 {
		s.x, s.y = 0, 0
		return
	}
	if s.y <= 0 {
		s.y = 0
		s.scroll(-1)
	} else {
		s.y--
	}
}
func (s *Screen) scroll(n int) {
	if s.buf.W == 0 || s.buf.H == 0 {
		return
	}
	if n < 0 {
		n = -n
		for y := s.buf.H - 1; y >= n; y-- {
			copy(s.buf.Cells[y*s.buf.W:], s.buf.Cells[(y-n)*s.buf.W:(y-n+1)*s.buf.W])
		}
		for y := 0; y < n && y < s.buf.H; y++ {
			s.clearRow(y)
		}
	} else {
		if n > s.buf.H {
			n = s.buf.H
		}
		for y := 0; y < s.buf.H-n; y++ {
			copy(s.buf.Cells[y*s.buf.W:], s.buf.Cells[(y+n)*s.buf.W:(y+n+1)*s.buf.W])
		}
		for y := s.buf.H - n; y < s.buf.H; y++ {
			s.clearRow(y)
		}
	}
}
func (s *Screen) clearRow(y int) {
	for x := 0; x < s.buf.W; x++ {
		s.buf.Set(x, y, CellSpace(DefaultStyle()))
	}
}
func (s *Screen) parseParams() (bool, []int) {
	v := string(s.seq)
	q := len(v) > 0 && v[0] == 63
	if q {
		v = v[1:]
	}
	parts := []string{""}
	for _, r := range v {
		if r == 59 {
			parts = append(parts, "")
		} else {
			parts[len(parts)-1] += string(r)
		}
	}
	p := make([]int, len(parts))
	for i, z := range parts {
		p[i], _ = strconv.Atoi(z)
	}
	return q, p
}
func (s *Screen) handleCSI(f byte) {
	q, p := s.parseParams()
	d := func(i, v int) int {
		if i >= len(p) || p[i] == 0 {
			return v
		}
		return p[i]
	}
	switch f {
	case 65:
		s.y = max(0, s.y-d(0, 1))
	case 66:
		s.y = min(s.buf.H-1, s.y+d(0, 1))
	case 67:
		s.x = min(s.buf.W-1, s.x+d(0, 1))
	case 68:
		s.x = max(0, s.x-d(0, 1))
	case 71:
		s.x = min(s.buf.W-1, max(0, d(0, 1)-1))
	case 72, 102:
		s.y = min(s.buf.H-1, max(0, d(0, 1)-1))
		s.x = min(s.buf.W-1, max(0, d(1, 1)-1))
	case 74:
		s.eraseDisplay(d(0, 0))
	case 75:
		s.eraseLine(d(0, 0))
	case 109:
		s.sgr(p)
	case 104, 108:
		if q && len(p) > 0 && p[0] == 25 {
			s.cursorVisible = f == 104
		} else if q && len(p) > 0 && p[0] == 1049 {
			s.altScreen(f == 104)
		}
	case 115:
		s.savedX, s.savedY = s.x, s.y
	case 117:
		s.x, s.y = s.savedX, s.savedY
	case 80:
		s.deleteChars(d(0, 1))
	case 64:
		s.insertChars(d(0, 1))
	case 76:
		s.insertLines(d(0, 1))
	case 77:
		s.deleteLines(d(0, 1))
	case 83:
		s.scroll(d(0, 1))
	case 84:
		s.scroll(-d(0, 1))
	}
}
func (s *Screen) eraseDisplay(m int) {
	if m == 2 || m == 3 {
		s.buf.Clear()
		return
	}
	for y := 0; y < s.buf.H; y++ {
		if m == 0 && y < s.y || m == 1 && y > s.y {
			continue
		}
		a, b := 0, s.buf.W
		if m == 0 {
			a = s.x
		}
		if m == 1 {
			b = s.x + 1
		}
		for x := a; x < b; x++ {
			s.buf.Set(x, y, CellSpace(s.style))
		}
	}
}
func (s *Screen) eraseLine(m int) {
	a, b := 0, s.buf.W
	if m == 0 {
		a = s.x
	}
	if m == 1 {
		b = s.x + 1
	}
	for x := a; x < b; x++ {
		s.buf.Set(x, s.y, CellSpace(s.style))
	}
}
func (s *Screen) sgr(p []int) {
	if len(p) == 0 {
		p = []int{0}
	}
	for _, v := range p {
		switch {
		case v == 0:
			s.style = DefaultStyle()
		case v == 1:
			s.style.Bold = true
		case v == 2:
			s.style.Dim = true
		case v == 3:
			s.style.Italic = true
		case v == 4:
			s.style.Underline = true
		case v == 7:
			s.style.Reverse = true
		case v == 9:
			s.style.Strikethrough = true
		case v == 22:
			s.style.Bold = false
			s.style.Dim = false
		case v == 23:
			s.style.Italic = false
		case v == 24:
			s.style.Underline = false
		case v == 27:
			s.style.Reverse = false
		case v == 29:
			s.style.Strikethrough = false
		case v == 39:
			s.style.Fg = ColorDefault
		case v == 49:
			s.style.Bg = ColorDefault
		case v >= 30 && v <= 37:
			s.style.Fg = ansiColor(v - 30)
		case v >= 40 && v <= 47:
			s.style.Bg = ansiColor(v - 40)
		case v >= 90 && v <= 97:
			s.style.Fg = ansiColor(v - 90 + 8)
		case v >= 100 && v <= 107:
			s.style.Bg = ansiColor(v - 100 + 8)
		}
	}
}
func (s *Screen) deleteChars(n int) {
	if n <= 0 {
		return
	}
	if n > s.buf.W-s.x {
		n = s.buf.W - s.x
	}
	for x := s.x; x < s.buf.W-n; x++ {
		s.buf.Set(x, s.y, s.buf.Get(x+n, s.y))
	}
	for x := s.buf.W - n; x < s.buf.W; x++ {
		s.buf.Set(x, s.y, CellSpace(s.style))
	}
}
func (s *Screen) insertChars(n int) {
	if n <= 0 {
		return
	}
	if n > s.buf.W-s.x {
		n = s.buf.W - s.x
	}
	for x := s.buf.W - 1; x >= s.x+n; x-- {
		s.buf.Set(x, s.y, s.buf.Get(x-n, s.y))
	}
	for x := s.x; x < s.x+n; x++ {
		s.buf.Set(x, s.y, CellSpace(s.style))
	}
}
func (s *Screen) insertLines(n int) {
	if n > s.buf.H-s.y {
		n = s.buf.H - s.y
	}
	for y := s.buf.H - 1; y >= s.y+n; y-- {
		copy(s.buf.Cells[y*s.buf.W:(y+1)*s.buf.W], s.buf.Cells[(y-n)*s.buf.W:(y-n+1)*s.buf.W])
	}
	for y := s.y; y < s.y+n; y++ {
		s.clearRow(y)
	}
}
func (s *Screen) deleteLines(n int) {
	if n > s.buf.H-s.y {
		n = s.buf.H - s.y
	}
	for y := s.y; y < s.buf.H-n; y++ {
		copy(s.buf.Cells[y*s.buf.W:(y+1)*s.buf.W], s.buf.Cells[(y+n)*s.buf.W:(y+n+1)*s.buf.W])
	}
	for y := s.buf.H - n; y < s.buf.H; y++ {
		s.clearRow(y)
	}
}

func ansiColor(i int) Color {
	a := []Color{RGB(0, 0, 0), RGB(128, 0, 0), RGB(0, 128, 0), RGB(128, 128, 0), RGB(0, 0, 128), RGB(128, 0, 128), RGB(0, 128, 128), RGB(192, 192, 192), RGB(128, 128, 128), RGB(255, 0, 0), RGB(0, 255, 0), RGB(255, 255, 0), RGB(0, 0, 255), RGB(255, 0, 255), RGB(0, 255, 255), RGB(255, 255, 255)}
	return a[i&15]
}
func (s *Screen) finishOSC() {
	v := string(s.seq)
	for i := range v {
		if v[i] == 59 && len(v) > 0 && (v[0] == 48 || v[0] == 50) {
			s.title = v[i+1:]
			break
		}
	}
	s.seq = nil
}
func (s *Screen) altScreen(on bool) {
	if on && s.alt == nil {
		s.main = s.buf
		s.alt = NewBuffer(s.buf.W, s.buf.H)
		s.buf = s.alt
		s.x, s.y = 0, 0
	} else if !on && s.alt != nil {
		s.buf = s.main
		s.alt = nil
		s.x, s.y = 0, 0
	}
}
