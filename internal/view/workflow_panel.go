package view

import (
	"fmt"
	"strings"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
	"github.com/cxykevin/tflow"
)

// 本文件渲染 workflow（dynworkflow）终端的预览：上分栏是用 tflow 布局的图，
// 下分栏是选中节点的日志。图、节点/agent 状态来自 model.WorkflowState
// （见 model/workflow.go），画布平移、节点选择与日志滚动游标同样保存在模型里，
// 因此跨帧、跨刷新都稳定；这里只负责把它们画出来。

// workflow 图的色号。tflow 的单元格只带 256 色索引，具体颜色由这里映射到主题语义色
// （见 workflowPalette），所以不依赖 xterm 调色板：同一色号在任何主题下都取该主题
// 的颜色，配色模式降级也由渲染层统一处理。
const (
	wfColorDefault    = -1 // tflow 的"默认色"（路由线与未着色文本）
	wfColorMuted      = 0
	wfColorWaiting    = 1
	wfColorRunning    = 2
	wfColorDone       = 3
	wfColorError      = 4
	wfColorTerminated = 5
	wfColorQueued     = 6
	wfColorText       = 7
	wfColorSelectedBg = 8
)

// workflowNodeLabelWidth 是节点框内文本的最大显示宽度：过长的节点名会撑宽图，
// 这里按列截断，保证整张图在预览框里尽量紧凑。
const workflowNodeLabelWidth = 28

// workflowPalette 把上面的色号映射为当前主题的颜色。
func workflowPalette(t renderer.Theme) map[int]renderer.Color {
	return map[int]renderer.Color{
		wfColorMuted:      t.BorderSubtle,
		wfColorWaiting:    t.TextMuted,
		wfColorRunning:    t.ToolRunning,
		wfColorDone:       t.ToolDone,
		wfColorError:      t.ToolFailed,
		wfColorTerminated: t.Warning,
		wfColorQueued:     t.Secondary,
		wfColorText:       t.Text,
		// 选中节点用低饱和的主题色作底：暗色主题下仍能看清文字。
		wfColorSelectedBg: t.Primary.Blend(t.Background, 0.3),
	}
}

// workflowCellColor 把色号转为单元格颜色；未知色号与 -1 都回退默认色。
func workflowCellColor(palette map[int]renderer.Color, idx int) renderer.Color {
	if idx < 0 {
		return renderer.ColorDefault
	}
	if c, ok := palette[idx]; ok {
		return c
	}
	return renderer.ColorDefault
}

// drawWorkflowPreview 渲染 workflow 终端的预览内容区：图分栏（上）与下栏（下）。
// 下栏是选中节点的两个页签——Agent 列表与节点日志，←→ 切换（见 drawWorkflowPane）；
// 分栏没有"焦点"概念：画布随时可用 h/j/k/l 平移、下栏随时可用 PgUp/PgDn 翻页，
// 因此不需要高亮边框来提示当前作用在哪个分栏。
// 绘制时顺带把分栏内尺寸写回模型，供"半屏平移 / 一屏翻页"的步长使用。
func (p *ShellPanel) drawWorkflowPreview(c *renderer.Canvas, r renderer.Rect, s *model.TerminalState, m *model.AppModel) {
	w := s.Workflow
	if w == nil || r.W < 2 || r.H < 2 {
		return
	}
	paneHeight := workflowPaneHeight(r.H)
	graphHeight := r.H - paneHeight
	p.drawWorkflowGraph(c, renderer.NewRect(r.X, r.Y, r.W, graphHeight), w, m)
	if paneHeight > 0 {
		p.drawWorkflowPane(c, renderer.NewRect(r.X, r.Y+graphHeight, r.W, paneHeight), w, m)
	}
}

// workflowPaneHeight 返回图下方那一栏的高度：约占预览的三分之一，太矮时干脆只画图
// （预览框小到分栏会互相挤占时不留该栏）。
func workflowPaneHeight(height int) int {
	if height < 6 {
		return 0
	}
	paneHeight := height / 3
	if paneHeight < 4 {
		paneHeight = 4
	}
	if paneHeight > height-3 {
		paneHeight = height - 3
	}
	return paneHeight
}

