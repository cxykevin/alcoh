package renderer

// Buffer 是屏幕单元格网格（行主序）。宽字符在数组中占 2 个 cell：
// 首列 Width=2，续列 Width=0（占位，不输出）。
type Buffer struct {
	W, H  int
	Cells []Cell
}

// NewBuffer 创建 w×h 的空 buffer（填充默认空格）。
func NewBuffer(w, h int) *Buffer {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	b := &Buffer{W: w, H: h, Cells: make([]Cell, w*h)}
	fill := Cell{R: ' ', Style: DefaultStyle(), Width: 1}
	for i := range b.Cells {
		b.Cells[i] = fill
	}
	return b
}

// Index 返回 (x,y) 在 Cells 数组中的下标，越界返回 -1。
func (b *Buffer) Index(x, y int) int {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return -1
	}
	return y*b.W + x
}

// Get 返回 (x,y) 处的 cell。越界返回空格 cell。
func (b *Buffer) Get(x, y int) Cell {
	if i := b.Index(x, y); i >= 0 {
		return b.Cells[i]
	}
	return Cell{R: ' ', Style: DefaultStyle(), Width: 1}
}

// Set 将 cell 写入 (x,y)。若 cell 为宽字符（Width=2）且未越界，自动写续列占位。
// 若该位置原是宽字符且新 cell 是窄字符，清除右侧续列（防残影）。
func (b *Buffer) Set(x, y int, c Cell) {
	i := b.Index(x, y)
	if i < 0 {
		return
	}
	// 若覆盖宽字符续列，先清除左侧首列，避免网格保留非法宽字符。
	if x > 0 && b.Cells[i].Width == 0 {
		if prev := b.Index(x-1, y); prev >= 0 && b.Cells[prev].Width == 2 {
			b.Cells[prev] = Cell{R: ' ', Style: c.Style, Width: 1}
		}
	}
	// 清除旧的续列残留：若旧 cell 是宽字符首列，其续列在 x+1
	if old := b.Cells[i]; old.Width == 2 && (c.Width == 1 || c.Width == 0) {
		if j := b.Index(x+1, y); j >= 0 {
			if b.Cells[j].Width == 0 {
				b.Cells[j] = Cell{R: ' ', Style: c.Style, Width: 1}
			}
		}
	}
	// 新写入宽字符：占 x 与 x+1 两列
	if c.Width == 2 {
		b.Cells[i] = c
		if j := b.Index(x+1, y); j >= 0 {
			// 续列保留前一个字符的样式，R 用占位空格
			b.Cells[j] = Cell{R: 0, Style: c.Style, Width: 0}
		}
		return
	}
	b.Cells[i] = c
}

// PutText 将字符串写入 (x,y)，样式 st，最多 maxW 列。tab 固定展开为 4 个空格。
// 返回实际占用的列宽。若超过 maxW 截断并补省略号。
func (b *Buffer) PutText(x, y int, s string, st Style, maxW int) int {
	if maxW < 0 {
		maxW = 0
	}
	cur := x
	for _, r := range s {
		if cur-x >= maxW {
			break
		}
		if r == '\t' {
			// tab 固定展开为 4 个空格，避免依赖终端 tab stop。
			for i := 0; i < 4 && cur-x < maxW; i++ {
				b.Set(cur, y, Cell{R: ' ', Style: st, Width: 1})
				cur++
			}
			continue
		}
		rw := runeWidth(r)
		if rw == 0 {
			// 零宽字符：附着于前一个字符之后，若前面有字符则在其后追加
			// 简化处理：直接忽略（对终端显示无影响）
			continue
		}
		if cur-x+rw > maxW {
			// 截断，末尾补省略号
			if cur-x == maxW {
				// 正好占满：替换最后一个为省略号
				if maxW > 0 {
					b.Set(cur-1, y, Cell{R: '…', Style: st, Width: 1})
				}
			} else {
				b.Set(cur, y, Cell{R: '…', Style: st, Width: 1})
				cur++
			}
			break
		}
		b.Set(cur, y, Cell{R: r, Style: st, Width: rw})
		cur += rw
	}
	return cur - x
}

// Fill 用 cell 填充矩形区域。
func (b *Buffer) Fill(r Rect, c Cell) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			b.Set(x, y, c)
		}
	}
}

// Clear 用默认空格清空整个 buffer。
func (b *Buffer) Clear() {
	fill := Cell{R: ' ', Style: DefaultStyle(), Width: 1}
	for i := range b.Cells {
		b.Cells[i] = fill
	}
}

// Canvas 是带裁剪矩形的 Buffer 视图，widget 通过它绘图。
type Canvas struct {
	B    *Buffer
	Clip Rect
}

// NewCanvas 为 buffer 的整个区域创建 canvas。
func NewCanvas(b *Buffer) *Canvas {
	return &Canvas{B: b, Clip: NewRect(0, 0, b.W, b.H)}
}

// Sub 返回指定矩形区域的子 canvas（先与 Clip 求交）。
func (c *Canvas) Sub(r Rect) *Canvas {
	r = r.Intersect(c.Clip)
	return &Canvas{B: c.B, Clip: r}
}

// Put 将 cell 写入 clip 内的 (x,y)。裁剪检查在 Index 前完成。
func (c *Canvas) Put(x, y int, cell Cell) {
	if !c.Clip.Contains(x, y) {
		return
	}
	// 宽字符续列可能越出 clip 右边界，但必须写入以保持布局完整
	c.B.Set(x, y, cell)
}

// PutText 在 clip 内写入文本（超出 clip 宽度被裁剪）。
func (c *Canvas) PutText(x, y int, s string, st Style) {
	if !c.Clip.Contains(x, y) {
		return
	}
	maxW := c.Clip.X + c.Clip.W - x
	if maxW <= 0 {
		return
	}
	c.B.PutText(x, y, s, st, maxW)
}

// Fill 在 clip 内填充。
func (c *Canvas) Fill(r Rect, cell Cell) {
	r = r.Intersect(c.Clip)
	c.B.Fill(r, cell)
}

// Rect 返回 clip 矩形。
func (c *Canvas) Rect() Rect { return c.Clip }
