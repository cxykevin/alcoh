package model

import (
	"slices"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
)

// 本文件实现 workflow（dynworkflow）终端在 TUI 侧的状态：图、节点/agent 状态、
// 节点日志与画布游标。协议见 ../alkaid0/docs/acp/extension.md §3.2：
// 图与状态来自 workflow 快照通知（随终端查询/推送附带）与 update_* 增量事件，
// 完整快照（含事件日志）由 workflow/status 查询；这里把两者收敛到同一条应用路径
// （applyEvent），因此实时累积与 status 日志重放的结果一致。

// workflowNodeLogLines 是每个节点保留的日志行数上限：视图只显示尾部若干行，
// 更早的行被丢弃，避免长跑 workflow 的日志无限占用内存。
const workflowNodeLogLines = 200

// WorkflowPane 是图下方那一栏的页签（类似 notebook 的两个页面）：节点日志与该
// 节点启动的 Agent 列表，一次只显示一页，←→ 切换。
type WorkflowPane int

const (
	// WorkflowPaneLog 是节点日志页：终值结果 + 节点输出行。
	WorkflowPaneLog WorkflowPane = iota
	// WorkflowPaneAgents 是 Agent 列表页：该节点启动的每个 agent 的状态。
	WorkflowPaneAgents
)

// workflowPaneCount 是页签数量（切页时用于环绕）。
const workflowPaneCount = int(WorkflowPaneAgents) + 1

// WorkflowAgent 是节点内一次 Agent 调用的状态（agent 事件）。
type WorkflowAgent struct {
	Index   int
	Count   int    // 本次调用的 agent 总数（agents_start.count / agent.agentCount）
	State   string // waiting / running / success / failure
	Prompt  string
	Attempt int
}

// WorkflowNode 是图里一个节点的运行状态，由 graph / node_code / node /
// node_result / agents_start / agent / node_log 事件累积而成。
type WorkflowNode struct {
	ID     string
	Name   string // 显示名：graph.nodes[].name 优先，缺失时回退 node_code.name
	State  string // wait / queue / running / done / error / terminated（空 = 未运行）
	Cached bool
	Result string
	Agents []WorkflowAgent
	Logs   []string
}

// Running 报告节点是否正在运行。
func (n *WorkflowNode) Running() bool {
	return n != nil && n.State == "running"
}

// Finished 报告节点是否已结束（done / error / terminated）。
func (n *WorkflowNode) Finished() bool {
	if n == nil {
		return false
	}
	switch n.State {
	case "done", "error", "terminated":
		return true
	default:
		return false
	}
}

// Label 返回节点的展示名：显示名优先，缺失时回退节点 ID（函数名）。
func (n *WorkflowNode) Label() string {
	if n == nil {
		return ""
	}
	if name := strings.TrimSpace(n.Name); name != "" {
		return name
	}
	return n.ID
}

// RunningAgents 返回节点内处于运行中的 agent 数量。
func (n *WorkflowNode) RunningAgents() int {
	if n == nil {
		return 0
	}
	count := 0
	for _, a := range n.Agents {
		if a.State == "running" {
			count++
		}
	}
	return count
}

func (n *WorkflowNode) appendLog(line string) {
	n.Logs = append(n.Logs, line)
	if len(n.Logs) > workflowNodeLogLines {
		n.Logs = n.Logs[len(n.Logs)-workflowNodeLogLines:]
	}
}

// agent 返回节点内指定序号的 agent 状态，不存在时补齐。
func (n *WorkflowNode) agent(index int) *WorkflowAgent {
	if index < 0 {
		index = 0
	}
	for len(n.Agents) <= index {
		n.Agents = append(n.Agents, WorkflowAgent{Index: len(n.Agents), State: "waiting"})
	}
	agent := &n.Agents[index]
	agent.Index = index
	return agent
}

