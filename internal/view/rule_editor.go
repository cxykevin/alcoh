package view

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

const (
	// ruleDocMinWidth 是显示右侧文档栏所需的最小面板宽度。窄终端只保留
	// 编辑器本体，文档栏直接隐藏，避免把代码区挤到无法阅读。
	ruleDocMinWidth = 100
	// ruleDocWidth 是文档栏的目标宽度（实际还会受面板宽度 1/3 约束）。
	ruleDocWidth = 36
	// ruleTabWidth 是编辑器中 tab 的显示宽度。
	ruleTabWidth = 4
)

// RuleEditor 绘制规则表达式的全屏 IDE：行号、语法高亮、硬件光标与右侧
// 表达式文档栏。编辑缓冲由 model.ConfigEditor 提供，视图只负责呈现。
type RuleEditor struct {
	Theme renderer.Theme
	Ed    *model.ConfigEditor
}

// Draw 实现 widget.Widget 接口。
func (re *RuleEditor) Draw(c *renderer.Canvas, r renderer.Rect) {
	if re.Ed == nil || re.Ed.EditInput == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	t := re.Theme
	buf := re.Ed.EditInput
	c.Fill(r, renderer.CellSpace(renderer.DefaultStyle()))

	title := i18n.T("规则编辑器")
	if re.Ed.EditNode != nil {
		title = i18n.T("规则编辑器 · %s", re.Ed.EditNode.DisplayKey())
	}
	c.PutText(r.X, r.Y, renderer.Truncate(title, r.W), t.Style(t.Primary).WithBold(true))
	pos := i18n.T("行 %d · 列 %d", buf.CY+1, buf.CX+1)
	if pw := renderer.StringWidth(pos); r.W > pw+1 {
		c.PutText(r.X+r.W-pw, r.Y, pos, t.Style(t.TextMuted))
	}
	if r.H < 3 {
		return
	}
	// 顶栏与正文之间的分隔线。
	for x := r.X; x < r.X+r.W; x++ {
		c.Put(x, r.Y+1, renderer.CellRune('─', t.Style(t.BorderSubtle)))
	}

	bodyY := r.Y + 2
	footerY := r.Y + r.H - 1
	bodyH := footerY - bodyY
	if bodyH < 0 {
		bodyH = 0
	}

	// 右侧文档栏：窄屏隐藏（ruleDocMinWidth 以下），并保证代码区至少
	// 还有 48 列可用。
	sidebar := 0
	if r.W >= ruleDocMinWidth {
		w := min(ruleDocWidth, r.W/3)
		if r.W-w-1 >= 48 {
			sidebar = w
		}
	}
	editorW := r.W
	if sidebar > 0 {
		editorW = r.W - sidebar - 1
	}

	lines := buf.Lines
	if len(lines) == 0 {
		lines = [][]rune{{}}
	}
	digits := max(len(strconv.Itoa(len(lines))), 2)
	gutterW := 1 + digits + 3 // "  1 │ "
	codeX := r.X + gutterW
	codeW := editorW - gutterW
	if codeW < 1 {
		codeW = 1
	}

	cursorCol := renderer.StringWidth(ruleExpandPrefix(lines[buf.CY], buf.CX))
	hoff := 0
	if cursorCol >= codeW {
		hoff = cursorCol - codeW + 1
	}
	start := 0
	if bodyH > 0 && buf.CY >= bodyH {
		start = buf.CY - bodyH + 1
	}

	diag := re.Ed.EditDiag
	currentBg := t.PanelBg
	for i := start; i < len(lines) && i < start+bodyH; i++ {
		y := bodyY + (i - start)
		lineNoStyle := t.Style(t.TextMuted).WithDim(true)
		bg := renderer.ColorDefault
		if i == buf.CY {
			lineNoStyle = t.Style(t.Primary).WithBold(true)
			bg = currentBg
			// 当前行整行铺底，形成 IDE 的光标行提示。
			c.Fill(renderer.NewRect(r.X, y, editorW, 1), renderer.CellSpace(t.StyleOn(t.Text, bg)))
		}
		// 诊断所在行的行号与分隔线标红，具体原因显示在底栏。
		barStyle := t.Style(t.BorderSubtle).WithBg(bg)
		if diag != nil && i == diag.Line-1 {
			lineNoStyle = t.Style(t.Error).WithBold(true)
			barStyle = t.Style(t.Error).WithBg(bg)
		}
		lineNoStyle.Bg = bg
		num := fmt.Sprintf("%*d", digits, i+1)
		c.PutText(r.X+1, y, num, lineNoStyle)
		c.PutText(r.X+1+digits+1, y, "│", barStyle)
		drawRuleSpans(c, codeX, y, codeW, hoff, exprSpans(ruleExpandLine(lines[i]), t), bg)
	}

	if bodyH > 0 && buf.CY >= start && buf.CY < start+bodyH {
		cx := codeX + cursorCol - hoff
		if cx < codeX {
			cx = codeX
		}
		if maxX := codeX + codeW - 1; cx > maxX {
			cx = maxX
		}
		row := bodyY + (buf.CY - start)
		c.SetInputCursor(cx, row)
		// 部分终端不显示（或很淡地显示）硬件光标：与主输入框一致，在
		// 光标处再画一个块状软光标，位置始终可见。
		drawRuleCursor(c, buf, codeX, codeW, row, cx, t)
	}

	// 补全下拉框：优先画在光标行下方；下方放不下且上方更宽裕时翻到上方。
	// 弹层遮住正文，最后绘制（在补全框范围内盖住高亮代码）。
	if re.Ed.RuleCompleteOpen && bodyH > 0 && buf.CY >= start && buf.CY < start+bodyH {
		row := bodyY + (buf.CY - start)
		below := bodyY + bodyH - (row + 1)
		above := row - bodyY
		h := min(len(re.Ed.RuleCompleteItems), 8)
		if below >= 1 || above >= 1 {
			if below < 1 || (h > below && above > below) {
				h = min(h, above)
				if h > 0 {
					drawRuleComplete(c, re.Ed, codeX+cursorCol-hoff, codeX, codeX+codeW, row-h, h, t)
				}
			} else {
				h = min(h, below)
				if h > 0 {
					drawRuleComplete(c, re.Ed, codeX+cursorCol-hoff, codeX, codeX+codeW, row+1, h, t)
				}
			}
		}
	}

	if sidebar > 0 {
		sx := r.X + editorW + 1
		for y := bodyY; y < bodyY+bodyH; y++ {
			c.Put(sx-1, y, renderer.CellRune('│', t.Style(t.BorderSubtle)))
		}
		drawRuleDoc(c, sx, bodyY, sidebar, bodyH, t)
	}

	// 底栏平时是按键提示；有编译诊断时让位给错误信息（行号已同步标红），
	// 按 Ctrl+S 后切换为"仍要保存"确认提示。
	footer := i18n.T("Ctrl+S 保存    Esc 取消    Enter 换行    Tab 缩进")
	footerStyle := t.Style(t.TextMuted)
	switch {
	case re.Ed.RuleSaveConfirm:
		line := 1
		if diag != nil {
			line = diag.Line
		}
		footer = i18n.T("⚠ 第 %d 行有编译错误：Enter 仍要保存 · Esc 继续编辑", line)
		footerStyle = t.Style(t.Warning)
	case re.Ed.RuleCompleteOpen:
		footer = i18n.T("↑↓ 选择    Enter/Tab 补全    Esc 关闭")
		footerStyle = t.Style(t.TextMuted)
	case diag != nil:
		footer = i18n.T("⚠ 第 %d 行 · 列 %d: %s", diag.Line, diag.Column, diag.Message)
		footerStyle = t.Style(t.Error)
	}
	c.PutText(r.X, footerY, renderer.Truncate(footer, r.W), footerStyle)
}

