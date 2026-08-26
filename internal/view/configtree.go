package view

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// ConfigTree 绘制服务端配置编辑器（/server 弹窗内容）。
// 采用子页面导航：每次只展示当前页面（一个对象/数组）的直接子项；
// 键名优先显示翻译后的中文名，未收录的显示服务端硬编码的原始字段名。
type ConfigTree struct {
	Theme renderer.Theme
	Tree  *model.ConfigEditor
}

// Draw 实现 widget.Widget 接口，绘制配置树。
func (ct *ConfigTree) Draw(c *renderer.Canvas, r renderer.Rect) {
	ct.draw(c, r, ct.Tree)
}

// draw 在内容区 r 内绘制配置编辑器：顶部面包屑路径、中部当前页面列表、
// 底部编辑输入框或操作提示。ed 为 nil 时显示加载中。
func (ct *ConfigTree) draw(c *renderer.Canvas, r renderer.Rect, ed *model.ConfigEditor) {
	t := ct.Theme
	if ed == nil {
		c.PutText(r.X, r.Y, i18n.T("正在获取服务端配置…"), t.Style(t.TextMuted))
		return
	}
	y := r.Y

	// 面包屑：展示从根到当前页面的路径（翻译后的显示名）。
	c.PutText(r.X, y, renderer.Truncate(strings.Join(ed.Crumb(), " / "), r.W), t.Style(t.TextMuted))
	y++

	// 当前页面列表：底部保留两行（编辑输入框 + 操作提示）。选中项保持可见：
	// 先尝试居中，越界时收拢到边界。可新增的集合页末尾有「(新增)」行。
	visible := r.H - (y - r.Y) - 2
	if visible < 1 {
		visible = 1
	}
	rows := ed.CurrentChildren()
	addIdx := ed.AddRowIndex()
	delIdx := ed.DelRowIndex()
	count := ed.RowCount()
	start := 0
	if count > 0 {
		start = ed.Selected - visible/2
		if start < 0 {
			start = 0
		}
		if maxStart := count - visible; start > maxStart {
			start = maxStart
		}
		if start < 0 {
			start = 0
		}
	}
	for i := start; i < start+visible && i < count; i++ {
		if y >= r.Y+r.H {
			break
		}
		st := t.Style(t.Text)
		marker := "  "
		selected := i == ed.Selected
		if selected {
			marker = "❯ "
			st = t.Style(t.Primary).WithBold(true)
		}
		var line string
		preview := ""
		switch {
		case i < len(rows):
			line = marker + ct.rowText(rows[i])
			// Model.Models 集合页行尾以灰色显示 ModelName 预览，便于辨认模型。
			if pv, ok := rows[i].ModelPreview(); ok {
				preview = pv
			}
		case i == addIdx:
			line = marker + i18n.T("(新增)")
			st = t.Style(t.TextMuted)
			if selected {
				st = st.WithBold(true)
			}
		case i == delIdx:
			line = marker + i18n.T("(删除该项)")
			st = t.Style(t.Error)
			if selected {
				st = st.WithBold(true)
			}
		default:
			continue
		}
		if preview != "" {
			// 主体用当前样式，预览接在主体后一个空格、灰色；主体本身
			// 已按行宽截断，预览放不下时自动跳过。
			c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
			if w := renderer.StringWidth(line); w < r.W {
				c.PutText(r.X+w+1, y, renderer.Truncate(preview, r.W-w-1), t.Style(t.TextMuted))
			}
		} else {
			c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
		}
		y++
	}

	// 底部：编辑/新增键输入框，或操作提示。
	bottomY := r.Y + r.H - 1
	if bottomY < r.Y || bottomY >= r.Y+r.H {
		return
	}
	inputY := bottomY - 1
	if ed.Saving {
		// 写回/全量重载进行中：阻塞改动，底部提示等待服务端确认。
		c.PutText(r.X, bottomY, i18n.T("保存中…  等待服务端确认后刷新"), t.Style(t.TextMuted))
		return
	}
	if ed.Editing {
		prompt := i18n.T("值: ")
		if ed.EditNode != nil {
			prompt = i18n.T("编辑 %s: ", ed.EditNode.DisplayKey())
		}
		if inputY >= r.Y {
			ib := &widget.InputBox{Buf: ed.EditInput, Prompt: prompt, Style: t.Style(t.Text), Cursor: t.StyleOn(t.Background, t.Primary), Focused: true}
			ib.Draw(c, renderer.NewRect(r.X, inputY, r.W, 1))
		}
		c.PutText(r.X, bottomY, i18n.T("Esc 取消    Enter 保存"), t.Style(t.TextMuted))
		return
	}
	if ed.AddingKey {
		if inputY >= r.Y {
			ib := &widget.InputBox{Buf: ed.AddInput, Prompt: i18n.T("新键名: "), Style: t.Style(t.Text), Cursor: t.StyleOn(t.Background, t.Primary), Focused: true}
			ib.Draw(c, renderer.NewRect(r.X, inputY, r.W, 1))
		}
		c.PutText(r.X, bottomY, i18n.T("Esc 取消    Enter 添加键"), t.Style(t.TextMuted))
		return
	}
	c.PutText(r.X, bottomY, i18n.T("Esc 关闭   ↑↓ 选择   Enter 进入/编辑   ← 返回   r 刷新"), t.Style(t.TextMuted))
}

// rowText 渲染当前页面的一行：对象/数组显示键名与箭头指示可进入，
// 标量显示键名、等号与当前值（敏感字段脱敏并标注）。
func (ct *ConfigTree) rowText(n *model.ConfigNode) string {
	name := n.DisplayKey()
	switch n.Kind {
	case model.ConfigObject, model.ConfigArray:
		return name + "  ▸"
	default:
		line := name + " = " + n.ValueText()
		if n.Sensitive {
			line += i18n.T(" (敏感)")
		}
		return line
	}
}
