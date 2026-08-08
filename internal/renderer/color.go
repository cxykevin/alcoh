package renderer

// Color 表示一个终端颜色。低 24 位为 RRGGBB truecolor；
// 高位的 sentinel 值区分"默认色"与显式色，避免 0 与黑色冲突。
type Color uint32

const (
	// ColorDefault 表示未指定颜色（使用终端默认）。
	ColorDefault Color = 0x8000_0000
	// ColorBlack 显式黑色（truecolor #000000）。
	ColorBlack Color = 0x0000_0000
)

// RGB 构造一个 truecolor 颜色。
func RGB(r, g, b uint8) Color {
	return Color(uint32(r)<<16 | uint32(g)<<8 | uint32(b))
}

// IsDefault 报告该颜色是否为"默认"（未指定）。
func (c Color) IsDefault() bool { return c&0x8000_0000 != 0 }

// Components 返回颜色的 r,g,b 分量。仅对非默认颜色有意义。
func (c Color) Components() (r, g, b uint8) {
	return uint8(c >> 16), uint8(c >> 8), uint8(c)
}

// Blend 将两个颜色按权重 w（0..1，w=1 表示完全用 from）混合，用于遮罩/dim。
func (c Color) Blend(other Color, w float64) Color {
	if c.IsDefault() {
		return other
	}
	rc, gc, bc := c.Components()
	ro, go_, bo := other.Components()
	// 若目标为默认色，直接返回源色（无法混合）。
	if other.IsDefault() {
		return c
	}
	mix := func(a, b uint8) uint8 {
		return uint8(float64(a)*w + float64(b)*(1-w))
	}
	return RGB(mix(rc, ro), mix(gc, go_), mix(bc, bo))
}

// Dim 返回把颜色压暗的变体（乘 0.6）。
func (c Color) Dim() Color {
	if c.IsDefault() {
		return c
	}
	r, g, b := c.Components()
	return RGB(uint8(float64(r)*0.6), uint8(float64(g)*0.6), uint8(float64(b)*0.6))
}