// drawWorkflowGraph 绘制图分栏：按 PanY/PanX 平移 tflow 布局好的网格，
// 选中节点用主题高亮底标出（节点级高亮本身就是"看哪里"的提示，分栏不再有焦点）。
// 每格都按自己在网格里的列号落位、宽度也取自网格（见 workflowCellWidth），
// 本地列宽表不用来推算位置与宽度——原因见下方循环里的说明。
func (p *ShellPanel) drawWorkflowGraph(c *renderer.Canvas, r renderer.Rect, w *model.WorkflowState, m *model.AppModel) {
	p.workflowPaneBox(c, r, workflowGraphTitle(w), nil)
	inner := renderer.NewRect(r.X+1, r.Y+1, r.W-2, r.H-2)
	if inner.W < 1 || inner.H < 1 {
		return
	}
	// 画布平移的步长是"半个分栏"：把分栏内尺寸记回模型。绘制先于按键处理，
	// 所以按键时读到的总是当前这一帧的尺寸。
	if m != nil {
		m.ShellWorkflowCols, m.ShellWorkflowRows = inner.W, inner.H
	}
	if !w.HasGraph() {
		c.PutText(inner.X, inner.Y, renderer.Truncate(i18n.T("等待 workflow 图…"), inner.W), p.Theme.Style(p.Theme.TextMuted).WithDim(true))
		return
	}
	diagram := p.workflowLayout(w)
	if diagram == nil {
		return
	}
	grid := diagram.Grid()
	if len(grid) == 0 {
		return
	}
	// 平移量按内容尺寸收敛：图变小（或预览变大）时不会停在空白区。
	if maxPan := max(len(grid)-inner.H, 0); w.PanY > maxPan {
		w.PanY = maxPan
	}
	if maxPan := max(workflowGridWidth(grid)-inner.W, 0); w.PanX > maxPan {
		w.PanX = maxPan
	}
	palette := workflowPalette(p.Theme)
	for row := 0; row < inner.H; row++ {
		source := w.PanY + row
		if source < 0 || source >= len(grid) {
			break
		}
		cells := grid[source]
		// 每格按自己在网格里的列号落位：tflow 的一列就是一显示列。这里刻意不累加
		// 本地列宽表——tflow 的 charWidth 与本地表对全角符号、半角片假名等判断不同，
		// 一旦累加，从那个字符起整行都会偏移；跳过宽字符续列时也必须照旧占掉那一列，
		// 否则窗口左边界正好落在宽字符中间时会整行左移一格（左右平移时最明显）。
		for col := w.PanX; col < len(cells); col++ {
			x := col - w.PanX
			if x >= inner.W {
				break
			}
			cell := cells[col]
			if cell.Ch == 0 {
				// 宽字符的续列占位：左半在左边（可能在视野外）已经写好，跳过。
				continue
			}
			if x+workflowCellWidth(cells, col) > inner.W {
				// 右边界放不下整个宽字符：不画，避免续列压到分栏边框上。
				break
			}
			style := renderer.Style{Fg: workflowCellColor(palette, cell.Fg), Bg: workflowCellColor(palette, cell.Bg)}
			c.Put(inner.X+x, inner.Y+row, renderer.CellRune(cell.Ch, style))
		}
	}
}

// workflowLayout 返回图的 tflow 布局。布局按"图结构 + 节点状态 + 选中项"缓存：
// 面板每帧都会重绘，但只有状态真正变化时才重算布局。
func (p *ShellPanel) workflowLayout(w *model.WorkflowState) *tflow.Diagram {
	key := workflowLayoutKey(w)
	if p.workflowLayoutCache != nil && p.workflowLayoutKey == key {
		return p.workflowLayoutCache
	}
	graph, styles := workflowTflowGraph(w)
	if len(styles) == 0 {
		return nil
	}
	diagram, err := tflow.Layout(graph, styles, nil, tflow.Config{
		Theme:        tflow.ThemeRound,
		DefaultColor: wfColorMuted,
		RowGap:       1,
		ColGap:       4,
	})
	if err != nil {
		// 布局失败（理论上只在色号越界时发生）：保留上一次的图，不清掉面板内容。
		return p.workflowLayoutCache
	}
	p.workflowLayoutKey = key
	p.workflowLayoutCache = diagram
	return diagram
}

