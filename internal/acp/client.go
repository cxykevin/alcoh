package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/cxykevin/alcoh/product"
)

const protocolVersion = 2

// ClientConfig 是真实 ACP client backend 的配置。
type ClientConfig struct {
	CommandConfig   CommandConfig
	ClientInfo      ClientInfo
	Capabilities    ClientCapabilities
	ProtocolVersion int
	// CWD 是 session/list 与 session/resume 使用的默认工作目录；
	// 空时对应方法会省略 cwd 参数。
	CWD string
	// TransportFactory 覆盖默认的 stdio 启动逻辑（如 WebSocket 连接）。
	// 为 nil 时 NewClientBackend 使用本地子进程 stdio。
	TransportFactory TransportFactory
	// Reconnect 控制 transport 意外断开后的自动重连；nil 表示不重连。
	// 只有 TransportFactory 可以重复执行并重新建连（如 WebSocket）时才应配置：
	// 重连成功后 backend 会重新完成 initialize 握手，并由调用方复用
	// SessionReattacher 把活动会话重新绑定到新连接。
	Reconnect *ReconnectConfig
}

type backendState uint8

const (
	backendNew backendState = iota
	backendInitializing
	backendReady
	backendReconnecting
	backendClosing
	backendClosed
)

// ClientBackend 是基于 JSON-RPC transport（stdio 或 WebSocket）的 ACP v2 Backend 实现。
type ClientBackend struct {
	config ClientConfig

	mu             sync.Mutex
	state          backendState
	transport      Transport
	initializeDone chan struct{}
	initErr        error
	terminalErr    error
	agentCaps      AgentCapabilities
	agentInfo      AgentInfo

	// reconnectCancel 取消进行中的重连循环；非 nil 时说明正在重连。
	reconnectCancel context.CancelFunc

	// inbound 是 callback 的私有队列；eventRelay 是 public events 的唯一 sender
	// 和 closer，因此 Close 与 callback 不会产生 send-on-closed panic。
	inbound   chan Event
	events    chan Event
	closing   chan struct{}
	eventDone chan struct{}
	closeOnce sync.Once

	permissions  map[string]permissionPending
	elicitations map[string]elicitationPending
}

type permissionPending struct {
	id        RPCID
	sessionID string
}

type elicitationPending struct {
	id        RPCID
	sessionID string
}

// NewClientBackend 创建尚未初始化的真实 ACP backend。
// 默认使用本地子进程 stdio transport；通过 config.TransportFactory 可替换（如 WebSocket）。
func NewClientBackend(config ClientConfig) *ClientBackend {
	if config.ProtocolVersion == 0 {
		config.ProtocolVersion = protocolVersion
	}
	if config.ClientInfo.Name == "" {
		config.ClientInfo = ClientInfo{Name: "alcoh", Version: product.Version}
	}
	if config.TransportFactory == nil {
		commandConfig := config.CommandConfig
		config.TransportFactory = func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return StartStdioTransport(ctx, commandConfig, handler, onError)
		}
	}
	b := &ClientBackend{
		config:       config,
		state:        backendNew,
		inbound:      make(chan Event, 256),
		events:       make(chan Event, 256),
		closing:      make(chan struct{}),
		eventDone:    make(chan struct{}),
		permissions:  make(map[string]permissionPending),
		elicitations: make(map[string]elicitationPending),
	}
	go b.eventRelay()
	return b
}

func (b *ClientBackend) eventRelay() {
	defer close(b.eventDone)
	defer close(b.events)
	for {
		select {
		case <-b.closing:
			return
		case ev := <-b.inbound:
			select {
			case <-b.closing:
				return
			case b.events <- ev:
			}
		}
	}
}

