package view

import (
	"encoding/json"
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

// workflowPreviewPanes 返回（图分栏, 日志分栏）在缓冲里的区域：与
// drawWorkflowPreview 的分栏规则一致（日志约占三分之一，其余给图）。
func workflowPreviewPanes(t *testing.T, b *renderer.Buffer, preview renderer.Rect) (renderer.Rect, renderer.Rect) {
	t.Helper()
	inner := renderer.NewRect(preview.X+1, preview.Y+1, preview.W-2, preview.H-2)
	content := renderer.NewRect(inner.X, inner.Y+1, inner.W, inner.H-1)
	if content.H < 1 || content.W < 1 {
		t.Fatalf("preview content too small: %#v", content)
	}
	logH := workflowLogPaneHeight(content.H)
	if logH == 0 {
		t.Fatalf("log pane must be drawn at %d rows", content.H)
	}
	return renderer.NewRect(content.X, content.Y, content.W, content.H-logH),
		renderer.NewRect(content.X, content.Y+content.H-logH, content.W, logH)
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

// TestWorkflowPreviewRendersGraphAndLogPanes 验证 workflow 终端的预览被替换成
// 上下分栏：上分栏为图（节点显示名）、下分栏为选中节点的终值结果与输出行；
// 两个分栏都不带焦点标记，并把分栏内尺寸写回模型（半屏平移/翻页步长要用）。
func TestWorkflowPreviewRendersGraphAndLogPanes(t *testing.T) {
	useEnglish(t)
	m := workflowPreview(t)
	b, preview := drawWorkflowPanel(t, m)
	graphRect, logRect := workflowPreviewPanes(t, b, preview)
	graphRows, logRows := regionRows(b, graphRect), regionRows(b, logRect)

	if !strings.Contains(strings.Join(graphRows, "\n"), "采集") {
		t.Fatalf("graph pane must show node labels: %q", graphRows)
	}
	if !strings.Contains(strings.Join(graphRows, "\n"), "汇总") {
		t.Fatalf("graph pane must show the second node: %q", graphRows)
	}
	// 日志分栏：标题带节点名，正文是 "→ 终值" 与输出行。
	if !strings.Contains(logRows[0], "采集") {
		t.Fatalf("log pane title must name the selected node: %q", logRows[0])
	}
	logText := strings.Join(logRows, "\n")
	for _, want := range []string{"→ 42", "第一行", "第二行"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log pane missing %q: %q", want, logRows)
		}
	}
	// 没有"焦点分栏"：两栏都不带 "▸" 标记，图与日志随时可操作。
	for name, rows := range map[string][]string{"graph": graphRows, "log": logRows} {
		if strings.Contains(rows[0], "▸") {
			t.Fatalf("%s pane must not carry a focus marker: %q", name, rows[0])
		}
	}
	// 绘制时把分栏内尺寸写回模型：hjkl 的半屏平移与 PgUp/PgDn 的翻页步长都要用。
	if m.ShellWorkflowCols != graphRect.W-2 || m.ShellWorkflowRows != graphRect.H-2 {
		t.Fatalf("graph pane inner size = %dx%d, want %dx%d",
			m.ShellWorkflowCols, m.ShellWorkflowRows, graphRect.W-2, graphRect.H-2)
	}
	if m.ShellWorkflowLogRows != logRect.H-2 {
		t.Fatalf("log pane inner height = %d, want %d", m.ShellWorkflowLogRows, logRect.H-2)
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

// TestWorkflowLogPaneHeight 验证日志分栏高度规则：预览太矮时只画图，
// 否则约占三分之一且至少 4 行（同时给图分栏留 3 行）。
func TestWorkflowLogPaneHeight(t *testing.T) {
	cases := map[int]int{0: 0, 5: 0, 6: 3, 12: 4, 20: 6, 21: 7}
	for height, want := range cases {
		if got := workflowLogPaneHeight(height); got != want {
			t.Fatalf("workflowLogPaneHeight(%d) = %d, want %d", height, got, want)
		}
	}
}

// TestWorkflowNodeOutputOrdersResultBeforeLogs 验证日志分栏内容：终值结果按行
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