// workflowTflowGraph 把 workflow 图转成 tflow 的邻接表与节点样式：节点按画布顺序
// 从 1 开始编号，边只保留两端都在图里的（事件可能先于图到达，不能因此产生悬空边）。
func workflowTflowGraph(w *model.WorkflowState) (tflow.Graph, map[int]tflow.NodeStyle) {
	nodes := w.OrderedNodes()
	if len(nodes) == 0 {
		return tflow.Graph{}, nil
	}
	index := make(map[string]int, len(nodes))
	for i, node := range nodes {
		index[node.ID] = i + 1
	}
	adjacency := make(map[int][]int, len(nodes))
	if graph := w.Graph(); graph != nil {
		for from, targets := range graph.Edges {
			u, ok := index[from]
			if !ok {
				continue
			}
			for _, target := range targets {
				v, ok := index[target]
				if !ok || v == u {
					continue
				}
				adjacency[u] = append(adjacency[u], v)
			}
		}
	}
	styles := make(map[int]tflow.NodeStyle, len(nodes))
	for i, node := range nodes {
		styles[i+1] = workflowNodeStyle(node, node.ID == w.Selected)
	}
	return tflow.Graph{AdjList: adjacency, Start: 1}, styles
}

// workflowNodeStyle 生成一个节点的框样式：第一行是显示名、第二行是状态摘要，
// 边框随状态着色；选中节点用主题高亮底 + 常规文字色。
func workflowNodeStyle(node *model.WorkflowNode, selected bool) tflow.NodeStyle {
	labelColor := workflowNodeColor(node)
	textColor := wfColorText
	if !selected {
		textColor = labelColor
	}
	style := tflow.NodeStyle{
		Lines: [][]tflow.TextSegment{
			{{Text: renderer.Truncate(node.Label(), workflowNodeLabelWidth), Color: textColor}},
			{{Text: renderer.Truncate(workflowNodeSummary(node), workflowNodeLabelWidth), Color: wfColorMuted}},
		},
		BorderColor: labelColor,
		BgColor:     wfColorDefault,
	}
	if selected {
		style.BgColor = wfColorSelectedBg
		style.Lines[1][0].Color = wfColorText
	}
	return style
}

// workflowNodeColor 按节点状态取边框色。
func workflowNodeColor(node *model.WorkflowNode) int {
	switch node.State {
	case "running":
		return wfColorRunning
	case "done":
		return wfColorDone
	case "error":
		return wfColorError
	case "terminated":
		return wfColorTerminated
	case "queue":
		return wfColorQueued
	case "wait":
		return wfColorWaiting
	default:
		return wfColorMuted
	}
}

// workflowNodeSummary 生成节点框第二行：状态 + agent 进度 + 命中缓存标记。
func workflowNodeSummary(node *model.WorkflowNode) string {
	parts := []string{workflowNodeStateLabel(node.State)}
	if total := len(node.Agents); total > 0 {
		parts = append(parts, i18n.T("agent %d/%d", node.RunningAgents(), total))
	}
	if node.Cached {
		parts = append(parts, i18n.T("缓存"))
	}
	return strings.Join(parts, " · ")
}

// workflowNodeStateLabel 把节点状态映射为本地化文案。
func workflowNodeStateLabel(state string) string {
	switch state {
	case "running":
		return i18n.T("运行中")
	case "done":
		return i18n.T("已完成")
	case "error":
		return i18n.T("失败")
	case "terminated":
		return i18n.T("已终止")
	case "queue":
		return i18n.T("排队")
	case "wait":
		return i18n.T("等待")
	default:
		return i18n.T("未运行")
	}
}

// workflowLayoutKey 生成布局缓存键：图结构、节点显示名与状态、选中项任一变化都要重排。
func workflowLayoutKey(w *model.WorkflowState) string {
	var sb strings.Builder
	sb.WriteString(w.RunID)
	sb.WriteByte('|')
	sb.WriteString(w.Selected)
	graph := w.Graph()
	for _, node := range w.OrderedNodes() {
		fmt.Fprintf(&sb, ";%s:%s:%s:%d:%d:%t", node.ID, node.Name, node.State, node.RunningAgents(), len(node.Agents), node.Cached)
		if graph != nil {
			for _, target := range graph.Edges[node.ID] {
				sb.WriteByte('>')
				sb.WriteString(target)
			}
		}
	}
	return sb.String()
}

// workflowGridWidth 返回网格的最大列数。
func workflowGridWidth(grid [][]tflow.Cell) int {
	width := 0
	for _, row := range grid {
		if len(row) > width {
			width = len(row)
		}
	}
	return width
}

