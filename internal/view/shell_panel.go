package view

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/term"
	"github.com/cxykevin/alcoh/internal/widget"
	"github.com/cxykevin/tflow"
)

// ShellPanel renders the live shell list and selected VT preview.
type ShellPanel struct {
	Theme       renderer.Theme
	PreviewRect renderer.Rect
	// workflow 预览的图布局缓存：面板每帧重绘，但 tflow 布局只在图 / 节点状态 /
	// 选中项变化时重算（见 workflow_panel.go）。
	workflowLayoutKey   string
	workflowLayoutCache *tflow.Diagram
}

func (p *ShellPanel) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	xs := m.Shells()
	if len(xs) == 0 {
		// 还没有任何 shell：仍画出面板骨架，提示内容由服务端查询而来。
		c.PutText(r.X+1, r.Y, i18n.T("终端"), p.Theme.Style(p.Theme.Text).WithBold(true))
		c.PutText(r.X+9, r.Y, i18n.T("%d", 0), p.Theme.Style(p.Theme.Accent).WithBold(true))
		if r.H > 2 {
			c.PutText(r.X+1, r.Y+2, i18n.T("暂无 shell，按 r 重新拉取"), p.Theme.Style(p.Theme.TextMuted).WithDim(true))
		}
		p.footer(c, r, false)
		return
	}
	if m.ShellSelected < 0 {
		m.ShellSelected = 0
	}
	if m.ShellSelected >= len(xs) {
		m.ShellSelected = len(xs) - 1
	}
	// 选中 workflow 终端时底部提示改为分栏按键（图已到达、预览走分栏渲染）。
	workflowHints := false
	if sel := m.SelectedShell(); sel != nil && sel.Workflow != nil {
		workflowHints = sel.Workflow.HasGraph()
	}
	// 列表顺序由模型保证：前 activeCount 个是活跃 shell，其余是历史 shell；
	// 两段内部都是"新的在上"。
	activeCount := len(m.ActiveShells())
	if activeCount > len(xs) {
		activeCount = len(xs)
	}
	if m.ShellFullscreen {
		box := r
		if box.H > 0 {
			box.H-- // Keep the footer outside the preview border.
		}
		p.PreviewRect = box
		p.drawPreviewBox(c, box, xs[m.ShellSelected], m)
		p.footer(c, r, workflowHints)
		return
	}
	// A narrow terminal cannot present a useful 40% preview; give the
	// command list the full width instead.
	showPreview := r.W >= 60
	left := r.W * 3 / 5
	if !showPreview {
		left = r.W
	}
	if left < 1 {
		left = 1
	}
	if left >= r.W && r.W > 1 {
		left = r.W - 1
	}
	right := r.W - left
	maxLeft := max(left-3, 1)
	c.PutText(r.X+1, r.Y, i18n.T("终端"), p.Theme.Style(p.Theme.Text).WithBold(true))
	c.PutText(r.X+9, r.Y, renderer.Truncate(shellSummary(activeCount, len(xs)-activeCount), max(left-9, 1)), p.Theme.Style(p.Theme.Accent).WithBold(true))
	y := r.Y + 2
	for i, s := range xs {
		if i == activeCount {
			if y >= r.Y+r.H-2 {
				break
			}
			p.sectionLabel(c, r.X+1, y, left, i18n.T("历史 (%d)", len(xs)-activeCount))
			y++
		}
		// 每条 shell 占两行：reason + 右对齐状态 / 命令。
		if y+1 >= r.Y+r.H-1 {
			break
		}
		p.drawShellRow(c, r.X+1, y, maxLeft, s, i == m.ShellSelected)
		y += 2
	}
	if showPreview && right > 1 {
		previewBox := renderer.NewRect(r.X+left+1, r.Y, right-1, r.H)
		if previewBox.H > 0 {
			previewBox.H-- // Keep the footer outside the preview border.
		}
		p.PreviewRect = previewBox
		p.drawPreviewBox(c, previewBox, xs[m.ShellSelected], m)
	}
	p.footer(c, r, workflowHints)
}

