package renderer

// Cell 是屏幕上的一个单元格。
// Width: 1=普通字符；2=宽字符（CJK/全角）的首列；0=宽字符的续列占位（不输出）。
type Cell struct {
	R     rune
	Style Style
	Width int
}

// CellSpace 返回一个空格单元格。
func CellSpace(st Style) Cell {
	return Cell{R: ' ', Style: st, Width: 1}
}

// CellRune 返回一个普通宽度字符单元格。
func CellRune(r rune, st Style) Cell {
	w := runeWidth(r)
	if w == 0 {
		w = 1 // 零宽字符兜底为普通单元格
	}
	return Cell{R: r, Style: st, Width: w}
}

// IsContinuation 报告该 cell 是否为宽字符的续列占位。
func (c Cell) IsContinuation() bool { return c.Width == 0 }

// Equal 判断两个 cell 是否完全相同。
func (c Cell) Equal(o Cell) bool {
	return c.R == o.R && c.Style == o.Style && c.Width == o.Width
}
