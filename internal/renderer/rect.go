package renderer

// Rect 是屏幕上的矩形区域（X,Y 左上角，W,H 尺寸）。
type Rect struct {
	X, Y, W, H int
}

// NewRect 构造一个矩形。
func NewRect(x, y, w, h int) Rect {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X: x, Y: y, W: w, H: h}
}

// Contains 报告坐标 (x,y) 是否在矩形内。
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Inset 返回向内缩进 dx,dy 后的矩形。
func (r Rect) Inset(dx, dy int) Rect {
	r.X += dx
	r.Y += dy
	r.W -= 2 * dx
	r.H -= 2 * dy
	if r.W < 0 {
		r.W = 0
	}
	if r.H < 0 {
		r.H = 0
	}
	return r
}

// Intersect 返回两个矩形的交集。
func (r Rect) Intersect(o Rect) Rect {
	x1 := max(r.X, o.X)
	y1 := max(r.Y, o.Y)
	x2 := min(r.X+r.W, o.X+o.W)
	y2 := min(r.Y+r.H, o.Y+o.H)
	return NewRect(x1, y1, x2-x1, y2-y1)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
