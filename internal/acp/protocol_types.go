package acp

import (
	"encoding/json"
	"fmt"
)

const (
	MethodInitialize          = "initialize"
	MethodInitialized         = "initialized"
	MethodSessionList         = "session/list"
	MethodSessionNew          = "session/new"
	MethodSessionResume       = "session/resume"
	MethodSessionPrompt       = "session/prompt"
	MethodSessionCancel       = "session/cancel"
	MethodSessionUpdate       = "session/update"
	MethodSessionSetConfig    = "session/set_config_option"
	MethodSessionDelete       = "session/delete"
	MethodRequestPermission   = "session/request_permission"
	MethodElicitationCreate   = "elicitation/create"
	MethodElicitationComplete = "elicitation/complete"

	// Alkaid0CapabilityV04 是 alkaid0 扩展协议版本能力标记。服务端在 initialize
	// 的 capabilities 中声明该键才表示支持 alkaid0 扩展方法（如 config/get、config/set）。
	// 见 docs/acp/extension.md。
	Alkaid0CapabilityV04 = "alk.cxykevin.top/alkaid0/v0.4"
	// Alkaid0CapabilityV05 gates the private terminal protocol.
	Alkaid0CapabilityV05 = "alk.cxykevin.top/alkaid0/v0.5"
	// Alkaid0CapabilityMsgQueue 是会话消息队列能力标记（alk.cxykevin.top/msgqueue）。
	// 客户端必须在 initialize 的 capabilities 顶层显式声明该键，服务端才会向该
	// 连接推送队列事件（见 ../alkaid0/docs/acp/extension.md §2.8）。服务端侧该
	// 能力与 v0.6 同期实现，客户端只在服务端同时声明 v0.6 与 msgqueue 时启用。
	Alkaid0CapabilityMsgQueue = "alk.cxykevin.top/msgqueue"

	// Alkaid0CapabilityV06 是已结束终端内容查询能力（terminal/history）。
	// 终端 ID 与 run id 统一为 @temp/run/<n>，终端内容持久化在该路径下，
	// 因此客户端可凭 terminalId 取回已结束终端（含服务端重启前结束的）。
	Alkaid0CapabilityV06 = "alk.cxykevin.top/alkaid0/v0.6"

	// Alkaid0CapabilityCron 是会话定时任务查询/删除能力标记
	// （alk.cxykevin.top/cron，见 ../alkaid0/docs/acp/extension.md §3.11）。
	// 纯请求-响应、无推送；/cron 面板据此门控。
	Alkaid0CapabilityCron = "alk.cxykevin.top/cron"

	// MethodConfigGet 是 alkaid0 扩展方法：获取完整服务端配置。见 docs/acp/extension.md。
	MethodConfigGet = "alk.cxykevin.top/config/get"

	// MethodConfigSet 是 alkaid0 扩展方法：部分更新服务端配置并自动持久化。
	// 见 docs/acp/extension.md。
	MethodConfigSet      = "alk.cxykevin.top/config/set"
	MethodTerminalList   = "alk.cxykevin.top/session/terminal/list"
	MethodTerminalStatus = "alk.cxykevin.top/session/terminal/status"
	MethodTerminalStop   = "alk.cxykevin.top/session/terminal/stop"
	// MethodTerminalHistory 查询已结束终端的内容（alkaid0 v0.6）。
	MethodTerminalHistory = "alk.cxykevin.top/session/terminal/history"
	// MethodWorkflowStatus 查询 workflow 的完整快照与事件日志（alkaid0 v0.5）。
	MethodWorkflowStatus = "alk.cxykevin.top/session/terminal/workflow/status"

	// 会话消息队列方法（alk.cxykevin.top/msgqueue，见 extension.md §3.10）。
	MethodMsgQueueList   = "alk.cxykevin.top/session/msgqueue/list"
	MethodMsgQueueCancel = "alk.cxykevin.top/session/msgqueue/cancel"
	MethodMsgQueueUpdate = "alk.cxykevin.top/session/msgqueue/update"
	MethodMsgQueueClear  = "alk.cxykevin.top/session/msgqueue/clear"

	// 会话定时任务方法（alk.cxykevin.top/cron，见 extension.md §3.11）：
	// 纯请求-响应，无推送；新增/修改仍由 AI 的 cron 工具负责。
	MethodCronList   = "alk.cxykevin.top/session/cron/list"
	MethodCronDelete = "alk.cxykevin.top/session/cron/delete"

	// SessionCursorField 是 alkaid0 v0.6 历史回放分页的扩展字段名。它出现在
	// session/resume 响应里表示还有更早的消息未回放；把它原样作为下一次
	// session/resume 请求的顶层参数传入即可继续取更早的一页（见 extension.md §3.3）。
	SessionCursorField = "alk.cxykevin.top/session/cursor"
)

