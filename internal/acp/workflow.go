package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// 本文件实现 alkaid0 v0.5 私有协议里的 workflow 部分（见 ../alkaid0/docs/acp/extension.md §3.2）：
// dynworkflow 的 Python run 会把图、节点与 agent 状态以 session/update 广播出来，
// 另有 workflow/status 查询完整（含事件日志）快照。

// workflow 相关的 sessionUpdate 判别值。
const (
	// UpdateWorkflowSnapshot 是终端"是 workflow"的快照通知（workflow/status 同构，但不含 logs）。
	UpdateWorkflowSnapshot = "alk.cxykevin.top/session/terminal/workflow/snapshot"
	// workflowUpdatePrefix 是增量事件前缀，其后是 dynworkflow 的事件类型（graph/node/node_result/...）。
	workflowUpdatePrefix = "alk.cxykevin.top/session/terminal/workflow/update_"
)

// WorkflowInfo 是 workflow 元信息。status 响应与快照通知里的 workflow 字段同构；
// name / currentAgent 是服务端预留字段，当前恒为空。
type WorkflowInfo struct {
	WorkflowID   string `json:"workflowId"`
	RunID        string `json:"runId"`
	TerminalID   string `json:"terminalId"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	CurrentNode  string `json:"currentNode"`
	CurrentAgent string `json:"currentAgent"`
	LastSequence int    `json:"lastSequence"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	ResultPath   string `json:"resultPath"`
}

// WorkflowGraphNode 是图里的一个节点；name 是 @flow.node("...") 的显示名。
type WorkflowGraphNode struct {
	Name string `json:"name"`
}

// WorkflowGraph 是完整的 workflow 图快照：nodes 是 id → 显示名，edges 是 id → 目标 id 列表，
// start 是起始节点 id 列表。
type WorkflowGraph struct {
	Nodes map[string]WorkflowGraphNode `json:"nodes"`
	Edges map[string][]string          `json:"edges"`
	Start []string                     `json:"start"`
}

// Empty 报告图里是否一个节点都没有（workflow 启动阶段尚未收到 graph）。
func (g *WorkflowGraph) Empty() bool {
	return g == nil || (len(g.Nodes) == 0 && len(g.Edges) == 0 && len(g.Start) == 0)
}