// Initialize 启动 agent，并完成 initialize/initialized 握手。
func (b *ClientBackend) Initialize(ctx context.Context) error {
	b.mu.Lock()
	switch b.state {
	case backendInitializing:
		done := b.initializeDone
		b.mu.Unlock()
		select {
		case <-done:
			b.mu.Lock()
			err := b.initErr
			b.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	case backendReady:
		b.mu.Unlock()
		return nil
	case backendReconnecting:
		b.mu.Unlock()
		return errors.New("ACP backend is reconnecting")
	case backendClosing, backendClosed:
		b.mu.Unlock()
		return errors.New("ACP backend is closed")
	}
	b.state = backendInitializing
	b.initializeDone = make(chan struct{})
	b.mu.Unlock()

	factory := b.config.TransportFactory
	if factory == nil {
		factory = func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return StartStdioTransport(ctx, b.config.CommandConfig, handler, onError)
		}
	}
	t, err := factory(ctx, b.handleIncoming, b.handleTransportError)
	if err != nil {
		b.finishInitialize(nil, err, AgentCapabilities{}, AgentInfo{})
		return err
	}

	b.mu.Lock()
	if b.state != backendInitializing {
		b.mu.Unlock()
		_ = t.Close()
		err := errors.New("ACP backend closed during initialize")
		b.finishInitialize(nil, err, AgentCapabilities{}, AgentInfo{})
		return err
	}
	b.transport = t
	b.mu.Unlock()

	result, err := b.handshake(ctx, t, 0)
	b.finishInitialize(t, err, result.Capabilities, result.Info)
	if err != nil {
		go b.shutdown(true, err)
	}
	return err
}

func (b *ClientBackend) finishInitialize(t Transport, err error, caps AgentCapabilities, info AgentInfo) {
	b.mu.Lock()
	if b.state == backendInitializing {
		b.initErr = err
		b.agentCaps = caps
		b.agentInfo = info
		if err == nil {
			b.state = backendReady
		}
	} else if b.initErr == nil {
		// Close 与 Initialize 竞争时 state 已经变为 closing；仍必须让所有
		// 并发 Initialize 调用者观察到确定错误并解除等待。
		b.initErr = err
	}
	if b.initializeDone != nil {
		close(b.initializeDone)
		b.initializeDone = nil
	}
	b.mu.Unlock()
}

func (b *ClientBackend) readyTransport() (Transport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == backendReconnecting {
		return nil, errors.New("ACP backend is reconnecting")
	}
	if b.state != backendReady || b.transport == nil {
		if b.terminalErr != nil {
			return nil, b.terminalErr
		}
		return nil, errors.New("ACP backend is not ready")
	}
	return b.transport, nil
}

// ListSessions 调用 session/list 获取第一页，保留历史兼容接口。
func (b *ClientBackend) ListSessions(ctx context.Context) ([]*SessionInfo, error) {
	page, err := b.ListSessionsPage(ctx, "")
	return page.Sessions, err
}

// ListSessionsPage 调用 session/list 获取指定 cursor 的一页。
func (b *ClientBackend) ListSessionsPage(ctx context.Context, cursor string) (SessionPage, error) {
	t, err := b.readyTransport()
	if err != nil {
		return SessionPage{}, err
	}
	var result SessionListResult
	if err := t.Request(ctx, MethodSessionList, SessionListParams{CWD: b.config.CWD, Cursor: cursor}, &result); err != nil {
		if isCWDNotInitError(err) {
			return SessionPage{}, nil
		}
		return SessionPage{}, err
	}
	return SessionPage{Sessions: result.Sessions, NextCursor: result.NextCursor}, nil
}

// isCWDNotInitError 判断 JSON-RPC error 是否为 alkaid0 在 cwd 未初始化
// （缺失 .alkaid0 目录）时返回的 -32099 "cwd not inited"。该错误在列出会话
// 时语义等价于「没有可恢复会话」，调用方应静默忽略。
func isCWDNotInitError(err error) bool {
	var remote *RPCRemoteError
	if !errors.As(err, &remote) {
		return false
	}
	if remote.RPCError.Code != -32099 {
		return false
	}
	return strings.Contains(strings.ToLower(remote.RPCError.Message), "cwd not init")
}

