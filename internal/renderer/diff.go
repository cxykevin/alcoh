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
	for y := 0; y < H; y++ {
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
			aw.MoveTo(y, x)
			for x < W {
				b = back.Get(x, y)
				f = front.Get(x, y)
				if b.Equal(f) {
					break
				}
				switch {
				case f.Width == 0:
					// 续列：由前一个宽字符自动覆盖，无需输出
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