// drawShellRow 绘制一条 shell：第一行左侧为 reason、右侧为彩色状态（右对齐），
// 第二行是启动命令。
func (p *ShellPanel) drawShellRow(c *renderer.Canvas, x, y, width int, s *model.TerminalState, selected bool) {
	if width <= 2 || s == nil {
		return
	}
	marker := "  "
	headStyle := p.Theme.Style(p.Theme.Text)
	cmdStyle := p.Theme.Style(p.Theme.TextMuted)
	if s.Finished() {
		headStyle = p.Theme.Style(p.Theme.TextMuted).WithDim(true)
		cmdStyle = cmdStyle.WithDim(true)
	}
	if selected {
		marker = "> "
		headStyle = p.Theme.Style(p.Theme.Primary).WithBold(true)
		cmdStyle = p.Theme.Style(p.Theme.Text)
	}
	// 第一行：reason（左）+ 状态（右对齐）。
	status := shellStatusLabel(s)
	statusW := renderer.StringWidth(status)
	headW := max(width-2-statusW-1, 1)
	c.PutText(x, y, marker+renderer.Truncate(shellReason(s), headW), headStyle)
	if sx := x + width - statusW; sx >= x+2 {
		c.PutText(sx, y, status, p.Theme.Style(shellStatusColor(p.Theme, s)).WithBold(selected))
	}
	// 第二行：命令。
	cmd := s.Command
	if cmd == "" {
		cmd = s.Title
	}
	if cmd == "" {
		cmd = s.ID
	}
	if cmd != "" {
		c.PutText(x+2, y+1, renderer.Truncate(cmd, max(width-2, 1)), cmdStyle)
	}
}

func (p *ShellPanel) drawPreviewBox(c *renderer.Canvas, r renderer.Rect, s *model.TerminalState, m *model.AppModel) {
	if r.W < 2 || r.H < 2 {
		return
	}
	style := p.Theme.Style(p.Theme.BorderSubtle)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		c.Put(x, r.Y, renderer.CellRune('─', style))
		c.Put(x, r.Y+r.H-1, renderer.CellRune('─', style))
	}
	c.Put(r.X, r.Y, renderer.CellRune('┌', style))
	c.Put(r.X+r.W-1, r.Y, renderer.CellRune('┐', style))
	c.Put(r.X, r.Y+r.H-1, renderer.CellRune('└', style))
	c.Put(r.X+r.W-1, r.Y+r.H-1, renderer.CellRune('┘', style))
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		c.Put(r.X, y, renderer.CellRune('│', style))
		c.Put(r.X+r.W-1, y, renderer.CellRune('│', style))
	}
	p.preview(c, renderer.NewRect(r.X+1, r.Y+1, r.W-2, r.H-2), s, m)
}

func (p *ShellPanel) preview(c *renderer.Canvas, r renderer.Rect, s *model.TerminalState, m *model.AppModel) {
	if s == nil {
		return
	}
	// 标题左侧、状态右对齐（与列表一致）；标题按状态宽度留位，避免互相覆盖。
	status := shellStatusLabel(s)
	sw := renderer.StringWidth(status)
	titleW := r.W - 2
	if sw > 0 && titleW > sw+2 {
		titleW -= sw + 2
	}
	c.PutText(r.X+1, r.Y, renderer.Truncate(i18n.T("终端: %s", shellTitle(s)), titleW), p.Theme.Style(p.Theme.Info).WithBold(true))
	if sw > 0 && r.W-2 > sw+2 {
		c.PutText(r.X+r.W-2-sw, r.Y, status, p.Theme.Style(shellStatusColor(p.Theme, s)).WithBold(true))
	}
	// 内容区占满预览框剩余高度，最右一列留给滚动条。
	content := renderer.NewRect(r.X, r.Y+1, r.W-1, r.H-1)
	if content.W < 1 || content.H < 1 {
		return
	}
	if m != nil {
		m.ShellPreviewRows = content.H
	}
	if s.Workflow != nil && s.Workflow.HasGraph() {
		// workflow 终端：图 + 下栏（Agent 列表 / 节点日志）替代原始输出。预览框
		// 最后一列留给 VT 侧边滚动条，这里用不上（滚动条由分栏自己按需绘制）。
		p.drawWorkflowPreview(c, renderer.NewRect(r.X, r.Y+1, r.W, r.H-1), s, m)
		return
	}
	p.drawTerminalScreen(c, content, s)
}

