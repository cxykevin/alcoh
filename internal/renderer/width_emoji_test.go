package renderer

import (
	"math/rand"
	"strconv"
	"testing"
	"unicode/utf8"
)

// 这些用例针对"滚动时行尾残留字符"：本地宽度表一旦把终端按 2 列渲染的字符
// （emoji 呈现形式、部分 CJK 符号）算成 1 列，diff 写出的整行会比终端少前进若干列，
// 行尾就会保留上一帧的字符。宽度取值来自终端实测（wcwidth / tmux），与本地表独立，
// 避免测试自证。
func refWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r >= 0x20 && r < 0x7F:
		return 1 // ASCII
	case r == '…':
		return 1 // 省略号：截断补位字符
	case r == '✅' || r == '❌' || r == '✨' || r == '⚡' || r == '☕' || r == '⭐':
		return 2 // Emoji_Presentation=Yes：终端按 2 列
	case r >= 0x4E00 && r <= 0x9FFF:
		return 2 // CJK 统一表意
	case r == '，' || r == '。':
		return 2 // 全角标点
	default:
		return -1 // 未覆盖的字符由 termEmu 报错，不静默跳过
	}
}

// TestRuneWidthEmojiPresentation 回归：Emoji_Presentation=Yes 的字符必须按 2 列，
// 否则含 ✅/❌ 的中文行会整体左错位，滚动时行尾残留上一帧的字符。
func TestRuneWidthEmojiPresentation(t *testing.T) {
	wide := []rune{'✅', '❌', '✨', '⚡', '☕', '⭐', '⌚', '⌛', '⏰', '☔', '⚽', '⛳', '➕', '⭕'}
	for _, r := range wide {
		if got := runeWidth(r); got != 2 {
			t.Errorf("runeWidth(%q U+%04X) = %d, want 2 (Emoji_Presentation)", r, r, got)
		}
	}
	// 兼容 emoji：Emoji_Presentation=No，单独出现时终端按文本呈现 1 列；
	// 应用侧会丢弃后面的变体选择符（零宽），因此这里必须保持 1 列。
	narrow := []rune{'⚠', '❤', '❯', '▌', '▸', '▾', '─', '│', '…', '·', '㉈', '🇦'}
	for _, r := range narrow {
		if got := runeWidth(r); got != 1 {
			t.Errorf("runeWidth(%q U+%04X) = %d, want 1", r, r, got)
		}
	}
	// CJK 符号与新增补的宽字符区块。
	cjkWide := []rune{'中', '，', 'Ａ', 'あ', 'ア', '한', '\u4DC0', '\U0001D300', '\U0001D360'}
	for _, r := range cjkWide {
		if got := runeWidth(r); got != 2 {
			t.Errorf("runeWidth(%q U+%04X) = %d, want 2", r, r, got)
		}
	}
	// 零宽：组合记号、ZWSP、变体选择符、半角谚文填充符。
	zero := []rune{'\u0301', '\u200B', '\uFE0F', '\uFFA0'}
	for _, r := range zero {
		if got := runeWidth(r); got != 0 {
			t.Errorf("runeWidth(%q U+%04X) = %d, want 0", r, r, got)
		}
	}
}

// TestWidthTablesSorted 保证三张区间表按 lo 升序且互不重叠——inRanges 是二分查找，
// 顺序错误会静默算错宽度（正是"行尾残留"这类问题的来源）。
func TestWidthTablesSorted(t *testing.T) {
	tables := map[string][]runeRange{
		"zeroWidth":       zeroWidth,
		"wideRanges":      wideRanges,
		"emojiWideRanges": emojiWideRanges,
	}
	for name, tbl := range tables {
		prev := rune(-1)
		for i, rr := range tbl {
			if rr.lo > rr.hi {
				t.Fatalf("%s[%d]: 空区间 %#x..%#x", name, i, rr.lo, rr.hi)
			}
			if rr.lo <= prev {
				t.Fatalf("%s[%d]: 顺序错误或区间重叠（lo=%#x <= 上一项 hi=%#x）", name, i, rr.lo, prev)
			}
			prev = rr.hi
		}
	}
}

