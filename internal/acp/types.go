// Package acp 定义 ACP v2（Agent Client Protocol）客户端契约。
// 本轮只定义协议类型与接口（types/events/backend），收发逻辑在后续迭代实现。
// 字段命名与 ACP v2 schema 对齐（camelCase JSON）。
package acp

import (
	"encoding/json"
)

// ---- 枚举/判别值 ----

// SessionState 是会话前台工作状态。
type SessionState string

const (
	StateRunning        SessionState = "running"
	StateIdle           SessionState = "idle"
	StateRequiresAction SessionState = "requires_action"
	StateOther          SessionState = "other"
)

// StopReason 描述前台工作结束原因。
type StopReason string

const (
	StopEndTurn         StopReason = "end_turn"
	StopMaxTokens       StopReason = "max_tokens"
	StopMaxTurnRequests StopReason = "max_turn_requests"
	StopRefusal         StopReason = "refusal"
	StopCancelled       StopReason = "cancelled"
	StopOther           StopReason = "other"
)

// ToolCallStatus 是工具调用状态。
type ToolCallStatus string

const (
	ToolPending    ToolCallStatus = "pending"
	ToolStreaming  ToolCallStatus = "streaming" // 模型正在流式生成工具调用参数（增量快照，patch 覆盖）
	ToolInProgress ToolCallStatus = "in_progress"
	ToolCompleted  ToolCallStatus = "completed"
	ToolFailed     ToolCallStatus = "failed"
	ToolCancelled  ToolCallStatus = "cancelled"
	ToolOther      ToolCallStatus = "other"
)

// ToolCallKind 是工具类型。
type ToolCallKind string

const (
	KindRead       ToolCallKind = "read"
	KindEdit       ToolCallKind = "edit"
	KindWrite      ToolCallKind = "write"
	KindDelete     ToolCallKind = "delete"
	KindMove       ToolCallKind = "move"
	KindSearch     ToolCallKind = "search"
	KindExecute    ToolCallKind = "execute"
	KindThink      ToolCallKind = "think"
	KindFetch      ToolCallKind = "fetch"
	KindSwitchMode ToolCallKind = "switch_mode"
	KindOther      ToolCallKind = "other"
	KindUnknown    ToolCallKind = "unknown"
)

// PlanEntryStatus 是计划条目状态。
type PlanEntryStatus string

const (
	PlanPending    PlanEntryStatus = "pending"
	PlanInProgress PlanEntryStatus = "in_progress"
	PlanCompleted  PlanEntryStatus = "completed"
	PlanCancelled  PlanEntryStatus = "cancelled"
	PlanOther      PlanEntryStatus = "other"
)

// PlanEntryPriority 是计划条目优先级。
type PlanEntryPriority string

const (
	PriorityHigh   PlanEntryPriority = "high"
	PriorityMedium PlanEntryPriority = "medium"
	PriorityLow    PlanEntryPriority = "low"
	PriorityOther  PlanEntryPriority = "other"
)

// PermissionOptionKind 是权限选项类型。
type PermissionOptionKind string

const (
	AllowOnce    PermissionOptionKind = "allow_once"
	AllowAlways  PermissionOptionKind = "allow_always"
	RejectOnce   PermissionOptionKind = "reject_once"
	RejectAlways PermissionOptionKind = "reject_always"
	OptionOther  PermissionOptionKind = "other"
)

// PermissionOutcome 是权限请求的客户端响应结果。
type PermissionOutcome string

const (
	OutcomeSelected  PermissionOutcome = "selected"
	OutcomeCancelled PermissionOutcome = "cancelled"
	OutcomeOther     PermissionOutcome = "other"
)

// ---- 消息内容块 ----