// workflowCellWidth 返回网格里一格占的显示列数：宽字符在网格里占两格，第二格是
// 续列占位（Ch==0）。宽度直接读布局，不再用本地列宽表推算——本地表与 tflow 的
// charWidth 规则对少数字符（全角符号、半角片假名等）判断不同，混用会让整行错位。
func workflowCellWidth(cells []tflow.Cell, col int) int {
	if col+1 < len(cells) && cells[col+1].Ch == 0 {
		return 2
	}
	return 1
}

// drawWorkflowPane 绘制图下方那一栏：上边框里嵌两个页签（Agent 列表 / 节点日志，
// 类似 notebook 的两个页面，←→ 切换），正文按当前页签渲染选中节点的内容。
func (p *ShellPanel) drawWorkflowPane(c *renderer.Canvas, r renderer.Rect, w *model.WorkflowState, m *model.AppModel) {
	node := w.SelectedNode()
	p.workflowPaneBox(c, r, "", workflowPaneTabs(w, node))
	inner := renderer.NewRect(r.X+1, r.Y+1, r.W-2, r.H-2)
	if inner.W < 1 || inner.H < 1 {
		return
	}
	// 下栏翻页（PgUp/PgDn）的步长是"一屏"：把分栏内高记回模型。
	if m != nil {
		m.ShellWorkflowLogRows = inner.H
	}
	if node == nil {
		c.PutText(inner.X, inner.Y, renderer.Truncate(i18n.T("选择节点后显示其输出"), inner.W), p.Theme.Style(p.Theme.TextMuted).WithDim(true))
		return
	}
	if w.Pane == model.WorkflowPaneAgents {
		p.drawWorkflowAgents(c, inner, w, node)
		return
	}
	p.drawWorkflowLog(c, inner, w, node)
}

// workflowPaneTabs 生成下栏的页签：Agent 列表与节点日志（列表在前，与默认页一致），
// 激活页带方括号标记（见 workflowTabStrip）；页签名带上选中节点，切页后一眼能看出
// 在看哪个节点。
func workflowPaneTabs(w *model.WorkflowState, node *model.WorkflowNode) []workflowTab {
	label := ""
	if node != nil {
		label = node.Label()
	}
	tab := func(name string, active bool) workflowTab {
		if label != "" {
			// 用 "页签名: %s" 这种整键查表，英文下才能整条换成 "node log: 采集"；
			// 拼 "%s: %s" 会绕过词条表，让中文页签名漏进英文界面。
			name = i18n.T(name+": %s", label)
		}
		return workflowTab{Title: name, Active: active}
	}
	return []workflowTab{
		tab(i18n.T("Agent 列表"), w.Pane == model.WorkflowPaneAgents),
		tab(i18n.T("节点日志"), w.Pane == model.WorkflowPaneLog),
	}
}

// drawWorkflowLog 绘制日志页正文：选中节点的终值结果与输出行，按 LogScroll
// 从末尾往回看（0 = 停在最新一行）。
func (p *ShellPanel) drawWorkflowLog(c *renderer.Canvas, inner renderer.Rect, w *model.WorkflowState, node *model.WorkflowNode) {
	lines, resultLines := workflowNodeOutput(node)
	if len(lines) == 0 {
		c.PutText(inner.X, inner.Y, renderer.Truncate(i18n.T("（暂无输出）"), inner.W), p.Theme.Style(p.Theme.TextMuted).WithDim(true))
		return
	}
	if maxScroll := max(len(lines)-inner.H, 0); w.LogScroll > maxScroll {
		w.LogScroll = maxScroll
	}
	start := max(len(lines)-inner.H-w.LogScroll, 0)
	textWidth := inner.W
	if len(lines) > inner.H && inner.W > 1 {
		// 内容超出时最右一列留给滚动条。
		textWidth = inner.W - 1
	}
	for i := 0; i < inner.H && start+i < len(lines); i++ {
		style := p.Theme.Style(p.Theme.TextMuted)
		if start+i < resultLines {
			style = p.Theme.Style(p.Theme.Text)
		}
		c.PutText(inner.X, inner.Y+i, renderer.Truncate(lines[start+i], textWidth), style)
	}
	p.workflowScrollbar(c, inner, len(lines), start, textWidth)
}

