package renderer

import (
	"strings"
	"testing"
)

// simulateTerminal 用 Diff 的输出模拟真实终端，重建屏幕网格。
// 宽字符按 2 列记录。
type simulateTerminal struct {
	w, h int
	grid []Cell
}

func newSim(w, h int) *simulateTerminal {
	st := &simulateTerminal{w: w, h: h, grid: make([]Cell, w*h)}
	for i := range st.grid {
		st.grid[i] = Cell{R: ' ', Style: DefaultStyle(), Width: 1}
	}
	return st
}

func (st *simulateTerminal) Write(p []byte) (int, error) {
	return len(p), nil // 直接由模拟器内部解析，这里留空
}

// 手动应用一帧 diff 到模拟终端。
func (st *simulateTerminal) apply(old, new *Buffer) {
	aw := NewAnsiWriter(ColorModeTrueColor)
	Diff(old, new, aw)
	s := aw.Bytes()
	// 简易状态机：解析 \x1b[row;colH 与普通字符（宽字符按 2 列）
	cx, cy := 0, 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				var params []int
				num := -1
				for j < len(s) && s[j] != 'm' && s[j] != 'H' {
					if s[j] == ';' {
						params = append(params, num)
						num = -1
					} else if s[j] >= '0' && s[j] <= '9' {
						if num == -1 {
							num = 0
						}
						num = num*10 + int(s[j]-'0')
					} else {
						break
					}
					j++
				}
				params = append(params, num)
				if j < len(s) && s[j] == 'H' {
					cy = params[0] - 1
					cx = params[1] - 1
				}
				i = j + 1
				continue
			}
			// 跳过其它 ESC 序列
			i++
			continue
		}
		// 解析 UTF-8 宽字符
		r := decodeRune(s, i)
		sz := runeSize(s[i])
		if cy >= 0 && cy < st.h && cx >= 0 && cx < st.w {
			w := runeWidth(r)
			st.grid[cy*st.w+cx] = Cell{R: r, Style: Style{}, Width: w}
			if w == 2 && cx+1 < st.w {
				st.grid[cy*st.w+cx+1] = Cell{R: 0, Style: Style{}, Width: 0}
			}
			cx += w
		}
		i += sz
	}
}

// text 导出终端的可见文本（每行）。
func (st *simulateTerminal) text() string {
	var b strings.Builder
	for y := 0; y < st.h; y++ {
		for x := 0; x < st.w; x++ {
			c := st.grid[y*st.w+x]
			if c.Width == 0 {
				continue
			}
			if c.R == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(c.R)
			}
		}
		if y < st.h-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

func decodeRune(s []byte, i int) rune {
	sz := runeSize(s[i])
	var r rune
	for k := 0; k < sz && i+k < len(s); k++ {
		r = r<<8 | rune(s[i+k])
	}
	// 简化 UTF-8 解码
	switch sz {
	case 1:
		return rune(s[i])
	case 2:
		return rune(s[i]&0x1f)<<6 | rune(s[i+1]&0x3f)
	case 3:
		return rune(s[i]&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
	default:
		return rune(s[i]&0x07)<<18 | rune(s[i+1]&0x3f)<<12 | rune(s[i+2]&0x3f)<<6 | rune(s[i+3]&0x3f)
	}
}

func runeSize(b byte) int {
	switch {
	case b < 0x80:
		return 1
	case b < 0xE0:
		return 2
	case b < 0xF0:
		return 3
	default:
		return 4
	}
}

// 场景：旧帧有一行宽字符长文本，新帧在该位置是短 ASCII + 右侧宽字符移位。
func TestDiffClearsStaleWideChars(t *testing.T) {
	W, H := 20, 1
	old := NewBuffer(W, H)
	// 旧行：宽字符长文本（模拟思考内容）"你好世界abc你好"
	old.PutText(0, 0, "你好世界abc你好", DefaultStyle(), W)
	new := NewBuffer(W, H)
	// 新行：窄字符替换 + 右侧宽字符错位
	new.PutText(0, 0, "if err", DefaultStyle(), W)
	new.PutText(7, 0, "世界", DefaultStyle(), W)
	// 其余为空格

	st := newSim(W, H)
	st.apply(old, new)
	// 期望：终端内容与新帧完全一致（无残留）
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			tc, wc := st.grid[y*W+x], new.Get(x, y)
			if tc.R != wc.R || tc.Width != wc.Width {
				t.Errorf("stale cell at (%d,%d): terminal=%q(%d) want=%q(%d)",
					x, y, tc.R, tc.Width, wc.R, wc.Width)
			}
		}
	}
	t.Logf("terminal text: %q", st.text())
}

// 场景：旧帧宽字符，新帧窄字符 + 紧邻新宽字符（旧残留覆盖）。
func TestDiffNarrowOverWideWithAdjacentWide(t *testing.T) {
	W, H := 10, 1
	old := NewBuffer(W, H)
	old.PutText(0, 0, "甲乙丙", DefaultStyle(), W)
	new := NewBuffer(W, H)
	// 新：'x' 占 0，'丁' 占 1-2，'戊' 占 3-4
	new.Set(0, 0, Cell{R: 'x', Style: DefaultStyle(), Width: 1})
	new.Set(1, 0, Cell{R: '丁', Style: DefaultStyle(), Width: 2})
	new.Set(3, 0, Cell{R: '戊', Style: DefaultStyle(), Width: 2})

	st := newSim(W, H)
	st.apply(old, new)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			tc, wc := st.grid[y*W+x], new.Get(x, y)
			if tc.R != wc.R || tc.Width != wc.Width {
				t.Errorf("stale cell at (%d,%d): terminal=%q(%d) want=%q(%d)",
					x, y, tc.R, tc.Width, wc.R, wc.Width)
			}
		}
	}
	t.Logf("terminal text: %q", st.text())
}

// 场景：同一行内容整体左移（宽字符位置变化），右侧应被清空。
func TestDiffShiftLeftClearsTail(t *testing.T) {
	W, H := 16, 1
	old := NewBuffer(W, H)
	old.PutText(2, 0, "中文内容", DefaultStyle(), W)
	new := NewBuffer(W, H)
	new.PutText(0, 0, "中文内容", DefaultStyle(), W)

	st := newSim(W, H)
	st.apply(old, new)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			tc, wc := st.grid[y*W+x], new.Get(x, y)
			if tc.R != wc.R || tc.Width != wc.Width {
				t.Errorf("stale cell at (%d,%d): terminal=%q(%d) want=%q(%d)",
					x, y, tc.R, tc.Width, wc.R, wc.Width)
			}
		}
	}
	t.Logf("terminal text: %q", st.text())
}

// 场景：旧行有宽字符，新行整体是窄 ASCII（旧宽字符残影需清除）。
func TestDiffWideToNarrowClearsContinuation(t *testing.T) {
	W, H := 10, 1
	old := NewBuffer(W, H)
	old.PutText(0, 0, "中文ab", DefaultStyle(), W)
	new := NewBuffer(W, H)
	new.PutText(0, 0, "hello", DefaultStyle(), W)

	st := newSim(W, H)
	st.apply(old, new)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			tc, wc := st.grid[y*W+x], new.Get(x, y)
			if tc.R != wc.R || tc.Width != wc.Width {
				t.Errorf("stale cell at (%d,%d): terminal=%q(%d) want=%q(%d)",
					x, y, tc.R, tc.Width, wc.R, wc.Width)
			}
		}
	}
	t.Logf("terminal text: %q", st.text())
}