// WorkflowState 是一个 workflow 终端的图状态与视图游标（挂在 TerminalState 上）。
// 前一组字段来自协议快照/事件，后一组（Selected/PanX/PanY/Pane/LogScroll/
// AgentScroll）是用户在 shells 面板里的浏览位置：与终端预览的 Scroll 同类，
// 跨帧与跨刷新保留。
type WorkflowState struct {
	RunID        string
	FlowID       string // dynworkflow 的 flow id（事件 workflow 字段，缺省时服务端填 run id）
	WorkflowID   string // 服务端持久化记录里的 workflowId（当前等于 run id）
	Status       string
	CurrentNode  string
	CurrentAgent string
	// CurrentAgentState 是当前 agent 的状态（waiting / running / success / failure）。
	CurrentAgentState string
	LastSequence      int

	// Nodes 是节点状态表，OrderedNodes 按画布顺序返回；graph 是最近一次图快照。
	Nodes map[string]*WorkflowNode
	graph *acp.WorkflowGraph
	order []string

	// Selected 是当前选中节点的 ID（下栏两个页签显示它的内容）。
	Selected string
	// PanX / PanY 是图画布左上角在整张图里的偏移（列 / 行）：视图从该处开始
	// 取窗口，因此值越大看到的越靠右下。
	PanX int
	PanY int
	// LogScroll 是选中节点日志向上回看的行数（0 = 停在末尾）。
	LogScroll int
	// Pane 是下栏当前显示的页签（节点日志 / Agent 列表），←→ 切换。
	Pane WorkflowPane
	// AgentScroll 是 Agent 列表向上回看的行数（0 = 停在末尾）。
	AgentScroll int
}

// EnsureWorkflow 返回终端的 workflow 状态，尚未建立时创建空状态。
func (t *TerminalState) EnsureWorkflow() *WorkflowState {
	if t.Workflow == nil {
		t.Workflow = &WorkflowState{Nodes: map[string]*WorkflowNode{}}
	}
	if t.Workflow.Nodes == nil {
		t.Workflow.Nodes = map[string]*WorkflowNode{}
	}
	return t.Workflow
}

// IsWorkflow 报告终端是否是 dynworkflow 工作流（服务端 kind 为 workflow，
// 或已收到过 workflow 状态/事件）。
func (t *TerminalState) IsWorkflow() bool {
	if t == nil {
		return false
	}
	return t.Workflow != nil || strings.EqualFold(t.Kind, "workflow")
}

// Graph 返回最近一次的图快照（可能为 nil）。
func (w *WorkflowState) Graph() *acp.WorkflowGraph {
	if w == nil {
		return nil
	}
	return w.graph
}

// HasGraph 报告是否已收到含节点的图快照（workflow 启动阶段只有原始输出）。
func (w *WorkflowState) HasGraph() bool {
	if w == nil {
		return false
	}
	return !w.graph.Empty()
}

// Node 返回指定节点的状态；不存在的节点返回 nil。
func (w *WorkflowState) Node(id string) *WorkflowNode {
	if w == nil {
		return nil
	}
	return w.Nodes[id]
}

// OrderedNodes 按画布顺序返回节点状态（图的深度优先顺序，未连通的节点按 ID 附后）。
func (w *WorkflowState) OrderedNodes() []*WorkflowNode {
	if w == nil {
		return nil
	}
	out := make([]*WorkflowNode, 0, len(w.order))
	for _, id := range w.order {
		if n := w.Nodes[id]; n != nil {
			out = append(out, n)
		}
	}
	return out
}

// NodeIndex 返回节点在画布顺序中的下标；不存在时返回 -1。
func (w *WorkflowState) NodeIndex(id string) int {
	if w == nil {
		return -1
	}
	return slices.Index(w.order, id)
}

// SelectedNode 返回当前选中的节点状态。
func (w *WorkflowState) SelectedNode() *WorkflowNode {
	if w == nil {
		return nil
	}
	return w.Nodes[w.Selected]
}

// MoveSelection 按画布顺序移动选中节点（带环绕）；无节点时不动作。
func (w *WorkflowState) MoveSelection(delta int) {
	if w == nil || len(w.order) == 0 || delta == 0 {
		return
	}
	index := w.NodeIndex(w.Selected)
	if index < 0 {
		index = 0
	}
	next := (index + delta) % len(w.order)
	if next < 0 {
		next += len(w.order)
	}
	if w.order[next] == w.Selected {
		return
	}
	w.Selected = w.order[next]
	// 换节点后下栏的日志与 Agent 列表都回到最新输出（与切换终端时的预览行为一致）。
	w.LogScroll = 0
	w.AgentScroll = 0
}

