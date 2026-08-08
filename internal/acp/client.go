package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

type backendState uint8

const (
	backendNew backendState = iota
	backendInitializing
	backendReady
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

	var result InitializeResult
	err = t.Request(ctx, MethodInitialize, InitializeParams{
		ProtocolVersion: b.config.ProtocolVersion,
		Capabilities:    b.config.Capabilities,
		Info:            b.config.ClientInfo,
	}, &result)
	if err == nil && result.ProtocolVersion != b.config.ProtocolVersion {
		err = fmt.Errorf("ACP protocol version mismatch: agent=%d client=%d", result.ProtocolVersion, b.config.ProtocolVersion)
	}
	if err == nil {
		err = t.Notify(MethodInitialized, InitializedParams{})
	}
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
	if b.state != backendReady || b.transport == nil {
		if b.terminalErr != nil {
			return nil, b.terminalErr
		}
		return nil, errors.New("ACP backend is not ready")
	}
	return b.transport, nil
}

// ListSessions 调用 session/list。
func (b *ClientBackend) ListSessions(ctx context.Context) ([]*SessionInfo, error) {
	t, err := b.readyTransport()
	if err != nil {
		return nil, err
	}
	var result SessionListResult
	if err := t.Request(ctx, MethodSessionList, SessionListParams{CWD: b.config.CWD}, &result); err != nil {
		if isCWDNotInitError(err) {
			// 工作目录尚未初始化（无 .alkaid0）时 agent 没有可恢复会话，
			// 按空列表处理，避免首页因 -32099 报错而无法进入。
			return nil, nil
		}
		return nil, err
	}
	return result.Sessions, nil
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
	return &clientSession{baseSession: baseSession{id: result.SessionID}, backend: b}, nil
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
		b.permissions = make(map[string]permissionPending)
		b.elicitations = make(map[string]elicitationPending)
		b.mu.Unlock()

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
