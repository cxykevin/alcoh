package model

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// workflowModel 返回一个已进入会话、声明 v0.5 的模型：workflow 事件按会话归属，
// 声明缺失时模型会丢弃事件。
func workflowModel() *AppModel {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`),
	})
	m.ActivateSession("s", "session")
	return m
}

// workflowGraph 是测试用的图：collect → merge。
func workflowGraph() *acp.WorkflowGraph {
	return &acp.WorkflowGraph{
		Nodes: map[string]acp.WorkflowGraphNode{
			"collect": {Name: "采集"},
			"merge":   {Name: "汇总"},
		},
		Edges: map[string][]string{"collect": {"merge"}},
		Start: []string{"collect"},
	}
}

// applyWorkflow 把一条 workflow 事件推给模型（模拟 backend 事件通道）。
func applyWorkflow(m *AppModel, ev *acp.WorkflowEvent) {
	ev.SessionID = "s"
	m.ApplyEvent(ev)
}

// TestWorkflowSnapshotBuildsTerminalGraphAndSelection 验证快照通知建立 workflow
// 终端、图与选中节点：终端在面板里可见（kind=workflow），画布顺序取图的深度优先
// 顺序，选中节点默认为当前节点。
func TestWorkflowSnapshotBuildsTerminalGraphAndSelection(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind:       acp.WorkflowKindSnapshot,
		RunID:      "@temp/run/7",
		TerminalID: "@temp/run/7",
		Workflow:   &acp.WorkflowInfo{WorkflowID: "w1", Status: "running", CurrentNode: "collect", LastSequence: 4},
		Graph:      workflowGraph(),
		AgentState: &acp.WorkflowAgentState{Type: "agent", NodeID: "collect", State: "running"},
	})

	shells := m.ActiveShells()
	if len(shells) != 1 {
		t.Fatalf("active shells = %v, want 1 workflow terminal", terminalIDs(shells))
	}
	terminal := shells[0]
	if terminal.ID != "@temp/run/7" || terminal.Kind != "workflow" || !terminal.IsWorkflow() {
		t.Fatalf("terminal = %#v", terminal)
	}
	w := terminal.Workflow
	if w == nil || w.RunID != "@temp/run/7" || w.WorkflowID != "w1" || w.LastSequence != 4 {
		t.Fatalf("workflow state = %#v", w)
	}
	if !w.HasGraph() {
		t.Fatal("graph must be recorded")
	}
	// 画布顺序：collect → merge（图的深度优先），起点在 start 里给出。
	if got := workflowNodeIDs(w); len(got) != 2 || got[0] != "collect" || got[1] != "merge" {
		t.Fatalf("node order = %v", got)
	}
	if n := w.Node("merge"); n == nil || n.Name != "汇总" || n.Label() != "汇总" {
		t.Fatalf("merge node = %#v", n)
	}
	if w.NodeIndex("merge") != 1 || w.NodeIndex("missing") != -1 {
		t.Fatalf("node index: merge=%d missing=%d", w.NodeIndex("merge"), w.NodeIndex("missing"))
	}
	// 选中节点默认跟随当前节点（agentState / workflow.currentNode）。
	if w.Selected != "collect" || w.SelectedNode() == nil || w.SelectedNode().ID != "collect" {
		t.Fatalf("selected = %q", w.Selected)
	}
	if w.CurrentAgent != "agent" || w.CurrentAgentState != "running" {
		t.Fatalf("agent state = %#v", w)
	}
	if w.Status != "running" {
		t.Fatalf("status = %q", w.Status)
	}
}

// TestWorkflowIncrementalEventsAccumulate 验证增量事件累加到同一份状态：
// node / node_code / node_result / agents_start / agent / node_log 各字段落位。
func TestWorkflowIncrementalEventsAccumulate(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/7", TerminalID: "@temp/run/7",
		Workflow: &acp.WorkflowInfo{Status: "running", CurrentNode: "collect"},
		Graph:    workflowGraph(),
	})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNode, RunID: "@temp/run/7", NodeID: "collect", State: "running"})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNodeCode, RunID: "@temp/run/7", NodeID: "extra", Name: "新增节点"})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNodeResult, RunID: "@temp/run/7", NodeID: "merge", Result: "42"})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindAgentsStart, RunID: "@temp/run/7", NodeID: "collect", Count: 2, Prompts: []string{"甲", "乙"}})
	// agentIndex 从 1 开始（第二个 agent 成功、重试过一次）。
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindAgent, RunID: "@temp/run/7", NodeID: "collect", AgentIndex: 2, AgentCount: 2, State: "success", Attempt: 2})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNodeLog, RunID: "@temp/run/7", NodeID: "collect", Message: "第一行"})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNodeLog, RunID: "@temp/run/7", NodeID: "collect", Message: "第二行"})
	// 未知类型（服务端不做白名单）不影响已有状态。
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowEventKind("brand_new"), RunID: "@temp/run/7", NodeID: "collect"})

	w := m.SelectedWorkflow()
	if w == nil {
		t.Fatal("workflow state missing")
	}
	collect := w.Node("collect")
	if collect == nil || !collect.Running() || collect.Finished() {
		t.Fatalf("collect = %#v", collect)
	}
	if got := collect.Logs; len(got) != 2 || got[0] != "第一行" || got[1] != "第二行" {
		t.Fatalf("logs = %v", got)
	}
	if len(collect.Agents) != 2 || collect.Agents[1].State != "success" || collect.Agents[1].Attempt != 2 {
		t.Fatalf("agents = %#v", collect.Agents)
	}
	if collect.Agents[0].Prompt != "甲" || collect.Agents[0].Count != 2 {
		t.Fatalf("agents[0] = %#v", collect.Agents[0])
	}
	if collect.RunningAgents() != 0 {
		t.Fatalf("running agents = %d (only agent 1 succeeded)", collect.RunningAgents())
	}
	// node_code 的显示名只在图没给名字时作为回退：extra 不在图里，用 node_code.name。
	if extra := w.Node("extra"); extra == nil || extra.Label() != "新增节点" {
		t.Fatalf("extra = %#v", extra)
	}
	// 图外节点附在画布末尾，顺序稳定。
	if ids := workflowNodeIDs(w); len(ids) != 3 || ids[2] != "extra" {
		t.Fatalf("node order = %v", ids)
	}
	merge := w.Node("merge")
	if merge == nil || merge.Result != "42" || merge.Label() != "汇总" {
		t.Fatalf("merge = %#v", merge)
	}
	if merge.Finished() {
		t.Fatal("merge must not be finished without a node state event")
	}
}

// TestWorkflowAgentIndexIsOneBased 验证 agent 事件里的 agentIndex 是"从 1 开始"的
// 序号（dynworkflow 协议：本次调用中的第几个 agent）。按 0 起处理会让第 1 个 agent
// 的事件落到第二行：列表第 1 行永远停在"等待"且与第 2 行内容重复，还会多出一条
// 幻影行。缺省（0 = 事件没带该字段）按第一个 agent 处理。
func TestWorkflowAgentIndexIsOneBased(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/7", TerminalID: "@temp/run/7", Graph: workflowGraph(),
	})
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgentsStart, RunID: "@temp/run/7", NodeID: "collect", Count: 2, Prompts: []string{"甲", "乙"},
	})
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgent, RunID: "@temp/run/7", NodeID: "collect",
		AgentIndex: 1, AgentCount: 2, State: "running", Prompt: "甲",
	})
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgent, RunID: "@temp/run/7", NodeID: "collect",
		AgentIndex: 2, AgentCount: 2, State: "success", Prompt: "乙",
	})

	agents := m.SelectedWorkflow().Node("collect").Agents
	if len(agents) != 2 {
		t.Fatalf("agent 数 = %d（%#v）：agentIndex 从 1 起就不该补出幻影行", len(agents), agents)
	}
	if agents[0].State != "running" || agents[1].State != "success" {
		t.Fatalf("第 1/2 个 agent 状态 = %q/%q，want running/success", agents[0].State, agents[1].State)
	}
	if agents[0].Prompt != "甲" || agents[1].Prompt != "乙" {
		t.Fatalf("提示词与序号错位：%q / %q", agents[0].Prompt, agents[1].Prompt)
	}
	if agents[0].Index != 0 || agents[1].Index != 1 {
		t.Fatalf("内部下标 = %d/%d，want 0/1（显示时再 +1）", agents[0].Index, agents[1].Index)
	}
	if got := m.SelectedWorkflow().Node("collect").RunningAgents(); got != 1 {
		t.Fatalf("运行中的 agent = %d, want 1", got)
	}

	// 没有 agents_start 时，第 1 个 agent 的事件建立唯一的第一行（不补空的前导行）。
	solo := workflowModel()
	applyWorkflow(solo, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/8", TerminalID: "@temp/run/8", Graph: workflowGraph(),
	})
	applyWorkflow(solo, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgent, RunID: "@temp/run/8", NodeID: "collect",
		AgentIndex: 1, AgentCount: 1, State: "running",
	})
	if got := solo.SelectedWorkflow().Node("collect").Agents; len(got) != 1 || got[0].State != "running" {
		t.Fatalf("单个 agent = %#v", got)
	}
	// 缺省 agentIndex（0）同样落在第一个 agent 上。
	applyWorkflow(solo, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgent, RunID: "@temp/run/8", NodeID: "collect", State: "success",
	})
	if got := solo.SelectedWorkflow().Node("collect").Agents; len(got) != 1 || got[0].State != "success" {
		t.Fatalf("缺省序号的 agent 事件 = %#v", got)
	}
}

// TestWorkflowStatusReplaysAgentIndexFromLogEntry 验证 status 重放时 payload 没带
// agentIndex 的 agent 事件用日志条目上的序号（同样是 1 起）落位。
func TestWorkflowStatusReplaysAgentIndexFromLogEntry(t *testing.T) {
	m := workflowModel()
	m.ApplyEvent(&acp.WorkflowStatusEvent{SessionID: "s", Result: acp.WorkflowStatusResult{
		RunID: "@temp/run/9", Graph: workflowGraph(),
		Logs: []acp.WorkflowLogEntry{
			{Sequence: 1, Type: "agents_start", NodeID: "collect",
				Payload: json.RawMessage(`{"type":"agents_start","nodeId":"collect","count":2,"prompts":["甲","乙"]}`)},
			{Sequence: 2, Type: "agent", NodeID: "collect", AgentIndex: 2,
				Payload: json.RawMessage(`{"type":"agent","nodeId":"collect","state":"success"}`)},
		},
	}})

	agents := m.SelectedWorkflow().Node("collect").Agents
	if len(agents) != 2 {
		t.Fatalf("agent 数 = %d（%#v）", len(agents), agents)
	}
	if agents[1].State != "success" || agents[1].Prompt != "乙" {
		t.Fatalf("第 2 个 agent = %#v，日志条目上的序号没落位", agents[1])
	}
	if agents[0].State != "waiting" {
		t.Fatalf("第 1 个 agent = %#v，不该被第 2 个的事件改到", agents[0])
	}
}

// TestWorkflowNodeStateTransitions 验证节点状态机：done / error / terminated 视为
// 已结束，running 不算，缓存命中标记跟随事件更新。
func TestWorkflowNodeStateTransitions(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/7", TerminalID: "@temp/run/7", Graph: workflowGraph(),
	})
	w := m.SelectedWorkflow()
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNode, RunID: "@temp/run/7", NodeID: "collect", State: "done", Cached: true})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNode, RunID: "@temp/run/7", NodeID: "merge", State: "error"})
	collect, merge := w.Node("collect"), w.Node("merge")
	if !collect.Finished() || collect.Running() || !collect.Cached {
		t.Fatalf("collect = %#v", collect)
	}
	if !merge.Finished() || merge.Running() {
		t.Fatalf("merge = %#v", merge)
	}
}

// TestWorkflowStatusReplaysLogsInSequenceOrder 验证 workflow/status 结果按 sequence
// 升序重放（图、agent 状态与事件日志重建），因此断线恢复不依赖实时推送。
func TestWorkflowStatusReplaysLogsInSequenceOrder(t *testing.T) {
	m := workflowModel()
	m.ApplyEvent(&acp.WorkflowStatusEvent{SessionID: "s", Result: acp.WorkflowStatusResult{
		RunID: "@temp/run/9", Status: "running",
		Workflow: acp.WorkflowInfo{WorkflowID: "w9", Status: "running", CurrentNode: "collect"},
		Graph:    workflowGraph(),
		Logs: []acp.WorkflowLogEntry{
			{Sequence: 3, Type: "node_log", NodeID: "collect", Payload: json.RawMessage(`{"type":"node_log","nodeId":"collect","message":"第三行"}`)},
			{Sequence: 1, Type: "node_log", NodeID: "collect", Payload: json.RawMessage(`{"type":"node_log","nodeId":"collect","message":"第一行"}`)},
			{Sequence: 2, Type: "node_result", NodeID: "collect", Payload: json.RawMessage(`{"type":"node_result","nodeId":"collect","result":"42"}`)},
		},
	}})

	w := m.SelectedWorkflow()
	if w == nil {
		t.Fatal("workflow state missing")
	}
	collect := w.Node("collect")
	if collect == nil {
		t.Fatalf("collect missing: %v", workflowNodeIDs(w))
	}
	if got := collect.Logs; len(got) != 2 || got[0] != "第一行" || got[1] != "第三行" {
		t.Fatalf("logs = %v (want sequence order)", got)
	}
	if collect.Result != "42" {
		t.Fatalf("result = %q", collect.Result)
	}
	if w.WorkflowID != "w9" || w.Selected != "collect" {
		t.Fatalf("workflow state = %#v", w)
	}
	// 终端仍属于活动段（状态查询不改变分段），kind 为 workflow。
	shells := m.ActiveShells()
	if len(shells) != 1 || shells[0].ID != "@temp/run/9" || shells[0].Status != "running" {
		t.Fatalf("shells = %#v", shells)
	}
}

// TestWorkflowStatusKeepsCursorAndDropsStaleNodes 验证刷新（重新查询完整快照）：
// 用户的选中节点 / 平移 / 日志滚动保留，节点表按新快照重建（旧日志不残留）；
// 选中节点在新图里消失时回退到合理默认值。
func TestWorkflowStatusKeepsCursorAndDropsStaleNodes(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/9", TerminalID: "@temp/run/9",
		Workflow: &acp.WorkflowInfo{CurrentNode: "collect"}, Graph: workflowGraph(),
	})
	applyWorkflow(m, &acp.WorkflowEvent{Kind: acp.WorkflowKindNodeLog, RunID: "@temp/run/9", NodeID: "collect", Message: "旧的"})
	w := m.SelectedWorkflow()
	// 用户浏览到 merge 节点，并平移画布、回看日志。
	w.Selected = "merge"
	w.PanX, w.PanY, w.LogScroll = 3, 1, 2

	m.ApplyEvent(&acp.WorkflowStatusEvent{SessionID: "s", Result: acp.WorkflowStatusResult{
		RunID: "@temp/run/9", Status: "running",
		Graph: workflowGraph(),
		Logs: []acp.WorkflowLogEntry{
			{Sequence: 1, Type: "node_log", NodeID: "merge", Payload: json.RawMessage(`{"type":"node_log","nodeId":"merge","message":"新的"}`)},
		},
	}})
	if w.Selected != "merge" || w.PanX != 3 || w.PanY != 1 || w.LogScroll != 2 {
		t.Fatalf("cursor reset by refresh: %#v", w)
	}
	if got := w.Node("merge").Logs; len(got) != 1 || got[0] != "新的" {
		t.Fatalf("merge logs = %v", got)
	}
	if got := w.Node("collect").Logs; len(got) != 0 {
		t.Fatalf("collect logs must be dropped on refresh, got %v", got)
	}

	// 新一轮快照里只剩 merge：选中节点消失，回退到画布第一个节点。
	m.ApplyEvent(&acp.WorkflowStatusEvent{SessionID: "s", Result: acp.WorkflowStatusResult{
		RunID: "@temp/run/9",
		Graph: &acp.WorkflowGraph{
			Nodes: map[string]acp.WorkflowGraphNode{"merge": {Name: "汇总"}},
			Start: []string{"merge"},
		},
	}})
	if w.Selected != "merge" {
		t.Fatalf("selected = %q, want fallback to the only node", w.Selected)
	}
	if w.Node("collect") != nil {
		t.Fatalf("stale node kept: %v", workflowNodeIDs(w))
	}
}

// TestWorkflowCursorControls 验证面板按键对应的游标操作：hjkl 按半屏平移画布、
// PgUp/PgDn 翻一页下栏当前页签的内容、Tab 换节点（环绕）并在换节点后把下栏
// 回到最新；平移与滚动都不为负。
func TestWorkflowCursorControls(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/7", TerminalID: "@temp/run/7", Graph: workflowGraph(),
	})
	w := m.SelectedWorkflow()

	// 画布平移步长取分栏内尺寸的一半（视图绘制时写回模型）。
	m.ShellWorkflowCols, m.ShellWorkflowRows = 40, 20
	if !m.PanWorkflow(1, 0) || w.PanX != 20 {
		t.Fatalf("pan right must move half a pane: panX = %d, want 20", w.PanX)
	}
	if !m.PanWorkflow(0, 1) || w.PanY != 10 {
		t.Fatalf("pan down must move half a pane: panY = %d, want 10", w.PanY)
	}
	if !m.PanWorkflow(-1, 0) || w.PanX != 0 {
		t.Fatalf("panX must return to the left edge, got %d", w.PanX)
	}
	if !m.PanWorkflow(-1, -1) || w.PanX != 0 || w.PanY != 0 {
		t.Fatalf("pan must clamp at 0: panX=%d panY=%d", w.PanX, w.PanY)
	}
	// 还没有画过一帧（分栏尺寸未知）时步长退化为 1 格，不会"按了不动"。
	m.ShellWorkflowCols, m.ShellWorkflowRows = 0, 0
	if !m.PanWorkflow(1, 1) || w.PanX != 1 || w.PanY != 1 {
		t.Fatalf("unknown pane size must still move one cell: panX=%d panY=%d", w.PanX, w.PanY)
	}
	w.PanX, w.PanY = 0, 0

	// PgUp/PgDn 翻一页日志：步长取下栏内高留一行重叠；向上不为负。
	// 下栏默认页是 Agent 列表，这里先切到日志页再验日志翻页。
	w.Pane = WorkflowPaneLog
	m.ShellWorkflowLogRows = 8
	if !m.ScrollWorkflowPanePage(1) || w.LogScroll != 7 {
		t.Fatalf("log page scroll = %d, want 7", w.LogScroll)
	}
	// 滚轮按行微调。
	if !m.ScrollWorkflowPane(3) || w.LogScroll != 10 {
		t.Fatalf("log line scroll = %d, want 10", w.LogScroll)
	}
	m.ScrollWorkflowPanePage(-10)
	if w.LogScroll != 0 {
		t.Fatalf("log scroll must clamp at 0, got %d", w.LogScroll)
	}
	// 下栏尺寸未知时退化为单行，不会静默不动作。
	m.ShellWorkflowLogRows = 0
	if !m.ScrollWorkflowPanePage(1) || w.LogScroll != 1 {
		t.Fatalf("unknown log pane size must scroll one line, got %d", w.LogScroll)
	}
	w.LogScroll = 0

	// 节点选择环绕，并在换节点时把下栏两个页签的滚动都回到最新。
	w.LogScroll, w.AgentScroll = 5, 4
	if !m.SelectWorkflowNode(1) || w.Selected != "merge" || w.LogScroll != 0 || w.AgentScroll != 0 {
		t.Fatalf("selection = %q logScroll = %d agentScroll = %d", w.Selected, w.LogScroll, w.AgentScroll)
	}
	if !m.SelectWorkflowNode(-1) || w.Selected != "collect" {
		t.Fatalf("selection = %q", w.Selected)
	}
	// 只有一个节点时环绕回自身，不报错。
	m.ApplyEvent(&acp.WorkflowStatusEvent{SessionID: "s", Result: acp.WorkflowStatusResult{
		RunID: "@temp/run/7", Graph: &acp.WorkflowGraph{Nodes: map[string]acp.WorkflowGraphNode{"solo": {}}, Start: []string{"solo"}},
	}})
	m.SelectWorkflowNode(1)
	if m.SelectedWorkflow().Selected != "solo" {
		t.Fatalf("selected = %q", m.SelectedWorkflow().Selected)
	}
}

// TestWorkflowPaneTabsAndAgentScroll 验证下栏的两个页签：默认停在 Agent 列表页，
// ←/→ 在 Agent 列表与节点日志间环绕切换，两个页签各自记住滚动位置，翻页/滚轮只
// 作用在当前页签，换节点时两个页签都回到最新。
func TestWorkflowPaneTabsAndAgentScroll(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/7", TerminalID: "@temp/run/7", Graph: workflowGraph(),
	})
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindAgentsStart, RunID: "@temp/run/7", NodeID: "collect", Count: 3, Prompts: []string{"甲", "乙", "丙"},
	})
	w := m.SelectedWorkflow()
	// 默认页是 Agent 列表（列表在前）：打开面板先看到该节点启动了哪些 agent。
	if w.Pane != WorkflowPaneAgents {
		t.Fatalf("pane = %v, want the agent page", w.Pane)
	}
	// 右到底再右一次环绕回 Agent 列表页，左键同理反向环绕。
	if !m.SwitchWorkflowPane(1) || w.Pane != WorkflowPaneLog {
		t.Fatalf("pane = %v, want the log page", w.Pane)
	}
	if !m.SwitchWorkflowPane(1) || w.Pane != WorkflowPaneAgents {
		t.Fatalf("pane must wrap forward to agents, got %v", w.Pane)
	}
	if !m.SwitchWorkflowPane(-1) || w.Pane != WorkflowPaneLog {
		t.Fatalf("pane must wrap backward to logs, got %v", w.Pane)
	}
	if !m.SwitchWorkflowPane(-1) || w.Pane != WorkflowPaneAgents {
		t.Fatalf("pane must wrap backward to agents, got %v", w.Pane)
	}

	// Agent 页上翻页/滚轮只动 Agent 列表，日志偏移保持原样。
	m.ShellWorkflowLogRows = 8
	w.LogScroll = 3
	if !m.ScrollWorkflowPanePage(-1) || w.AgentScroll != 0 || w.LogScroll != 3 {
		t.Fatalf("agent pane must clamp at 0: agentScroll=%d logScroll=%d", w.AgentScroll, w.LogScroll)
	}
	if !m.ScrollWorkflowPanePage(1) || w.AgentScroll != 7 || w.LogScroll != 3 {
		t.Fatalf("agent page scroll = %d logScroll = %d", w.AgentScroll, w.LogScroll)
	}
	if !m.ScrollWorkflowPane(2) || w.AgentScroll != 9 {
		t.Fatalf("agent line scroll = %d", w.AgentScroll)
	}
	// 切回日志页后同样的按键落在日志上。
	if !m.SwitchWorkflowPane(-1) || w.Pane != WorkflowPaneLog {
		t.Fatalf("pane = %v, want back on logs", w.Pane)
	}
	if !m.ScrollWorkflowPane(4) || w.LogScroll != 7 || w.AgentScroll != 9 {
		t.Fatalf("log scroll = %d agentScroll = %d", w.LogScroll, w.AgentScroll)
	}
	// 换节点：图下方栏整体回到最新状态。
	if !m.SelectWorkflowNode(1) || w.LogScroll != 0 || w.AgentScroll != 0 {
		t.Fatalf("node switch must reset both pages: log=%d agents=%d", w.LogScroll, w.AgentScroll)
	}
}

// TestWorkflowPreviewResetOnTerminalSwitch 验证切走再切回 workflow 终端时预览
// 回到起点：画布平移与下栏两个页签的滚动都清零，选中节点与当前页签保留
// （便于在列表里来回比较同一个节点）。
func TestWorkflowPreviewResetOnTerminalSwitch(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/7", TerminalID: "@temp/run/7", Graph: workflowGraph(),
	})
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/1", Kind: "shell", Command: "make", Status: "running"})
	m.ShellPanel = true
	// 普通 shell 后并入，排在活动段最前：选中第 1 项才是 workflow 终端。
	m.ShellSelected = 1
	w := m.SelectedWorkflow()
	if w == nil {
		t.Fatal("workflow terminal must be selected")
	}
	w.PanX, w.PanY, w.LogScroll, w.AgentScroll = 3, 2, 5, 4
	w.Pane = WorkflowPaneAgents

	// 先在普通 shell 上停留，再切回 workflow 终端。
	m.ShellSelected = 0
	if m.SelectedWorkflow() != nil {
		t.Fatal("plain shell must not expose workflow state")
	}
	m.ShellSelected = 1
	m.ResetShellPreviewScroll()
	if w.PanX != 0 || w.PanY != 0 || w.LogScroll != 0 || w.AgentScroll != 0 {
		t.Fatalf("switch must reset the preview: pan=%d,%d log=%d agents=%d", w.PanX, w.PanY, w.LogScroll, w.AgentScroll)
	}
	if w.Selected != "collect" || w.Pane != WorkflowPaneAgents {
		t.Fatalf("selection and tab must survive a switch: selected=%q pane=%v", w.Selected, w.Pane)
	}
}

// TestWorkflowControlsIgnorePlainTerminals 验证非 workflow 终端不受分栏按键影响：
// 面板里选中普通 shell 时 workflow 操作一律返回 false。
func TestWorkflowControlsIgnorePlainTerminals(t *testing.T) {
	m := workflowModel()
	m.ApplyEvent(&acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, SessionID: "s", RunID: "@temp/run/7", TerminalID: "@temp/run/7", Graph: workflowGraph(),
	})
	// 普通 shell 后并入，排在活动段最前（活动段"新的在上"），因此选中第 0 项。
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/1", Kind: "shell", Command: "make", Status: "running"})
	m.ShellPanel = true
	m.ShellSelected = 0
	if sel := m.SelectedShell(); sel == nil || sel.ID != "@temp/run/1" {
		t.Fatalf("selected shell = %#v", sel)
	}
	if m.SelectedWorkflow() != nil {
		t.Fatal("plain shell must not expose workflow state")
	}
	if m.SelectWorkflowNode(1) || m.PanWorkflow(1, 1) || m.ScrollWorkflowPane(3) ||
		m.ScrollWorkflowPanePage(1) || m.SwitchWorkflowPane(1) {
		t.Fatal("workflow controls must be ignored for plain shells")
	}
	// 普通 shell 的 IsWorkflow 为 false（避免面板误走 workflow 渲染）。
	if m.Active.Terminal("@temp/run/1").IsWorkflow() {
		t.Fatal("shell terminal must not report IsWorkflow")
	}
}

// TestWorkflowNodeLogLimit 验证长跑 workflow 的日志上限：只保留最后
// workflowNodeLogLines 行，避免内存无界增长。
func TestWorkflowNodeLogLimit(t *testing.T) {
	m := workflowModel()
	applyWorkflow(m, &acp.WorkflowEvent{
		Kind: acp.WorkflowKindSnapshot, RunID: "@temp/run/8", TerminalID: "@temp/run/8", Graph: workflowGraph(),
	})
	total := workflowNodeLogLines + 50
	for i := 0; i < total; i++ {
		applyWorkflow(m, &acp.WorkflowEvent{
			Kind: acp.WorkflowKindNodeLog, RunID: "@temp/run/8", NodeID: "collect",
			Message: "line-" + strconv.Itoa(i),
		})
	}
	logs := m.SelectedWorkflow().Node("collect").Logs
	if len(logs) != workflowNodeLogLines {
		t.Fatalf("logs = %d, want %d", len(logs), workflowNodeLogLines)
	}
	if want := "line-50"; logs[0] != want {
		t.Fatalf("oldest kept log = %q, want %q", logs[0], want)
	}
	if want := "line-" + strconv.Itoa(total-1); logs[len(logs)-1] != want {
		t.Fatalf("newest log = %q, want %q", logs[len(logs)-1], want)
	}
}

// workflowNodeIDs 返回画布顺序里的节点 ID。
func workflowNodeIDs(w *WorkflowState) []string {
	nodes := w.OrderedNodes()
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}