// SwitchPane 切换下栏的页签（←→，带环绕）：节点日志 ⇄ Agent 列表。
func (w *WorkflowState) SwitchPane(delta int) {
	if w == nil || delta == 0 {
		return
	}
	pane := (int(w.Pane) + delta) % workflowPaneCount
	if pane < 0 {
		pane += workflowPaneCount
	}
	w.Pane = WorkflowPane(pane)
}

// ScrollPane 按下栏当前页签滚动内容（滚轮按行、PgUp/PgDn 按屏）：delta>0 表示
// 向上回看。两个页签各有自己的滚动偏移，来回切页不会互相影响。
func (w *WorkflowState) ScrollPane(delta int) {
	if w == nil || delta == 0 {
		return
	}
	if w.Pane == WorkflowPaneAgents {
		w.ScrollAgents(delta)
		return
	}
	w.ScrollLog(delta)
}

// ScrollAgents 向上（delta>0）或向下调整 Agent 列表的滚动偏移；
// 上限由渲染层按列表长度收敛。
func (w *WorkflowState) ScrollAgents(delta int) {
	if w == nil {
		return
	}
	w.AgentScroll += delta
	if w.AgentScroll < 0 {
		w.AgentScroll = 0
	}
}

// ScrollLog 向上（delta>0）或向下调整选中节点日志的滚动偏移；
// 上限由渲染层按日志长度收敛。
func (w *WorkflowState) ScrollLog(delta int) {
	if w == nil {
		return
	}
	w.LogScroll += delta
	if w.LogScroll < 0 {
		w.LogScroll = 0
	}
}

// reset 清空快照数据（图、节点、日志），保留浏览游标：重新查询/刷新不应把
// 用户的选区与平移重置掉。
func (w *WorkflowState) reset() {
	w.graph = nil
	w.Nodes = map[string]*WorkflowNode{}
	w.order = nil
	w.Status = ""
	w.CurrentNode = ""
	w.CurrentAgent = ""
}

// node 返回节点状态，不存在时按需创建；节点 ID 为空时返回 nil。
func (w *WorkflowState) node(id string) *WorkflowNode {
	if id == "" {
		return nil
	}
	if n, ok := w.Nodes[id]; ok {
		return n
	}
	n := &WorkflowNode{ID: id}
	if w.graph != nil {
		n.Name = strings.TrimSpace(w.graph.Nodes[id].Name)
	}
	w.Nodes[id] = n
	if !slices.Contains(w.order, id) {
		w.order = append(w.order, id)
	}
	return n
}

// setGraph 用图快照覆盖图与节点显示名，并按新图重排画布顺序。
func (w *WorkflowState) setGraph(graph *acp.WorkflowGraph) {
	if w == nil || graph == nil {
		return
	}
	w.graph = graph
	for id, node := range graph.Nodes {
		state := w.node(id)
		// 显示名以 graph.nodes[].name 为准（node_code.name 只是旧版回退）。
		if name := strings.TrimSpace(node.Name); name != "" {
			state.Name = name
		}
	}
	w.order = workflowNodeOrder(graph)
	// 事件可能先于 graph 到达（已建有图外节点）：按 ID 排序附在画布末尾，
	// 保证它们仍然可见且顺序稳定。
	var extra []string
	for id := range w.Nodes {
		if !slices.Contains(w.order, id) {
			extra = append(extra, id)
		}
	}
	if len(extra) > 0 {
		slices.Sort(extra)
		w.order = append(w.order, extra...)
	}
	w.syncSelection()
}

// syncSelection 在选区为空或指向已消失的节点时挑一个合理默认值：
// 当前节点优先，其次正在运行的节点，最后画布第一个节点。
// 用户已做过的选择不会被覆盖。
func (w *WorkflowState) syncSelection() {
	if w == nil {
		return
	}
	if w.Selected != "" && w.Nodes[w.Selected] != nil {
		return
	}
	w.Selected = ""
	if n := w.Nodes[w.CurrentNode]; n != nil {
		w.Selected = n.ID
		return
	}
	for _, id := range w.order {
		if n := w.Nodes[id]; n != nil && n.Running() {
			w.Selected = id
			return
		}
	}
	if len(w.order) > 0 {
		w.Selected = w.order[0]
	}
}