// NewSession 调用 session/new。
func (b *ClientBackend) NewSession(ctx context.Context, cwd string) (Session, error) {
	t, err := b.readyTransport()
	if err != nil {
		return nil, err
	}
	var result SessionResult
	if err := t.Request(ctx, MethodSessionNew, SessionNewParams{CWD: cwd}, &result); err != nil {
		return nil, err
	}
	if result.SessionID == "" {
		return nil, errors.New("ACP session/new response missing sessionId")
	}
	// ACP v2：session/new 响应携带初始 configOptions（agent 可能不另行广播
	// config_option_update），作为配置快照推给模型，确保 thought_level 等
	// 配置在会话建立即可见。
	if len(result.ConfigOptions) > 0 {
		b.emit(&ConfigOptionUpdateEvent{SessionID: result.SessionID, Options: result.ConfigOptions})
	}
	return &clientSession{baseSession: baseSession{id: result.SessionID}, backend: b}, nil
}

// ResumeSession 调用 session/resume。
func (b *ClientBackend) ResumeSession(ctx context.Context, id string) (Session, error) {
	if id == "" {
		return nil, errors.New("ACP session id is empty")
	}
	t, err := b.readyTransport()
	if err != nil {
		return nil, err
	}
	var result SessionResult
	if err := t.Request(ctx, MethodSessionResume, SessionResumeParams{
		SessionID:  id,
		CWD:        b.config.CWD,
		ReplayFrom: &ReplayFrom{Type: "start"},
	}, &result); err != nil {
		return nil, err
	}
	if result.SessionID == "" {
		result.SessionID = id
	}
	if len(result.ConfigOptions) > 0 {
		b.emit(&ConfigOptionUpdateEvent{SessionID: result.SessionID, Options: result.ConfigOptions})
	}
	// alkaid0 v0.6：响应携带游标表示还有更早的消息未回放，记在会话句柄上，
	// UI 滚到正文顶部时据此继续翻页（见 extension.md §3.3）。
	return &clientSession{baseSession: baseSession{id: result.SessionID}, backend: b, historyCursor: result.Cursor}, nil
}

// Reattach 在自动重连后重新加载会话历史（见 SessionReattacher）。
// 先广播 HistoryReloadEvent 让 UI 清空本地正文，再用 replayFrom: start 让服务端
// 按活跃分支重放最近的消息（断线期间可能产生的空洞、另一条分支的残留都会因此
// 消失）；响应游标成为新的分页起点，供用户继续向前翻页。
func (s *clientSession) Reattach(ctx context.Context) error {
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	// 回放是 upsert 语义：不清空旧内容就无法对齐（缺口无法补、废弃分支无法删）。
	s.backend.emit(&HistoryReloadEvent{SessionID: s.id})
	s.historyMu.Lock()
	s.historyCursor = ""
	s.historyMu.Unlock()
	var result SessionResult
	if err := t.Request(ctx, MethodSessionResume, SessionResumeParams{
		SessionID:  s.id,
		CWD:        s.backend.config.CWD,
		ReplayFrom: &ReplayFrom{Type: "start"},
	}, &result); err != nil {
		return err
	}
	if result.SessionID == "" {
		result.SessionID = s.id
	}
	// 本地历史已按本次回放重建，分页游标必须取响应里的值（而不是断线前那个）。
	s.historyMu.Lock()
	s.historyCursor = result.Cursor
	s.historyMu.Unlock()
	if len(result.ConfigOptions) > 0 {
		s.backend.emit(&ConfigOptionUpdateEvent{SessionID: result.SessionID, Options: result.ConfigOptions})
	}
	return nil
}

// DeleteSession 调用 session/delete。
func (b *ClientBackend) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("ACP session id is empty")
	}
	t, err := b.readyTransport()
	if err != nil {
		return err
	}
	// 成功响应为空 result，result 传 nil 即可（见 transport.Request）。
	return t.Request(ctx, MethodSessionDelete, SessionDeleteParams{SessionID: id}, nil)
}

// AgentInfo 返回 agent 在 initialize 中声明的标识信息。
func (b *ClientBackend) AgentInfo() AgentInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agentInfo
}

// AgentCapabilities 返回 agent 在 initialize 中声明的能力。
func (b *ClientBackend) AgentCapabilities() AgentCapabilities {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agentCaps
}

