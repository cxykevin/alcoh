package term

// VTScreen is a small, dependency-free VT100/xterm screen model for terminal previews.
type VTScreen struct {
	Width, Height int
	Cells         [][]rune
	X, Y          int
	state         vtState
	params        []int
	private       bool
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
	s.Y++
	if s.Y >= s.Height {
		copy(s.Cells, s.Cells[1:])
		s.Cells[s.Height-1] = make([]rune, s.Width)
		for x := range s.Cells[s.Height-1] {
			s.Cells[s.Height-1][x] = ' '
		}
		s.Y = s.Height - 1
	}
}
func (s *VTScreen) put(r rune) {
	if r == '\n' {
		s.newline()
		return
	}
	if r == '\r' {
		s.X = 0
		return
	}
	if r == '\b' {
		if s.X > 0 {
			s.X--
		}
		return
	}
	if r == '\t' {
		s.X = (s.X + 8) &^ 7
		if s.X >= s.Width {
			s.newline()
		}
		return
	}
	if s.X >= s.Width {
		s.newline()
	}
	s.Cells[s.Y][s.X] = r
	s.X++
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
}
func (s *VTScreen) Lines() []string {
	out := make([]string, s.Height)
	for y, row := range s.Cells {
		out[y] = string(row)
	}
	return out
}
