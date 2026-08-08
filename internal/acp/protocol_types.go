package acp

import (
	"encoding/json"
	"fmt"
)

const (
	MethodInitialize           = "initialize"
	MethodInitialized          = "initialized"
	MethodSessionList          = "session/list"
	MethodSessionNew           = "session/new"
	MethodSessionResume        = "session/resume"
	MethodSessionPrompt        = "session/prompt"
	MethodSessionCancel        = "session/cancel"
	MethodSessionUpdate        = "session/update"
	MethodSessionSetConfig     = "session/set_config_option"
	MethodSessionDelete        = "session/delete"
	MethodRequestPermission    = "session/request_permission"
	MethodElicitationCreate    = "elicitation/create"
	MethodElicitationComplete  = "elicitation/complete"

	// Alkaid0CapabilityV04 是 alkaid0 扩展协议版本能力标记。服务端在 initialize
	// 的 capabilities 中声明该键才表示支持 alkaid0 扩展方法（如 config/get、config/set）。
	// 见 docs/acp/extension.md。
	Alkaid0CapabilityV04 = "alk.cxykevin.top/alkaid0/v0.4"

	// MethodConfigGet 是 alkaid0 扩展方法：获取完整服务端配置。见 docs/acp/extension.md。
	MethodConfigGet = "alk.cxykevin.top/config/get"

	// MethodConfigSet 是 alkaid0 扩展方法：部分更新服务端配置并自动持久化。
	// 见 docs/acp/extension.md。
	MethodConfigSet = "alk.cxykevin.top/config/set"
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
	FileSystem  *json.RawMessage     `json:"fs,omitempty"`
	Terminal    *json.RawMessage     `json:"terminal,omitempty"`
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
	Raw         json.RawMessage      `json:"-"`
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
}

// SessionResult 是 session/new、session/resume 的结果。
type SessionResult struct {
	SessionID     string         `json:"sessionId"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
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
	ElicitationID string          `json:"elicitationId,omitempty"` // URL 模式必需
	URL           string          `json:"url,omitempty"`            // URL 模式必需
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
