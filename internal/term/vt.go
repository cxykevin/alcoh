package term

import "github.com/cxykevin/alcoh/internal/renderer"

// VTScreen is a small, dependency-free VT100/xterm screen model for terminal previews.
type VTScreen struct {
	Width, Height int
	Cells         [][]rune
	X, Y          int
	// Scrolled 记录自上次 Reset 以来被顶出屏幕上沿的行数：内容总行数即
	// Scrolled+Height（见 UsedRows），预览滚动条据此计算可回看的范围。
	Scrolled int
	// col 是当前行已占用的显示列数（宽字符按 2 列计）：换行按显示宽度而非
	// rune 个数判断，避免 CJK 行末刚好放不下时整字被挤掉。
	col     int
	state   vtState
	params  []int
	private bool
}

// UsedRows 返回内容占用的行数（屏幕尚未滚动时即光标行 + 1）。
func (s *VTScreen) UsedRows() int {
	if s.Scrolled > 0 {
		return s.Scrolled + s.Height
	}
	return s.Y + 1
}

// Line 返回指定行的文本（宽度不足或越界时返回空串）。
func (s *VTScreen) Line(y int) string {
	if y < 0 || y >= len(s.Cells) {
		return ""
	}
	return string(s.Cells[y])
}

type vtState uint8

const (
	vtText vtState = iota
	vtEsc
	vtCSI
)

func NewVTScreen(width, height int) *VTScreen {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	s := &VTScreen{Width: width, Height: height, state: vtText}
	s.reset()
	return s
}

// Reset clears the screen and returns the cursor to the origin.
func (s *VTScreen) Reset() {
	s.reset()
}

func (s *VTScreen) reset() {
	s.Cells = make([][]rune, s.Height)
	for y := range s.Cells {
		s.Cells[y] = make([]rune, s.Width)
		for x := range s.Cells[y] {
			s.Cells[y][x] = ' '
		}
	}
	s.X = 0
	s.Y = 0
	s.Scrolled = 0
	s.col = 0
}

// rowWidth 返回当前行前 n 个 rune 占用的显示列数。
func (s *VTScreen) rowWidth(n int) int {
	if n > len(s.Cells[s.Y]) {
		n = len(s.Cells[s.Y])
	}
	w := 0
	for i := 0; i < n; i++ {
		w += renderer.RuneWidth(s.Cells[s.Y][i])
	}
	return w
}
func (s *VTScreen) clearLine() {
	for x := 0; x < s.Width; x++ {
		s.Cells[s.Y][x] = ' '
	}
}
func (s *VTScreen) clearBelow() {
	for y := s.Y; y < s.Height; y++ {
		start := 0
		if y == s.Y {
			start = s.X
		}
		for x := start; x < s.Width; x++ {
			s.Cells[y][x] = ' '
		}
	}
}
func (s *VTScreen) newline() {
	s.X = 0
	s.col = 0
	s.Y++
	if s.Y >= s.Height {
		copy(s.Cells, s.Cells[1:])
		s.Cells[s.Height-1] = make([]rune, s.Width)
		for x := range s.Cells[s.Height-1] {
			s.Cells[s.Height-1][x] = ' '
		}
		s.Y = s.Height - 1
		s.Scrolled++
	}
}
func (s *VTScreen) put(r rune) {
	if r == '\n' {
		s.newline()
		return
	}
	if r == '\r' {
		s.X = 0
		s.col = 0
		return
	}
	if r == '\b' {
		if s.X > 0 {
			s.X--
			s.col = s.rowWidth(s.X)
		}
		return
	}
	if r == '\t' {
		// 走到下一个 8 列制表位：按显示列推进（宽字符同样算 2 列）。
		for {
			s.put(' ')
			if s.col%8 == 0 {
				break
			}
		}
		return
	}
	// 按显示宽度换行：宽度不足时先换行再写，CJK 不会被当成 1 列而挤出整字。
	rw := renderer.RuneWidth(r)
	if s.col+rw > s.Width {
		s.newline()
	}
	s.Cells[s.Y][s.X] = r
	s.X++
	s.col += rw
}
func (s *VTScreen) Feed(input string) {
	for _, r := range input {
		switch s.state {
		case vtText:
			if r == 27 {
				s.state = vtEsc
			} else if r < 32 {
				s.put(r)
			} else {
				s.put(r)
			}
		case vtEsc:
			if r == '[' {
				s.state = vtCSI
				s.params = nil
				s.private = false
			} else if r == 'c' {
				s.reset()
				s.state = vtText
			} else {
				s.state = vtText
			}
		case vtCSI:
			if r == '?' && len(s.params) == 0 {
				s.private = true
				continue
			}
			if r >= '0' && r <= '9' {
				if len(s.params) == 0 {
					s.params = []int{0}
				}
				s.params[len(s.params)-1] = s.params[len(s.params)-1]*10 + int(r-'0')
				continue
			}
			if r == ';' {
				s.params = append(s.params, 0)
				continue
			}
			s.csi(r)
			s.state = vtText
		}
	}
}
func (s *VTScreen) param(i, def int) int {
	if i < len(s.params) && s.params[i] > 0 {
		return s.params[i]
	}
	return def
}
func (s *VTScreen) csi(final rune) {
	switch final {
	case 'A':
		s.Y -= s.param(0, 1)
	case 'B':
		s.Y += s.param(0, 1)
	case 'C':
		s.X += s.param(0, 1)
	case 'D':
		s.X -= s.param(0, 1)
	case 'G':
		s.X = s.param(0, 1) - 1
	case 'd':
		s.Y = s.param(0, 1) - 1
	case 'H', 'f':
		y, x := s.param(0, 1)-1, s.param(1, 1)-1
		s.X = x
		s.Y = y
	case 'J':
		if s.param(0, 0) == 2 {
			s.reset()
		} else if s.param(0, 0) == 0 {
			s.clearBelow()
		}
	case 'K':
		s.clearLine()
	case 'm':
	default:
	}
	if s.X < 0 {
		s.X = 0
	}
	if s.X >= s.Width {
		s.X = s.Width - 1
	}
	if s.Y < 0 {
		s.Y = 0
	}
	if s.Y >= s.Height {
		s.Y = s.Height - 1
	}
	s.col = s.rowWidth(s.X)
}
func (s *VTScreen) Lines() []string {
	out := make([]string, s.Height)
	for y, row := range s.Cells {
		out[y] = string(row)
	}
	return out
}
