package acp

import "encoding/json"

// Event 是 TUI 消费的类型化事件流项。
// 由 model.ApplyEvent 通过类型断言分发。
type Event interface{ isEvent() }

// MessageChunkEvent 对应 *message_chunk 通知（流式文本片段）。
type MessageChunkEvent struct {
	SessionID string
	MessageID string
	IsUser    bool
	IsThought bool
	Text      string
}

// MessageUpdateEvent 对应完整消息（整体替换/清空语义）。
type MessageUpdateEvent struct {
	SessionID string
	Message   Message
	IsUser    bool
	IsThought bool
}

// ToolCallUpdateEvent 对应 tool_call_update（upsert）。
type ToolCallUpdateEvent struct {
	SessionID     string
	ToolCallID    string
	Status        *ToolCallStatus
	Title         *string
	Kind          *ToolCallKind
	Content       []ToolCallContent
	Locations     []ToolCallLocation
	RawInput      json.RawMessage
	RawOutput     json.RawMessage
	ContentSet    bool
	ContentAppend bool // tool_call_content_chunk 追加既有内容。
}

// PlanUpdateEvent 对应 plan_update（entries 整体替换）。
type PlanUpdateEvent struct {
	SessionID string
	Plan      Plan
}

// PermissionRequestEvent 对应 session/request_permission。
type PermissionRequestEvent struct {
	SessionID string
	Request   PermissionRequest
}

// StateChangeEvent 对应 state_update。
type StateChangeEvent struct {
	SessionID  string
	State      SessionState
	StopReason *StopReason
	// Notice 携带 agent 私有错误扩展（alk.cxykevin.top/error_msg）；
	// 非空时由 model 以 system notice 呈现。
	Notice *string
}

// UsageUpdateEvent 对应 usage_update。
type UsageUpdateEvent struct {
	SessionID string
	Used      int
	Size      int
	Cost      *Cost
}

// SessionListEvent 是 session/list 结果回流。
type SessionListEvent struct {
	Sessions []*SessionInfo
}

// BackendErrorEvent 表示后端错误。
type BackendErrorEvent struct {
	Err error
}

// NewSessionEvent 表示会话建立完成。
type NewSessionEvent struct {
	Session Session
}

// UnknownSessionUpdateEvent 保留尚未识别的合法扩展，避免协议升级时静默丢失。
type UnknownSessionUpdateEvent struct {
	SessionID     string
	Discriminator string
	Raw           json.RawMessage
}

// TerminalUpdateEvent 保存 agent 终端状态与输出。Raw 保留全部未来字段。
type TerminalUpdateEvent struct {
	SessionID  string
	TerminalID string
	Title      string
	Status     string
	Output     string
	UpdateType string
	Terminals  []TerminalInfo
	// Terminal carries metadata supplied by an incremental update.
	Terminal TerminalInfo
	Command  string
	Raw      json.RawMessage
}

// AvailableCommand 是 agent 公布的 slash 命令。未识别字段保留在 Raw。
type AvailableCommand struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// CommandsUpdateEvent 保存 agent 当前可用命令列表。
type CommandsUpdateEvent struct {
	SessionID string
	Commands  []AvailableCommand
	Raw       json.RawMessage
}

// ConfigOptionValue 是 select 类型配置项的一个候选值。
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption 是 agent 广播的一项配置（ACP v2：configId/name/category/type/currentValue/options）。
// 未识别字段保留在 Raw。
type ConfigOption struct {
	ConfigID     string              `json:"configId"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type"` // "select" | "boolean" | ...
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options"`
	Raw          json.RawMessage     `json:"-"`
}

// ConfigOptionUpdateEvent 保存 agent 配置项更新；本版只做只读呈现，不伪造写回 RPC。
type ConfigOptionUpdateEvent struct {
	SessionID string
	Options   []ConfigOption
	Raw       json.RawMessage
}

// SessionInfoUpdateEvent 保存会话元数据更新。
type SessionInfoUpdateEvent struct {
	SessionID string
	Title     *string
	Model     *string
	CWD       *string
	UpdatedAt *string
	Raw       json.RawMessage
}

// ElicitationRequestEvent 对应 elicitation/create 请求。
type ElicitationRequestEvent struct {
	SessionID string
	RequestID RPCID
	Request   ElicitationCreateParams
}

func (*UnknownSessionUpdateEvent) isEvent() {}
func (*TerminalUpdateEvent) isEvent()       {}
func (*CommandsUpdateEvent) isEvent()       {}
func (*ConfigOptionUpdateEvent) isEvent()   {}
func (*SessionInfoUpdateEvent) isEvent()    {}
func (*MessageChunkEvent) isEvent()         {}
func (*MessageUpdateEvent) isEvent()        {}
func (*ToolCallUpdateEvent) isEvent()       {}
func (*PlanUpdateEvent) isEvent()           {}
func (*PermissionRequestEvent) isEvent()    {}
func (*StateChangeEvent) isEvent()          {}
func (*UsageUpdateEvent) isEvent()          {}
func (*SessionListEvent) isEvent()          {}
func (*BackendErrorEvent) isEvent()         {}
func (*NewSessionEvent) isEvent()           {}
func (*ElicitationRequestEvent) isEvent()   {}