// GetConfig 调用 alkaid0 扩展方法 alk.cxykevin.top/config/get 获取完整服务端配置。
// 返回值是 JSON 对象，字段名为服务端 Go 结构体硬编码的名称。
func (b *ClientBackend) GetConfig(ctx context.Context) (json.RawMessage, error) {
	t, err := b.readyTransport()
	if err != nil {
		return nil, err
	}
	var result ConfigGetResult
	if err := t.Request(ctx, MethodConfigGet, struct{}{}, &result); err != nil {
		return nil, err
	}
	if len(result.Config) == 0 {
		return nil, errors.New("config/get response missing config")
	}
	return result.Config, nil
}

// SetConfig 调用 alkaid0 扩展方法 alk.cxykevin.top/config/set 部分更新服务端配置。
// 服务端自动持久化并触发所有重载钩子（含配置广播推送）。
func (b *ClientBackend) SetConfig(ctx context.Context, patch json.RawMessage) error {
	t, err := b.readyTransport()
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return nil
	}
	// 成功响应为 JSON null，result 传 nil 即可（见 transport.Request）。
	return t.Request(ctx, MethodConfigSet, ConfigSetParams{Config: patch}, nil)
}

// Events 返回 ACP 通知与 server request 转换而来的类型化事件。
func (b *ClientBackend) Events() <-chan Event { return b.events }

func (b *ClientBackend) emit(ev Event) {
	select {
	case <-b.closing:
		return
	case b.inbound <- ev:
	}
}

func (b *ClientBackend) handleIncoming(incoming IncomingMessage) {
	if incoming.Notification != nil {
		if incoming.Notification.Method != MethodSessionUpdate {
			return
		}
		ev, err := DecodeSessionUpdate(incoming.Notification.Params)
		if err != nil {
			b.emit(&BackendErrorEvent{Err: err})
			return
		}
		b.emit(ev)
		return
	}
	if incoming.Request == nil {
		return
	}
	switch incoming.Request.Method {
	case MethodRequestPermission:
		var request PermissionRequest
		if err := json.Unmarshal(incoming.Request.Params, &request); err != nil {
			b.respondInvalidParams(incoming.Request, err)
			return
		}
		if request.SessionID == "" || len(request.Options) == 0 {
			b.respondInvalidParams(incoming.Request, errors.New("permission request missing sessionId or options"))
			return
		}
		request.RequestID = incoming.Request.ID.Key()
		b.mu.Lock()
		if b.state != backendReady {
			b.mu.Unlock()
			return
		}
		b.permissions[request.RequestID] = permissionPending{id: incoming.Request.ID, sessionID: request.SessionID}
		b.mu.Unlock()
		b.emit(&PermissionRequestEvent{SessionID: request.SessionID, Request: request})

	case MethodElicitationCreate:
		var request ElicitationCreateParams
		if err := json.Unmarshal(incoming.Request.Params, &request); err != nil {
			b.respondInvalidParams(incoming.Request, err)
			return
		}
		// 验证必需字段
		if request.Mode == "" {
			b.respondInvalidParams(incoming.Request, errors.New("elicitation request missing mode"))
			return
		}
		if request.Mode == ElicitationModeForm && len(request.Schema) == 0 {
			b.respondInvalidParams(incoming.Request, errors.New("form mode requires requestedSchema"))
			return
		}
		if request.Mode == ElicitationModeURL && (request.ElicitationID == "" || request.URL == "") {
			b.respondInvalidParams(incoming.Request, errors.New("url mode requires elicitationId and url"))
			return
		}
		// 确定 session ID
		sessionID := request.SessionID
		if sessionID == "" && request.RequestID != nil {
			// requestId 作用域，暂不支持跨会话
			sessionID = ""
		}
		b.mu.Lock()
		if b.state != backendReady {
			b.mu.Unlock()
			return
		}
		reqKey := incoming.Request.ID.Key()
		b.elicitations[reqKey] = elicitationPending{id: incoming.Request.ID, sessionID: sessionID}
		b.mu.Unlock()
		b.emit(&ElicitationRequestEvent{SessionID: sessionID, RequestID: incoming.Request.ID, Request: request})

	default:
		b.respondMethodNotFound(incoming.Request)
	}
}

func (b *ClientBackend) respondMethodNotFound(request *RPCRequest) {
	t, err := b.readyTransport()
	if err != nil {
		return
	}
	_ = t.RespondError(request.ID, RPCError{Code: -32601, Message: "method not found"})
}

