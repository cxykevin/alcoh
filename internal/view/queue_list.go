package view

// 输入框上方的排队消息列表（非模态）。存在排队项时始终显示；输入框里按 ↑
// 聚焦到列表，Tab/空格/i 编辑、Backspace/Delete 删除（见 app/keymap.go）。

import (
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// queueListMaxRows 是排队框最多显示几项（更多时窗口跟随选中项滚动）。
const queueListMaxRows = 5

// queueListHeight 返回排队框高度：分隔标题 1 行 + 可见排队项。没有排队项时为 0。
func queueListHeight(m *model.AppModel) int {
	n := len(m.QueueItems())
	if n == 0 {
		return 0
	}
	return 1 + min(n, queueListMaxRows)
}

// drawQueueList 绘制输入框上方的排队框：顶部是带操作提示的分隔行，下面按 FIFO
// 顺序列出排队项。聚焦时以 ❯ 标记选中项并保证它落在可见窗口内。
func (v *AppView) drawQueueList(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	items := m.QueueItems()
	if r.H <= 0 || len(items) == 0 {
		return
	}
	t := v.Theme
	// 顶部分隔线 + 标题/操作提示。
	sep := t.Style(t.BorderSubtle)
	for x := r.X; x < r.X+r.W; x++ {
		c.Put(x, r.Y, renderer.CellRune('─', sep))
	}
	label := i18n.T("排队消息 %d", len(items))
	if m.QueueFocused {
		label = i18n.T("%d 条排队消息  ↑↓ 选择  Tab/空格/i 编辑  ⌫ 删除  Esc 返回", len(items))
	}
	c.PutText(r.X+1, r.Y, renderer.Truncate(label, max(r.W-2, 0)), t.Style(t.TextMuted))

	visible := min(len(items), queueListMaxRows)
	start := 0
	if m.QueueFocused {
		// 选中项尽量居中；越界时收拢到两端。
		start = max(m.QueueSelect-visible/2, 0)
		if maxStart := len(items) - visible; start > maxStart {
			start = maxStart
		}
		if start < 0 {
			start = 0
		}
	}
	for i := start; i < start+visible && i < len(items); i++ {
		y := r.Y + 1 + (i - start)
		if y >= r.Y+r.H {
			break
		}
		marker := "  "
		st := t.Style(t.TextMuted)
		if m.QueueFocused && i == m.QueueSelect {
			marker = "❯ "
			st = t.Style(t.Primary).WithBold(true)
		}
		line := marker + itoa(i+1) + ". " + queueItemLine(items[i])
		c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
	}
}
