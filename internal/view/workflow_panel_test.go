package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

const (
	workflowTestW = 120
	workflowTestH = 24
)

// workflowPreview 构造一个选中 workflow 终端、已展开 shells 面板的模型：
// 图是 采集 → 汇总，选中节点是 采集，带一条终值结果与两行输出。
func workflowPreview(t *testing.T) *model.AppModel {
	t.Helper()
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`),
	})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	m.ShellSelected = 0
	apply := func(ev *acp.WorkflowEvent) {
		ev.TerminalID = "@temp/run/7"
		m.Active.ApplyWorkflowEvent(ev)
	}
	apply(&acp.WorkflowEvent{
		Kind:     acp.WorkflowKindSnapshot,
		RunID:    "@temp/run/7",
		Workflow: &acp.WorkflowInfo{Status: "running", CurrentNode: "collect"},
		Graph: &acp.WorkflowGraph{
			Nodes: map[string]acp.WorkflowGraphNode{"collect": {Name: "采集"}, "merge": {Name: "汇总"}},
			Edges: map[string][]string{"collect": {"merge"}},
			Start: []string{"collect"},
		},
	})
	apply(&acp.WorkflowEvent{Kind: acp.WorkflowKindNode, RunID: "@temp/run/7", NodeID: "collect", State: "running"})
	apply(&acp.WorkflowEvent{Kind: acp.WorkflowKindNodeResult, RunID: "@temp/run/7", NodeID: "collect", Result: "42"})
	apply(&acp.WorkflowEvent{Kind: acp.WorkflowKindNodeLog, RunID: "@temp/run/7", NodeID: "collect", Message: "第一行"})
	apply(&acp.WorkflowEvent{Kind: acp.WorkflowKindNodeLog, RunID: "@temp/run/7", NodeID: "collect", Message: "第二行"})
	return m
}

// drawWorkflowPanel 把 shells 面板画进新缓冲并返回缓冲与预览框几何。
func drawWorkflowPanel(t *testing.T, m *model.AppModel) (*renderer.Buffer, renderer.Rect) {
	t.Helper()
	b := renderer.NewBuffer(workflowTestW, workflowTestH)
	(&ShellPanel{Theme: renderer.DefaultTheme()}).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, workflowTestW, workflowTestH), m)
	// 面板布局：列表占 3/5 宽，预览框占剩余宽度且底部留一行按键提示。
	left := workflowTestW * 3 / 5
	return b, renderer.NewRect(left+1, 0, workflowTestW-left-1, workflowTestH-1)
}

// workflowPreviewPanes 返回（图分栏, 下栏）在缓冲里的区域：与
// drawWorkflowPreview 的分栏规则一致（下栏约占三分之一，其余给图）。
func workflowPreviewPanes(t *testing.T, b *renderer.Buffer, preview renderer.Rect) (renderer.Rect, renderer.Rect) {
	t.Helper()
	inner := renderer.NewRect(preview.X+1, preview.Y+1, preview.W-2, preview.H-2)
	content := renderer.NewRect(inner.X, inner.Y+1, inner.W, inner.H-1)
	if content.H < 1 || content.W < 1 {
		t.Fatalf("preview content too small: %#v", content)
	}
	paneH := workflowPaneHeight(content.H)
	if paneH == 0 {
		t.Fatalf("lower pane must be drawn at %d rows", content.H)
	}
	return renderer.NewRect(content.X, content.Y, content.W, content.H-paneH),
		renderer.NewRect(content.X, content.Y+content.H-paneH, content.W, paneH)
}

// regionRows 把缓冲区域按行拼成文本（跳过宽字符续列），与 bufferText 同构。
func regionRows(b *renderer.Buffer, r renderer.Rect) []string {
	out := make([]string, r.H)
	for y := 0; y < r.H; y++ {
		var sb strings.Builder
		for x := 0; x < r.W; x++ {
			cell := b.Get(r.X+x, r.Y+y)
			if cell.Width == 0 {
				continue
			}
			sb.WriteRune(cell.R)
		}
		out[y] = strings.TrimRight(sb.String(), " ")
	}
	return out
}

// agentRow 返回 Agent 列表页里序号为 index 的那一行（行内含 "#<index> "），
// 找不到时返回空串：用来按行比对序号、状态与提示词，避免只断言"页面上出现过"。
func agentRow(rows []string, index int) string {
	prefix := fmt.Sprintf("#%d ", index)
	for _, row := range rows {
		if strings.Contains(row, prefix) {
			return row
		}
	}
	return ""
}

// TestWorkflowPreviewRendersGraphAndLogPanes 验证 workflow 终端的预览被替换成
// 上下分栏：上分栏为图（节点显示名）、下栏是页签条 + 激活页正文（这里切到节点
// 日志页：选中节点的终值结果与输出行）；两个分栏都不带焦点标记，并把分栏内尺寸
// 写回模型（半屏平移/翻页步长要用）。
func TestWorkflowPreviewRendersGraphAndLogPanes(t *testing.T) {
	useEnglish(t)
	m := workflowPreview(t)
	// 下栏默认停在 Agent 列表页：这个用例验日志页正文，先切过去。
	if !m.SwitchWorkflowPane(1) {
		t.Fatal("switching the pane tab must be handled")
	}
	b, preview := drawWorkflowPanel(t, m)
	graphRect, paneRect := workflowPreviewPanes(t, b, preview)
	graphRows, paneRows := regionRows(b, graphRect), regionRows(b, paneRect)

	if !strings.Contains(strings.Join(graphRows, "\n"), "采集") {
		t.Fatalf("graph pane must show node labels: %q", graphRows)
	}
	if !strings.Contains(strings.Join(graphRows, "\n"), "汇总") {
		t.Fatalf("graph pane must show the second node: %q", graphRows)
	}
	// 下栏首行是页签条：每个页签都带上选中节点的名字。
	if !strings.Contains(paneRows[0], "采集") {
		t.Fatalf("pane tab strip must name the selected node: %q", paneRows[0])
	}
	paneText := strings.Join(paneRows, "\n")
	for _, want := range []string{"→ 42", "第一行", "第二行"} {
		if !strings.Contains(paneText, want) {
			t.Fatalf("log page missing %q: %q", want, paneRows)
		}
	}
	// 没有"焦点分栏"：两栏都不带 "▸" 标记，图与下栏随时可操作。
	for name, rows := range map[string][]string{"graph": graphRows, "pane": paneRows} {
		if strings.Contains(rows[0], "▸") {
			t.Fatalf("%s pane must not carry a focus marker: %q", name, rows[0])
		}
	}
	// 绘制时把分栏内尺寸写回模型：hjkl 的半屏平移与 PgUp/PgDn 的翻页步长都要用。
	if m.ShellWorkflowCols != graphRect.W-2 || m.ShellWorkflowRows != graphRect.H-2 {
		t.Fatalf("graph pane inner size = %dx%d, want %dx%d",
			m.ShellWorkflowCols, m.ShellWorkflowRows, graphRect.W-2, graphRect.H-2)
	}
	if m.ShellWorkflowLogRows != paneRect.H-2 {
		t.Fatalf("lower pane inner height = %d, want %d", m.ShellWorkflowLogRows, paneRect.H-2)
	}
}

// TestWorkflowPaneTabsAndAgentList 验证下栏的页签条与 Agent 列表页：默认激活
// "Agent 列表"（页签名带方括号），正文是 agent 行——序号、本地化状态与提示词摘要
// （重试过附上尝试次数），日志内容不再出现；← 切到节点日志页后正文换成节点输出。
func TestWorkflowPaneTabsAndAgentList(t *testing.T) {
	useEnglish(t)
	m := workflowPreview(t)
	apply := func(ev *acp.WorkflowEvent) {
		ev.TerminalID = "@temp/run/7"
		m.Active.ApplyWorkflowEvent(ev)
	}
	apply(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgentsStart, RunID: "@temp/run/7", NodeID: "collect",
		Count: 2, Prompts: []string{"甲", "乙"},
	})
	// agentIndex 从 1 开始：第 1 个 agent 运行中，第 2 个已完成（重试过一次）。
	apply(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgent, RunID: "@temp/run/7", NodeID: "collect",
		AgentIndex: 1, AgentCount: 2, State: "running",
	})
	apply(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgent, RunID: "@temp/run/7", NodeID: "collect",
		AgentIndex: 2, AgentCount: 2, State: "success", Attempt: 2,
	})

	b, preview := drawWorkflowPanel(t, m)
	_, paneRect := workflowPreviewPanes(t, b, preview)
	rows := regionRows(b, paneRect)
	if !strings.Contains(rows[0], "[agent list: 采集]") || !strings.Contains(rows[0], "node log: 采集") {
		t.Fatalf("agent page must be active in the tab strip: %q", rows[0])
	}
	text := strings.Join(rows, "\n")
	// 序号、状态与提示词按行对齐：agentIndex 从 1 起，第 1 行是"甲"（不该停在
	// waiting、也不该与第 2 行重复），第 2 行是"乙"并带上尝试次数。
	if row := agentRow(rows, 1); !strings.Contains(row, "running") || !strings.Contains(row, "甲") {
		t.Fatalf("第 1 个 agent 行 = %q，want 运行中 + 甲", row)
	}
	if row := agentRow(rows, 2); !strings.Contains(row, "finished") || !strings.Contains(row, "乙") || !strings.Contains(row, "(attempt 2)") {
		t.Fatalf("第 2 个 agent 行 = %q，want 已完成 + 乙 + 尝试次数", row)
	}
	if strings.Contains(text, "#3") {
		t.Fatalf("Agent 列表多出了幻影行（agentIndex 当成了 0 起下标）: %q", rows)
	}
	if strings.Contains(text, "第一行") {
		t.Fatalf("agent page must not show the node log: %q", rows)
	}

	// 切到节点日志页：方括号跟着搬家，正文换成节点输出。
	if !m.SwitchWorkflowPane(-1) {
		t.Fatal("switching the pane tab must be handled")
	}
	b, preview = drawWorkflowPanel(t, m)
	_, paneRect = workflowPreviewPanes(t, b, preview)
	rows = regionRows(b, paneRect)
	if !strings.Contains(rows[0], "[node log: 采集]") || strings.Contains(rows[0], "[agent list: 采集]") {
		t.Fatalf("log page must be active in the tab strip: %q", rows[0])
	}
	if text := strings.Join(rows, "\n"); !strings.Contains(text, "第一行") {
		t.Fatalf("log page content = %q", rows)
	}
}

// TestWorkflowAgentPageEmptyHint 验证 Agent 列表页（下栏默认页）在节点还没有 agent
// 时的占位提示（页签仍可切换，正文给一句提示而不是空白页）。
func TestWorkflowAgentPageEmptyHint(t *testing.T) {
	useEnglish(t)
	m := workflowPreview(t)
	b, preview := drawWorkflowPanel(t, m)
	_, paneRect := workflowPreviewPanes(t, b, preview)
	text := strings.Join(regionRows(b, paneRect), "\n")
	if !strings.Contains(text, i18n.T("（暂无 Agent）")) {
		t.Fatalf("agent page must hint that no agent started yet: %q", text)
	}
}

// TestWorkflowPreviewHighlightsSelectedNode 验证选中节点用主题高亮底标出：
// 选中框内文字的背景色是主题色混合底，未选中的节点保持默认背景。
func TestWorkflowPreviewHighlightsSelectedNode(t *testing.T) {
	useEnglish(t)
	theme := renderer.DefaultTheme()
	selectedBg := workflowPalette(theme)[wfColorSelectedBg]
	m := workflowPreview(t)

	// labelBg 返回节点框里标签首字的背景色：跳过上下边框行，因为上边框的标题
	// 也会带上当前节点名（那是标题文本，不是节点框）。
	labelBg := func(b *renderer.Buffer, graphRect renderer.Rect, label string) (renderer.Color, bool) {
		r := []rune(label)[0]
		for y := 1; y < graphRect.H-1; y++ {
			for x := 0; x < graphRect.W; x++ {
				cell := b.Get(graphRect.X+x, graphRect.Y+y)
				if cell.R == r {
					return cell.Style.Bg, true
				}
			}
		}
		return renderer.ColorDefault, false
	}

	b, preview := drawWorkflowPanel(t, m)
	graphRect, _ := workflowPreviewPanes(t, b, preview)
	selected, ok := labelBg(b, graphRect, "采集")
	if !ok {
		t.Fatalf("selected node label not rendered")
	}
	other, ok := labelBg(b, graphRect, "汇总")
	if !ok {
		t.Fatalf("other node label not rendered")
	}
	if selected != selectedBg {
		t.Fatalf("selected node bg = %v, want theme highlight %v", selected, selectedBg)
	}
	if other == selectedBg {
		t.Fatalf("unselected node must not reuse the highlight bg (%v)", other)
	}

	// 选中项移到 汇总：高亮跟着移动。
	m.SelectWorkflowNode(1)
	if m.SelectedWorkflow().Selected != "merge" {
		t.Fatalf("selected = %q", m.SelectedWorkflow().Selected)
	}
	b, preview = drawWorkflowPanel(t, m)
	graphRect, _ = workflowPreviewPanes(t, b, preview)
	if got, _ := labelBg(b, graphRect, "汇总"); got != selectedBg {
		t.Fatalf("moved selection: bg = %v, want %v", got, selectedBg)
	}
	if got, _ := labelBg(b, graphRect, "采集"); got == selectedBg {
		t.Fatalf("deselected node kept the highlight bg (%v)", got)
	}
}

// TestWorkflowPreviewWithoutGraphFallsBackToOutput 验证图尚未到达（workflow 只
// 广播了元信息）时预览仍走原始输出：此时不画分栏，也不会画出一张空图。
func TestWorkflowPreviewWithoutGraphFallsBackToOutput(t *testing.T) {
	useEnglish(t)
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`),
	})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	m.ShellSelected = 0
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{
		TerminalID: "@temp/run/7", Kind: "workflow", Status: "running", Content: "workflow starting\n",
	})
	m.Active.ApplyWorkflowEvent(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, TerminalID: "@temp/run/7", RunID: "@temp/run/7",
		Workflow: &acp.WorkflowInfo{Status: "running"},
	})

	b, preview := drawWorkflowPanel(t, m)
	inner := renderer.NewRect(preview.X+1, preview.Y+1, preview.W-2, preview.H-2)
	text := strings.Join(regionRows(b, inner), "\n")
	if !strings.Contains(text, "workflow starting") {
		t.Fatalf("preview must fall back to raw output: %q", text)
	}
	if strings.Contains(text, i18n.T("等待 workflow 图…")) {
		t.Fatalf("preview must not draw an empty graph pane before the graph arrives: %q", text)
	}
}