// ConfigGetResult 是 alk.cxykevin.top/config/get 的响应。Config 为完整的
// 全局配置 JSON 对象，字段名即服务端 Go 结构体硬编码的名称。
type ConfigGetResult struct {
	Config json.RawMessage `json:"config"`
}

// ConfigSetParams 是 alk.cxykevin.top/config/set 的请求参数。Config 接受完整或
// 部分的配置 JSON；支持深层嵌套的部分更新（如 Model.DefaultModelID），
// 未显式指定的字段保持现有值不变。
type ConfigSetParams struct {
	Config json.RawMessage `json:"config"`
}

// MsgQueueItem 是会话消息队列中的一项（alk.cxykevin.top/msgqueue，见
// extension.md §2.8）。prompt 项是待发送的普通消息；command 项是参与 loop 的
// 斜杠命令，出队时才执行。
type MsgQueueItem struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // prompt | command
	Command string `json:"command,omitempty"`
	Args    string `json:"args,omitempty"`
	Text    string `json:"text"`
	// CreatedAt 是 RFC3339 时间；为空的项来自旧服务端/持久化副本。
	CreatedAt string `json:"createdAt,omitempty"`
	// MessageID 仅在 started 事件里可能非空：该项出队落库后得到的真实消息 ID。
	MessageID string `json:"messageId,omitempty"`
}

// MsgQueueSnapshot 是队列方法返回的全量快照。Revision 单调递增，客户端据此
// 丢弃乱序/过期的快照。
type MsgQueueSnapshot struct {
	Revision uint64         `json:"revision"`
	Items    []MsgQueueItem `json:"items"`
}

// MsgQueueCancelParams 是 msgqueue/cancel 请求参数。
type MsgQueueCancelParams struct {
	SessionID string `json:"sessionId"`
	ItemID    string `json:"itemId"`
}

// MsgQueueUpdateParams 是 msgqueue/update 请求参数：prompt 项改 text，
// command 项改 args 并由服务端据此重建 text。字段为 nil 表示不修改。
type MsgQueueUpdateParams struct {
	SessionID string  `json:"sessionId"`
	ItemID    string  `json:"itemId"`
	Text      *string `json:"text,omitempty"`
	Args      *string `json:"args,omitempty"`
}

// MsgQueueClearParams 是 msgqueue/clear 请求参数。
type MsgQueueClearParams struct {
	SessionID string `json:"sessionId"`
}

// MsgQueueListParams 是 msgqueue/list 请求参数。
type MsgQueueListParams struct {
	SessionID string `json:"sessionId"`
}

// CronJob 是会话定时任务的展示快照（alk.cxykevin.top/cron，见
// extension.md §3.11）。任务由 AI 的 cron 工具创建，客户端只做查看与删除。
type CronJob struct {
	// ID 与 @cron 行首的数字、cron 工具的 id 参数一致。
	ID uint32 `json:"id"`
	// Cron 是标准 5 段表达式（@daily 等简写在入库时已展开）。
	Cron string `json:"cron"`
	// Prompt 到点后投递到会话的提示词（不含 "[cronjob] " 前缀）。
	Prompt string `json:"prompt"`
	// RunOnce 为 true 表示投递一次后自动删除。
	RunOnce bool `json:"runOnce"`
	// Workdir 是任务所属工作目录（绝对路径）。
	Workdir string `json:"workdir"`
	// NextRuns 是接下来 4 次（一次性任务 1 次）触发时间，本地时区。
	NextRuns []string `json:"nextRuns"`
}

