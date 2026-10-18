package renderer

// 字符列宽计算：不依赖第三方库，采用内建区间表（见 width_tables.go，
// 由 widthgen.go 从 Unicode 17.0.0 UCD 生成）。
// 规则（对齐终端惯例）：
//   - 组合记号 Mn/Me/Cf、Grapheme_Extend、ZWJ(U+200D)、变体选择符(FE00-FE0F)、
//     零宽空格(U+200B)、Emoji_Modifier（肤色修饰符）等 → 0 列
//   - East_Asian_Width=W/F、Emoji_Presentation=Yes（✅❌✨ 等）→ 2 列
//   - 区域指示符（U+1F1E6-1F1FF）按 1 列：成对组成的旗帜终端只占 2 列
//   - Ambiguous 宽度按窄（1 列）处理
//   - 其余 → 1 列

type runeRange struct{ lo, hi rune }

func inRanges(r rune, ranges []runeRange) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		rr := ranges[mid]
		switch {
		case r < rr.lo:
			hi = mid - 1
		case r > rr.hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// runeWidth 返回单个 rune 占用的终端列宽（0/1/2）。
func runeWidth(r rune) int {
	switch {
	case r == '\t':
		return 1 // tab 由调用方按列展开
	case r < 0x20:
		return 0 // 控制字符不输出
	case inRanges(r, zeroWidth):
		return 0
	case inRanges(r, wideRanges):
		return 2
	default:
		return 1
	}
}

// RuneWidth 导出单个 rune 的列宽。
func RuneWidth(r rune) int { return runeWidth(r) }

// StringWidth 返回字符串的总列宽（不含 tab 展开，tab 按 1 列计）。
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// Truncate 按列宽截断 s 到 maxW 列。若发生截断，末尾补 "…"（占 1 列）。
func Truncate(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	var out []rune
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if rw == 0 {
			continue // 零宽字符直接丢弃（截断语境）
		}
		if w+rw > maxW {
			// 用省略号替换末尾
			if w == maxW {
				// 正好占满，替换最后一个字符为省略号（若为宽字符需回溯）
				if len(out) > 0 && w > 1 {
					last := out[len(out)-1]
					if runeWidth(last) == 2 {
						out = out[:len(out)-1]
						out = append(out, '…')
					} else {
						out[len(out)-1] = '…'
					}
				} else {
					out = []rune{'…'}
				}
			} else {
				out = append(out, '…')
			}
			return string(out)
		}
		out = append(out, r)
		w += rw
	}
	return string(out)
}

// Wrap 按 maxW 列宽换行，返回多行。宽字符不跨行断开。
func Wrap(s string, maxW int) []string {
	if maxW <= 1 {
		maxW = 1
	}
	var lines []string
	line := []rune{}
	w := 0
	flush := func() {
		if len(line) > 0 {
			lines = append(lines, string(line))
			line = line[:0]
			w = 0
		}
	}
	for _, r := range s {
		rw := runeWidth(r)
		switch {
		case r == '\n':
			flush()
		case rw == 0:
			line = append(line, r) // 保留零宽字符附着于前一个字符
		case w+rw > maxW:
			flush()
			line = append(line, r)
			w = rw
		default:
			line = append(line, r)
			w += rw
		}
	}
	flush()
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}