// TestWorkflowGraphPaneWaitingHint 验证图分栏在没有任何节点时的占位提示
// （主链路上无图时预览回退到原始输出，这里是分栏自身的兜底渲染）。
func TestWorkflowGraphPaneWaitingHint(t *testing.T) {
	useEnglish(t)
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`),
	})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	m.ShellSelected = 0
	// 只有 workflow 元信息、还没有图。
	m.Active.ApplyWorkflowEvent(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, TerminalID: "@temp/run/7", RunID: "@temp/run/7",
		Workflow: &acp.WorkflowInfo{Status: "running"},
	})
	w := m.SelectedWorkflow()
	if w == nil || w.HasGraph() {
		t.Fatalf("state must be a workflow without graph: %#v", w)
	}
	b := renderer.NewBuffer(46, 8)
	(&ShellPanel{Theme: renderer.DefaultTheme()}).drawWorkflowGraph(
		renderer.NewCanvas(b), renderer.NewRect(0, 0, 46, 8), w, m)
	text := strings.Join(regionRows(b, renderer.NewRect(0, 0, 46, 8)), "\n")
	if !strings.Contains(text, i18n.T("等待 workflow 图…")) {
		t.Fatalf("graph pane must show the waiting hint: %q", text)
	}
}

// TestWorkflowPaneHeight 验证下栏高度规则：预览太矮时只画图，
// 否则约占三分之一且至少 4 行（同时给图分栏留 3 行）。
func TestWorkflowPaneHeight(t *testing.T) {
	cases := map[int]int{0: 0, 5: 0, 6: 3, 12: 4, 20: 6, 21: 7}
	for height, want := range cases {
		if got := workflowPaneHeight(height); got != want {
			t.Fatalf("workflowPaneHeight(%d) = %d, want %d", height, got, want)
		}
	}
}

// TestWorkflowNodeOutputOrdersResultBeforeLogs 验证节点日志页内容：终值结果按行
// 铺开并加 "→ " 前缀（算作结果行），节点输出行附在其后。
func TestWorkflowNodeOutputOrdersResultBeforeLogs(t *testing.T) {
	lines, resultLines := workflowNodeOutput(&model.WorkflowNode{
		ID: "collect", Result: "甲\n乙\n", Logs: []string{"日志一", "日志二"},
	})
	want := []string{"→ 甲", "→ 乙", "日志一", "日志二"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
	if resultLines != 2 {
		t.Fatalf("resultLines = %d, want 2", resultLines)
	}
	// 没有终值结果时全部是输出行。
	lines, resultLines = workflowNodeOutput(&model.WorkflowNode{ID: "collect", Logs: []string{"只有日志"}})
	if len(lines) != 1 || resultLines != 0 || lines[0] != "只有日志" {
		t.Fatalf("lines = %q resultLines = %d", lines, resultLines)
	}
}

// TestWorkflowLayoutCacheKey 验证图布局缓存键：节点状态、显示名与选中项变化都
// 触发重排，状态不变时键稳定（面板每帧重绘不会重算布局）。
func TestWorkflowLayoutCacheKey(t *testing.T) {
	m := workflowPreview(t)
	w := m.SelectedWorkflow()
	key := workflowLayoutKey(w)
	if key == "" {
		t.Fatal("layout key must not be empty")
	}
	if again := workflowLayoutKey(w); again != key {
		t.Fatalf("layout key must be stable: %q vs %q", key, again)
	}
	m.SelectWorkflowNode(1)
	if moved := workflowLayoutKey(w); moved == key {
		t.Fatal("selection change must invalidate the layout key")
	}
	m.Active.ApplyWorkflowEvent(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindNode, TerminalID: "@temp/run/7", RunID: "@temp/run/7", NodeID: "merge", State: "done",
	})
	if done := workflowLayoutKey(w); done == key {
		t.Fatal("node state change must invalidate the layout key")
	}
}

// TestWorkflowCellColorFallback 验证色号到颜色的映射：默认色号（-1）与未知色号
// 都回退终端默认色，已知色号取当前主题的颜色。
func TestWorkflowCellColorFallback(t *testing.T) {
	palette := workflowPalette(renderer.DefaultTheme())
	if got := workflowCellColor(palette, wfColorDefault); got != renderer.ColorDefault {
		t.Fatalf("default index = %v, want ColorDefault", got)
	}
	if got := workflowCellColor(palette, 99); got != renderer.ColorDefault {
		t.Fatalf("unknown index = %v, want ColorDefault", got)
	}
	if got := workflowCellColor(palette, wfColorRunning); got != renderer.DefaultTheme().ToolRunning {
		t.Fatalf("running index = %v, want theme ToolRunning", got)
	}
}

// workflowWideGraph 构造一张明显比分栏宽的图：6 节点长链、中文节点名与状态。
// 图宽度远超预览框，左右平移才有内容可滚——宽字符错位只在能滚动的图上出现。
func workflowWideGraph(t *testing.T) *model.AppModel {
	t.Helper()
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`),
	})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	m.ShellSelected = 0
	ids := []string{"n1", "n2", "n3", "n4", "n5", "n6"}
	names := []string{"采集输入", "清洗数据", "汇总统计", "生成报告", "复核结论", "归档结果"}
	nodes := make(map[string]acp.WorkflowGraphNode, len(ids))
	edges := make(map[string][]string, len(ids))
	for i, id := range ids {
		nodes[id] = acp.WorkflowGraphNode{Name: names[i]}
		if i+1 < len(ids) {
			edges[id] = []string{ids[i+1]}
		}
	}
	apply := func(ev *acp.WorkflowEvent) {
		ev.TerminalID = "@temp/run/9"
		m.Active.ApplyWorkflowEvent(ev)
	}
	apply(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/9",
		Workflow: &acp.WorkflowInfo{Status: "running", CurrentNode: ids[0]},
		Graph:    &acp.WorkflowGraph{Nodes: nodes, Edges: edges, Start: ids[:1]},
	})
	for i, id := range ids {
		state := "wait"
		if i > 0 {
			state = "running"
		}
		apply(&acp.WorkflowEvent{Kind: acp.WorkflowKindNode, RunID: "@temp/run/9", NodeID: id, State: state})
	}
	return m
}

