package renderer

// Theme 定义 TUI 的配色槽位（深色背景默认主题）。
type Theme struct {
	// 基础
	Text         Color
	TextMuted    Color
	Background   Color
	PanelBg      Color
	Border       Color
	BorderActive Color
	BorderSubtle Color

	// 语义
	Primary   Color // 强调/主题色
	Secondary Color
	Accent    Color
	Error     Color
	Warning   Color
	Success   Color
	Info      Color

	// Markdown
	MDHeading    Color
	MDLink       Color
	MDCode       Color
	MDCodeBg     Color
	MDBlockquote Color
	MDStrong     Color
	MDEmph       Color
	MDList       Color

	// 工具/状态
	ToolPending Color
	ToolRunning Color
	ToolDone    Color
	ToolFailed  Color
}

// DefaultTheme 返回深色背景默认主题。
func DefaultTheme() Theme {
	return Theme{
		Text:         RGB(0xE4, 0xE4, 0xE4),
		TextMuted:    RGB(0x8B, 0x94, 0x9E),
		Background:   RGB(0x0D, 0x11, 0x17),
		PanelBg:      RGB(0x16, 0x1B, 0x22),
		Border:       RGB(0x6B, 0x6B, 0x6B),
		BorderActive: RGB(0x4F, 0x9C, 0xF9),
		BorderSubtle: RGB(0x30, 0x36, 0x3D),

		Primary:   RGB(0x4F, 0x9C, 0xF9),
		Secondary: RGB(0xA3, 0x71, 0xF7),
		Accent:    RGB(0x2E, 0xC4, 0x9E),
		Error:     RGB(0xFF, 0x5F, 0x56),
		Warning:   RGB(0xD2, 0x99, 0x22),
		Success:   RGB(0x3F, 0xB9, 0x50),
		Info:      RGB(0x58, 0xA6, 0xFF),

		MDHeading:    RGB(0x4F, 0x9C, 0xF9),
		MDLink:       RGB(0x58, 0xA6, 0xFF),
		MDCode:       RGB(0xF0, 0x8E, 0x6C),
		MDCodeBg:     RGB(0x16, 0x1B, 0x22),
		MDBlockquote: RGB(0x8B, 0x94, 0x9E),
		MDStrong:     RGB(0xFF, 0xFF, 0xFF),
		MDEmph:       RGB(0xC9, 0xD1, 0xD9),
		MDList:       RGB(0x8B, 0x94, 0x9E),

		ToolPending: RGB(0x8B, 0x94, 0x9E),
		ToolRunning: RGB(0x4F, 0x9C, 0xF9),
		ToolDone:    RGB(0x3F, 0xB9, 0x50),
		ToolFailed:  RGB(0xFF, 0x5F, 0x56),
	}
}

// Style 便捷方法：用主题构造 Style。
func (t Theme) Style(fg Color) Style {
	return Style{Fg: fg, Bg: ColorDefault}
}

// StyleOn 用主题前景 + 背景构造 Style。
func (t Theme) StyleOn(fg, bg Color) Style {
	return Style{Fg: fg, Bg: bg}
}