// workflowNodeOrder 返回图的画布顺序：从 graph.start 出发按边深度优先，
// 未连通的节点按 ID 排序附在其后。顺序稳定，同一张图每次渲染一致。
func workflowNodeOrder(graph *acp.WorkflowGraph) []string {
	if graph == nil {
		return nil
	}
	seen := make(map[string]bool, len(graph.Nodes))
	order := make([]string, 0, len(graph.Nodes))
	var visit func(id string)
	visit = func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		order = append(order, id)
		for _, next := range graph.Edges[id] {
			visit(next)
		}
	}
	starts := graph.Start
	if len(starts) == 0 {
		starts = make([]string, 0, len(graph.Nodes))
		for id := range graph.Nodes {
			starts = append(starts, id)
		}
		slices.Sort(starts)
	}
	for _, id := range starts {
		visit(id)
	}
	if len(order) < len(graph.Nodes) {
		rest := make([]string, 0, len(graph.Nodes)-len(order))
		for id := range graph.Nodes {
			if !seen[id] {
				rest = append(rest, id)
			}
		}
		slices.Sort(rest)
		order = append(order, rest...)
	}
	return order
}

// ApplyWorkflowEvent 应用一条 workflow 事件（实时推送，或 status 日志重放）。
// 事件按 runId/terminalId 归属终端；终端尚未出现在面板时按 workflow 终端建立条目，
// 随后的终端查询/推送会补齐命令等元数据。
func (s *SessionState) ApplyWorkflowEvent(ev *acp.WorkflowEvent) {
	if s == nil || ev == nil {
		return
	}
	id := ev.TerminalID
	if id == "" {
		id = ev.RunID
	}
	if id == "" {
		return
	}
	terminal := s.workflowTerminal(id)
	if terminal.Kind == "" {
		terminal.Kind = "workflow"
	}
	terminal.EnsureWorkflow().applyEvent(ev)
}

// ApplyWorkflowStatus 用 workflow/status 的完整结果重建 workflow 状态：先应用
// workflow / agentState / graph 快照，再按 sequence 升序重放事件日志。已结束或
// 不在内存中的 workflow 同样可重建（服务端从数据库返回），因此断线恢复、会话
// resume、刷新面板都不依赖实时推送。
func (s *SessionState) ApplyWorkflowStatus(result acp.WorkflowStatusResult) {
	if s == nil {
		return
	}
	id := result.TerminalID
	if id == "" {
		id = result.RunID
	}
	if id == "" {
		return
	}
	terminal := s.workflowTerminal(id)
	if terminal.Kind == "" {
		terminal.Kind = "workflow"
	}
	// status 是终端/Job 的实时状态（running / finished / killed），与终端状态同义。
	if result.Status != "" {
		terminal.Status = result.Status
	}
	w := terminal.EnsureWorkflow()
	w.reset()
	w.RunID = id
	w.applyInfo(result.Workflow)
	if result.AgentState != nil {
		w.applyAgentState(*result.AgentState)
	}
	if result.Graph != nil {
		w.setGraph(result.Graph)
	}
	logs := slices.Clone(result.Logs)
	slices.SortStableFunc(logs, func(a, b acp.WorkflowLogEntry) int { return a.Sequence - b.Sequence })
	for _, entry := range logs {
		applyWorkflowLogEntry(id, w, entry)
	}
	w.syncSelection()
}

// applyWorkflowLogEntry 重放一条持久化事件。payload 是结构化事件本身；
// 其缺省字段（nodeId / agentIndex）用日志条目上的值补齐。
func applyWorkflowLogEntry(runID string, w *WorkflowState, entry acp.WorkflowLogEntry) {
	ev, ok := acp.DecodeWorkflowPayload(runID, entry.Payload)
	if !ok {
		return
	}
	if ev.NodeID == "" {
		ev.NodeID = entry.NodeID
	}
	if ev.AgentIndex == 0 {
		ev.AgentIndex = entry.AgentIndex
	}
	w.applyEvent(ev)
}