// drawWorkflowAgents 绘制 Agent 列表页：选中节点启动的每个 agent 一行——序号、
// 状态（按状态着色）与提示词摘要；行数超出时按 AgentScroll 从末尾往回看。
func (p *ShellPanel) drawWorkflowAgents(c *renderer.Canvas, inner renderer.Rect, w *model.WorkflowState, node *model.WorkflowNode) {
	if len(node.Agents) == 0 {
		c.PutText(inner.X, inner.Y, renderer.Truncate(i18n.T("（暂无 Agent）"), inner.W), p.Theme.Style(p.Theme.TextMuted).WithDim(true))
		return
	}
	if maxScroll := max(len(node.Agents)-inner.H, 0); w.AgentScroll > maxScroll {
		w.AgentScroll = maxScroll
	}
	start := max(len(node.Agents)-inner.H-w.AgentScroll, 0)
	textWidth := inner.W
	if len(node.Agents) > inner.H && inner.W > 1 {
		textWidth = inner.W - 1
	}
	palette := workflowPalette(p.Theme)
	for i := 0; i < inner.H && start+i < len(node.Agents); i++ {
		p.drawWorkflowAgentRow(c, inner.X, inner.Y+i, textWidth, node.Agents[start+i], palette)
	}
	p.workflowScrollbar(c, inner, len(node.Agents), start, textWidth)
}

// Agent 列表页的列位：序号占 0 列起，状态对齐到第 4 列，提示词从第 12 列起——
// 状态文案最长 6 列，中英文都不会与提示词挤在一起。
const (
	workflowAgentStateColumn  = 4
	workflowAgentPromptColumn = 12
)

// drawWorkflowAgentRow 画一行 agent：序号（暗色）、状态（按状态着色）、提示词摘要。
func (p *ShellPanel) drawWorkflowAgentRow(c *renderer.Canvas, x, y, width int, agent model.WorkflowAgent, palette map[int]renderer.Color) {
	c.PutText(x, y, renderer.Truncate(fmt.Sprintf("#%d", agent.Index+1), width), p.Theme.Style(p.Theme.TextMuted))
	if width > workflowAgentStateColumn {
		style := p.Theme.Style(workflowCellColor(palette, workflowAgentColor(agent.State)))
		c.PutText(x+workflowAgentStateColumn, y,
			renderer.Truncate(workflowAgentStateLabel(agent.State), width-workflowAgentStateColumn), style)
	}
	if width <= workflowAgentPromptColumn {
		return
	}
	if detail := workflowAgentDetail(agent); detail != "" {
		c.PutText(x+workflowAgentPromptColumn, y,
			renderer.Truncate(detail, width-workflowAgentPromptColumn), p.Theme.Style(p.Theme.Text))
	}
}

// workflowAgentStateLabel 把 agent 状态映射为本地化文案。
func workflowAgentStateLabel(state string) string {
	switch state {
	case "running":
		return i18n.T("运行中")
	case "success":
		return i18n.T("已完成")
	case "failure":
		return i18n.T("失败")
	default:
		return i18n.T("等待")
	}
}

// workflowAgentColor 按 agent 状态取色号（等待 / 运行中 / 成功 / 失败）。
func workflowAgentColor(state string) int {
	switch state {
	case "running":
		return wfColorRunning
	case "success":
		return wfColorDone
	case "failure":
		return wfColorError
	default:
		return wfColorWaiting
	}
}

// workflowAgentDetail 返回 agent 行的提示词摘要：压成一行（换行与连续空白折叠），
// 过长由渲染层截断；重试过的调用附上尝试次数。
func workflowAgentDetail(agent model.WorkflowAgent) string {
	text := strings.Join(strings.Fields(agent.Prompt), " ")
	if agent.Attempt > 1 {
		if text == "" {
			return i18n.T("第 %d 次尝试", agent.Attempt)
		}
		// 译文自带前导空格（中英排版需要），这里不再补空格，否则英文会出现双空格。
		return text + i18n.T("（第 %d 次尝试）", agent.Attempt)
	}
	return text
}

// workflowScrollbar 在分栏最右一列画滚动条：只有内容确实超出（渲染层把正文宽度
// 让出了一列）时才画，避免占满内容时右侧多出一条线。
func (p *ShellPanel) workflowScrollbar(c *renderer.Canvas, inner renderer.Rect, total, top, textWidth int) {
	if textWidth >= inner.W || inner.W < 1 {
		return
	}
	(&widget.Scrollbar{
		Total: total, View: inner.H, Top: top,
		Track: p.Theme.Style(p.Theme.BorderSubtle),
		Thumb: p.Theme.Style(p.Theme.Border),
	}).Draw(c, renderer.NewRect(inner.X+inner.W-1, inner.Y, 1, inner.H))
}