// drawRuleCursor 在光标处绘制块状软光标：光标下的字符（行尾/制表符时为
// 空格）用主色背景反显。硬件光标仍由帧尾定位，两者位置一致；部分终端
// 不显示硬件光标或样式很淡时，这个色块保证插入点始终可见。
func drawRuleCursor(c *renderer.Canvas, buf *widget.InputBuffer, codeX, codeW, row, cx int, t renderer.Theme) {
	if buf.CY < 0 || buf.CY >= len(buf.Lines) {
		return
	}
	r := ' '
	if line := buf.Lines[buf.CY]; buf.CX >= 0 && buf.CX < len(line) && line[buf.CX] != '\t' {
		r = line[buf.CX]
	}
	// 宽字符放不下时退回一个空格块，避免把续列画到编辑区外。
	if w := renderer.RuneWidth(r); w > 1 && cx+w > codeX+codeW {
		r = ' '
	}
	c.Put(cx, row, renderer.CellRune(r, t.StyleOn(t.Background, t.Primary)))
}

// drawRuleComplete 绘制补全下拉框：整块填充弹层背景色遮住下方代码，
// 选中行用主题色整行高亮。x 是期望的左边（跟随光标），最终夹在
// [minX,maxX) 内，保证不越出编辑区。
func drawRuleComplete(c *renderer.Canvas, ed *model.ConfigEditor, x, minX, maxX, y, h int, t renderer.Theme) {
	items := ed.RuleCompleteItems
	if h <= 0 || len(items) == 0 || maxX-minX < 4 {
		return
	}
	nameW, detailW := 0, 0
	for _, it := range items {
		nameW = max(nameW, renderer.StringWidth(it.Insert))
		if it.Detail != "" {
			detailW = max(detailW, renderer.StringWidth(it.Detail))
		}
	}
	w := 2 + nameW + 1
	if detailW > 0 {
		w += 2 + detailW
	}
	if w < 16 {
		w = 16
	}
	if w > maxX-minX {
		w = maxX - minX
	}
	if x+w > maxX {
		x = maxX - w
	}
	if x < minX {
		x = minX
	}
	c.Fill(renderer.NewRect(x, y, w, h), renderer.CellSpace(t.StyleOn(t.Text, t.PopupBg)))

	sel := ed.RuleCompleteSel
	if sel < 0 || sel >= len(items) {
		sel = 0
	}
	startIdx := 0
	if sel >= h {
		startIdx = sel - h + 1
	}
	endIdx := min(startIdx+h, len(items))
	detailX := min(2+nameW+2, w)
	for i := startIdx; i < endIdx; i++ {
		row := y + i - startIdx
		st := t.StyleOn(t.Text, t.PopupBg)
		if i == sel {
			st = t.StyleOn(t.Background, t.Primary).WithBold(true)
			// 选中行整行铺主色底，和 nvim 的补全菜单一致。
			c.Fill(renderer.NewRect(x, row, w, 1), renderer.CellSpace(t.StyleOn(t.Background, t.Primary)))
		}
		c.PutText(x, row, renderer.Truncate("  "+items[i].Insert, w), st)
		if items[i].Detail != "" && detailX < w {
			dst := t.StyleOn(t.TextMuted, t.PopupBg)
			if i == sel {
				dst = st
			}
			c.PutText(x+detailX, row, renderer.Truncate(items[i].Detail, w-detailX), dst)
		}
	}
}