func (b *ClientBackend) respondInvalidParams(request *RPCRequest, err error) {
	t, transportErr := b.readyTransport()
	if transportErr != nil {
		return
	}
	_ = t.RespondError(request.ID, RPCError{Code: -32602, Message: err.Error()})
}

func (b *ClientBackend) handleTransportError(err error) {
	b.mu.Lock()
	if b.state == backendReconnecting {
		// 重连过程中本次尝试失败由 reconnectLoop 处理；不能再次触发关闭。
		b.mu.Unlock()
		return
	}
	if b.config.Reconnect != nil && b.state == backendReady {
		old := b.transport
		b.transport = nil
		b.state = backendReconnecting
		b.mu.Unlock()
		b.emit(&ReconnectEvent{Phase: ReconnectStarted, Err: err})
		// 回调可能运行在 transport reader 内：重连（含关闭旧 transport）必须
		// 放到独立 goroutine，否则会在等待 reader 退出时自锁。
		go b.reconnectLoop(old)
		return
	}
	b.mu.Unlock()
	// 回调可能运行在 transport reader 内；优先开始关闭，避免 event queue 满时
	// emit 反压住 reader，进而与 transport.Close 相互等待。
	go b.shutdown(true, err)
	b.emit(&BackendErrorEvent{Err: err})
}

func (b *ClientBackend) shutdown(unexpected bool, err error) {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		if unexpected {
			b.terminalErr = err
		}
		if b.state == backendInitializing {
			b.initErr = err
			if b.initializeDone != nil {
				close(b.initializeDone)
				b.initializeDone = nil
			}
		}
		b.state = backendClosing
		t := b.transport
		b.transport = nil
		b.permissions = make(map[string]permissionPending)
		b.elicitations = make(map[string]elicitationPending)
		cancelReconnect := b.reconnectCancel
		b.reconnectCancel = nil
		b.mu.Unlock()

		// 先停掉重连循环，避免 shutdown 之后它又装上新 transport。
		if cancelReconnect != nil {
			cancelReconnect()
		}
		if t != nil {
			_ = t.Close()
		}
		close(b.closing)
		<-b.eventDone
		b.mu.Lock()
		b.state = backendClosed
		b.mu.Unlock()
	})
}

// Close 关闭 agent transport 和事件流。可重复调用。
func (b *ClientBackend) Close() error {
	b.shutdown(false, nil)
	return nil
}

type clientSession struct {
	baseSession
	backend *ClientBackend

	// historyMu 保护历史回放分页游标：首屏游标由 ResumeSession 写入，之后由
	// app 事件循环（MoreHistory）与后台加载（LoadEarlierHistory）并发读写。
	historyMu     sync.Mutex
	historyCursor string
}

func (s *clientSession) SendPrompt(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result SessionPromptResult
	if err := t.Request(ctx, MethodSessionPrompt, SessionPromptParams{
		SessionID: s.id,
		Prompt:    []ContentBlock{{Type: "text", Text: &text}},
	}, &result); err != nil {
		return err
	}
	// 部分 agent 只在 prompt response 中回传 stopReason；转成事件让 UI 能感知。
	if result.StopReason != nil && *result.StopReason != "" {
		s.backend.emit(&StateChangeEvent{SessionID: s.id, State: StateIdle, StopReason: result.StopReason})
	}
	return nil
}

func (s *clientSession) Cancel(ctx context.Context) error {
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	return t.Notify(MethodSessionCancel, SessionCancelParams{SessionID: s.id})
}

// Close 只丢弃 UI 侧句柄。ACP v2 目前没有对应的 session-close RPC，因此不发任何请求。
func (s *clientSession) Close(context.Context) error { return nil }

func (s *clientSession) ListTerminals(ctx context.Context) ([]TerminalInfo, error) {
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV05) {
		return nil, errors.New("alkaid0 v0.5 terminal protocol is not supported")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return nil, err
	}
	var result TerminalListResult
	if err := t.Request(ctx, MethodTerminalList, TerminalListParams{SessionID: s.id}, &result); err != nil {
		return nil, err
	}
	// 活动终端快照同时广播给 UI：前台 run 没有任何 terminal_update 推送，
	// 只能靠这次查询把正在运行的终端列出来。
	s.backend.emit(&TerminalListEvent{SessionID: s.id, Terminals: result.Terminals})
	return result.Terminals, nil
}

