package renderer

import (
	"io"
)

// Diff 把 back（上一帧）到 front（新帧）的差异编码为 ANSI 序列写入 aw。
// 逐行扫描、跳过相同 cell、为连续变化段移动游标并写出。
// 处理：宽字符续列不输出、窄字符覆盖宽字符时清除残影。
func Diff(back, front *Buffer, aw *AnsiWriter) {
	W, H := back.W, back.H
	if front.W != W || front.H != H {
		return // 尺寸不匹配由调用方负责 resize
	}
	for y := range H {
		x := 0
		for x < W {
			b := back.Get(x, y)
			f := front.Get(x, y)
			if b.Equal(f) {
				x++
				continue
			}
			// 若 run 起点是续列且旧位置非续列，回退到宽字符首列确保完整覆盖
			if f.Width == 0 && b.Width != 0 && x > 0 {
				x--
			}
			// conhost 覆盖宽字符时可能只擦掉半个字：含宽字符的 run 先按整段
			// 宽度铺一遍空格，再写正文（见 AnsiWriter.wideClear）。
			if aw.wideClear {
				if from, end, wide := changedRun(back, front, x, y); wide && end > from {
					aw.MoveTo(y, from)
					for i := from; i < end; i++ {
						aw.WriteSpace(front.Get(i, y).Style)
					}
				}
			}
			aw.MoveTo(y, x)
			for x < W {
				b = back.Get(x, y)
				f = front.Get(x, y)
				if b.Equal(f) {
					break
				}
				switch {
				case f.Width == 0:
					// 续列不单独输出（由左侧宽字符覆盖）。但游标模型必须同步：
					// run 从续列开始时 MoveTo 会把终端光标停在续列上，若不推进，
					// 本 run 后续字符会整体左移一格，行尾留下上一帧的字符。
					if x+1 < W {
						aw.MoveTo(y, x+1)
					}
					x++
				case f.Width == 2:
					aw.WriteRune(f.R, 2, f.Style)
					x += 2
				default: // Width == 1
					if b.Width == 2 {
						// 窄字符覆盖宽字符首列：先写窄字符，再处理旧宽字符的续列残影。
						// 注意：x+1 是否清除取决于新帧在该处是否有实际内容——
						// 若有新内容（窄字符/宽字符首列）则绝不能清成空格，也不能跳过它。
						aw.WriteRune(f.R, 1, f.Style)
						nx := x + 1
						if nx < W {
							nf := front.Get(nx, y)
							if nf.Width == 1 && nf.R == ' ' {
								// 新帧 x+1 为空：清除旧宽字符续列残影。
								// 该 cell 自己的样式必须以新帧为准；不能沿用 x 的
								// 光标样式，否则删除 CJK 后续列会残留光标背景色。
								aw.WriteRune(' ', 1, nf.Style)
								x += 2
							} else {
								// x+1 有新内容（窄字符或宽字符首列）→ 交给循环继续处理
								x++
							}
						} else {
							x += 2
						}
					} else {
						aw.WriteRune(f.R, 1, f.Style)
						x++
					}
				}
			}
		}
	}
}

// changedRun 计算从 x 开始的变化 run 的擦除区间 [clearFrom, end) 以及它是否
// 涉及宽字符。end 是 run 结束列（不含），按 Diff 的写入步进推进：宽字符占 2 列，
// 续列占 1 列。clearFrom 跳过 run 开头的续列——那半格属于左侧未变化的宽字符，
// 擦掉会把原字符打坏。
func changedRun(back, front *Buffer, x, y int) (clearFrom, end int, wide bool) {
	W := front.W
	clearFrom = x
	if front.Get(x, y).Width == 0 {
		clearFrom = x + 1
	}
	end = x
	for end < W {
		b, f := back.Get(end, y), front.Get(end, y)
		if b.Equal(f) {
			break
		}
		if f.Width == 2 || b.Width == 2 {
			wide = true
		}
		if f.Width == 2 {
			end += 2
		} else {
			end++
		}
	}
	if end > W {
		end = W
	}
	if clearFrom > end {
		clearFrom = end
	}
	return clearFrom, end, wide
}

// Render 将 back→front 的差异渲染到 w。
// 返回写入的字节数。若两帧完全相同则返回 0。
func Render(back, front *Buffer, mode ColorMode, w io.Writer) (int, error) {
	aw := NewAnsiWriter(mode)
	Diff(back, front, aw)
	if aw.Len() == 0 {
		return 0, nil
	}
	n, err := w.Write(aw.Bytes())
	if err != nil {
		return n, err
	}
	return aw.Len(), nil
}