// applyInfo 合并 workflow 元信息（status 响应与快照通知里的 workflow 对象同构）。
func (w *WorkflowState) applyInfo(info acp.WorkflowInfo) {
	if w == nil {
		return
	}
	if info.WorkflowID != "" {
		w.WorkflowID = info.WorkflowID
	}
	if info.RunID != "" {
		w.RunID = info.RunID
	}
	if info.Status != "" {
		w.Status = info.Status
	}
	if info.CurrentNode != "" {
		w.CurrentNode = info.CurrentNode
	}
	if info.CurrentAgent != "" {
		w.CurrentAgent = info.CurrentAgent
	}
	if info.LastSequence > 0 {
		w.LastSequence = info.LastSequence
	}
}

// applyAgentState 合并当前的 agent 状态快照。
func (w *WorkflowState) applyAgentState(state acp.WorkflowAgentState) {
	if w == nil {
		return
	}
	if state.NodeID != "" {
		w.CurrentNode = state.NodeID
	}
	if state.Type != "" {
		w.CurrentAgent = state.Type
	}
	if state.State != "" {
		w.CurrentAgentState = state.State
	}
}

// applyEvent 把一条事件合并进状态（所有来源共用）。
func (w *WorkflowState) applyEvent(ev *acp.WorkflowEvent) {
	if w == nil || ev == nil {
		return
	}
	if ev.RunID != "" && w.RunID == "" {
		w.RunID = ev.RunID
	}
	// 公共字段 workflow：dynworkflow 的 flow id；服务端在缺失时填 run id 兜底，
	// 那种情况下没有额外信息，不覆盖已有值。
	if ev.FlowID != "" && ev.FlowID != ev.RunID {
		w.FlowID = ev.FlowID
	}
	switch ev.Kind {
	case acp.WorkflowKindSnapshot:
		if ev.Workflow != nil {
			w.applyInfo(*ev.Workflow)
		}
		if ev.Graph != nil {
			w.setGraph(ev.Graph)
		}
		if ev.AgentState != nil {
			w.applyAgentState(*ev.AgentState)
		}
	case acp.WorkflowKindGraph:
		if ev.Graph != nil {
			w.setGraph(ev.Graph)
		}
	case acp.WorkflowKindNode:
		if n := w.node(ev.NodeID); n != nil {
			if ev.State != "" {
				n.State = ev.State
			}
			n.Cached = ev.Cached
		}
	case acp.WorkflowKindNodeCode:
		// 节点启动事件：显示名只在图还没给出名字时兜底（graph.name 优先）。
		if n := w.node(ev.NodeID); n != nil && strings.TrimSpace(n.Name) == "" {
			n.Name = strings.TrimSpace(ev.Name)
		}
	case acp.WorkflowKindNodeResult:
		if n := w.node(ev.NodeID); n != nil {
			n.Result = ev.Result
		}
	case acp.WorkflowKindAgentsStart:
		if n := w.node(ev.NodeID); n != nil {
			count := ev.Count
			if count < 0 {
				count = 0
			}
			agents := make([]WorkflowAgent, 0, count)
			for i := 0; i < count; i++ {
				prompt := ""
				if i < len(ev.Prompts) {
					prompt = ev.Prompts[i]
				}
				agents = append(agents, WorkflowAgent{Index: i, Count: count, State: "waiting", Prompt: prompt})
			}
			n.Agents = agents
		}
	case acp.WorkflowKindAgent:
		if n := w.node(ev.NodeID); n != nil {
			agent := n.agent(ev.AgentIndex)
			if ev.AgentCount > 0 {
				agent.Count = ev.AgentCount
			}
			if ev.State != "" {
				agent.State = ev.State
			}
			if ev.Prompt != "" {
				agent.Prompt = ev.Prompt
			}
			if ev.Attempt > 0 {
				agent.Attempt = ev.Attempt
			}
		}
	case acp.WorkflowKindNodeLog:
		if n := w.node(ev.NodeID); n != nil {
			n.appendLog(ev.Message)
		}
	default:
		// 未知事件类型（服务端不做白名单）与类型不符的载荷：忽略。
		return
	}
	w.syncSelection()
}

