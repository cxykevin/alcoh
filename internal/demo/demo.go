// Package demo 实现一个假的 ACP backend，用于在没有真实 agent 时演示 TUI。
// 通过时间轴脚本模拟：思考流 → 工具调用 → 助手流式输出 → 计划 → 权限 → idle。
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
)

// Backend 是演示用假 backend。
type Backend struct {
	mu      sync.Mutex
	events  chan acp.Event
	seq     int
	fast    bool
	done    chan struct{}
	stopped bool
	// sessions 维护可恢复会话列表。初始含一个示例会话；NewSession 追加新会话，
	// DeleteSession 移除，使 ListSessions 反映删除结果。
	sessions []*acp.SessionInfo
}

// New 创建演示 backend。fast=true 时加速脚本（短间隔）。
func New(fast bool) *Backend {
	return &Backend{
		events: make(chan acp.Event, 256),
		fast:   fast,
		done:   make(chan struct{}),
		sessions: []*acp.SessionInfo{
			{SessionID: "demo-0", Title: "示例会话（可恢复）"},
		},
	}
}

// Initialize 演示 backend 直接成功。
func (b *Backend) Initialize(ctx context.Context) error { return nil }

// AgentInfo 返回演示 backend 的标识信息。
func (b *Backend) AgentInfo() acp.AgentInfo {
	return acp.AgentInfo{Name: "alcoh-demo", Version: "dev"}
}