// CronListParams 是 cron/list 请求参数。
type CronListParams struct {
	SessionID string `json:"sessionId"`
}

// CronListResult 是 cron/list 响应。
type CronListResult struct {
	Jobs []CronJob `json:"jobs"`
}

// CronDeleteParams 是 cron/delete 请求参数。
type CronDeleteParams struct {
	SessionID string `json:"sessionId"`
	ID        uint32 `json:"id"`
}

// CronDeleteResult 是 cron/delete 响应。
type CronDeleteResult struct {
	ID uint32 `json:"id"`
}

// ClientInfo 标识 ACP 客户端。
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AgentInfo 标识 ACP agent。
type AgentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities 表示本客户端实际实现的可选能力。保留 Raw 供 schema 升级。
type ClientCapabilities struct {
	FileSystem  *json.RawMessage       `json:"fs,omitempty"`
	Terminal    *json.RawMessage       `json:"terminal,omitempty"`
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
	// MsgQueue 为 true 时在 capabilities 顶层声明 alk.cxykevin.top/msgqueue：
	// 服务端据此向本连接推送会话消息队列事件（值为 {} 对象）。
	MsgQueue bool            `json:"-"`
	Raw      json.RawMessage `json:"-"`
}

// ElicitationCapability 表示客户端支持的 elicitation 模式。
type ElicitationCapability struct {
	Form *json.RawMessage `json:"form,omitempty"`
	URL  *json.RawMessage `json:"url,omitempty"`
}

// MarshalJSON 合并 Raw 中的扩展字段与已知 capability 字段。已知字段优先。
func (c ClientCapabilities) MarshalJSON() ([]byte, error) {
	values := map[string]json.RawMessage{}
	if len(c.Raw) != 0 {
		if err := json.Unmarshal(c.Raw, &values); err != nil {
			return nil, fmt.Errorf("client capabilities raw must be a JSON object: %w", err)
		}
		if values == nil {
			return nil, fmt.Errorf("client capabilities raw must be a JSON object")
		}
	}
	if c.FileSystem != nil {
		values["fs"] = append(json.RawMessage(nil), (*c.FileSystem)...)
	}
	if c.Terminal != nil {
		values["terminal"] = append(json.RawMessage(nil), (*c.Terminal)...)
	}
	if c.Elicitation != nil {
		elicitationData, err := json.Marshal(c.Elicitation)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal elicitation capability: %w", err)
		}
		values["elicitation"] = elicitationData
	}
	if c.MsgQueue {
		values[Alkaid0CapabilityMsgQueue] = json.RawMessage("{}")
	}
	return json.Marshal(values)
}