// workflowTerminal 返回终端（活动段优先，其次历史段）；两段都没有时新建一个活动
// 条目：workflow 事件可能早于 terminal/list 到达（如 resume 后的事件回放），
// 先建条目可以让面板立即可用，随后由终端查询补齐元数据。
func (s *SessionState) workflowTerminal(id string) *TerminalState {
	if t := s.terminals[id]; t != nil {
		return t
	}
	if t := s.terminalHistory[id]; t != nil {
		return t
	}
	terminal := NewTerminalState(id)
	terminal.Kind = "workflow"
	terminal.Status = "running"
	s.putTerminalActive(terminal)
	return terminal
}

// SelectedWorkflow 返回 shells 面板当前选中终端的 workflow 状态；不是 workflow
// 或未选中时返回 nil。
func (m *AppModel) SelectedWorkflow() *WorkflowState {
	s := m.SelectedShell()
	if s == nil {
		return nil
	}
	return s.Workflow
}

// SelectWorkflowNode 按画布顺序选择上/下一个节点（Tab / Shift+Tab）；选中终端不是
// workflow 时返回 false。
func (m *AppModel) SelectWorkflowNode(delta int) bool {
	w := m.SelectedWorkflow()
	if w == nil {
		return false
	}
	w.MoveSelection(delta)
	return true
}

// PanWorkflow 平移图画布：dirX / dirY 取 -1 / 0 / 1 表示方向（h ← 向左、l → 向右、
// k ↑ 向上、j ↓ 向下）。图通常比预览分栏大得多，所以一次走半个分栏——按几下就能
// 浏览完整张图，也不会因为步长过小而"按了像没反应"。
// 上/左边界在这里收敛到 0；右/下边界由渲染层按整张图的尺寸收敛。
func (m *AppModel) PanWorkflow(dirX, dirY int) bool {
	w := m.SelectedWorkflow()
	if w == nil || (dirX == 0 && dirY == 0) {
		return false
	}
	if dirX != 0 {
		w.PanX = clampWorkflowPan(w.PanX + dirX*workflowPanStep(m.ShellWorkflowCols))
	}
	if dirY != 0 {
		w.PanY = clampWorkflowPan(w.PanY + dirY*workflowPanStep(m.ShellWorkflowRows))
	}
	return true
}

// SwitchWorkflowPane 切换图下方栏的页签（←→）：节点日志 ⇄ Agent 列表；
// 选中终端不是 workflow 时返回 false。
func (m *AppModel) SwitchWorkflowPane(delta int) bool {
	w := m.SelectedWorkflow()
	if w == nil || delta == 0 {
		return false
	}
	w.SwitchPane(delta)
	return true
}

// ScrollWorkflowPane 按行滚动下栏当前页签的内容（滚轮等小步调整）：delta>0 表示
// 向上回看。选中终端不是 workflow 时返回 false。
func (m *AppModel) ScrollWorkflowPane(delta int) bool {
	w := m.SelectedWorkflow()
	if w == nil || delta == 0 {
		return false
	}
	w.ScrollPane(delta)
	return true
}

// ScrollWorkflowPanePage 翻一页下栏当前页签的内容（PgUp / PgDn）：dir>0 表示向上
// 回看。步长取下栏的内高（一屏），尺寸还没测到时退化为单行。
func (m *AppModel) ScrollWorkflowPanePage(dir int) bool {
	if dir == 0 {
		return false
	}
	step := m.ShellWorkflowLogRows - 1
	if step < 1 {
		step = 1
	}
	return m.ScrollWorkflowPane(dir * step)
}

// workflowPanStep 把分栏尺寸换算成一次平移的步长：半个分栏，至少 1 格。
func workflowPanStep(size int) int {
	if size <= 1 {
		return 1
	}
	return size / 2
}

// clampWorkflowPan 收敛平移偏移的上/左边界（0）。
func clampWorkflowPan(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