// termEmu 是最小终端网格模型：宽度取自参考表 refWidth（而非 runeWidth）。
// 它按终端语义处理宽字符：写入宽字符首列时清除旧续列，写入续列时清除旧首列。
type termEmu struct {
	w, h int
	grid []Cell
	// unknown 记录参考表未覆盖的字符：出现即为用例自身的问题（不能静默跳过，
	// 否则参考终端会算错列宽，测试失去意义）。
	unknown rune
}

func newTermEmu(w, h int) *termEmu {
	e := &termEmu{w: w, h: h, grid: make([]Cell, w*h)}
	for i := range e.grid {
		e.grid[i] = Cell{R: ' ', Style: DefaultStyle(), Width: 1}
	}
	return e
}

func (e *termEmu) put(x, y int, r rune) {
	if x < 0 || x >= e.w || y < 0 || y >= e.h {
		return
	}
	w := refWidth(r)
	if w <= 0 {
		return
	}
	i := y*e.w + x
	if x > 0 && e.grid[i].Width == 0 && e.grid[i-1].Width == 2 {
		e.grid[i-1] = Cell{R: ' ', Style: DefaultStyle(), Width: 1}
	}
	if e.grid[i].Width == 2 && x+1 < e.w {
		e.grid[i+1] = Cell{R: ' ', Style: DefaultStyle(), Width: 1}
	}
	e.grid[i] = Cell{R: r, Style: DefaultStyle(), Width: w}
	if w == 2 && x+1 < e.w {
		e.grid[i+1] = Cell{R: 0, Style: DefaultStyle(), Width: 0}
	}
}

