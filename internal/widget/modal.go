package widget

import (
	"github.com/cxykevin/alcoh/internal/renderer"
)

// Modal 是全屏模态覆盖层：在整屏上居中绘制一个带边框的框。
// 内容 widget 绘制在框内。
type Modal struct {
	Width   int
	Height  int
	Title   string
	Style   renderer.Style
	Content Widget
}

// Draw 在整屏 r 上居中绘制模态框。
func (m *Modal) Draw(c *renderer.Canvas, r renderer.Rect) {
	if r.W < m.Width || r.H < m.Height {
		// 屏幕太小：退化为直接绘制内容
		if m.Content != nil {
			m.Content.Draw(c, r)
		}
		return
	}
	x := r.X + (r.W-m.Width)/2
	y := r.Y + (r.H-m.Height)/2
	m.drawAt(c, renderer.NewRect(x, y, m.Width, m.Height))
}

// DrawBottom 在输入区所在的屏幕底部绘制模态框，覆盖输入框而不居中到消息区。
// bottomMargin 表示底部保留的行数；通常为 2，使状态栏和最底部空行仍可见。
func (m *Modal) DrawBottom(c *renderer.Canvas, r renderer.Rect, bottomMargin int) {
	if bottomMargin < 0 {
		bottomMargin = 0
	}
	w := m.Width
	if w < 1 {
		w = r.W
	}
	if w > r.W {
		w = r.W
	}
	bottom := r.Y + r.H - bottomMargin
	if bottom > r.Y+r.H {
		bottom = r.Y + r.H
	}
	if bottom <= r.Y {
		return
	}
	h := m.Height
	if h < 1 {
		h = 1
	}
	if h > bottom-r.Y {
		h = bottom - r.Y
	}
	if h < 1 {
		return
	}
	x := r.X + (r.W-w)/2
	y := bottom - h
	m.drawAt(c, renderer.NewRect(x, y, w, h))
}

// DrawSheet 在底部绘制无侧边框的全宽弹窗面板。
// 面板只保留顶部横线，底部区域由弹窗完全接管。
func (m *Modal) DrawSheet(c *renderer.Canvas, r renderer.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	h := m.Height
	if h < 1 {
		h = 1
	}
	if h > r.H {
		h = r.H
	}
	box := renderer.NewRect(r.X, r.Y+r.H-h, r.W, h)
	c.Fill(box, renderer.CellRune(' ', renderer.DefaultStyle()))

	st := m.Style
	for x := box.X; x < box.X+box.W; x++ {
		c.Put(x, box.Y, renderer.CellRune('─', st))
	}
	if m.Title != "" && box.W > 2 {
		title := " " + renderer.Truncate(m.Title, box.W-2) + " "
		c.PutText(box.X+1, box.Y, title, st.WithBold(true))
	}
	content := box.Inset(0, 1)
	if m.Content != nil && content.H > 0 {
		m.Content.Draw(c, content)
	}
}

func (m *Modal) drawAt(c *renderer.Canvas, box renderer.Rect) {
	if box.W < 2 || box.H < 2 {
		if m.Content != nil {
			m.Content.Draw(c, box)
		}
		return
	}
	x, y := box.X, box.Y
	st := m.Style
	for bx := x; bx < x+box.W; bx++ {
		c.Put(bx, y, renderer.CellRune('─', st))
		c.Put(bx, y+box.H-1, renderer.CellRune('─', st))
	}
	for by := y + 1; by < y+box.H-1; by++ {
		c.Put(x, by, renderer.CellRune('│', st))
		c.Put(x+box.W-1, by, renderer.CellRune('│', st))
	}
	c.Put(x, y, renderer.CellRune('┌', st))
	c.Put(x+box.W-1, y, renderer.CellRune('┐', st))
	c.Put(x, y+box.H-1, renderer.CellRune('└', st))
	c.Put(x+box.W-1, y+box.H-1, renderer.CellRune('┘', st))
	if m.Title != "" {
		title := " " + renderer.Truncate(m.Title, box.W-2) + " "
		c.PutText(x+1, y, title, st.WithBold(true))
	}
	if m.Content != nil {
		m.Content.Draw(c, box.Inset(1, 1))
	}
}

type DimOverlay struct {
	Style renderer.Style
}

func (d *DimOverlay) Draw(c *renderer.Canvas, r renderer.Rect) {
	cell := renderer.CellRune(' ', d.Style)
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c.Put(x, y, cell)
		}
	}
}
