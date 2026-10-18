package renderer

import (
	"strings"
	"testing"
)

// TestDiffWideClearFillsSpacesFirst 回归 Windows conhost 的宽字符覆盖问题：
// 开启 wideClear 后，含宽字符的变化 run 先按整段宽度铺一遍空格，再写正文。
func TestDiffWideClearFillsSpacesFirst(t *testing.T) {
	const W, H = 8, 1
	back := NewBuffer(W, H)
	back.PutText(0, 0, "ab", DefaultStyle(), W)
	front := NewBuffer(W, H)
	front.PutText(0, 0, "中", DefaultStyle(), W)

	aw := NewAnsiWriter(ColorModeTrueColor)
	aw.SetWideClear(true)
	Diff(back, front, aw)
	out := string(aw.Bytes())
	// 先 CUP 到 run 起点铺两个空格（"中"占 2 列），再 CUP 回来写正文。
	if !strings.Contains(out, "  \x1b[1;1H中") {
		t.Fatalf("wideClear 未先铺空格再写宽字符: %q", out)
	}
	emu := newTermEmu(W, H)
	emu.apply(aw.Bytes())
	emu.check(t, front, "wide clear")

	plain := NewAnsiWriter(ColorModeTrueColor)
	Diff(back, front, plain)
	if strings.Contains(string(plain.Bytes()), "  \x1b[1;1H") {
		t.Fatalf("未开启 wideClear 时不应额外铺空格: %q", plain.Bytes())
	}
}

// TestChangedRunWideHead 验证宽字符首列变化时，擦除区间覆盖它的两列。
func TestChangedRunWideHead(t *testing.T) {
	const W = 8
	back := NewBuffer(W, 1)
	back.PutText(0, 0, "ab", DefaultStyle(), W)
	front := NewBuffer(W, 1)
	front.PutText(0, 0, "中", DefaultStyle(), W)

	from, end, wide := changedRun(back, front, 0, 0)
	if from != 0 || end != 2 || !wide {
		t.Fatalf("changedRun = (%d, %d, %v), want (0, 2, true)", from, end, wide)
	}
}

// TestChangedRunSkipsContinuation 验证擦除区间不会覆盖左侧未变化的宽字符：
// run 从宽字符续列开始时（例如只改了续列的样式），第 0 列必须排除在外。
func TestChangedRunSkipsContinuation(t *testing.T) {
	const W = 8
	back := NewBuffer(W, 1)
	back.PutText(0, 0, "中abc", DefaultStyle(), W)
	front := NewBuffer(W, 1)
	copy(front.Cells, back.Cells)
	if i := front.Index(1, 0); i >= 0 {
		front.Cells[i].Style = front.Cells[i].Style.WithReverse(true)
	}

	from, end, wide := changedRun(back, front, 1, 0)
	if from != 2 || end != 2 || wide {
		t.Fatalf("changedRun = (%d, %d, %v), want (2, 2, false)", from, end, wide)
	}
}