// UnmarshalJSON 兼容两种 graph 载荷：
//   - 扁平结构 {"nodes": …, "edges": …, "start": …}：增量事件（update_graph）里的形状；
//   - 嵌套信封 {"graph": {"nodes": …}, "time": …}：真实 alkaid0 的 workflow/status
//     直接把 dynworkflow 的 graph 事件整体塞进 graph 字段，于是多包了一层。
//
// 扁平解析不出节点时再尝试解包内层图，两种形状最终得到相同的图。
func (g *WorkflowGraph) UnmarshalJSON(data []byte) error {
	type flat WorkflowGraph // 新类型不带本方法，避免递归
	var direct flat
	if err := json.Unmarshal(data, &direct); err != nil {
		return err
	}
	parsed := WorkflowGraph(direct)
	if !parsed.Empty() {
		*g = parsed
		return nil
	}
	var envelope struct {
		Graph *flat `json:"graph"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Graph != nil {
		*g = WorkflowGraph(*envelope.Graph)
		return nil
	}
	*g = parsed
	return nil
}

// WorkflowAgentState 是快照里的"当前 agent"状态。
type WorkflowAgentState struct {
	Type   string `json:"type"`
	NodeID string `json:"nodeId"`
	State  string `json:"state"`
}

// WorkflowEventKind 是 workflow 事件类型（update_<type> 的 <type>）。
type WorkflowEventKind string

const (
	// WorkflowKindSnapshot 是快照通知（graph + workflow + agentState，不含日志）。
	WorkflowKindSnapshot WorkflowEventKind = "snapshot"
	// WorkflowKindGraph 是完整图快照。
	WorkflowKindGraph WorkflowEventKind = "graph"
	// WorkflowKindNode 是节点生命周期状态变化。
	WorkflowKindNode WorkflowEventKind = "node"
	// WorkflowKindNodeResult 是节点 Result(value) 终值。
	WorkflowKindNodeResult WorkflowEventKind = "node_result"
	// WorkflowKindAgentsStart 是节点即将启动 Agent/MultiAgent 调用。
	WorkflowKindAgentsStart WorkflowEventKind = "agents_start"
	// WorkflowKindAgent 是单个 Agent 的状态变化。
	WorkflowKindAgent WorkflowEventKind = "agent"
	// WorkflowKindNodeCode 是节点启动（携带显示名与源码）。
	WorkflowKindNodeCode WorkflowEventKind = "node_code"
	// WorkflowKindNodeLog 是节点线程 print 的一行输出。
	WorkflowKindNodeLog WorkflowEventKind = "node_log"
)

// WorkflowEvent 是解码后的 workflow 事件。不同 Kind 只填充各自的字段，
// 其余字段保持零值；未知 Kind 也会作为事件保留（客户端应忽略不认识的类型）。
// AgentIndex 是"从 1 开始"的序号（本次调用中的第几个 agent），0 表示事件没带该字段。
type WorkflowEvent struct {
	SessionID string
	Kind      WorkflowEventKind
	RunID     string
	// TerminalID 与 RunID 同值（统一为 @temp/run/<n>），快照里两个字段都发。
	TerminalID string
	// FlowID 是增量事件里的 workflow 字段（dynworkflow 的 flow id，服务端在缺失时填 run id）。
	FlowID string
	// Workflow / Graph / AgentState 只在快照事件里出现。
	Workflow   *WorkflowInfo
	Graph      *WorkflowGraph
	AgentState *WorkflowAgentState

	// node / agent / node_* 事件字段。
	NodeID     string
	State      string
	Cached     bool
	Args       json.RawMessage
	Result     string
	CallIndex  int
	Count      int
	Prompts    []string
	AgentIndex int
	AgentCount int
	Prompt     string
	Path       string
	Attempt    int
	Tools      json.RawMessage
	Name       string
	Code       json.RawMessage
	Message    string

	Raw json.RawMessage
}

// WorkflowLogEntry 是 workflow/status 返回的一条持久化事件。
// AgentIndex 与事件里的同名字段一致：从 1 开始的序号。
type WorkflowLogEntry struct {
	Sequence   int             `json:"sequence"`
	Type       string          `json:"type"`
	NodeID     string          `json:"nodeId"`
	AgentIndex int             `json:"agentIndex"`
	Payload    json.RawMessage `json:"payload"`
	Raw        json.RawMessage `json:"raw"`
	CreatedAt  string          `json:"createdAt"`
}

// WorkflowStatusResult 是 alk.cxykevin.top/session/terminal/workflow/status 的响应。
type WorkflowStatusResult struct {
	RunID      string              `json:"runId"`
	TerminalID string              `json:"terminalId"`
	Status     string              `json:"status"`
	Workflow   WorkflowInfo        `json:"workflow"`
	Graph      *WorkflowGraph      `json:"graph"`
	AgentState *WorkflowAgentState `json:"agentState"`
	Logs       []WorkflowLogEntry  `json:"logs"`
	Error      string              `json:"error"`
}

// decodeWorkflowSnapshot 解码 workflow 快照通知。
func decodeWorkflowSnapshot(sessionID string, raw json.RawMessage) (*WorkflowEvent, error) {
	var wire struct {
		RunID      string              `json:"runId"`
		TerminalID string              `json:"terminalId"`
		Workflow   WorkflowInfo        `json:"workflow"`
		Graph      *WorkflowGraph      `json:"graph"`
		AgentState *WorkflowAgentState `json:"agentState"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode workflow snapshot: %w", err)
	}
	ev := &WorkflowEvent{
		SessionID:  sessionID,
		Kind:       WorkflowKindSnapshot,
		RunID:      wire.RunID,
		TerminalID: wire.TerminalID,
		Workflow:   &wire.Workflow,
		Graph:      wire.Graph,
		AgentState: wire.AgentState,
		Raw:        append(json.RawMessage(nil), raw...),
	}
	if ev.TerminalID == "" {
		ev.TerminalID = ev.RunID
	}
	if ev.RunID == "" {
		ev.RunID = ev.TerminalID
	}
	return ev, nil
}

