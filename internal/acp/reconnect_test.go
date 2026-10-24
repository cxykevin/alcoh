package acp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

const (
	testReconnectMinDelay = 5 * time.Millisecond
	testReconnectMaxDelay = 20 * time.Millisecond
)

// reconnectTransport 是可主动触发 onError 的假 transport：用于模拟长连接断开。
type reconnectTransport struct {
	handler IncomingHandler
	onError TransportErrorHandler

	mu       sync.Mutex
	requests []string
	handlers map[string]func(any, any) error
	closed   chan struct{}
	closeOne sync.Once
}

func newReconnectTransport(handler IncomingHandler, onError TransportErrorHandler) *reconnectTransport {
	return &reconnectTransport{
		handler:  handler,
		onError:  onError,
		handlers: map[string]func(any, any) error{},
		closed:   make(chan struct{}),
	}
}

func (t *reconnectTransport) set(method string, h func(any, any) error) {
	t.mu.Lock()
	t.handlers[method] = h
	t.mu.Unlock()
}

func (t *reconnectTransport) Request(ctx context.Context, method string, params any, result any) error {
	t.mu.Lock()
	t.requests = append(t.requests, method)
	h := t.handlers[method]
	t.mu.Unlock()
	if h == nil {
		return errors.New("unexpected method " + method)
	}
	return h(params, result)
}

func (t *reconnectTransport) Notify(method string, params any) error {
	t.mu.Lock()
	t.requests = append(t.requests, "notify:"+method)
	t.mu.Unlock()
	return nil
}

func (t *reconnectTransport) Respond(RPCID, any) error           { return nil }
func (t *reconnectTransport) RespondError(RPCID, RPCError) error { return nil }
func (t *reconnectTransport) Close() error {
	t.closeOne.Do(func() { close(t.closed) })
	return nil
}

// fail 模拟服务端断开或网络错误：等价于读循环退出后调用 transport 的错误回调。
func (t *reconnectTransport) fail(err error) {
	if t.onError != nil {
		t.onError(err)
	}
}

func (t *reconnectTransport) sent(method string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range t.requests {
		if m == method {
			return true
		}
	}
	return false
}

// scriptedInitialize 让 transport 把 initialize 握手回成成功。
func (t *reconnectTransport) scriptedInitialize() *reconnectTransport {
	t.set(MethodInitialize, func(_ any, result any) error {
		r := result.(*InitializeResult)
		r.ProtocolVersion = protocolVersion
		r.Capabilities = AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.6":{}}`)}
		return nil
	})
	return t
}

// dialedTransport 安全读取第 index 次建连得到的 transport。
func dialedTransport(t *testing.T, mu *sync.Mutex, dialed *[]*reconnectTransport, index int) *reconnectTransport {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if index >= len(*dialed) {
		t.Fatalf("dial count = %d, want > %d", len(*dialed), index)
	}
	return (*dialed)[index]
}

// waitReconnectPhase 等待指定阶段的重连事件，忽略其它阶段。
func waitReconnectPhase(t *testing.T, b *ClientBackend, phase ReconnectPhase, timeout time.Duration) *ReconnectEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-b.Events():
			if !ok {
				t.Fatalf("events closed while waiting for reconnect phase %v", phase)
			}
			re, ok := ev.(*ReconnectEvent)
			if ok && re.Phase == phase {
				return re
			}
		case <-deadline:
			t.Fatalf("timeout waiting for reconnect phase %v", phase)
		}
	}
}

// waitClosed 等待 backend 的事件流关闭。
func waitClosed(t *testing.T, b *ClientBackend, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-b.Events():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("backend event stream did not close")
		}
	}
}

func testReconnectConfig() *ReconnectConfig {
	return &ReconnectConfig{
		MinDelay:         testReconnectMinDelay,
		MaxDelay:         testReconnectMaxDelay,
		HandshakeTimeout: time.Second,
	}
}

// newReconnectTestBackend 构造带自动重连的 backend；每次建连都得到一个可完成
// initialize 握手的假 transport。
func newReconnectTestBackend(t *testing.T) (*ClientBackend, *[]*reconnectTransport, *sync.Mutex) {
	t.Helper()
	dialed := &[]*reconnectTransport{}
	var mu sync.Mutex
	b := NewClientBackend(ClientConfig{
		ProtocolVersion: protocolVersion,
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			tr := newReconnectTransport(handler, onError).scriptedInitialize()
			mu.Lock()
			*dialed = append(*dialed, tr)
			mu.Unlock()
			return tr, nil
		},
		Reconnect: testReconnectConfig(),
	})
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return b, dialed, &mu
}