// AgentCapabilities 演示 backend 声明 session.delete 能力（支持按 d 删除会话），
// 但不声明任何扩展能力（含 alkaid0 扩展），因此 /server 等按能力门控的
// 扩展命令在 demo 下不可用。
func (b *Backend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"session":{"delete":{}}}`)}
}

// GetConfig 演示 backend 不声明 alkaid0 扩展能力，因此不会调用 config/get。
func (b *Backend) GetConfig(ctx context.Context) (json.RawMessage, error) {
	return nil, fmt.Errorf("demo backend 无服务端配置")
}

// SetConfig 演示 backend 不声明 alkaid0 扩展能力，因此不会调用 config/set。
func (b *Backend) SetConfig(ctx context.Context, patch json.RawMessage) error {
	return fmt.Errorf("demo backend 无服务端配置")
}

// ListSessions 返回可恢复的会话列表（内部维护，删除操作实时反映）。
func (b *Backend) ListSessions(ctx context.Context) ([]*acp.SessionInfo, error) {
	page, err := b.ListSessionsPage(ctx, "")
	return page.Sessions, err
}

// ListSessionsPage 演示 backend 不分页，返回完整列表。
func (b *Backend) ListSessionsPage(ctx context.Context, cursor string) (acp.SessionPage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*acp.SessionInfo, len(b.sessions))
	for i, s := range b.sessions {
		cp := *s
		out[i] = &cp
	}
	return acp.SessionPage{Sessions: out}, nil
}

// DeleteSession 从可恢复会话列表移除指定会话。幂等：会话不存在时静默成功，
// 与 ACP v2 session/delete 语义一致（删除已删除/不存在的会话 SHOULD 静默成功）。
func (b *Backend) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.sessions {
		if s.SessionID == id {
			b.sessions = append(b.sessions[:i], b.sessions[i+1:]...)
			return nil
		}
	}
	return nil
}

// NewSession 创建新演示会话，并追加到可恢复会话列表。
func (b *Backend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	b.mu.Lock()
	b.seq++
	id := fmt.Sprintf("demo-%d", b.seq)
	b.sessions = append(b.sessions, &acp.SessionInfo{SessionID: id, Title: "会话 " + id})
	b.mu.Unlock()
	s := &session{
		backend: b,
		id:      id,
		title:   "会话 " + id,
		done:    make(chan struct{}),
	}
	s.start()
	return s, nil
}

// ResumeSession 恢复会话（用同一套脚本）。
func (b *Backend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s := &session{
		backend: b,
		id:      id,
		title:   "恢复 " + id,
		done:    make(chan struct{}),
	}
	s.start()
	return s, nil
}

// Events 返回类型化事件流。
func (b *Backend) Events() <-chan acp.Event { return b.events }

// Close 停止所有脚本并关闭事件流。
func (b *Backend) Close() error {
	b.mu.Lock()
	if !b.stopped {
		b.stopped = true
		close(b.done)
	}
	b.mu.Unlock()
	return nil
}

// push 推送一个事件（非阻塞）。
func (b *Backend) push(ev acp.Event) {
	select {
	case b.events <- ev:
	case <-b.done:
	}
}

// session 是演示会话，实现 acp.Session。
type session struct {
	backend *Backend
	id      string
	title   string
	mu      sync.Mutex
	done    chan struct{}
	running bool
	cancel  chan struct{}
	effort  string
	model   string
}

func (s *session) ID() string    { return s.id }
func (s *session) Title() string { return s.title }

// SendPrompt 启动脚本。
func (s *session) SendPrompt(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.cancel = make(chan struct{})
	done := s.cancel
	s.mu.Unlock()
	go s.runScript(text, done)
	return nil
}

// Cancel 取消当前脚本。
func (s *session) Cancel(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		close(s.cancel)
		s.cancel = nil
	}
	s.running = false
	s.mu.Unlock()
	// 通知 UI 回到 idle（折叠思考、关闭权限模态）
	s.backend.push(&acp.StateChangeEvent{SessionID: s.id, State: acp.StateIdle})
	return nil
}

// Close 关闭会话。
func (s *session) Close(ctx context.Context) error {
	return s.Cancel(ctx)
}

// ApprovePermission 记录用户选择并继续脚本。
func (s *session) ApprovePermission(ctx context.Context, reqID string, outcome acp.PermissionOutcome, optionID *string) error {
	s.backend.push(&acp.StateChangeEvent{
		SessionID: s.id,
		State:     acp.StateRunning,
	})
	if outcome == acp.OutcomeSelected {
		s.resumeStream()
	} else {
		s.backend.push(&acp.StateChangeEvent{SessionID: s.id, State: acp.StateIdle})
	}
	return nil
}

func (s *session) RespondElicitation(ctx context.Context, reqID acp.RPCID, action acp.ElicitationAction, content json.RawMessage) error {
	// Demo 模式暂不实现 elicitation
	s.backend.push(&acp.StateChangeEvent{SessionID: s.id, State: acp.StateIdle})
	return nil
}

// SetConfigOption 演示更新会话配置：更新对应 config 的 currentValue 并广播。
func (s *session) SetConfigOption(ctx context.Context, configID, configType, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 持久化已知配置，使 configOptions() 在后续调用中保持用户选择。
	switch configID {
	case "thought_level":
		s.effort = value
	case "model":
		s.model = value
	}
	options := s.configOptions()
	s.backend.push(&acp.ConfigOptionUpdateEvent{SessionID: s.id, Options: options})
	return nil
}

// configOptions 返回当前演示会话配置项。
func (s *session) configOptions() []acp.ConfigOption {
	efforts := []acp.ConfigOptionValue{
		{Value: "unset", Name: "Unset"},
		{Value: "low", Name: "Low"},
		{Value: "medium", Name: "Medium"},
		{Value: "high", Name: "High"},
		{Value: "xhigh", Name: "XHigh"},
		{Value: "max", Name: "Max"},
	}
	cur := s.effort
	if cur == "" {
		cur = "medium"
	}
	model := s.model
	if model == "" {
		model = "demo-go-1"
	}
	return []acp.ConfigOption{
		{ConfigID: "thought_level", Name: "Thought Level", Category: "thought_level", Type: "select", CurrentValue: cur, Options: efforts},
		{ConfigID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: model,
			Options: []acp.ConfigOptionValue{
				{Value: "demo-go-1", Name: "Demo Go 1", Description: "最快的模型"},
				{Value: "demo-go-2", Name: "Demo Go 2", Description: "最强大的模型"},
				{Value: "demo-go-3", Name: "Demo Go 3", Description: "平衡型模型"},
				{Value: "demo-go-4", Name: "Demo Go 4", Description: "大上下文模型"},
				{Value: "demo-go-5", Name: "Demo Go 5", Description: "低成本模型"},
			}},
		{ConfigID: "temperature", Name: "Temperature", Description: "采样温度", Type: "number", CurrentValue: "0.7",
			Options: []acp.ConfigOptionValue{{Value: "0.7", Name: "0.7"}, {Value: "1.0", Name: "1.0"}}},
		{ConfigID: "top-p", Name: "Top P", Description: "核采样阈值", Type: "number", CurrentValue: "0.95",
			Options: []acp.ConfigOptionValue{{Value: "0.95", Name: "0.95"}, {Value: "1.0", Name: "1.0"}}},
	}
}

func (s *session) start() {
	// 注意：不推送 NewSessionEvent。真实 agent（如 alkaid0）在 session/new 或
	// resume 响应返回前只广播各类 update，会话激活由客户端在拿到响应后负责；
	// demo 若在此推送 NewSessionEvent 会与 app 在命令完成后的激活重复，导致
	// 已重放的初始元数据（config / commands）被重建会话清空。
	s.backend.push(&acp.SessionInfoUpdateEvent{
		SessionID: s.id,
		Title:     strPtr(s.title),
		Model:     strPtr("demo-go-1"),
		CWD:       strPtr("."),
		UpdatedAt: strPtr("2026-08-06T00:00:00Z"),
	})
	s.backend.push(&acp.CommandsUpdateEvent{SessionID: s.id, Commands: []acp.AvailableCommand{
		{Name: "explain", Description: "解释当前实现"},
		{Name: "tests", Description: "生成测试建议"},
	}})
	// agent 广播若干配置项，验证结构化 ConfigOption 通路。
	// 含 thought_level（推理强度），使客户端 /effort 命令可被启用。
	s.backend.push(&acp.ConfigOptionUpdateEvent{
		SessionID: s.id,
		Options:   s.configOptions(),
	})
	// 未来协议扩展：作为诊断而非静默丢弃。
	s.backend.push(&acp.UnknownSessionUpdateEvent{
		SessionID:     s.id,
		Discriminator: "demo_future_update",
		Raw:           []byte(`{"sessionUpdate":"demo_future_update","future":true}`),
	})
	// 一条用户思考流片段，验证 user_thought_chunk 走 Message 流水线。
	s.backend.push(&acp.MessageChunkEvent{
		SessionID: s.id,
		MessageID: "user-thought-0",
		IsUser:    true,
		IsThought: true,
		Text:      "（用户初始想法：先做一个最小可用版本，再叠加错误处理）",
	})
	s.backend.push(&acp.MessageUpdateEvent{
		SessionID: s.id,
		IsUser:    true,
		IsThought: true,
		Message: acp.Message{MessageID: "user-thought-0", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: strPtr("（用户初始想法：先做一个最小可用版本，再叠加错误处理）")}}},
	})
	// 推送一条欢迎助手消息（附带非文本 ContentBlock 占位）
	imgMime := "image/png"
	imgURI := "file:///tmp/architecture.png"
	imgName := "architecture"
	s.backend.push(&acp.MessageChunkEvent{
		SessionID: s.id,
		MessageID: "welcome",
		Text:      "演示 agent 已就绪。输入提示词试试（如：帮我实现一个 TCP 服务器）。",
	})
	s.backend.push(&acp.MessageUpdateEvent{
		SessionID: s.id,
		Message: acp.Message{MessageID: "welcome", ContentSet: true,
			Content: []acp.ContentBlock{
				{Type: "text", Text: strPtr("演示 agent 已就绪。输入提示词试试（如：帮我实现一个 TCP 服务器）。")},
				{Type: "image", MimeType: &imgMime, URI: &imgURI, Name: &imgName},
			}},
	})
}

// sleep 间隔（fast 模式加速）。
func (s *session) sleep(d time.Duration, done <-chan struct{}) bool {
	if s.backend.fast {
		d /= 4
	}
	select {
	case <-time.After(d):
		return true
	case <-done:
		return false
	}
}

// runScript 执行完整的演示脚本。
func (s *session) runScript(prompt string, done <-chan struct{}) {
	b := s.backend

	b.push(&acp.StateChangeEvent{SessionID: s.id, State: acp.StateRunning})
	if !s.sleep(200*time.Millisecond, done) {
		return
	}

	// 1. 思考流（3 段）
	thoughts := []string{
		"用户要求实现 TCP 服务器。\n先分析需求：需要一个监听端口、处理连接、支持回显的简单服务器。",
		"考虑结构：main 函数创建 listener，accept 循环，每个连接一个 goroutine 处理。\n需要处理 SIGINT 优雅关闭。",
		"决定用 Go 标准库 net 包，不引入外部依赖。\n计划：1) 监听 2) accept 3) 回显处理 4) 优雅退出。",
	}
	for i, t := range thoughts {
		if !s.sleep(280*time.Millisecond, done) {
			return
		}
		text := t
		if i < len(thoughts)-1 {
			text += "\n"
		}
		b.push(&acp.MessageChunkEvent{SessionID: s.id, MessageID: "thought-1", IsThought: true, Text: text})
	}
	if !s.sleep(120*time.Millisecond, done) {
		return
	}
	b.push(&acp.MessageUpdateEvent{
		SessionID: s.id,
		Message:   acp.Message{MessageID: "thought-1", ContentSet: true, Content: []acp.ContentBlock{{Type: "text", Text: strPtr(joinLines(thoughts))}}},
	})

	// 2. 工具调用序列
	if !s.sleep(100*time.Millisecond, done) {
		return
	}
	terminalTitle := "go test ./..."
	running := "running"
	b.push(&acp.TerminalUpdateEvent{SessionID: s.id, TerminalID: "term-1", Title: terminalTitle, Status: running, Output: "$ go test ./...\n"})
	s.runTool("c1", acp.KindRead, "read_file", "internal/server.go", "{\"path\":\"internal/server.go\"}", "file not found", done)
	s.runTool("c2", acp.KindWrite, "write_file", "internal/server.go", "{\"path\":\"internal/server.go\",\"content\":\"...\"}", "ok: 134 lines written", done)
	s.runTool("c3", acp.KindExecute, "go build", "./...", "{\"cmd\":\"go build ./...\"}", "ok (0.8s)", done)
	completed := "completed"
	b.push(&acp.TerminalUpdateEvent{SessionID: s.id, TerminalID: "term-1", Status: completed, Output: "ok\tgithub.com/example/server\t0.013s\n"})

	// 3. 计划面板
	if !s.sleep(100*time.Millisecond, done) {
		return
	}
	b.push(&acp.PlanUpdateEvent{
		SessionID: s.id,
		Plan: acp.Plan{Type: "items", PlanID: "plan-1", Entries: []acp.PlanEntry{
			{Content: "创建 main 函数与监听", Status: acp.PlanCompleted, Priority: acp.PriorityHigh},
			{Content: "实现 accept 循环与连接处理", Status: acp.PlanInProgress, Priority: acp.PriorityHigh},
			{Content: "实现回显协议", Status: acp.PlanPending, Priority: acp.PriorityMedium},
			{Content: "优雅关闭与测试", Status: acp.PlanPending, Priority: acp.PriorityLow},
		}},
	})

	// 4. 助手消息流式（每个 chunk 都以 \n 结束，保证 markdown 按行解析、代码围栏可识别）
	assistant := []string{
		"好的，我来实现一个 TCP 服务器。\n",
		"\n",
		"```go\nfunc main() {\n\tln, err := net.Listen(\"tcp\", \":8080\")\n\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n\tfor {\n\t\tconn, err := ln.Accept()\n\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n\t\tgo handleConn(conn)\n\t}\n}\n```\n",
		"\n",
		"这个服务器监听 **8080** 端口，每个连接独立 goroutine 处理。\n",
		"要查看运行效果可以运行 `go run .`。\n",
	}
	msgID := "assistant-1"
	for _, seg := range assistant {
		if !s.sleep(60*time.Millisecond, done) {
			return
		}
		b.push(&acp.MessageChunkEvent{SessionID: s.id, MessageID: msgID, Text: seg})
	}

	// 5. 权限请求（等待用户响应）
	if !s.sleep(200*time.Millisecond, done) {
		return
	}
	b.push(&acp.StateChangeEvent{SessionID: s.id, State: acp.StateRequiresAction})
	b.push(&acp.PermissionRequestEvent{
		SessionID: s.id,
		Request: acp.PermissionRequest{
			Title: "允许执行命令？",
			Subject: &acp.PermissionSubject{
				Type: "tool_call",
				ToolCall: &acp.ToolCallInfo{
					ToolCallID: "perm-cmd-0",
					Title:      "run_command",
					Kind:       acp.KindExecute,
					Content:    []acp.ToolCallContent{{Type: "text", Text: strPtr("command: go run .")}},
				},
			},
			Options: []acp.PermissionOption{
				{OptionID: "allow-once", Name: "允许一次", Kind: acp.AllowOnce},
				{OptionID: "allow-always", Name: "始终允许", Kind: acp.AllowAlways},
				{OptionID: "reject-once", Name: "拒绝", Kind: acp.RejectOnce},
			},
		},
	})
	// 注意：脚本在这里等待 ApprovePermission 回调继续（resumeStream）。
}

