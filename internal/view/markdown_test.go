package view

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestInlineCodeStyleRestored 验证行内代码结束后前景色恢复，不再泄漏到后续文本。
func TestInlineCodeStyleRestored(t *testing.T) {
	theme := renderer.DefaultTheme()
	base := theme.Style(theme.Text)
	spans := inlineStyle("前 `code` 后", base, theme)
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