func (s *clientSession) TerminalStatus(ctx context.Context, id string) (TerminalInfo, error) {
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV05) {
		return TerminalInfo{}, errors.New("alkaid0 v0.5 terminal protocol is not supported")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return TerminalInfo{}, err
	}
	var result TerminalStatusResult
	if err := t.Request(ctx, MethodTerminalStatus, TerminalStatusParams{SessionID: s.id, TerminalID: id}, &result); err != nil {
		return TerminalInfo{}, err
	}
	return result.Terminal, nil
}

func (s *clientSession) StopTerminal(ctx context.Context, id string) error {
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV05) {
		return errors.New("alkaid0 v0.5 terminal protocol is not supported")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result TerminalStopResult
	return t.Request(ctx, MethodTerminalStop, TerminalStopParams{SessionID: s.id, TerminalID: id}, &result)
}

// TerminalHistory 查询已结束终端内容（v0.6）。id 为空时返回该会话全部
// 已结束终端；服务端按 createdAt（再按 terminalId）升序返回，只有内容
// 落库的持久化副本 createdAt 为空，因此排在最前。
func (s *clientSession) TerminalHistory(ctx context.Context, id string) ([]TerminalInfo, error) {
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV06) {
		return nil, errors.New("alkaid0 v0.6 terminal history is not supported")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return nil, err
	}
	var result TerminalHistoryResult
	if err := t.Request(ctx, MethodTerminalHistory, TerminalHistoryParams{SessionID: s.id, TerminalID: id}, &result); err != nil {
		return nil, err
	}
	for i := range result.Terminals {
		if result.Terminals[i].SessionID == "" {
			result.Terminals[i].SessionID = s.id
		}
	}
	// 结果同时以事件形式广播：历史内容与 terminal_update 走同一条通道，
	// UI 不必为查询结果单独接线（与 SendPrompt 回传 stopReason 同构）。
	s.backend.emit(&TerminalHistoryEvent{SessionID: s.id, Terminals: result.Terminals})
	return result.Terminals, nil
}

// MoreHistory 报告是否还有更早的消息未回放（alkaid0 v0.6 分页游标非空）。
// 服务端未声明 v0.6 或已回放到活跃分支最早一条时返回 false。
func (s *clientSession) MoreHistory() bool {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	return s.historyCursor != ""
}

// LoadEarlierHistory 用上次响应携带的游标再发一次 session/resume，回放更早的
// 一页（alkaid0 v0.6，见 extension.md §3.3）。分页只作用于回放本身：重放的
// 消息由服务端经 session/update 推送（与首屏一致），configOptions 等仍照常
// 返回，这里按首屏同样的方式转成配置事件。返回是否仍有更早的一页。
func (s *clientSession) LoadEarlierHistory(ctx context.Context) (bool, error) {
	s.historyMu.Lock()
	cursor := s.historyCursor
	s.historyMu.Unlock()
	if cursor == "" {
		return false, nil
	}
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV06) {
		return false, errors.New("alkaid0 v0.6 history pagination is not supported")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return false, err
	}
	var result SessionResult
	if err := t.Request(ctx, MethodSessionResume, SessionResumeParams{
		SessionID:  s.id,
		CWD:        s.backend.config.CWD,
		ReplayFrom: &ReplayFrom{Type: "start"},
		Cursor:     cursor,
	}, &result); err != nil {
		return false, err
	}
	s.historyMu.Lock()
	s.historyCursor = result.Cursor
	more := result.Cursor != ""
	s.historyMu.Unlock()
	if len(result.ConfigOptions) > 0 {
		s.backend.emit(&ConfigOptionUpdateEvent{SessionID: s.id, Options: result.ConfigOptions})
	}
	return more, nil
}