// drawTerminalScreen 把终端最近的输出画进内容区，并按回看偏移（s.Scroll）取窗口。
func (p *ShellPanel) drawTerminalScreen(c *renderer.Canvas, content renderer.Rect, s *model.TerminalState) {
	syncTerminalScreen(s, content.W, content.H)
	if s.Screen == nil {
		return
	}
	// 内容行数、可视窗口与滚动偏移：FollowBottom（粘滞）时始终显示最新输出。
	// 回看范围受屏幕保留行数限制（更早的行已被顶出屏幕，不能再滚上去看到空白）。
	retained := min(s.ScreenRows, s.Screen.Height)
	maxScroll := max(retained-content.H, 0)
	if s.Scroll > maxScroll {
		s.Scroll = maxScroll
	}
	if s.FollowBottom {
		s.Scroll = 0
	}
	top := max(s.ScreenRows-content.H-s.Scroll, 0)
	// 屏幕里第一行对应的内容行号（屏幕只保留了尾部若干行）。
	offset := max(s.ScreenRows-s.Screen.Height, 0)
	barTop := max(top-offset, 0)
	style := p.Theme.Style(renderer.RGB(0xFF, 0xFF, 0xFF))
	for i := 0; i < content.H; i++ {
		row := top + i - offset
		if row < 0 || row >= s.Screen.Height {
			continue
		}
		line := strings.TrimRight(s.Screen.Line(row), " ")
		if line == "" {
			continue
		}
		c.PutText(content.X, content.Y+i, cutToWidth(line, content.W), style)
	}
	// 滚动条：贴预览框右侧内边（Total/Top 以屏幕保留的内容为范围）。
	(&widget.Scrollbar{
		Total: max(retained, content.H), View: content.H, Top: barTop,
		Track: p.Theme.Style(p.Theme.BorderSubtle),
		Thumb: p.Theme.Style(p.Theme.Border),
	}).Draw(c, renderer.NewRect(content.X+content.W, content.Y, 1, content.H))
}

// syncTerminalScreen 让终端的 VT 屏幕适配预览内容区：宽度即换行宽度；高度在
// 视口基础上留出回滚余量（内容超出时按倍扩容，上限见 previewScrollbackCap），
// 这样滚动条能回看历史输出。尺寸或容量变化时才重建并重放 transcript。
func syncTerminalScreen(s *model.TerminalState, w, h int) {
	if s == nil || w < 1 || h < 1 {
		return
	}
	capH := h + previewScrollbackCap
	rebuild := s.Screen == nil || s.Screen.Width != w || s.ScreenViewH != h
	if !rebuild && s.Screen.Scrolled > 0 && s.Screen.Height < capH {
		// 内容已顶出屏幕上沿：扩容以便回看（每次翻倍，摊薄重放成本）。
		rebuild = true
	}
	if rebuild {
		// 一次涨到内容放得下（按倍扩容，上限 capH）：面板空闲时不会再有下一帧，
		// 不能依赖"下一帧继续扩容"。
		target := h
		if s.Screen != nil && s.Screen.Width == w && s.Screen.Height > h {
			target = min(s.Screen.Height*2, capH)
		}
		for {
			scr := term.NewVTScreen(w, target)
			if s.Transcript != "" {
				scr.Feed(s.Transcript)
			}
			s.Screen = scr
			if scr.Scrolled == 0 || target >= capH {
				break
			}
			next := min(target*2, capH)
			if next == target {
				break
			}
			target = next
		}
		s.ScreenViewH = h
	}
	s.ScreenRows = s.Screen.UsedRows()
}

// previewScrollbackCap 是预览回滚缓冲的最大额外行数。
const previewScrollbackCap = 400

