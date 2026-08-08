package view

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/renderer"
)

// Span 是带样式的一段文本。
type Span struct {
	Text  string
	Style renderer.Style
}

// StyledLine 是一行 span 序列（渲染时不再换行）。
// Src 记录该渲染行对应的原始 markdown 文本（含 `#`/`**`/“ ` “ 等标记符），
// 供鼠标选择时从原始文本精确截取，而不复制渲染后的结果。
type StyledLine struct {
	Spans []Span
	Src   string
}

// Markdown 把纯文本渲染为带样式的行（简易 markdown 着色）。
// 支持：行首 #标题 / >引用 / -列表 / ```代码块；行内 **粗体** *斜体* `代码` [链接](url)。
func Markdown(text string, t renderer.Theme) []StyledLine {
	var lines []StyledLine
	inCode := false
	codeLang := ""
	codeLines := []string{}
	flushCode := func() {
		hl := highlightCode(codeLang, codeLines, t)
		for i := range hl {
			if i < len(codeLines) {
				hl[i].Src = codeLines[i] // 插针：代码行对应原始行
			}
		}
		lines = append(lines, hl...)
		codeLines = nil
		codeLang = ""
	}
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimRight(raw, " \t")
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				inCode = false
				flushCode()
			} else {
				inCode = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			}
			continue
		}
		if inCode {
			codeLines = append(codeLines, raw)
			continue
		}
		line := markdownLine(trimmed, t)
		line.Src = trimmed // 插针：渲染行对应原始逻辑行
		lines = append(lines, line)
	}
	if inCode {
		flushCode()
	}
	if len(lines) == 0 {
		lines = []StyledLine{{Spans: []Span{{Text: "", Style: t.Style(t.Text)}}}}
	}
	return lines
}

func markdownLine(line string, t renderer.Theme) StyledLine {
	base := t.Style(t.Text)
	var spans []Span

	switch {
	case strings.HasPrefix(line, "# "):
		spans = append(spans, Span{Text: strings.TrimPrefix(line, "# "), Style: t.Style(t.MDHeading).WithBold(true)})
		return StyledLine{Spans: spans}
	case strings.HasPrefix(line, "## "):
		spans = append(spans, Span{Text: strings.TrimPrefix(line, "## "), Style: t.Style(t.MDHeading).WithBold(true)})
		return StyledLine{Spans: spans}
	case strings.HasPrefix(line, "### "):
		spans = append(spans, Span{Text: strings.TrimPrefix(line, "### "), Style: t.Style(t.MDHeading)})
		return StyledLine{Spans: spans}
	case strings.HasPrefix(line, "> "):
		content := strings.TrimPrefix(line, "> ")
		return StyledLine{Spans: inlineStyle(content, t.Style(t.MDBlockquote).WithItalic(true), t)}
	case strings.HasPrefix(line, "- "):
		content := strings.TrimPrefix(line, "- ")
		marker := Span{Text: "• ", Style: t.Style(t.MDList)}
		return StyledLine{Spans: append([]Span{marker}, inlineStyle(content, base, t)...)}
	case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "***"):
		return StyledLine{Spans: []Span{{Text: strings.Repeat("─", 40), Style: t.Style(t.MDList)}}}
	}
	return StyledLine{Spans: inlineStyle(line, base, t)}
}

// inlineStyle 解析行内标记：**bold**、*italic*、`code`、[text](url)。
func inlineStyle(s string, base renderer.Style, t renderer.Theme) []Span {
	var spans []Span
	var buf strings.Builder
	flush := func(st renderer.Style) {
		if buf.Len() > 0 {
			spans = append(spans, Span{Text: buf.String(), Style: st})
			buf.Reset()
		}
	}
	cur := base
	inCode := false
	var prev renderer.Style
	rs := []rune(s)
	for i := 0; i < len(rs); {
		switch {
		case i+1 < len(rs) && rs[i] == '*' && rs[i+1] == '*':
			flush(cur)
			cur = cur.WithBold(!cur.Bold)
			i += 2
		case rs[i] == '*' && (i == 0 || i == len(rs)-1 || !isRuneSpace(rs[i-1])):
			flush(cur)
			cur = cur.WithItalic(!cur.Italic)
			i++
		case rs[i] == '`':
			flush(cur)
			if inCode {
				cur = prev // 退出行内代码，恢复进入前的样式
			} else {
				prev = cur
				cur = cur.WithFg(t.MDCode)
			}
			inCode = !inCode
			i++
		case rs[i] == '[':
			// 尝试 [text](url)
			if end := strings.Index(string(rs[i:]), "]("); end >= 0 {
				rel := end + 2
				if closeIdx := strings.Index(string(rs[i+rel:]), ")"); closeIdx >= 0 {
					text := string(rs[i+1 : i+end])
					flush(cur)
					spans = append(spans, Span{Text: text, Style: t.Style(t.MDLink).WithUnderline(true)})
					i += rel + closeIdx + 1
					continue
				}
			}
			buf.WriteRune(rs[i])
			i++
		default:
			buf.WriteRune(rs[i])
			i++
		}
	}
	flush(cur)
	if len(spans) == 0 {
		spans = append(spans, Span{Text: "", Style: base})
	}
	return spans
}

func isRuneSpace(r rune) bool { return r == ' ' || r == '\t' }