// ContentBlock 是消息内容块（MCP 风格，type 判别）。
// text 块的文本字段名为 text（不是 content）。
type ContentBlock struct {
	Type     string          `json:"type"` // text|image|audio|resource|resource_link|other
	Text     *string         `json:"text,omitempty"`
	Data     *string         `json:"data,omitempty"` // image/audio base64
	MimeType *string         `json:"mimeType,omitempty"`
	Name     *string         `json:"name,omitempty"`
	URI      *string         `json:"uri,omitempty"`
	Title    *string         `json:"title,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

// Message 是一条消息（user/agent/thought 三者的共同形状）。
// content 采用 upsert/patch 语义：nil 且未指定=不变；空数组=清空；数组=整体替换。
type Message struct {
	MessageID  string         `json:"messageId"`
	Content    []ContentBlock `json:"content,omitempty"`
	ContentSet bool           `json:"-"` // 区分"未指定"与"空/替换"
}

// ---- session/update 变体 ----

// StateUpdate 是 state_update 的载荷。
type StateUpdate struct {
	State      SessionState `json:"state"`
	StopReason *StopReason  `json:"stopReason,omitempty"`
	// ErrorMsg 携带 agent 私有错误扩展（alk.cxykevin.top/error_msg），
	// 供 TUI 以 system notice 呈现，而不是吞掉失败原因。
	ErrorMsg *string `json:"alk.cxykevin.top/error_msg,omitempty"`
}

// ToolCallUpdate 是 tool_call_update 的载荷。
type ToolCallUpdate struct {
	ToolCallID string             `json:"toolCallId"`
	Title      *string            `json:"title,omitempty"`
	Kind       *ToolCallKind      `json:"kind,omitempty"`
	Status     *ToolCallStatus    `json:"status,omitempty"`
	Content    []ToolCallContent  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage    `json:"rawOutput,omitempty"`
}

// ToolCallContent 是工具输出内容项（type 判别）。
type ToolCallContent struct {
	Type    string          `json:"type"` // content|diff|terminal|other
	Content *ContentBlock   `json:"content,omitempty"`
	Text    *string         `json:"text,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

// ToolCallLocation 是工具操作的文件位置（follow-along）。
type ToolCallLocation struct {
	Path string  `json:"path"`
	Line *uint32 `json:"line,omitempty"`
}

// Plan 是 plan_update 的载荷（PlanItems 完整替换）。
type Plan struct {
	Type    string      `json:"type"` // "items"
	PlanID  string      `json:"planId"`
	Entries []PlanEntry `json:"entries"`
}

// PlanEntry 是计划条目。
type PlanEntry struct {
	Content  string            `json:"content"`
	Priority PlanEntryPriority `json:"priority"`
	Status   PlanEntryStatus   `json:"status"`
}

// Usage 是 usage_update 的载荷。
type Usage struct {
	Used int   `json:"used"`
	Size int   `json:"size"`
	Cost *Cost `json:"cost,omitempty"`
}

// Cost 是 token 成本。
type Cost struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// ---- 权限 ----

// PermissionSubject 是权限请求的对象（ACP v2：type + 可选 toolCall 嵌套）。
type PermissionSubject struct {
	Type     string          `json:"type"` // "tool_call"
	ToolCall *ToolCallInfo   `json:"toolCall,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

// UnmarshalJSON 保留未知 subject 扩展字段的完整原始 JSON。
func (s *PermissionSubject) UnmarshalJSON(data []byte) error {
	type subjectAlias PermissionSubject
	var value subjectAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = PermissionSubject(value)
	s.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ToolCallInfo 是权限 subject 中的工具调用摘要（ToolCallUpdate 子集）。
type ToolCallInfo struct {
	ToolCallID string            `json:"toolCallId"`
	Title      string            `json:"title,omitempty"`
	Kind       ToolCallKind      `json:"kind,omitempty"`
	Status     ToolCallStatus    `json:"status,omitempty"`
	Content    []ToolCallContent `json:"content,omitempty"`
}

// PermissionRequest 是 session/request_permission 的请求载荷。
type PermissionRequest struct {
	RequestID   string             `json:"-"`
	SessionID   string             `json:"sessionId"`
	Title       string             `json:"title"`
	Description *string            `json:"description,omitempty"`
	Options     []PermissionOption `json:"options"`
	Subject     *PermissionSubject `json:"subject,omitempty"`
}

// PermissionOption 是权限选项。
type PermissionOption struct {
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

// PermissionResponse 是权限请求的客户端响应。
type PermissionResponse struct {
	Outcome  PermissionOutcome `json:"outcome"`
	OptionID *string           `json:"optionId,omitempty"`
}

// ---- 会话 ----

// SessionInfo 描述一个会话（session/list 结果项）。
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ---- JSON-RPC 2.0 ----

// RPCRequest 是 JSON-RPC 请求。
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RPCID           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse 是 JSON-RPC 响应。
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RPCID           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError 是 JSON-RPC 错误。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RPCNotification 是 JSON-RPC 通知（无 id）。
type RPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// SessionUpdateEnvelope 是 session/update 通知的 params。
type SessionUpdateEnvelope struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}
