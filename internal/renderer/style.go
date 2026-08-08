package renderer

// Style 描述一个单元格的渲染样式。所有字段可直接 == 比较，diff 无需哈希。
type Style struct {
	Fg, Bg        Color
	Bold          bool
	Italic        bool
	Underline     bool
	Reverse       bool
	Dim           bool
	Strikethrough bool
}

// DefaultStyle 返回空样式（完全使用终端默认）。
func DefaultStyle() Style { return Style{Fg: ColorDefault, Bg: ColorDefault} }

// WithFg 返回设置前景色后的样式副本。
func (s Style) WithFg(c Color) Style { s.Fg = c; return s }

// WithBg 返回设置背景色后的样式副本。
func (s Style) WithBg(c Color) Style { s.Bg = c; return s }

// WithBold 返回设置粗体后的样式副本。
func (s Style) WithBold(b bool) Style { s.Bold = b; return s }

// WithItalic 返回设置斜体后的样式副本。
func (s Style) WithItalic(b bool) Style { s.Italic = b; return s }

// WithUnderline 返回设置下划线后的样式副本。
func (s Style) WithUnderline(b bool) Style { s.Underline = b; return s }

// WithReverse 返回设置反显后的样式副本。
func (s Style) WithReverse(b bool) Style { s.Reverse = b; return s }

// WithDim 返回设置 dim 后的样式副本。
func (s Style) WithDim(b bool) Style { s.Dim = b; return s }

// Overlay 把上层的部分样式应用到本样式上（用于遮罩/高亮）：非默认色覆盖，bool 取或。
func (s Style) Overlay(o Style) Style {
	if !o.Fg.IsDefault() {
		s.Fg = o.Fg
	}
	if !o.Bg.IsDefault() {
		s.Bg = o.Bg
	}
	s.Bold = s.Bold || o.Bold
	s.Italic = s.Italic || o.Italic
	s.Underline = s.Underline || o.Underline
	s.Reverse = s.Reverse || o.Reverse
	s.Dim = s.Dim || o.Dim
	s.Strikethrough = s.Strikethrough || o.Strikethrough
	return s
}

// DimStyle 返回整体压暗的样式（前景色压暗 + Dim 位）。
func (s Style) DimStyle() Style {
	if !s.Fg.IsDefault() {
		s.Fg = s.Fg.Dim()
	}
	if !s.Bg.IsDefault() {
		s.Bg = s.Bg.Dim()
	}
	s.Dim = true
	return s
}
