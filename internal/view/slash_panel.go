package view

import (
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// SlashPanel 绘制输入框上方的命令建议列表。
type SlashPanel struct{ Theme renderer.Theme }

const slashVisibleCommands = 8

func (p *SlashPanel) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	if !m.SlashOpen || r.H <= 0 {
		return
	}
	// 面板是浮层：先整体填充默认背景空格，遮住下方内容（如首页 logo/欢迎语）
	// 的残留像素，避免命令列表文字叠在旧内容上；背景用终端默认色而非主题色，
	// 使面板与页面其它区域底色一致。
	c.Fill(r, renderer.CellRune(' ', renderer.Style{Bg: renderer.ColorDefault}))
	commands, indices := m.FilteredSlashCommands()
	if len(commands) == 0 {
		c.PutText(r.X, r.Y, "  无匹配命令", p.Theme.Style(p.Theme.TextMuted))
		return
	}
	descriptions := m.SlashCommandDescriptions()

	selected := 0
	for i, index := range indices {
		if index == m.SlashSelected {
			selected = i
			break
		}
	}
	visible := slashVisibleCommands
	if visible > len(commands) {
		visible = len(commands)
	}
	if visible > r.H {
		visible = r.H
	}
	if visible < 1 {
		return
	}
	start := selected - visible/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(commands) - visible; start > maxStart {
		start = maxStart
	}
	end := start + visible

	nameCol := 0
	for i := start; i < end; i++ {
		if w := renderer.StringWidth(commands[i]); w > nameCol {
			nameCol = w
		}
	}
	nameCol += 4

	for i := start; i < end; i++ {
		st := p.Theme.Style(p.Theme.TextMuted)
		prefix := "  "
		if indices[i] == m.SlashSelected {
			prefix, st = "❯ ", p.Theme.Style(p.Theme.Primary).WithBold(true)
		}
		row := r.Y + i - start
		name := commands[i]
		c.PutText(r.X, row, prefix+renderer.Truncate(name, r.W-2), st)
		desc := descriptions[indices[i]]
		if desc == "" {
			continue
		}
		descX := r.X + 2 + nameCol
		if descX >= r.X+r.W {
			continue
		}
		c.PutText(descX, row, renderer.Truncate(desc, r.X+r.W-descX), p.Theme.Style(p.Theme.TextMuted))
	}
}
