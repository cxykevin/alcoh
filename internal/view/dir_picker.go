package view

import (
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// dirPickerTwoColumnMinWidth 是目录选择器切换为双栏的宽度阈值：宽度 > 60
// 时左右两栏，否则只显示当前聚焦栏。
const dirPickerTwoColumnMinWidth = 60

// DirPickerContent 绘制 /cd 目录选择器：左栏是当前目录的严格上一级；根目录
// 没有上一级时左栏留空，常用目录与 Root 一起出现在右栏待选列表的最前面。
type DirPickerContent struct {
	Theme renderer.Theme
	State *model.DirPickerState
}

// dirPickerContent 构造 /cd 目录选择器内容。
func dirPickerContent(t renderer.Theme, m *model.AppModel) *DirPickerContent {
	return &DirPickerContent{Theme: t, State: m.DirPicker}
}

// Draw 绘制目录选择器内容区（sheet 已扣掉标题行）。
func (dc *DirPickerContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	if r.W <= 0 || r.H <= 0 || dc.State == nil {
		return
	}
	st := dc.State
	t := dc.Theme
	footerY := r.Y + r.H - 1

	editing := st.Editing != model.DirPickerIdle
	bodyBottom := footerY // 列表可用的最后一行（不含）
	if editing {
		bodyBottom = footerY - 1
	}
	bodyH := max(bodyBottom-r.Y, 0)

	if r.W > dirPickerTwoColumnMinWidth && bodyH > 0 {
		leftW := (r.W - 1) / 2
		rightX := r.X + leftW + 1
		rightW := r.W - leftW - 1
		// 逻辑根（最近使用）没有上一级：左栏整块留空。
		if !st.AtLogicalRoot() {
			dc.drawLeftPane(c, t, st, renderer.NewRect(r.X, r.Y, leftW, bodyH))
		}
		// 中间分隔线（只画列表区域，不压标题行）。
		for y := r.Y; y < bodyBottom; y++ {
			c.Put(rightX-1, y, renderer.CellRune('│', t.Style(t.Border)))
		}
		dc.drawRightPane(c, t, st, renderer.NewRect(rightX, r.Y, rightW, bodyH))
	} else if bodyH > 0 {
		if st.Focus == model.DirPickerLeft {
			dc.drawLeftPane(c, t, st, renderer.NewRect(r.X, r.Y, r.W, bodyH))
		} else {
			dc.drawRightPane(c, t, st, renderer.NewRect(r.X, r.Y, r.W, bodyH))
		}
	}

	if editing {
		label := i18n.T("新建文件夹（%s）：Enter 创建    Esc 取消", st.Dir)
		if st.Editing == model.DirPickerJump {
			label = i18n.T("跳转路径（绝对路径或相对当前目录）：Enter 确认    Esc 取消")
		}
		style := t.Style(t.TextMuted)
		if st.Err != "" {
			label = st.Err
			style = t.Style(t.Error)
		}
		if footerY-1 >= r.Y {
			c.PutText(r.X, footerY-1, renderer.Truncate(label, r.W), style)
		}
		if st.Input != nil {
			ib := &widget.InputBox{
				Buf:     st.Input,
				Prompt:  "> ",
				Style:   t.Style(t.Text),
				Cursor:  t.Style(t.Primary).WithBold(true),
				Focused: true,
			}
			ib.Draw(c, renderer.NewRect(r.X, footerY, r.W, 1))
		}
		return
	}

	hint := i18n.T("↑↓ 选择  Tab 切换  Enter 进入  ← 上级  s 选中  n 新建  / 跳转  Esc 关闭")
	if st.AtLogicalRoot() {
		// 逻辑根没有上一级、也不是真实目录：不提供切栏/选中/新建。
		hint = i18n.T("↑↓ 选择  Enter 进入  / 跳转  h 隐藏文件  Esc 关闭")
	} else if r.W <= dirPickerTwoColumnMinWidth {
		hint = i18n.T("↑↓ 选择  Tab 切换栏  Enter 进入  ← 上级  s 选中  n 新建  h 隐藏文件  Esc 关闭")
	}
	style := t.Style(t.TextMuted)
	if st.Err != "" {
		hint = st.Err
		style = t.Style(t.Error)
	}
	c.PutText(r.X, footerY, renderer.Truncate(hint, r.W), style)
}

func (dc *DirPickerContent) drawLeftPane(c *renderer.Canvas, t renderer.Theme, st *model.DirPickerState, r renderer.Rect) {
	dc.drawPane(c, t, r, i18n.T("上一级目录"), st.Left, st.LeftSel, st.Dir, true, st.Loading, i18n.T("（没有子目录）"))
}

func (dc *DirPickerContent) drawRightPane(c *renderer.Canvas, t renderer.Theme, st *model.DirPickerState, r renderer.Rect) {
	title := i18n.T("最近使用")
	if !st.AtLogicalRoot() {
		title = i18n.T("当前目录: %s", st.Dir)
	}
	dc.drawPane(c, t, r, title, st.Right, st.RightSel, "", false, st.Loading, i18n.T("（没有子目录）"))
}

// drawPane 绘制一栏：标题行 + 目录列表。markCurrent 为 true 时在当前目录
// （current）条目上加 "●" 标记，与选中光标 "❯" 区分。
func (dc *DirPickerContent) drawPane(c *renderer.Canvas, t renderer.Theme, r renderer.Rect, title string, entries []model.DirEntry, sel int, current string, markCurrent, loading bool, empty string) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	c.PutText(r.X, r.Y, renderer.Truncate(title, r.W), t.Style(t.TextMuted).WithBold(true))
	rows := r.H - 1
	if rows <= 0 {
		return
	}
	if len(entries) == 0 {
		msg := empty
		if loading {
			msg = i18n.T("加载中…")
		}
		c.PutText(r.X+2, r.Y+1, renderer.Truncate(msg, max(r.W-2, 0)), t.Style(t.TextMuted))
		return
	}
	start := max(sel-rows/2, 0)
	if maxStart := len(entries) - rows; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	for i := start; i < start+rows && i < len(entries); i++ {
		y := r.Y + 1 + (i - start)
		entry := entries[i]
		marker := "  "
		st := t.Style(t.Text)
		if entry.IsDir {
			// 目录黄色，文件用普通文字色；选中项再加粗。
			st = t.Style(t.Warning)
		}
		if i == sel {
			marker = "❯ "
			st = st.WithBold(true)
		}
		mark := "  "
		if markCurrent && entry.Path == current {
			mark = "● "
		}
		line := marker + mark + entry.Name
		c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
	}
}