// runTool 推送一次工具调用生命周期。
func (s *session) runTool(id string, kind acp.ToolCallKind, title, action, input, output string, done <-chan struct{}) {
	b := s.backend
	if !s.sleep(120*time.Millisecond, done) {
		return
	}
	pending := acp.ToolPending
	inprog := acp.ToolInProgress
	completed := acp.ToolCompleted
	toolTitle := title + " " + action
	b.push(&acp.ToolCallUpdateEvent{
		SessionID: s.id, ToolCallID: id, Status: &pending, Kind: &kind, Title: &toolTitle,
	})
	if !s.sleep(200*time.Millisecond, done) {
		return
	}
	b.push(&acp.ToolCallUpdateEvent{
		SessionID: s.id, ToolCallID: id, Status: &inprog, Kind: &kind, Title: &toolTitle,
	})
	if !s.sleep(300*time.Millisecond, done) {
		return
	}
	b.push(&acp.ToolCallUpdateEvent{
		SessionID: s.id, ToolCallID: id, Status: &completed, Kind: &kind, Title: &toolTitle,
		RawInput:  []byte(input),
		RawOutput: []byte(output),
	})
}

// resumeStream 在权限响应后继续输出并结束。
func (s *session) resumeStream() {
	b := s.backend
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return
	}
	done := s.cancel
	s.mu.Unlock()

	after := []string{
		"命令已获批准，正在启动……\n",
		"\n",
		"服务器已监听 **:8080**。可以使用 `nc localhost 8080` 测试。\n",
		"当前完成了主要实现，工具执行全部通过。\n",
	}
	for _, seg := range after {
		if !s.sleep(60*time.Millisecond, done) {
			return
		}
		b.push(&acp.MessageChunkEvent{SessionID: s.id, MessageID: "assistant-2", Text: seg})
	}
	if !s.sleep(120*time.Millisecond, done) {
		return
	}
	stop := acp.StopEndTurn
	b.push(&acp.StateChangeEvent{SessionID: s.id, State: acp.StateIdle, StopReason: &stop})
	b.push(&acp.UsageUpdateEvent{SessionID: s.id, Used: 12840, Size: 200000, Cost: &acp.Cost{Amount: "0.0042", Currency: "USD"}})
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

func strPtr(s string) *string { return &s }