// apply 解析 diff 输出（CUP / CUF / SGR / UTF-8 字符）。
func (e *termEmu) apply(b []byte) {
	cx, cy := 0, 0
	i := 0
	for i < len(b) {
		if b[i] == 0x1b {
			j := i + 1
			if j < len(b) && b[j] == '[' {
				j++
				start := j
				for j < len(b) && (b[j] == ';' || (b[j] >= '0' && b[j] <= '9')) {
					j++
				}
				if j < len(b) {
					body, final := string(b[start:j]), b[j]
					num := func(idx, def int) int {
						parts := []string{}
						cur := ""
						for _, c := range body {
							if c == ';' {
								parts = append(parts, cur)
								cur = ""
								continue
							}
							cur += string(c)
						}
						parts = append(parts, cur)
						if idx < len(parts) && parts[idx] != "" {
							if v, err := strconv.Atoi(parts[idx]); err == nil {
								return v
							}
						}
						return def
					}
					switch final {
					case 'H', 'f':
						cy, cx = num(0, 1)-1, num(1, 1)-1
					case 'C':
						cx += num(0, 1)
					}
					i = j + 1
					continue
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if w := refWidth(r); w >= 0 {
			e.put(cx, cy, r)
			cx += w
		} else {
			e.unknown = r
		}
		i += size
	}
}

func (e *termEmu) check(t *testing.T, want *Buffer, label string) {
	t.Helper()
	if e.unknown != 0 {
		t.Fatalf("%s: 参考宽度表未覆盖 %q U+%04X，请先补 refWidth", label, e.unknown, e.unknown)
	}
	for y := 0; y < e.h; y++ {
		for x := 0; x < e.w; x++ {
			got, exp := e.grid[y*e.w+x], want.Get(x, y)
			if got.R != exp.R || got.Width != exp.Width {
				t.Fatalf("%s: 第 %d 行第 %d 列终端=%q/%d 期望=%q/%d（行尾残留/错位）",
					label, y, x, got.R, got.Width, exp.R, exp.Width)
			}
		}
	}
}

// TestDiffEmojiLineHasNoStaleTail 回归：整行换成以 ✅ 开头的中文内容后，
// 终端行尾不得残留上一帧的字符。修复前 ✅ 被算成 1 列，整行少前进 1 列，
// 行尾保留旧字符；滚动时这就是用户看到的"中文残留"。
func TestDiffEmojiLineHasNoStaleTail(t *testing.T) {
	const W, H = 40, 1
	back := NewBuffer(W, H)
	back.PutText(0, 0, "中文字符残留测试ABCDEFG", DefaultStyle(), W)

	front := NewBuffer(W, H)
	front.PutText(0, 0, "✅ 完成需要检查的残留问题测试", DefaultStyle(), W)

	emu := newTermEmu(W, H)
	aw := NewAnsiWriter(ColorModeTrueColor)
	Diff(back, front, aw)
	emu.apply(aw.Bytes())
	emu.check(t, front, "emoji line")
}

// TestDiffRunStartingAtContinuation 回归：当写入 run 从宽字符"续列"开始时
// （例如选择高亮只改了续列的样式），MoveTo 会把终端光标停在续列上；
// 必须把光标推进到续列之后，否则本 run 后续字符整体左移一格，行尾残留旧字符。
func TestDiffRunStartingAtContinuation(t *testing.T) {
	const W, H = 10, 1
	back := NewBuffer(W, H)
	back.PutText(0, 0, "中文内容", DefaultStyle(), W)

	front := NewBuffer(W, H)
	front.PutText(0, 0, "中文内容", DefaultStyle(), W)
	if i := front.Index(1, 0); i >= 0 { // 只反显续列，首列不变
		front.Cells[i].Style = front.Cells[i].Style.WithReverse(true)
	}
	front.Set(2, 0, Cell{R: 'X', Style: DefaultStyle(), Width: 1}) // 让 run 继续向后写

	emu := newTermEmu(W, H)
	sentinel := NewBuffer(W, H)
	for i := range sentinel.Cells {
		sentinel.Cells[i] = Cell{R: 0, Style: DefaultStyle(), Width: 0}
	}
	aw0 := NewAnsiWriter(ColorModeTrueColor)
	Diff(sentinel, back, aw0)
	emu.apply(aw0.Bytes())

	aw := NewAnsiWriter(ColorModeTrueColor)
	Diff(back, front, aw)
	emu.apply(aw.Bytes())
	emu.check(t, front, "run starts at continuation")
}

// TestDiffEmojiRandomFrames 随机滚动帧序列交叉验证：每一帧渲染后，
// 参考宽度终端必须逐格等于目标帧（覆盖宽字符换行、截断、清尾）。
func TestDiffEmojiRandomFrames(t *testing.T) {
	const W, H = 36, 8
	alphabet := []rune("abc xyz 中文字符测试完成需要检查残留问题✅❌✨")
	r := rand.New(rand.NewSource(20260926))

	back := NewBuffer(W, H)
	sentinel := Cell{R: 0, Style: DefaultStyle(), Width: 0}
	for i := range back.Cells {
		back.Cells[i] = sentinel
	}
	emu := newTermEmu(W, H)
	for iter := 0; iter < 300; iter++ {
		front := NewBuffer(W, H)
		for y := 0; y < H; y++ {
			if r.Intn(4) == 0 {
				continue
			}
			var s []rune
			for n := r.Intn(14); n >= 0; n-- {
				s = append(s, alphabet[r.Intn(len(alphabet))])
			}
			x := r.Intn(W)
			front.PutText(x, y, string(s), DefaultStyle(), 1+r.Intn(W-x))
		}
		aw := NewAnsiWriter(ColorModeTrueColor)
		Diff(back, front, aw)
		emu.apply(aw.Bytes())
		emu.check(t, front, "iter "+strconv.Itoa(iter))
		back = front
	}
}
