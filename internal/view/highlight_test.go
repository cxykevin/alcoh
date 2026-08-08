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

func TestWrapSpansExpandsTabToFourSpaces(t *testing.T) {
	st := renderer.DefaultTheme().Style(renderer.DefaultTheme().Text)
	lines := wrapSpans([]Span{{Text: "a\tb", Style: st}}, 20)
	if len(lines) != 1 || len(lines[0]) != 1 || lines[0][0].Text != "a    b" {
		t.Fatalf("tab expansion got %#v", lines)
	}
}
