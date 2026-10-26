package view

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestInlineCodeStyleRestored 验证行内代码结束后前景色恢复，不再泄漏到后续文本。
func TestInlineCodeStyleRestored(t *testing.T) {
	theme := renderer.DefaultTheme()
	base := theme.Style(theme.Text)
	spans, idents := inlineStyle("前 `code` 后", base, theme)
	// 渲染 rune 逐个对应源下标：标记符（反引号）不渲染，因而不在下标序列里。
	if want := []int{0, 1, 3, 4, 5, 6, 8, 9}; !equalInts(idents, want) {
		t.Fatalf("idents = %v, want %v", idents, want)
	}
	if len(spans) != 3 {
		t.Fatalf("want 3 spans, got %d: %+v", len(spans), spans)
	}
	if spans[0].Text != "前 " || spans[0].Style != base {
		t.Fatalf("before-code span = %+v", spans[0])
	}
	if spans[1].Text != "code" || spans[1].Style.Fg != theme.MDCode {
		t.Fatalf("code span = %+v", spans[1])
	}
	if spans[2].Text != " 后" || spans[2].Style != base {
		t.Fatalf("after-code span = %+v", spans[2])
	}
}

// TestMarkdownSrcPinned 验证渲染行携带原始逻辑行（插针），供选择复制原始 markdown。
func TestMarkdownSrcPinned(t *testing.T) {
	theme := renderer.DefaultTheme()
	lines := Markdown("# 标题\n普通 `code` 行", theme)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if lines[0].Src != "# 标题" {
		t.Fatalf("heading src = %q", lines[0].Src)
	}
	if lines[1].Src != "普通 `code` 行" {
		t.Fatalf("line src = %q", lines[1].Src)
	}
}

// TestMarkdownCodeSrc 验证代码块每行对应原始代码行。
func TestMarkdownCodeSrc(t *testing.T) {
	theme := renderer.DefaultTheme()
	lines := Markdown("```go\nfmt.Println(1)\n```\n\n正文", theme)
	found := false
	for _, ln := range lines {
		if ln.Src == "fmt.Println(1)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("code src not pinned: %+v", lines)
	}
}

// TestMarkdownLineMap 验证源下标映射：标题跳过 "# "、引用跳过 "> "、列表符号与
// 分隔线是渲染合成（-1），正文逐字对应原始行；映射长度与渲染 rune 数一致。
func TestMarkdownLineMap(t *testing.T) {
	theme := renderer.DefaultTheme()
	cases := []struct {
		line string
		want []int
	}{
		{"# 标题", []int{2, 3}},
		{"> 引用", []int{2, 3}},
		{"- 项", []int{-1, -1, 2}},
		{"---", negIdents(40)},
	}
	for _, c := range cases {
		got := markdownLine(c.line, theme)
		if !equalInts(got.Map, c.want) {
			t.Errorf("%q map = %v, want %v", c.line, got.Map, c.want)
		}
		if n := renderedRunes(got.Spans); n != len(got.Map) {
			t.Errorf("%q: map len %d != 渲染 rune 数 %d", c.line, len(got.Map), n)
		}
	}
}

// TestMarkdownCodeLineMap 验证代码行映射：行号前缀是渲染合成（-1），tab 展开出的
// 每个空格都指向同一个 tab。
func TestMarkdownCodeLineMap(t *testing.T) {
	theme := renderer.DefaultTheme()
	lines := Markdown("```go\n\ta\n```", theme)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	prefix := len([]rune("  1 │ "))
	want := append(negIdents(prefix), 0, 0, 0, 0, 1)
	if !equalInts(lines[0].Map, want) {
		t.Fatalf("code map = %v, want %v", lines[0].Map, want)
	}
}

// TestInlineLinkCJKTextNoPanic 验证链接文字含多字节字符时按 rune 下标切分，
// 不会把 strings.Index 的字节偏移当成 rune 下标（曾 panic: slice bounds out of range）。
func TestInlineLinkCJKTextNoPanic(t *testing.T) {
	theme := renderer.DefaultTheme()
	base := theme.Style(theme.Text)
	cases := []struct {
		line     string
		wantText string
		want     []int
	}{
		// 短行 + 中文链接文字：字节偏移超过 rune 长度，曾直接 panic。
		{"[中文](x)", "中文", []int{1, 2}},
		// 长 URL：不越界但按字节截取会把 "](url" 混进链接文字。
		{"[中文说明](https://x.io)", "中文说明", []int{1, 2, 3, 4}},
	}
	for _, c := range cases {
		spans, idents := inlineStyle(c.line, base, theme)
		if len(spans) != 1 {
			t.Fatalf("%q: want 1 span, got %d: %+v", c.line, len(spans), spans)
		}
		if spans[0].Text != c.wantText {
			t.Fatalf("%q: link text = %q, want %q", c.line, spans[0].Text, c.wantText)
		}
		if !equalInts(idents, c.want) {
			t.Fatalf("%q: idents = %v, want %v", c.line, idents, c.want)
		}
	}
}

// TestInlineLinkCJKTextAround 验证链接前后还有中文时文字截取与源下标偏移都正确。
func TestInlineLinkCJKTextAround(t *testing.T) {
	theme := renderer.DefaultTheme()
	base := theme.Style(theme.Text)
	spans, idents := inlineStyle("看 [中文说明](https://x.io) 吧", base, theme)
	if len(spans) != 3 {
		t.Fatalf("want 3 spans, got %d: %+v", len(spans), spans)
	}
	if spans[0].Text != "看 " || spans[1].Text != "中文说明" || spans[2].Text != " 吧" {
		t.Fatalf("spans = %+v", spans)
	}
	if n := renderedRunes(spans); n != len(idents) {
		t.Fatalf("idents len %d != 渲染 rune 数 %d", len(idents), n)
	}
	if want := []int{0, 1, 3, 4, 5, 6, 22, 23}; !equalInts(idents, want) {
		t.Fatalf("idents = %v, want %v", idents, want)
	}
}

// equalInts 比较两个下标序列（nil 与空序列等价）。
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// renderedRunes 统计渲染 rune 数（Map 长度应与之一致）。
func renderedRunes(spans []Span) int {
	n := 0
	for _, sp := range spans {
		n += len([]rune(sp.Text))
	}
	return n
}