// ruleExpandLine 把一行 rune 展开为显示文本：tab 按固定宽度换成空格，
// 与行号/光标的列宽计算保持一致。
func ruleExpandLine(line []rune) string {
	var b strings.Builder
	for _, r := range line {
		if r == '\t' {
			b.WriteString(strings.Repeat(" ", ruleTabWidth))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ruleExpandPrefix 只展开光标前的文本，用于计算光标的显示列。
func ruleExpandPrefix(line []rune, cx int) string {
	if cx > len(line) {
		cx = len(line)
	}
	if cx < 0 {
		cx = 0
	}
	return ruleExpandLine(line[:cx])
}

// drawRuleSpans 在 (x,y) 起绘制一行高亮文本，先跳过 skip 列（水平滚动），
// 最多绘制 width 列。bg 非默认色时用于当前行底色（含行尾空白）。
func drawRuleSpans(c *renderer.Canvas, x, y, width, skip int, spans []Span, bg renderer.Color) {
	col := 0
	left := width
	for _, sp := range spans {
		st := sp.Style
		st.Bg = bg
		for _, rr := range sp.Text {
			rw := renderer.RuneWidth(rr)
			if rw <= 0 {
				continue
			}
			if col+rw <= skip {
				col += rw
				continue
			}
			if col < skip {
				// 跳过位置落在宽字符中间：用空格补齐剩余列。
				pad := col + rw - skip
				if pad > left {
					pad = left
				}
				if pad > 0 {
					c.PutText(x, y, strings.Repeat(" ", pad), st)
					x += pad
					left -= pad
				}
				col += rw
				continue
			}
			if left < rw {
				return
			}
			c.PutText(x, y, string(rr), st)
			x += rw
			left -= rw
			col += rw
		}
	}
}

// drawRuleDoc 绘制右侧表达式文档栏：变量、可用函数、运算符与注释语法。
func drawRuleDoc(c *renderer.Canvas, x, y, w, h int, t renderer.Theme) {
	for i, spans := range ruleDocLines(t) {
		if i >= h {
			return
		}
		drawSpansLine(c, x, y+i, w, spans)
	}
}

// drawSpansLine 按列宽裁剪并绘制一段多样式文本。
func drawSpansLine(c *renderer.Canvas, x, y, w int, spans []Span) {
	for _, sp := range spans {
		if w <= 0 {
			return
		}
		text := renderer.Truncate(sp.Text, w)
		c.PutText(x, y, text, sp.Style)
		sw := renderer.StringWidth(text)
		x += sw
		w -= sw
		if sw < renderer.StringWidth(sp.Text) {
			return
		}
	}
}

// ruleDocLines 返回文档栏内容。描述以 alkaid0 源码中的
// provider/request/request.go 为准（README 的示例有偏差）。
func ruleDocLines(t renderer.Theme) [][]Span {
	section := t.Style(t.Primary).WithBold(true)
	dim := t.Style(t.TextMuted)
	entry := func(name, desc string) []Span {
		spans := []Span{{Text: name, Style: t.Style(t.Info).WithBold(true)}}
		if desc != "" {
			spans = append(spans, Span{Text: "  " + desc, Style: dim})
		}
		return spans
	}
	return [][]Span{
		{{Text: i18n.T("表达式文档"), Style: t.Style(t.Text).WithBold(true)}},
		{{Text: i18n.T("Reject 优先；Approve 需全部命中"), Style: t.Style(t.Warning)}},
		{{Text: "", Style: dim}},
		{{Text: i18n.T("变量"), Style: section}},
		entry("ToolCalls", i18n.T("全部待审批调用")),
		entry("ToolCall", i18n.T("当前工具调用")),
		entry("  Name", i18n.T("工具名")),
		entry("  ID", i18n.T("调用 ID")),
		entry("  Parameters", i18n.T("参数表")),
		entry("Agent", i18n.T("当前 Agent 配置")),
		entry("Workdir", i18n.T("当前工作目录")),
		{{Text: "", Style: dim}},
		{{Text: i18n.T("函数"), Style: section}},
		entry("truthy(v)", i18n.T("按 Go 语义转布尔")),
		entry("regex(p, s)", i18n.T("正则匹配，p 在前")),
		entry("contains(s, sub)", i18n.T("包含子串")),
		entry("hasParam(call, key)", i18n.T("参数是否存在")),
		entry("param(call, key)", i18n.T("取参数值")),
		{{Text: "", Style: dim}},
		{{Text: i18n.T("运算符"), Style: section}},
		entry("&&  ||  !", i18n.T("逻辑")),
		entry("==  !=  <  >", i18n.T("比较")),
		entry("//  /* */", i18n.T("注释")),
	}
}

// ruleKeywords 是 expr-lang 的关键字。
var ruleKeywords = map[string]bool{
	"let": true, "in": true, "not": true, "and": true, "or": true,
	"true": true, "false": true, "nil": true,
}

// ruleVariables 是 alkaid0 注入表达式环境的变量名。
var ruleVariables = map[string]bool{
	"ToolCalls": true, "ToolCall": true, "Agent": true, "Workdir": true,
}

// exprSpans 把一行 expr-lang 表达式切成带样式的片段：字符串、数字、注释、
// 关键字、变量、函数调用与运算符分别着色，其余按普通文本处理。
func exprSpans(text string, t renderer.Theme) []Span {
	rs := []rune(text)
	var spans []Span
	add := func(s string, st renderer.Style) {
		if s == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Style == st {
			spans[len(spans)-1].Text += s
			return
		}
		spans = append(spans, Span{Text: s, Style: st})
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			add(string(r), t.Style(t.Text))
			i++
		case r == '/' && i+1 < len(rs) && rs[i+1] == '/':
			add(string(rs[i:]), t.Style(t.TextMuted).WithItalic(true))
			i = len(rs)
		case r == '/' && i+1 < len(rs) && rs[i+1] == '*':
			j := i + 2
			for j+1 < len(rs) && !(rs[j] == '*' && rs[j+1] == '/') {
				j++
			}
			end := j + 2
			if end > len(rs) {
				end = len(rs)
			}
			add(string(rs[i:end]), t.Style(t.TextMuted).WithItalic(true))
			i = end
		case r == '"' || r == '\'' || r == '`':
			j := i + 1
			for j < len(rs) {
				if rs[j] == '\\' && r != '`' {
					j += 2
					continue
				}
				if rs[j] == r {
					j++
					break
				}
				j++
			}
			if j > len(rs) {
				j = len(rs)
			}
			add(string(rs[i:j]), t.Style(t.Accent))
			i = j
		case unicode.IsDigit(r):
			j := i + 1
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			if j < len(rs) && rs[j] == '.' {
				j++
				for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '_') {
					j++
				}
			}
			add(string(rs[i:j]), t.Style(t.Warning))
			i = j
		case ruleIdentStart(r):
			j := i + 1
			for j < len(rs) && ruleIdentPart(rs[j]) {
				j++
			}
			word := string(rs[i:j])
			st := t.Style(t.Text)
			switch {
			case ruleKeywords[word]:
				st = t.Style(t.Primary).WithBold(true)
			case ruleVariables[word]:
				st = t.Style(t.Info).WithBold(true)
			case ruleAfterDot(rs, i):
				st = t.Style(t.Text)
			case ruleFunctionCall(rs, j):
				st = t.Style(t.Secondary).WithBold(true)
			}
			add(word, st)
			i = j
		case strings.ContainsRune("(),[]{}", r):
			add(string(r), t.Style(t.TextMuted))
			i++
		case strings.ContainsRune("+-*/%<>=!&|?:^", r):
			add(string(r), t.Style(t.Primary))
			i++
		default:
			add(string(r), t.Style(t.Text))
			i++
		}
	}
	return spans
}

func ruleIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func ruleIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ruleAfterDot 报告 i 之前最近的非空白字符是否为 '.'，用于把字段访问
// （ToolCall.Name）与变量/函数区分开。
func ruleAfterDot(rs []rune, i int) bool {
	for j := i - 1; j >= 0; j-- {
		if unicode.IsSpace(rs[j]) {
			continue
		}
		return rs[j] == '.'
	}
	return false
}

// ruleFunctionCall 报告标识符之后（跳过空白）是否紧跟 '('。
func ruleFunctionCall(rs []rune, j int) bool {
	for ; j < len(rs); j++ {
		if unicode.IsSpace(rs[j]) {
			continue
		}
		return rs[j] == '('
	}
	return false
}