// AgentCapabilities 保存 agent 在 initialize 中声明的能力。
type AgentCapabilities struct {
	LoadSession *bool           `json:"loadSession,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON 读取已知字段并保留完整 capability object，以便未来协议扩展诊断。
func (c *AgentCapabilities) UnmarshalJSON(data []byte) error {
	var wire struct {
		LoadSession *bool `json:"loadSession"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	c.LoadSession = wire.LoadSession
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// SupportsSessionDelete 报告 agent 是否声明 session.delete 能力。
// ACP v2 中该能力以 capabilities.session.delete 存在（非 null）为标记；
// 缺失或 null 表示 agent 不支持删除会话，客户端不应调用 session/delete。
func (c AgentCapabilities) SupportsSessionDelete() bool {
	if len(c.Raw) == 0 {
		return false
	}
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(c.Raw, &caps); err != nil {
		return false
	}
	session, ok := caps["session"]
	if !ok {
		return false
	}
	var s struct {
		Delete *json.RawMessage `json:"delete"`
	}
	if err := json.Unmarshal(session, &s); err != nil {
		return false
	}
	return s.Delete != nil
}

// Has 报告能力声明中是否包含给定扩展能力标记（如 Alkaid0CapabilityV04）。
// 扩展标记以 JSON 对象的键形式出现在 capabilities 顶层。
func (c AgentCapabilities) Has(marker string) bool {
	if len(c.Raw) == 0 || marker == "" {
		return false
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(c.Raw, &keys); err != nil {
		return false
	}
	_, ok := keys[marker]
	return ok
}

// InitializeParams 是 initialize request 参数。
// ACP v2 中客户端与服务端共享 capabilities + info 两个顶层对象。
type InitializeParams struct {
	ProtocolVersion int                `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	Info            ClientInfo         `json:"info"`
}

// InitializeResult 是 initialize response。字段外的未来扩展保留在 Raw。
type InitializeResult struct {
	ProtocolVersion int               `json:"protocolVersion"`
	Capabilities    AgentCapabilities `json:"capabilities"`
	Info            AgentInfo         `json:"info"`
	AuthMethods     []json.RawMessage `json:"authMethods"`
	Raw             json.RawMessage   `json:"-"`
}

// UnmarshalJSON 读取已知 initialize response 字段并保留完整 raw result。
func (r *InitializeResult) UnmarshalJSON(data []byte) error {
	type wireResult struct {
		ProtocolVersion int               `json:"protocolVersion"`
		Capabilities    AgentCapabilities `json:"capabilities"`
		Info            AgentInfo         `json:"info"`
		AuthMethods     []json.RawMessage `json:"authMethods"`
	}
	var wire wireResult
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.ProtocolVersion = wire.ProtocolVersion
	r.Capabilities = wire.Capabilities
	r.Info = wire.Info
	r.AuthMethods = wire.AuthMethods
	r.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// InitializedParams 是 initialized notification 的空参数对象。
type InitializedParams struct{}

// SessionListParams 是 session/list request 参数。CWD 用于按工作目录过滤会话。
type SessionListParams struct {
	CWD    string `json:"cwd,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// SessionListResult 是 session/list response。
type SessionListResult struct {
	Sessions   []*SessionInfo `json:"sessions"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

// SessionNewParams 是 session/new request 参数。CWD 是 agent 工作目录。
type SessionNewParams struct {
	CWD string `json:"cwd,omitempty"`
}

// ReplayFrom 是 session/resume 的历史回放方式。Type 目前仅支持 "start"。
type ReplayFrom struct {
	Type string `json:"type"`
}

// SessionResumeParams 是 session/resume request 参数。
// CWD 必须与会话的工作目录一致；ReplayFrom 省略时不回放历史。
type SessionResumeParams struct {
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd,omitempty"`
	ReplayFrom *ReplayFrom `json:"replayFrom,omitempty"`
	// Cursor 是 alkaid0 v0.6 的历史回放分页游标：省略时取活跃分支最近的一页，
	// 传入上一次响应里的游标则继续回放更早的一页。游标对客户端不透明，
	// 必须原样回传（见 extension.md §3.3）。
	Cursor string `json:"alk.cxykevin.top/session/cursor,omitempty"`
}

// SessionResult 是 session/new、session/resume 的结果。
type SessionResult struct {
	SessionID     string         `json:"sessionId"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
	// Cursor 是 alkaid0 v0.6 的历史回放分页游标：非空表示还有更早的消息
	// 未回放，可用它再发一次 session/resume 取更早的一页。
	Cursor string `json:"alk.cxykevin.top/session/cursor,omitempty"`
}

// SessionSetConfigOptionParams 是 session/set_config_option request 参数。
// Type 在 ACP v2 中为 "select" | "boolean"（选择/开关类配置必填）。
type SessionSetConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Type      string `json:"type"`
	Value     string `json:"value"`
}

// SessionSetConfigOptionResult 是 session/set_config_option response。
// 服务端返回更新后的完整配置项列表。
type SessionSetConfigOptionResult struct {
	ConfigOptions []ConfigOption `json:"configOptions"`
}

// SessionPromptParams 是 session/prompt request 参数。
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResult 是 session/prompt 完成后的结果。
type SessionPromptResult struct {
	StopReason *StopReason `json:"stopReason,omitempty"`
}

// SessionCancelParams 是 session/cancel notification 参数。
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionDeleteParams 是 session/delete request 参数。
type SessionDeleteParams struct {
	SessionID string `json:"sessionId"`
}

// TerminalListParams 是 alkaid0 v0.5 terminal/list request 参数。
type TerminalListParams struct {
	SessionID string `json:"sessionId"`
}

// TerminalListResult 是 terminal/list response。
type TerminalListResult struct {
	Terminals []TerminalInfo `json:"terminals"`
}

// TerminalStatusParams 是 alkaid0 v0.5 terminal/status request 参数。
type TerminalStatusParams struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalStatusResult 是 terminal/status response。
type TerminalStatusResult struct {
	Terminal TerminalInfo `json:"terminal"`
}

// TerminalStopParams 是 alkaid0 v0.5 terminal/stop request 参数。
type TerminalStopParams struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalHistoryParams 是 alkaid0 v0.6 terminal/history request 参数。
// 省略 TerminalID 时返回该会话全部已结束终端。
type TerminalHistoryParams struct {
	SessionID string `json:"sessionId"`
	// TerminalID 为统一的 @temp/run/<n>；ID 只在工作目录内唯一，
	// 因此必须同时带 SessionID 才能定位。
	TerminalID string `json:"terminalId,omitempty"`
}

// TerminalHistoryResult 是 terminal/history response，只包含已结束
// （finished / killed）终端，按 createdAt（再按 terminalId）升序。
type TerminalHistoryResult struct {
	Terminals []TerminalInfo `json:"terminals"`
}

// WorkflowStatusParams 是 alkaid0 v0.5 workflow/status request 参数。
// RunID 是 workflow 的 run id（@temp/run/<n>，与 terminal ID 同值）；
// 响应类型见 WorkflowStatusResult（workflow.go）。
type WorkflowStatusParams struct {
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
}

// TerminalStopResult 是 terminal/stop response。
type TerminalStopResult struct {
	TerminalID string `json:"terminalId,omitempty"`
	Status     string `json:"status,omitempty"`
}

// ElicitationMode 是 elicitation 的模式。
type ElicitationMode string

const (
	ElicitationModeForm ElicitationMode = "form"
	ElicitationModeURL  ElicitationMode = "url"
)

// ElicitationAction 是 elicitation 的响应动作。
type ElicitationAction string

const (
	ElicitationActionAccept  ElicitationAction = "accept"
	ElicitationActionDecline ElicitationAction = "decline"
	ElicitationActionCancel  ElicitationAction = "cancel"
)

// ElicitationCreateParams 是 elicitation/create request 参数。
type ElicitationCreateParams struct {
	SessionID     string          `json:"sessionId,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	RequestID     *int            `json:"requestId,omitempty"`
	Mode          ElicitationMode `json:"mode"`
	Message       string          `json:"message"`
	ElicitationID string          `json:"elicitationId,omitempty"`   // URL 模式必需
	URL           string          `json:"url,omitempty"`             // URL 模式必需
	Schema        json.RawMessage `json:"requestedSchema,omitempty"` // Form 模式必需
}

// ElicitationResponse 是 elicitation/create 的响应。
type ElicitationResponse struct {
	Action  ElicitationAction `json:"action"`
	Content json.RawMessage   `json:"content,omitempty"` // accept 时的表单数据
}

// ElicitationCompleteParams 是 elicitation/complete notification 参数。
type ElicitationCompleteParams struct {
	ElicitationID string `json:"elicitationId"`
}