// SupportsMsgQueue 报告服务端是否同时声明 alkaid0 v0.6 与 msgqueue 能力：
// 消息队列与 v0.6 同期实现（未发版），只声明 msgqueue 而缺少 v0.6 的服务端
// 不按队列客户端对待，避免客户端把输入交给看不到的队列。
func (b *ClientBackend) SupportsMsgQueue() bool {
	caps := b.AgentCapabilities()
	return caps.Has(Alkaid0CapabilityV06) && caps.Has(Alkaid0CapabilityMsgQueue)
}

func (s *clientSession) msgQueueReady() error {
	if !s.backend.SupportsMsgQueue() {
		return errors.New("alkaid0 msgqueue is not supported")
	}
	return nil
}

// ListMsgQueue 拉取当前会话的消息队列快照。
func (s *clientSession) ListMsgQueue(ctx context.Context) (MsgQueueSnapshot, error) {
	if err := s.msgQueueReady(); err != nil {
		return MsgQueueSnapshot{}, err
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return MsgQueueSnapshot{}, err
	}
	var result MsgQueueSnapshot
	if err := t.Request(ctx, MethodMsgQueueList, MsgQueueListParams{SessionID: s.id}, &result); err != nil {
		return MsgQueueSnapshot{}, err
	}
	if result.Items == nil {
		result.Items = []MsgQueueItem{}
	}
	return result, nil
}

