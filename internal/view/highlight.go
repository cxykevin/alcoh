package view

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/cxykevin/alcoh/internal/renderer"
)

// highlightCode 把 fenced code block 转换为带行号和语法样式的行。
// chroma 内置主流语言 lexer；无法识别时安全回退为纯文本代码样式。
func highlightCode(lang string, lines []string, t renderer.Theme) []StyledLine {
	if len(lines) == 0 {
		return nil
	}

	lexer := lexers.Get(strings.TrimSpace(lang))
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iter, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		iter, _ = chroma.Coalesce(lexers.Fallback).Tokenise(nil, strings.Join(lines, "\n"))
	}

	codeLines := make([][]Span, 1, len(lines))
	for token := iter(); token != chroma.EOF; token = iter() {
		style := highlightStyle(token.Type, t)
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if part != "" {
				codeLines[len(codeLines)-1] = appendSpan(codeLines[len(codeLines)-1], expandCodeTabs(part), style)
			}
			if i < len(parts)-1 {
				codeLines = append(codeLines, nil)
			}
		}
	}
	// Token 流以换行结束时会额外产生一行；只保留源代码真实行数。
	if len(codeLines) > len(lines) {
		codeLines = codeLines[:len(lines)]
	}
	for len(codeLines) < len(lines) {
		codeLines = append(codeLines, nil)
	}

	digits := len(fmt.Sprintf("%d", len(lines)))
	lineNoStyle := t.Style(t.TextMuted).WithDim(true)
	out := make([]StyledLine, 0, len(lines))
	for i, line := range codeLines {
		prefix := fmt.Sprintf("  %*d │ ", digits, i+1)
		spans := []Span{{Text: prefix, Style: lineNoStyle}}
		spans = append(spans, line...)
		out = append(out, StyledLine{Spans: spans})
	}
	return out
}

func appendSpan(spans []Span, text string, style renderer.Style) []Span {
	if text == "" {
		return spans
	}
	if len(spans) > 0 && spans[len(spans)-1].Style == style {
		spans[len(spans)-1].Text += text
		return spans
	}
	return append(spans, Span{Text: text, Style: style})
}

// expandCodeTabs 使用固定 4 个空格，避免终端 tab stop 差异导致代码列错位。
func expandCodeTabs(text string) string {
	return strings.ReplaceAll(text, "\t", "    ")
}

func highlightStyle(tt chroma.TokenType, t renderer.Theme) renderer.Style {
	// 代码只改变前景色与字形属性，背景保持终端默认色。
	// 这样高亮区不会形成额外色块，也与普通对话的视觉层级一致。
	base := t.Style(t.MDCode)
	switch {
	case tt.InCategory(chroma.Comment):
		return t.Style(t.TextMuted).WithItalic(true)
	case tt.InCategory(chroma.Keyword):
		return t.Style(t.Secondary).WithBold(true)
	case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
		return t.Style(t.Info)
	case tt == chroma.NameClass || tt == chroma.NameNamespace || tt == chroma.NameBuiltin:
		return t.Style(t.Accent)
	case tt.InCategory(chroma.LiteralString):
		return t.Style(t.Success)
	case tt.InCategory(chroma.LiteralNumber):
		return t.Style(t.Warning)
	case tt.InCategory(chroma.Operator) || tt == chroma.Punctuation:
		return t.Style(t.TextMuted)
	case tt == chroma.NameConstant || tt == chroma.KeywordConstant:
		return t.Style(t.Warning)
	default:
		return base
	}
}
