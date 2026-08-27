package acp

import (
	"context"
	"encoding/json"
)

// Backend 是 ACP 后端的抽象接口。
// 真实实现（stdio JSON-RPC 客户端）与 demo（假 agent）都实现本接口，
// 使 TUI 层不感知协议实现细节。
// SessionPage 是 session/list 的一页结果。
type SessionPage struct {
	Sessions   []*SessionInfo
	NextCursor string
}

type Backend interface {
	// Initialize 建立连接并完成 initialize 握手。
	Initialize(ctx context.Context) error
	// ListSessions 列出第一页可恢复的会话。
	ListSessions(ctx context.Context) ([]*SessionInfo, error)
	// ListSessionsPage 按 cursor 获取一页可恢复的会话。空 cursor 从头开始。
	ListSessionsPage(ctx context.Context, cursor string) (SessionPage, error)
	// NewSession 在 cwd 创建新会话。
	NewSession(ctx context.Context, cwd string) (Session, error)
	// ResumeSession 恢复指定会话。
	ResumeSession(ctx context.Context, id string) (Session, error)
	// DeleteSession 删除指定会话。仅当 agent 声明 session.delete 能力时客户端调用。
	DeleteSession(ctx context.Context, id string) error
	// AgentInfo 返回 agent 在 initialize 中声明的标识信息。
	AgentInfo() AgentInfo
	// AgentCapabilities 返回 agent 在 initialize 中声明的能力。
	AgentCapabilities() AgentCapabilities
	// GetConfig 调用 alkaid0 扩展方法 alk.cxykevin.top/config/get 获取完整
	// 服务端配置（JSON 对象）。仅当服务端声明 alkaid0 扩展能力时可用。
	GetConfig(ctx context.Context) (json.RawMessage, error)
	// SetConfig 调用 alkaid0 扩展方法 alk.cxykevin.top/config/set 部分更新
	// 服务端配置并触发持久化与重载。patch 为部分配置 JSON。
	SetConfig(ctx context.Context, patch json.RawMessage) error
	// Events 返回类型化事件流（协议层持续推送）。
	Events() <-chan Event
	// Close 关闭连接并释放资源。
	Close() error
}

// Session 是一个活动会话的句柄。
type Session interface {
	// ID 返回会话标识。
	ID() string
	// Title 返回会话标题（可空）。
	Title() string
	// SendPrompt 向 agent 提交一条用户消息。
	SendPrompt(ctx context.Context, text string) error
	// Cancel 取消进行中的前台工作。
	Cancel(ctx context.Context) error
	// Close 关闭会话。
	Close(ctx context.Context) error
	// ApprovePermission 响应一个挂起的权限请求。
	// outcome 为 selected 时 optionID 指向用户选择的选项。
	ApprovePermission(ctx context.Context, reqID string, outcome PermissionOutcome, optionID *string) error
	// SetConfigOption 设置会话配置项（如 thought_level）。仅当 agent 已公布该
	// configId 时客户端才调用；Type 由调用方按配置类型指定。
	SetConfigOption(ctx context.Context, configID, configType, value string) error
	// RespondElicitation 响应一个 elicitation 请求。
	RespondElicitation(ctx context.Context, reqID RPCID, action ElicitationAction, content json.RawMessage) error
}

// baseSession 提供 ID/Title 的默认实现，供各 backend 嵌入复用。
type baseSession struct {
	id    string
	title string
}

func (s *baseSession) ID() string    { return s.id }
func (s *baseSession) Title() string { return s.title }