// CancelMsgQueueItem 取消一条排队项；服务端随后广播 removed 事件。
func (s *clientSession) CancelMsgQueueItem(ctx context.Context, itemID string) error {
	if err := s.msgQueueReady(); err != nil {
		return err
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result MsgQueueSnapshot
	return t.Request(ctx, MethodMsgQueueCancel, MsgQueueCancelParams{SessionID: s.id, ItemID: itemID}, &result)
}

// UpdateMsgQueueItem 修改一条排队项；服务端随后广播 updated 事件。
func (s *clientSession) UpdateMsgQueueItem(ctx context.Context, itemID string, text, args *string) error {
	if err := s.msgQueueReady(); err != nil {
		return err
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result MsgQueueSnapshot
	return t.Request(ctx, MethodMsgQueueUpdate, MsgQueueUpdateParams{
		SessionID: s.id,
		ItemID:    itemID,
		Text:      text,
		Args:      args,
	}, &result)
}

// ClearMsgQueue 清空当前会话的全部排队项；服务端随后广播 cleared 事件。
func (s *clientSession) ClearMsgQueue(ctx context.Context) error {
	if err := s.msgQueueReady(); err != nil {
		return err
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result MsgQueueSnapshot
	return t.Request(ctx, MethodMsgQueueClear, MsgQueueClearParams{SessionID: s.id}, &result)
}

// SupportsCron 报告服务端是否声明会话定时任务能力（alk.cxykevin.top/cron）。
func (b *ClientBackend) SupportsCron() bool {
	return b.AgentCapabilities().Has(Alkaid0CapabilityCron)
}

func (s *clientSession) cronReady() error {
	if !s.backend.SupportsCron() {
		return errors.New("alkaid0 cron is not supported")
	}
	return nil
}

// ListCrons 列出当前会话的定时任务（按 id 升序，服务端已排序）。
func (s *clientSession) ListCrons(ctx context.Context) ([]CronJob, error) {
	if err := s.cronReady(); err != nil {
		return nil, err
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return nil, err
	}
	var result CronListResult
	if err := t.Request(ctx, MethodCronList, CronListParams{SessionID: s.id}, &result); err != nil {
		return nil, err
	}
	if result.Jobs == nil {
		result.Jobs = []CronJob{}
	}
	return result.Jobs, nil
}

// DeleteCron 删除当前会话下的一条定时任务。
func (s *clientSession) DeleteCron(ctx context.Context, id uint32) error {
	if err := s.cronReady(); err != nil {
		return err
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result CronDeleteResult
	return t.Request(ctx, MethodCronDelete, CronDeleteParams{SessionID: s.id, ID: id}, &result)
}

// GetBackground 查询会话的后台运行模式（alk.cxykevin.top/session/get_background，
// 见 extension.md §3.7）。模式由服务端命令 /background on|off 修改，服务端不推送
// 变更，客户端按需查询；未声明 alkaid0 扩展能力的服务端不调用。
func (s *clientSession) GetBackground(ctx context.Context) (bool, error) {
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV04) {
		return false, errors.New("alkaid0 background mode is not supported")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return false, err
	}
	var result SessionGetBackgroundResult
	if err := t.Request(ctx, MethodSessionGetBackground, SessionGetBackgroundParams{SessionID: s.id}, &result); err != nil {
		return false, err
	}
	return result.Background, nil
}

// WorkflowStatus 查询 workflow 的完整快照（v0.5）。runID 是 workflow 的 run id
// （@temp/run/<n>，与 terminal ID 同值）；workflow 已结束或不在内存中时同样可查，
// 响应含当前状态、完整图与按 sequence 排序的事件日志（见 extension.md §3.2）。
func (s *clientSession) WorkflowStatus(ctx context.Context, runID string) (WorkflowStatusResult, error) {
	if !s.backend.AgentCapabilities().Has(Alkaid0CapabilityV05) {
		return WorkflowStatusResult{}, errors.New("alkaid0 v0.5 workflow protocol is not supported")
	}
	if runID == "" {
		return WorkflowStatusResult{}, errors.New("workflow run id is empty")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return WorkflowStatusResult{}, err
	}
	var result WorkflowStatusResult
	if err := t.Request(ctx, MethodWorkflowStatus, WorkflowStatusParams{SessionID: s.id, RunID: runID}, &result); err != nil {
		return WorkflowStatusResult{}, err
	}
	if result.RunID == "" {
		result.RunID = runID
	}
	if result.TerminalID == "" {
		result.TerminalID = result.RunID
	}
	// 结果同样以事件形式广播：与 terminal/history 同构，模型与应用层不必为
	// 查询结果单独接线，刷新/进入面板时直接复用同一条事件通道。
	s.backend.emit(&WorkflowStatusEvent{SessionID: s.id, Result: result})
	return result, nil
}

func (s *clientSession) SetConfigOption(ctx context.Context, configID, configType, value string) error {
	if configID == "" || value == "" {
		return errors.New("ACP set config option requires configId and value")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	var result SessionSetConfigOptionResult
	if err := t.Request(ctx, MethodSessionSetConfig, SessionSetConfigOptionParams{
		SessionID: s.id,
		ConfigID:  configID,
		Type:      configType,
		Value:     value,
	}, &result); err != nil {
		return err
	}
	// 服务端在响应外通常还会广播 config_option_update；这里直接透传响应中的
	// 最新配置快照，避免 UI 在广播到达前短暂显示旧值。
	if len(result.ConfigOptions) > 0 {
		s.backend.emit(&ConfigOptionUpdateEvent{SessionID: s.id, Options: result.ConfigOptions})
	}
	return nil
}

func (s *clientSession) ApprovePermission(ctx context.Context, reqID string, outcome PermissionOutcome, optionID *string) error {
	s.backend.mu.Lock()
	pending, ok := s.backend.permissions[reqID]
	s.backend.mu.Unlock()
	if !ok || pending.sessionID != s.id {
		return errors.New("ACP permission request is unknown, expired, or belongs to another session")
	}
	if outcome == OutcomeSelected && (optionID == nil || *optionID == "") {
		return errors.New("ACP selected permission response missing optionId")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	if err := t.Respond(pending.id, PermissionResponse{Outcome: outcome, OptionID: optionID}); err != nil {
		return err
	}
	s.backend.mu.Lock()
	delete(s.backend.permissions, reqID)
	s.backend.mu.Unlock()
	return nil
}

func (s *clientSession) RespondElicitation(ctx context.Context, reqID RPCID, action ElicitationAction, content json.RawMessage) error {
	s.backend.mu.Lock()
	pending, ok := s.backend.elicitations[reqID.Key()]
	s.backend.mu.Unlock()
	if !ok || (pending.sessionID != "" && pending.sessionID != s.id) {
		return errors.New("ACP elicitation request is unknown, expired, or belongs to another session")
	}
	t, err := s.backend.readyTransport()
	if err != nil {
		return err
	}
	if err := t.Respond(pending.id, ElicitationResponse{Action: action, Content: content}); err != nil {
		return err
	}
	s.backend.mu.Lock()
	delete(s.backend.elicitations, reqID.Key())
	s.backend.mu.Unlock()
	return nil
}
