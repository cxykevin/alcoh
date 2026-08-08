package widget

import (
	"github.com/cxykevin/alcoh/internal/renderer"
)

// VBox 垂直布局子组件。Split[i] 指定第 i 个子组件的高度；-1 表示占据剩余空间。
type VBox struct {
	Children []Widget
	Split    []int
}

func (v *VBox) Draw(c *renderer.Canvas, r renderer.Rect) {
	n := len(v.Children)
	if n == 0 {
		return
	}
	// 计算固定高度总和与剩余
	fixed := 0
	flexCount := 0
	for i, ch := range v.Children {
		if ch == nil {
			continue
		}
		sp := 0
		if i < len(v.Split) {
			sp = v.Split[i]
		}
		if sp >= 0 {
			fixed += sp
		} else {
			flexCount++
		}
	}
	remaining := r.H - fixed
	if remaining < 0 {
		remaining = 0
	}
	flexH := 0
	if flexCount > 0 {
		flexH = remaining / flexCount
	}

	y := r.Y
	for i, ch := range v.Children {
		if ch == nil {
			continue
		}
		sp := 0
		if i < len(v.Split) {
			sp = v.Split[i]
		}
		h := 0
		if sp >= 0 {
			h = sp
		} else {
			h = flexH
		}
		childRect := renderer.NewRect(r.X, y, r.W, h)
		ch.Draw(c, childRect)
		y += h
	}
}

// HBox 水平布局子组件。Split[i] 指定第 i 个子组件的宽度；-1 表示占据剩余空间。
type HBox struct {
	Children []Widget
	Split    []int
}

func (h *HBox) Draw(c *renderer.Canvas, r renderer.Rect) {
	n := len(h.Children)
	if n == 0 {
		return
	}
	fixed := 0
	flexCount := 0
	for i, ch := range h.Children {
		if ch == nil {
			continue
		}
		sp := 0
		if i < len(h.Split) {
			sp = h.Split[i]
		}
		if sp >= 0 {
			fixed += sp
		} else {
			flexCount++
		}
	}
	remaining := r.W - fixed
	if remaining < 0 {
		remaining = 0
	}
	flexW := 0
	if flexCount > 0 {
		flexW = remaining / flexCount
	}

	x := r.X
	for i, ch := range h.Children {
		if ch == nil {
			continue
		}
		sp := 0
		if i < len(h.Split) {
			sp = h.Split[i]
		}
		w := 0
		if sp >= 0 {
			w = sp
		} else {
			w = flexW
		}
		childRect := renderer.NewRect(x, r.Y, w, r.H)
		ch.Draw(c, childRect)
		x += w
	}
}

// Padded 给子组件加内边距。
type Padded struct {
	Inner Widget
	PadX  int
	PadY  int
}

func (p *Padded) Draw(c *renderer.Canvas, r renderer.Rect) {
	if p.Inner == nil {
		return
	}
	p.Inner.Draw(c, r.Inset(p.PadX, p.PadY))
}

// Bordered 给子组件加 box 边框与可选标题。
type Bordered struct {
	Inner  Widget
	Title  string
	Style  renderer.Style
	Active bool
}

func (b *Bordered) Draw(c *renderer.Canvas, r renderer.Rect) {
	if r.W < 2 || r.H < 2 {
		return
	}
	st := b.Style
	if b.Active {
		// 高亮边框：使用主题主色由调用方传入；此处仅加粗
		st = st.WithBold(true)
	}
	// 画边框
	for x := r.X; x < r.X+r.W; x++ {
		c.Put(x, r.Y, renderer.CellRune('─', st))
		c.Put(x, r.Y+r.H-1, renderer.CellRune('─', st))
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		c.Put(r.X, y, renderer.CellRune('│', st))
		c.Put(r.X+r.W-1, y, renderer.CellRune('│', st))
	}
	c.Put(r.X, r.Y, renderer.CellRune('┌', st))
	c.Put(r.X+r.W-1, r.Y, renderer.CellRune('┐', st))
	c.Put(r.X, r.Y+r.H-1, renderer.CellRune('└', st))
	c.Put(r.X+r.W-1, r.Y+r.H-1, renderer.CellRune('┘', st))
	// 标题
	if b.Title != "" {
		title := " " + renderer.Truncate(b.Title, r.W-2) + " "
		c.PutText(r.X+1, r.Y, title, st)
	}
	// 内容
	if b.Inner != nil {
		b.Inner.Draw(c, r.Inset(1, 1))
	}
}

// Text 绘制一行文本（支持对齐）。
type Text struct {
	Content string
	Style   renderer.Style
	Align   Align
}

// Align 文本对齐方式。
type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

func (t *Text) Draw(c *renderer.Canvas, r renderer.Rect) {
	w := renderer.StringWidth(t.Content)
	x := r.X
	switch t.Align {
	case AlignCenter:
		x = r.X + (r.W-w)/2
		if x < r.X {
			x = r.X
		}
	case AlignRight:
		x = r.X + r.W - w
		if x < r.X {
			x = r.X
		}
	}
	c.PutText(x, r.Y, t.Content, t.Style)
}

// Line 绘制水平填充线（用于分隔）。
type Line struct {
	Fill  rune
	Style renderer.Style
}

func (l *Line) Draw(c *renderer.Canvas, r renderer.Rect) {
	for x := r.X; x < r.X+r.W; x++ {
		c.Put(x, r.Y, renderer.CellRune(l.Fill, l.Style))
	}
}

// Spacer 空白占位。
type Spacer struct{}

func (Spacer) Draw(c *renderer.Canvas, r renderer.Rect) {}