// cutToWidth 按显示宽度截断且不加省略号：VT 屏幕按 rune 数换行，宽字符行可能
// 略超框宽，这里只削掉溢出部分（换行已由屏幕完成，不需要提示截断）。
func cutToWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if renderer.StringWidth(s) <= w {
		return s
	}
	var sb strings.Builder
	used := 0
	for _, r := range s {
		rw := renderer.StringWidth(string(r))
		if used+rw > w {
			break
		}
		sb.WriteRune(r)
		used += rw
	}
	return sb.String()
}

// footer 绘制面板底部提示行。workflow 为 true 时追加图/下栏页签的按键说明。
func (p *ShellPanel) footer(c *renderer.Canvas, r renderer.Rect, workflow bool) {
	if r.H <= 0 {
		return
	}
	hint := i18n.T("x 结束  r 刷新  PgUp/PgDn 滚动  Esc 返回")
	if workflow {
		hint = i18n.T("Tab 节点  ←→ 页签  hjkl 平移  PgUp/PgDn 翻页  x 结束  r 刷新  Esc 返回")
	}
	c.PutText(r.X+1, r.Y+r.H-1, renderer.Truncate(hint, max(r.W-2, 1)), p.Theme.Style(p.Theme.TextMuted).WithDim(true))
}

// sectionLabel 在列表内绘制分段标题（如历史段），右侧用横线补满列表宽度。
func (p *ShellPanel) sectionLabel(c *renderer.Canvas, x, y, width int, label string) {
	if width <= 2 {
		return
	}
	st := p.Theme.Style(p.Theme.BorderSubtle)
	text := "── " + label + " "
	c.PutText(x, y, renderer.Truncate(text, width-2), st)
	if fill := width - 2 - renderer.StringWidth(text); fill > 0 {
		c.PutText(x+renderer.StringWidth(text), y, strings.Repeat("─", fill), st)
	}
}

// shellSummary 生成面板标题右侧的计数摘要：第一个数只统计活动 shell，
// 历史段数量单独标注（两段互不重叠）。
func shellSummary(active, history int) string {
	if history <= 0 {
		return i18n.T("%d", active)
	}
	return i18n.T("%d", active) + "  · " + i18n.T("历史 (%d)", history)
}

// shellReason 返回列表首行文案：run 的 reason 优先，其次标题、最后终端 ID。
func shellReason(s *model.TerminalState) string {
	if s == nil {
		return ""
	}
	if s.Reason != "" {
		return s.Reason
	}
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

// shellTitle 返回终端的展示标题：命令优先，其次标题，最后 ID。
func shellTitle(s *model.TerminalState) string {
	if s == nil {
		return ""
	}
	if s.Command != "" {
		return s.Command
	}
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

// shellStatusLabel 把服务端状态映射为本地化文案。
func shellStatusLabel(s *model.TerminalState) string {
	if s == nil {
		return ""
	}
	switch strings.ToLower(s.Status) {
	case "start", "running", "in_progress", "pending":
		return i18n.T("运行中")
	case "finished", "completed", "done", "exited":
		return i18n.T("已完成")
	case "killed", "cancelled", "stopped", "stop":
		return i18n.T("已终止")
	case "failed", "error":
		return i18n.T("失败")
	case "":
		if s.Finished() {
			return i18n.T("已完成")
		}
		return i18n.T("运行中")
	default:
		return s.Status
	}
}

// shellStatusColor 按状态取色：运行中蓝、成功绿、终止黄、失败红、其余弱化。
func shellStatusColor(t renderer.Theme, s *model.TerminalState) renderer.Color {
	if s == nil {
		return t.TextMuted
	}
	switch strings.ToLower(s.Status) {
	case "start", "running", "in_progress", "pending", "":
		return t.ToolRunning
	case "finished", "completed", "done", "exited":
		return t.ToolDone
	case "killed", "cancelled", "stopped", "stop":
		return t.Warning
	case "failed", "error":
		return t.ToolFailed
	default:
		return t.TextMuted
	}
}
