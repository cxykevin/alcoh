package view

import (
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"strings"
)

// ShellPanel renders the live shell list and selected VT preview.
type ShellPanel struct{ Theme renderer.Theme }

func (p *ShellPanel) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	xs := m.Shells()
	if len(xs) == 0 {
		return
	}
	if m.ShellSelected < 0 {
		m.ShellSelected = 0
	}
	if m.ShellSelected >= len(xs) {
		m.ShellSelected = len(xs) - 1
	}
	if m.ShellFullscreen {
		p.preview(c, r, xs[m.ShellSelected])
		p.footer(c, r)
		return
	}
	// A narrow terminal cannot present a useful 40% preview; give the
	// command list the full width instead.
	showPreview := r.W >= 60
	left := r.W * 3 / 5
	if !showPreview {
		left = r.W
	}
	if left < 1 {
		left = 1
	}
	if left >= r.W && r.W > 1 {
		left = r.W - 1
	}
	right := r.W - left
	c.PutText(r.X+1, r.Y, "shells", p.Theme.Style(p.Theme.Text).WithBold(true))
	c.PutText(r.X+8, r.Y, "n "+shellCount(len(xs)), p.Theme.Style(p.Theme.Accent).WithBold(true))
	for i, s := range xs {
		y := r.Y + 2 + i
		if y >= r.Y+r.H-2 {
			break
		}
		st := p.Theme.Style(p.Theme.TextMuted)
		prefix := "  "
		if i == m.ShellSelected {
			prefix = "> "
			st = p.Theme.Style(p.Theme.Primary).WithBold(true)
		}
		title := s.Title
		if title == "" {
			title = s.ID
		}
		if s.Command != "" {
			title = s.Command
		}
		maxTitle := left - 3
		if maxTitle < 1 {
			maxTitle = 1
		}
		c.PutText(r.X+1, y, prefix+renderer.Truncate(title, maxTitle), st)
	}
	if showPreview && right > 1 {
		for y := r.Y; y < r.Y+r.H; y++ {
			c.Put(r.X+left, y, renderer.CellRune('│', p.Theme.Style(p.Theme.BorderSubtle)))
		}
		p.preview(c, renderer.NewRect(r.X+left+1, r.Y, right-1, r.H), xs[m.ShellSelected])
	}
	p.footer(c, r)
}
func (p *ShellPanel) preview(c *renderer.Canvas, r renderer.Rect, s *model.TerminalState) {
	if s == nil {
		return
	}
	title := s.Title
	if title == "" {
		title = s.ID
	}
	if s.Command != "" {
		title = s.Command
	}
	c.PutText(r.X+1, r.Y, "terminal: "+renderer.Truncate(title, r.W-2), p.Theme.Style(p.Theme.Info).WithBold(true))
	lines := []string{s.Transcript}
	if s.Screen != nil {
		lines = s.Screen.Lines()
	}
	for i, line := range lines {
		if i+2 >= r.H-1 {
			break
		}
		for _, part := range strings.Split(line, "\n") {
			c.PutText(r.X+1, r.Y+i+1, renderer.Truncate(part, r.W-2), p.Theme.Style(p.Theme.MDCode))
			i++
		}
	}
}
func (p *ShellPanel) footer(c *renderer.Canvas, r renderer.Rect) {
	if r.H > 0 {
		c.PutText(r.X+1, r.Y+r.H-1, "x kill  esc return", p.Theme.Style(p.Theme.TextMuted).WithDim(true))
	}
}
func shellCount(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
