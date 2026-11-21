package view

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// AtPanel 绘制输入框上方的 @ 文件引用选择框：展示当前目录的条目，按输入的
// 前缀过滤，↑↓ 移动、Tab/Enter 补全（见 model/at_complete.go）。屏幕足够宽
// 且选中的是文件时，右侧分栏显示该文件前 1024 字节的代码预览。
type AtPanel struct{ Theme renderer.Theme }

// atPreviewMinWidth 是显示文件预览分栏所需的最小面板宽度（列）。会话视图的
// 输入框在右侧面板里（约终端宽度的 60%），取 70 让 120 列终端也能看到预览；
// 更窄时列表占满整行，避免分栏把文件名挤成几个字。
const atPreviewMinWidth = 70

// atPreviewMinCols 是预览分栏至少占用的列数。
const atPreviewMinCols = 24

// atPanelHeight 返回 @ 选择框占用的行数：打开时固定占满 AtPanelRows 行。
// 预览分栏跟着面板高度走，按条目数收缩会让只含一两个文件的目录只能预览
// 一两行；内容不足时下方留空，切换目录/选中项时面板高度也不跳动。极矮终端
// 的裁剪由调用方布局负责（见 view.go 的 atH 计算）。
func atPanelHeight(m *model.AppModel) int {
	if !m.AtOpen {
		return 0
	}
	return model.AtPanelRows
}

// atPanelSplit 返回列表分栏宽度，以及是否显示右侧预览（宽度不足或选中的
// 是目录时不显示预览，列表占满整行）。
func atPanelSplit(m *model.AppModel, width int) (listW int, preview bool) {
	listW = width
	if width < atPreviewMinWidth {
		return listW, false
	}
	if _, ok := m.AtSelectedPath(); !ok {
		return listW, false
	}
	listW = max(width*55/100, 30)
	if maxList := width - atPreviewMinCols - 1; listW > maxList {
		listW = maxList
	}
	if listW < 1 {
		listW = 1
	}
	return listW, true
}

func (p *AtPanel) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	if !m.AtOpen || r.H <= 0 || r.W <= 0 {
		return
	}
	// 整个选择框（列表 + 预览）使用弹窗灰色背景，与正文区分。
	bg := p.Theme.PopupBg
	c.Fill(r, renderer.CellRune(' ', renderer.Style{Bg: bg}))

	listW, preview := atPanelSplit(m, r.W)
	p.drawList(c, renderer.NewRect(r.X, r.Y, listW, r.H), m, bg)
	if !preview {
		return
	}
	sepStyle := p.Theme.StyleOn(p.Theme.BorderSubtle, bg)
	for y := r.Y; y < r.Y+r.H; y++ {
		c.Put(r.X+listW, y, renderer.CellRune('│', sepStyle))
	}
	p.drawPreview(c, renderer.NewRect(r.X+listW+1, r.Y, r.W-listW-1, r.H), m, bg)
}

func (p *AtPanel) drawList(c *renderer.Canvas, r renderer.Rect, m *model.AppModel, bg renderer.Color) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	muted := p.Theme.StyleOn(p.Theme.TextMuted, bg)
	if m.AtError != "" {
		c.PutText(r.X, r.Y, "  "+renderer.Truncate(m.AtError, r.W-2), p.Theme.StyleOn(p.Theme.Error, bg))
		return
	}
	if m.AtLoading {
		c.PutText(r.X, r.Y, "  "+i18n.T("加载中…"), muted)
		return
	}
	entries := m.AtFilteredEntries()
	if len(entries) == 0 {
		c.PutText(r.X, r.Y, "  "+i18n.T("无匹配文件"), muted)
		return
	}
	visible := min(len(entries), r.H, model.AtPanelRows)
	start := max(m.AtSelected-visible/2, 0)
	if maxStart := len(entries) - visible; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+visible; i++ {
		entry := entries[i]
		st := muted
		prefix := "  "
		if i == m.AtSelected {
			prefix, st = "❯ ", p.Theme.StyleOn(p.Theme.Primary, bg).WithBold(true)
		}
		name := entry.Name
		if entry.IsDir {
			name += "/"
		}
		c.PutText(r.X, r.Y+i-start, prefix+renderer.Truncate(name, r.W-2), st)
	}
}

// drawPreview 绘制选中文件的前几行高亮预览。选中项与已加载预览不一致时
// 显示加载提示，避免短暂显示上一个文件的内容。
func (p *AtPanel) drawPreview(c *renderer.Canvas, r renderer.Rect, m *model.AppModel, bg renderer.Color) {
	path, ok := m.AtSelectedPath()
	if !ok || r.W <= 0 || r.H <= 0 {
		return
	}
	muted := p.Theme.StyleOn(p.Theme.TextMuted, bg)
	if m.AtPreviewErr != "" {
		c.PutText(r.X, r.Y, renderer.Truncate(m.AtPreviewErr, r.W), p.Theme.StyleOn(p.Theme.Error, bg))
		return
	}
	if m.AtPreviewLoading || m.AtPreviewPath != path {
		c.PutText(r.X, r.Y, i18n.T("加载中…"), muted)
		return
	}
	lines := previewLines(m.AtPreview)
	if len(lines) == 0 {
		c.PutText(r.X, r.Y, i18n.T("（空文件）"), muted)
		return
	}
	visible := min(len(lines), r.H)
	styled := highlightFile(path, lines[:visible], p.Theme)
	for i, line := range styled {
		p.drawStyledLine(c, r.X, r.Y+i, line, r.W, bg)
	}
}

// previewLines 按行拆分预览文本；末尾换行不产生多余空行。
func previewLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// drawStyledLine 在预览分栏内绘制一行高亮代码：保留语法前景色，背景统一为
// 弹窗灰色，按列宽截断（宽字符不切半）。
func (p *AtPanel) drawStyledLine(c *renderer.Canvas, x, y int, line StyledLine, width int, bg renderer.Color) {
	limit := x + width
	for _, span := range line.Spans {
		if x >= limit {
			return
		}
		text := renderer.Truncate(span.Text, limit-x)
		if text == "" {
			continue
		}
		style := span.Style
		style.Bg = bg
		c.PutText(x, y, text, style)
		x += renderer.StringWidth(text)
	}
}
