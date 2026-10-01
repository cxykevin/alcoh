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
// Map 与 Spans 拼接后的 rune 顺序一一对应，给出每个渲染 rune 在 Src 中的下标；
// -1 表示该 rune 是渲染合成、原文里没有（列表符号、分隔线、代码行号前缀等）。
// 部分框选时据此只取选中的字符：未渲染的标记符（`**`、`# ` 等）自然被排除，
// 复制结果不会出现落单的标记。
type StyledLine struct {
	Spans []Span
	Src   string
	Map   []int
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
				hl[i].Map = codeLineIdents(hl[i].Spans, codeLines[i])
			}
		}
		lines = append(lines, hl...)
		codeLines = nil
		codeLang = ""
	}
	for raw := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimRight(raw, " \t")
		if after, ok := strings.CutPrefix(trimmed, "```"); ok {
			if inCode {
				inCode = false
				flushCode()
			} else {
				inCode = true
				codeLang = strings.TrimSpace(after)
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

	switch {
	case strings.HasPrefix(line, "# "):
		return headingLine(line, "# ", t.Style(t.MDHeading).WithBold(true))
	case strings.HasPrefix(line, "## "):
		return headingLine(line, "## ", t.Style(t.MDHeading).WithBold(true))
	case strings.HasPrefix(line, "### "):
		return headingLine(line, "### ", t.Style(t.MDHeading))
	case strings.HasPrefix(line, "> "):
		spans, idents := inlineStyle(strings.TrimPrefix(line, "> "), t.Style(t.MDBlockquote).WithItalic(true), t)
		return StyledLine{Spans: spans, Map: shiftIdents(idents, len([]rune("> ")))}
	case strings.HasPrefix(line, "- "):
		spans, idents := inlineStyle(strings.TrimPrefix(line, "- "), base, t)
		marker := Span{Text: "• ", Style: t.Style(t.MDList)}
		return StyledLine{
			Spans: append([]Span{marker}, spans...),
			// 列表符号是渲染合成：占 2 格，原文里对应 "- "。
			Map: append(negIdents(len([]rune("• "))), shiftIdents(idents, len([]rune("- ")))...),
		}
	case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "***"):
		rule := strings.Repeat("─", 40)
		return StyledLine{Spans: []Span{{Text: rule, Style: t.Style(t.MDList)}}, Map: negIdents(len([]rune(rule)))}
	}
	spans, idents := inlineStyle(line, base, t)
	return StyledLine{Spans: spans, Map: idents}
}

// headingLine 生成标题渲染行：前缀标记（"# " 等）不渲染，源下标整体后移 marker 个。
func headingLine(line, marker string, style renderer.Style) StyledLine {
	content := strings.TrimPrefix(line, marker)
	return StyledLine{
		Spans: []Span{{Text: content, Style: style}},
		Map:   seqIdents(len([]rune(marker)), len([]rune(content))),
	}
}

// seqIdents 返回 n 个从 offset 起的连续源下标（渲染 rune 与原文逐字对应）。
func seqIdents(offset, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = offset + i
	}
	return out
}

// negIdents 返回 n 个 -1（渲染合成字符，原文里没有对应字符）。
func negIdents(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	return out
}

// shiftIdents 把源下标整体后移 d（跳过的标记前缀长度），-1 保持不变。
func shiftIdents(idents []int, d int) []int {
	if d == 0 {
		return idents
	}
	out := make([]int, len(idents))
	for i, id := range idents {
		if id < 0 {
			out[i] = -1
			continue
		}
		out[i] = id + d
	}
	return out
}

// inlineStyle 解析行内标记：**bold**、*italic*、`code`、[text](url)。
// 同时返回与渲染 rune 一一对应的源下标（见 StyledLine.Map）：标记符本身不渲染，
// 因此不出现在下标序列里——部分框选复制时它们自然被排除。
func inlineStyle(s string, base renderer.Style, t renderer.Theme) ([]Span, []int) {
	var spans []Span
	var idents []int
	var buf strings.Builder
	var bufIdents []int
	flush := func(st renderer.Style) {
		if buf.Len() > 0 {
			spans = append(spans, Span{Text: buf.String(), Style: st})
			idents = append(idents, bufIdents...)
			buf.Reset()
			bufIdents = bufIdents[:0]
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
					// 链接只渲染方括号里的文字：源下标逐字对应，"(url)" 不占渲染位。
					for k := range []rune(text) {
						idents = append(idents, i+1+k)
					}
					i += rel + closeIdx + 1
					continue
				}
			}
			buf.WriteRune(rs[i])
			bufIdents = append(bufIdents, i)
			i++
		default:
			buf.WriteRune(rs[i])
			bufIdents = append(bufIdents, i)
			i++
		}
	}
	flush(cur)
	if len(spans) == 0 {
		spans = append(spans, Span{Text: "", Style: base})
	}
	return spans, idents
}

func isRuneSpace(r rune) bool { return r == ' ' || r == '\t' }