// newBackendWithFirstDial 让首次 Initialize 使用一条可用连接，之后的自动重连
// 才走 retryFactory。
func newBackendWithFirstDial(t *testing.T, retryFactory TransportFactory, cfg *ReconnectConfig) (*ClientBackend, *reconnectTransport) {
	t.Helper()
	first := newReconnectTransport(nil, nil).scriptedInitialize()
	var mu sync.Mutex
	used := false
	b := NewClientBackend(ClientConfig{
		ProtocolVersion: protocolVersion,
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			mu.Lock()
			firstUse := !used
			if firstUse {
				used = true
				first.handler = handler
				first.onError = onError
			}
			mu.Unlock()
			if firstUse {
				return first, nil
			}
			return retryFactory(ctx, handler, onError)
		},
		Reconnect: cfg,
	})
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return b, first
}

// TestReconnectRedialsAndReinitializes 验证 transport 断开后自动重连：重新建连、
// 重新完成 initialize 握手，能力声明刷新，后续请求走新连接。
func TestReconnectRedialsAndReinitializes(t *testing.T) {
	b, dialed, mu := newReconnectTestBackend(t)
	defer b.Close()

	first := dialedTransport(t, mu, dialed, 0)
	first.fail(errors.New("connection reset by peer"))

	if ev := waitReconnectPhase(t, b, ReconnectStarted, 2*time.Second); ev.Err == nil {
		t.Fatal("ReconnectStarted should carry the disconnect cause")
	}
	success := waitReconnectPhase(t, b, ReconnectSucceeded, 2*time.Second)
	if success.Attempt != 1 {
		t.Errorf("succeeded after attempt %d, want 1", success.Attempt)
	}

	second := dialedTransport(t, mu, dialed, 1)
	if !second.sent(MethodInitialize) {
		t.Error("reconnected transport did not receive initialize")
	}
	if !second.sent("notify:" + MethodInitialized) {
		t.Error("reconnected transport did not receive initialized notification")
	}
	if !b.AgentCapabilities().Has(Alkaid0CapabilityV06) {
		t.Error("agent capabilities were not refreshed from the reconnect handshake")
	}
	second.set(MethodSessionList, func(any, any) error { return nil })
	if _, err := b.ListSessionsPage(context.Background(), ""); err != nil {
		t.Fatalf("request after reconnect: %v", err)
	}
}

// TestReconnectFailureEventAndRetry 验证建连失败会报告 AttemptFailed，随后继续
// 重试直到成功，且成功事件带累计尝试次数。
func TestReconnectFailureEventAndRetry(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	retry := func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
		mu.Lock()
		dials++
		n := dials
		mu.Unlock()
		if n == 1 {
			return nil, errors.New("connection refused")
		}
		return newReconnectTransport(handler, onError).scriptedInitialize(), nil
	}
	b, first := newBackendWithFirstDial(t, retry, testReconnectConfig())
	defer b.Close()

	first.fail(errors.New("pipe closed"))

	failed := waitReconnectPhase(t, b, ReconnectAttemptFailed, 2*time.Second)
	if failed.Attempt != 1 || failed.Err == nil {
		t.Fatalf("attempt failed event = %#v", failed)
	}
	success := waitReconnectPhase(t, b, ReconnectSucceeded, 2*time.Second)
	if success.Attempt != 2 {
		t.Errorf("succeeded after attempt %d, want 2", success.Attempt)
	}
}

// TestReconnectGivesUp 验证达到最大尝试次数后 backend 关闭事件流并返回明确错误。
func TestReconnectGivesUp(t *testing.T) {
	cfg := testReconnectConfig()
	cfg.MaxAttempts = 2
	retry := func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
		return nil, errors.New("connection refused")
	}
	b, first := newBackendWithFirstDial(t, retry, cfg)
	defer b.Close()

	first.fail(errors.New("pipe closed"))
	waitClosed(t, b, 3*time.Second)
	if _, err := b.ListSessionsPage(context.Background(), ""); err == nil {
		t.Fatal("backend should report an error after giving up")
	}
}