// workflowNodeOutput 返回节点日志页的行：终值结果在前（多行按行铺开、加 "→ "
// 前缀），随后是节点线程的输出行。resultLines 是结果行数，用于区分文字样式。
func workflowNodeOutput(node *model.WorkflowNode) (lines []string, resultLines int) {
	if node.Result != "" {
		for _, line := range strings.Split(strings.TrimRight(node.Result, "\n"), "\n") {
			lines = append(lines, "→ "+line)
		}
		resultLines = len(lines)
	}
	return append(lines, node.Logs...), resultLines
}

// workflowGraphTitle 生成图分栏标题：节点完成进度 + 当前节点。
func workflowGraphTitle(w *model.WorkflowState) string {
	nodes := w.OrderedNodes()
	if len(nodes) == 0 {
		return i18n.T("图")
	}
	done := 0
	for _, node := range nodes {
		if node.Finished() {
			done++
		}
	}
	title := i18n.T("图 %d/%d 完成", done, len(nodes))
	if current := w.Node(w.CurrentNode); current != nil {
		title += " · " + current.Label()
	}
	return title
}

// workflowTab 是下栏的一个页签：标题与是否处于激活态。
type workflowTab struct {
	Title  string
	Active bool
}

// workflowPaneBox 画 workflow 预览的分栏边框，标题嵌在上边框里。tabs 非空时标题
// 位置改画页签条（见 workflowTabStrip）；两个分栏用同一套弱边框与标题样式：
// 不再有"焦点分栏"，图与下栏的页签都可以随时操作。
func (p *ShellPanel) workflowPaneBox(c *renderer.Canvas, r renderer.Rect, title string, tabs []workflowTab) {
	if r.W < 2 || r.H < 2 {
		return
	}
	borderStyle := p.Theme.Style(p.Theme.BorderSubtle)
	titleStyle := p.Theme.Style(p.Theme.TextMuted).WithDim(true)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		c.Put(x, r.Y, renderer.CellRune('─', borderStyle))
		c.Put(x, r.Y+r.H-1, renderer.CellRune('─', borderStyle))
	}
	c.Put(r.X, r.Y, renderer.CellRune('┌', borderStyle))
	c.Put(r.X+r.W-1, r.Y, renderer.CellRune('┐', borderStyle))
	c.Put(r.X, r.Y+r.H-1, renderer.CellRune('└', borderStyle))
	c.Put(r.X+r.W-1, r.Y+r.H-1, renderer.CellRune('┘', borderStyle))
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		c.Put(r.X, y, renderer.CellRune('│', borderStyle))
		c.Put(r.X+r.W-1, y, renderer.CellRune('│', borderStyle))
	}
	if len(tabs) > 0 {
		p.workflowTabStrip(c, r, tabs)
		return
	}
	if title == "" || r.W <= 6 {
		return
	}
	label := " " + renderer.Truncate(title, r.W-4) + " "
	if renderer.StringWidth(label) > r.W-2 {
		return
	}
	c.PutText(r.X+1, r.Y, label, titleStyle)
}

// workflowTabStrip 在上边框里画页签条：激活页用方括号与常规文字色标出，其余页
// 暗色；一次性铺满整条上边框的可用宽度，放不下的页签从右往左截断。
func (p *ShellPanel) workflowTabStrip(c *renderer.Canvas, r renderer.Rect, tabs []workflowTab) {
	if r.W <= 6 {
		return
	}
	activeStyle := p.Theme.Style(p.Theme.Text).WithBold(true)
	idleStyle := p.Theme.Style(p.Theme.TextMuted).WithDim(true)
	// 每个页签前留一格与左/上一个页签分隔：图下方的栏不画标题，页签条就是标题。
	x, limit := r.X+1, r.X+r.W-1
	for _, tab := range tabs {
		if x >= limit {
			return
		}
		text, style := tab.Title, idleStyle
		if tab.Active {
			text, style = "["+tab.Title+"]", activeStyle
		}
		text = renderer.Truncate(" "+text, limit-x)
		if text == "" {
			return
		}
		c.PutText(x, r.Y, text, style)
		x += renderer.StringWidth(text)
	}
}
