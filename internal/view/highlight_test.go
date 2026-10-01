package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/renderer"
)

func TestMarkdownCodeHighlightAddsLineNumbers(t *testing.T) {
	lines := Markdown("```go\nfunc main() {\n\tprintln(1)\n}\n```", renderer.DefaultTheme())
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if got := lines[0].Spans[0].Text; got != "  1 │ " {
		t.Errorf("first line prefix = %q, want line number prefix", got)
	}
	var second strings.Builder
	for _, sp := range lines[1].Spans {
		second.WriteString(sp.Text)
	}
	if !strings.Contains(second.String(), "    println(1)") {
		t.Errorf("tab should expand to four spaces, got %q", second.String())
	}
}

func TestWrapLineExpandsTabToFourSpaces(t *testing.T) {
	st := renderer.DefaultTheme().Style(renderer.DefaultTheme().Text)
	lines := wrapLine(StyledLine{Spans: []Span{{Text: "a\tb", Style: st}}, Map: []int{0, 1, 2}}, 20)
	if len(lines) != 1 || len(lines[0].Spans) != 1 || lines[0].Spans[0].Text != "a    b" {
		t.Fatalf("tab expansion got %#v", lines)
	}
	// 展开出的 4 个空格都指向同一个 tab（源下标 1），复制时只算一个字符。
	want := []int{0, 1, 1, 1, 1, 2}
	if len(lines[0].Map) != len(want) {
		t.Fatalf("tab map = %v, want %v", lines[0].Map, want)
	}
	for i, id := range want {
		if lines[0].Map[i] != id {
			t.Fatalf("tab map = %v, want %v", lines[0].Map, want)
		}
	}
}