// TestTransportErrorWithoutReconnectStillCloses 验证未配置 Reconnect 时保持原有
// 语义：transport 断开即关闭 backend。
func TestTransportErrorWithoutReconnectStillCloses(t *testing.T) {
	tr := newReconnectTransport(nil, nil).scriptedInitialize()
	b := NewClientBackend(ClientConfig{
		ProtocolVersion: protocolVersion,
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			tr.handler = handler
			tr.onError = onError
			return tr, nil
		},
	})
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer b.Close()

	tr.fail(errors.New("connection reset"))
	waitClosed(t, b, 2*time.Second)
	if _, err := b.ListSessionsPage(context.Background(), ""); err == nil {
		t.Fatal("closed backend should report an error")
	}
}

// TestSessionReattachReloadsHistory 验证重连后的历史重载：先广播
// HistoryReloadEvent 让 UI 清空本地正文，再发出 session/resume（replayFrom:
// start）；分页游标取本次响应（而不是断线前那个），保证重建后的历史能继续翻页。
func TestSessionReattachReloadsHistory(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodInitialize, func(_ any, result any) error {
		result.(*InitializeResult).ProtocolVersion = protocolVersion
		return nil
	})
	resumes := 0
	ft.set(MethodSessionResume, func(params, result any) error {
		p := params.(SessionResumeParams)
		resumes++
		if p.SessionID != "sess-1" || p.CWD != "/work" {
			t.Errorf("resume params = %#v", p)
		}
		if p.ReplayFrom == nil || p.ReplayFrom.Type != "start" {
			t.Errorf("replayFrom = %#v, want start", p.ReplayFrom)
		}
		r := result.(*SessionResult)
		r.SessionID = "sess-1"
		if resumes == 1 {
			r.Cursor = "page-2"
		} else {
			// 重连重建后本地只剩这次回放的内容，游标必须归零表示已到最早。
			r.Cursor = ""
			r.ConfigOptions = []ConfigOption{{ConfigID: "thought_level", CurrentValue: "high"}}
		}
		return nil
	})
	b := NewClientBackend(ClientConfig{
		ProtocolVersion: protocolVersion,
		CWD:             "/work",
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return ft, nil
		},
	})
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer b.Close()

	s, err := b.ResumeSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	reattacher, ok := s.(SessionReattacher)
	if !ok {
		t.Fatalf("session does not implement SessionReattacher: %T", s)
	}
	if err := reattacher.Reattach(context.Background()); err != nil {
		t.Fatalf("reattach: %v", err)
	}
	// 回放之前必须先通知 UI 清空本地正文，否则 upsert 无法删除废弃分支/补齐空洞。
	select {
	case ev := <-b.Events():
		reload, ok := ev.(*HistoryReloadEvent)
		if !ok || reload.SessionID != "sess-1" {
			t.Fatalf("first reattach event = %#v, want HistoryReloadEvent", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for HistoryReloadEvent")
	}
	if ev := drainConfigEvent(t, b); len(ev.Options) != 1 || ev.Options[0].CurrentValue != "high" {
		t.Errorf("reattach config event = %#v", ev.Options)
	}
	if pager := s.(HistoryPager); pager.MoreHistory() {
		t.Error("reattach must adopt the resume response cursor (empty means fully replayed)")
	}
	if resumes != 2 {
		t.Errorf("session/resume calls = %d, want 2", resumes)
	}
}

// TestReconnectLoopStopsOnClose 验证关闭 backend 会停止重连循环，不会在退出后
// 继续建连。
func TestReconnectLoopStopsOnClose(t *testing.T) {
	cfg := testReconnectConfig()
	cfg.MinDelay = 20 * time.Millisecond
	cfg.MaxDelay = 20 * time.Millisecond
	var mu sync.Mutex
	dials := 0
	retry := func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		return nil, errors.New("connection refused")
	}
	b, first := newBackendWithFirstDial(t, retry, cfg)

	first.fail(errors.New("pipe closed"))
	time.Sleep(5 * time.Millisecond) // 让重连循环进入退避等待
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	mu.Lock()
	before := dials
	mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	after := dials
	mu.Unlock()
	if after != before {
		t.Errorf("reconnect loop kept dialing after Close: %d -> %d", before, after)
	}
}
