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
	// TerminalID 是 alkaid0 v0.6 的 alk.cxykevin.top/terminal_id：run 工具调用
	// 对应的终端 ID（与 run_id 同值），直播与历史回放都会携带。
	TerminalID string
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

// TerminalListEvent 携带 alkaid0 v0.5 terminal/list 的查询结果：当前活动终端。
// 前台 run（不带 background）不推送 terminal_update 的 start/running，
// 正在运行的终端只能靠这次查询列出来，因此客户端查询后广播该事件。
type TerminalListEvent struct {
	SessionID string
	Terminals []TerminalInfo
}

// TerminalHistoryEvent 携带 alkaid0 v0.6 terminal/history 的查询结果：
// 已结束终端（含服务端重启后仅剩持久化副本的条目）。它由客户端主动查询后
// 广播，走与 terminal_update 相同的事件通道，UI 无需为查询结果单独接线。
type TerminalHistoryEvent struct {
	SessionID string
	Terminals []TerminalInfo
}

// ShellStopEvent is the alkaid0 v0.5 notification emitted when a background shell exits.
// It is metadata only and must not become a user-visible conversation message.
type ShellStopEvent struct {
	SessionID  string
	RunID      string
	TerminalID string
	Command    string
	Status     string
	Success    bool
	Killed     bool
	Raw        json.RawMessage
}

// WorkflowStatusEvent 携带 workflow/status 的查询结果（由客户端主动查询后广播，与
// TerminalHistoryEvent 同构）。它含完整图、当前 agent 状态与有序事件日志：模型据此
// 重建 workflow 状态（日志按 sequence 重放），因此已结束的 workflow 也能显示
// 节点、agent 与日志，而不依赖断线前的实时推送。
type WorkflowStatusEvent struct {
	SessionID string
	Result    WorkflowStatusResult
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
func (*TerminalHistoryEvent) isEvent()      {}
func (*TerminalListEvent) isEvent()         {}
func (*ShellStopEvent) isEvent()            {}
func (*WorkflowEvent) isEvent()             {}
func (*WorkflowStatusEvent) isEvent()       {}
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