// paneColumns 返回缓冲一行按显示列铺开的文本：宽字符续列占位（Width=0）折叠成
// 空格，这样拼起来的字符串与终端上"一格一列"的观感一致，可以直接比列。
func paneColumns(b *renderer.Buffer, x, y, w int) []string {
	cols := make([]string, w)
	for i := 0; i < w; i++ {
		cell := b.Get(x+i, y)
		if cell.Width == 0 || cell.R == 0 {
			cols[i] = " "
			continue
		}
		cols[i] = string(cell.R)
	}
	return cols
}

// TestWorkflowGraphPanClipsColumns 验证图分栏的左右平移等价于"按列裁剪整张图"：
// 平移量落在哪一列都成立，包括正好落在宽字符的续列上、以及右边界切在宽字符中间。
// 做法是先把整张图完整画一次作为基准（分栏宽到不用裁剪），再逐个平移量比对分栏
// 内容与基准的对应列。旧实现按本地列宽表累加 x、跳过续列时不推列号，平移量落在
// 宽字符中间时该行会整行左移一格，这个用例就会失败。
func TestWorkflowGraphPanClipsColumns(t *testing.T) {
	useEnglish(t)
	m := workflowWideGraph(t)
	w := m.SelectedWorkflow()
	const (
		paneW = 44
		paneH = 16
	)
	rect := renderer.NewRect(0, 0, paneW, paneH)
	inner := renderer.NewRect(1, 1, paneW-2, paneH-2)
	panel := &ShellPanel{Theme: renderer.DefaultTheme()}

	graph := panel.workflowLayout(w)
	if graph == nil {
		t.Fatal("用例需要一张可布局的图")
	}
	grid := graph.Grid()
	full := workflowGridWidth(grid)
	if full <= inner.W {
		t.Fatalf("图必须比分栏宽才能平移：grid=%d 分栏内宽=%d", full, inner.W)
	}

	// 基准画面：分栏宽度取整图宽度，此时平移量会被收敛到 0，画出来就是完整的图。
	w.PanX, w.PanY = 0, 0
	ref := renderer.NewBuffer(full+2, paneH)
	panel.drawWorkflowGraph(renderer.NewCanvas(ref), renderer.NewRect(0, 0, full+2, paneH), w, m)
	refRows := make([][]renderer.Cell, inner.H)
	for row := 0; row < inner.H; row++ {
		refRows[row] = make([]renderer.Cell, full)
		for i := 0; i < full; i++ {
			refRows[row][i] = ref.Get(1+i, 1+row)
		}
	}

	// midGlyph 记录是否出现过"平移量正好落在宽字符续列上"的情形：这是旧实现
	// 整行错位的触发条件，用例必须覆盖到。
	midGlyph := false
	for panX := 0; panX <= full-inner.W; panX++ {
		w.PanX = panX
		b := renderer.NewBuffer(paneW, paneH)
		panel.drawWorkflowGraph(renderer.NewCanvas(b), rect, w, m)
		for row := 0; row < len(grid) && row < inner.H; row++ {
			for col := panX; col < len(grid[row]); col++ {
				if grid[row][col].Ch == 0 && col > 0 && grid[row][col-1].Ch != 0 {
					midGlyph = true
				}
			}
		}
		for row := 0; row < inner.H; row++ {
			want := make([]string, inner.W)
			for i := range want {
				j := panX + i
				switch {
				case j >= full:
					want[i] = " "
				case refRows[row][j].Width == 0:
					// 宽字符的右半列：分栏里只剩空格（整字落在窗口左边之外）。
					want[i] = " "
				case refRows[row][j].Width == 2 && j+1 >= panX+inner.W:
					// 右边界放不下整个宽字符：不画，避免续列压到分栏边框上。
					want[i] = " "
				default:
					want[i] = string(refRows[row][j].R)
				}
			}
			gotRow := strings.TrimRight(strings.Join(paneColumns(b, inner.X, inner.Y+row, inner.W), ""), " ")
			wantRow := strings.TrimRight(strings.Join(want, ""), " ")
			if gotRow != wantRow {
				t.Fatalf("PanX=%d 第 %d 行 = %q，应为基准画面的同列切片 %q", panX, row, gotRow, wantRow)
			}
		}
	}
	if !midGlyph {
		t.Fatal("用例必须覆盖平移量落在宽字符中间的情形")
	}
}
