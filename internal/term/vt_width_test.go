package term

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestVTScreenWrapsByDisplayWidth 验证换行按显示宽度而非 rune 个数：
// CJK 占 2 列，行末放不下时整字换到下一行，而不是被算成 1 列后挤出/丢掉。
func TestVTScreenWrapsByDisplayWidth(t *testing.T) {
	cases := []struct {
		width int
		text  string
	}{
		{4, "abc中文"},
		{6, "中文字符测试换行"},
		{5, "中文abc中文"},
		{2, "中文"},
	}
	for _, c := range cases {
		scr := NewVTScreen(c.width, 8)
		scr.Feed(c.text)
		var sb strings.Builder
		for _, line := range scr.Lines() {
			sb.WriteString(strings.TrimRight(line, " "))
		}
		if got := sb.String(); got != c.text {
			t.Fatalf("width=%d: chars lost: %q -> %q", c.width, c.text, got)
		}
		for i, line := range scr.Lines() {
			if w := renderer.StringWidth(strings.TrimRight(line, " ")); w > c.width {
				t.Fatalf("width=%d row %d overflow: %d columns (%q)", c.width, i, w, line)
			}
		}
	}
}

// TestVTScreenCJKColumnCursor 验证光标列按显示宽度推进（CJK 算 2 列），
// 回车与制表位也随之按列计算。
func TestVTScreenCJKColumnCursor(t *testing.T) {
	scr := NewVTScreen(10, 3)
	scr.Feed("中文")
	if scr.col != 4 {
		t.Fatalf("col after two CJK = %d, want 4", scr.col)
	}
	scr.Feed("\r")
	if scr.col != 0 || scr.X != 0 {
		t.Fatalf("CR must reset column: col=%d x=%d", scr.col, scr.X)
	}
	scr.Feed("a\tb")
	if scr.col != 9 {
		// 'a' 占 1 列 → 制表到第 8 列，再写 'b' → 9 列。
		t.Fatalf("col after tab = %d, want 9", scr.col)
	}
}