// decodeWorkflowUpdate 解码 update_<type> 增量事件（载荷字段直接放在 update 顶层）。
func decodeWorkflowUpdate(sessionID string, kind WorkflowEventKind, raw json.RawMessage) (*WorkflowEvent, error) {
	var payload struct {
		RunID      string          `json:"runId"`
		TerminalID string          `json:"terminalId"`
		FlowID     string          `json:"workflow"`
		Graph      *WorkflowGraph  `json:"graph"`
		NodeID     string          `json:"nodeId"`
		State      string          `json:"state"`
		Cached     bool            `json:"cached"`
		Args       json.RawMessage `json:"args"`
		Result     json.RawMessage `json:"result"`
		CallIndex  int             `json:"callIndex"`
		Count      int             `json:"count"`
		Prompts    []string        `json:"prompts"`
		AgentIndex int             `json:"agentIndex"`
		AgentCount int             `json:"agentCount"`
		Prompt     string          `json:"prompt"`
		Path       string          `json:"path"`
		Attempt    int             `json:"attempt"`
		Tools      json.RawMessage `json:"tools"`
		Name       string          `json:"name"`
		Code       json.RawMessage `json:"code"`
		Message    string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode workflow %s update: %w", kind, err)
	}
	ev := &WorkflowEvent{
		SessionID:  sessionID,
		Kind:       kind,
		RunID:      payload.RunID,
		TerminalID: payload.TerminalID,
		FlowID:     payload.FlowID,
		Graph:      payload.Graph,
		NodeID:     payload.NodeID,
		State:      payload.State,
		Cached:     payload.Cached,
		Args:       payload.Args,
		Result:     workflowResultText(payload.Result),
		CallIndex:  payload.CallIndex,
		Count:      payload.Count,
		Prompts:    payload.Prompts,
		AgentIndex: payload.AgentIndex,
		AgentCount: payload.AgentCount,
		Prompt:     payload.Prompt,
		Path:       payload.Path,
		Attempt:    payload.Attempt,
		Tools:      payload.Tools,
		Name:       payload.Name,
		Code:       payload.Code,
		Message:    payload.Message,
		Raw:        append(json.RawMessage(nil), raw...),
	}
	if ev.TerminalID == "" {
		ev.TerminalID = ev.RunID
	}
	return ev, nil
}

// workflowUpdateKind 报告 sessionUpdate 是否是 workflow 增量事件并返回其类型。
// 未知类型同样返回 true（客户端应忽略不认识的类型，不必当成协议诊断）。
func workflowUpdateKind(sessionUpdate string) (WorkflowEventKind, bool) {
	if !strings.HasPrefix(sessionUpdate, workflowUpdatePrefix) {
		return "", false
	}
	kind := strings.TrimPrefix(sessionUpdate, workflowUpdatePrefix)
	if kind == "" || kind == string(WorkflowKindSnapshot) {
		return "", false
	}
	return WorkflowEventKind(kind), true
}

// DecodeWorkflowPayload 解码一条 workflow 事件载荷（workflow/status 的 logs[].payload）。
// runID 用于在载荷缺少 runId 时补齐终端标识；载荷没有可识别的 type 时返回 false。
func DecodeWorkflowPayload(runID string, payload json.RawMessage) (*WorkflowEvent, bool) {
	var header struct {
		Type string `json:"type"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &header) != nil || header.Type == "" {
		return nil, false
	}
	ev, err := decodeWorkflowUpdate("", WorkflowEventKind(header.Type), payload)
	if err != nil {
		return nil, false
	}
	if ev.RunID == "" {
		ev.RunID = runID
	}
	if ev.TerminalID == "" {
		ev.TerminalID = runID
	}
	return ev, true
}

// workflowResultText 把 node_result 的 result 值转成展示文本：字符串原样返回，
// 其余 JSON 紧凑序列化（不可 JSON 序列化的值服务端已经转成字符串）。
func workflowResultText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
