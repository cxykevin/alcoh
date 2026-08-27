package acp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// fakeTransport 记录请求并回填 result，用于验证客户端方法发出的 RPC 与事件。
type fakeTransport struct {
	requests []string
	params   []any
	handlers map[string]func(any, any) error
}

func (f *fakeTransport) Request(ctx context.Context, method string, params any, result any) error {
	f.requests = append(f.requests, method)
	f.params = append(f.params, params)
	if h, ok := f.handlers[method]; ok {
		return h(params, result)
	}
	return errors.New("unexpected method " + method)
}
func (f *fakeTransport) Notify(method string, params any) error { return nil }
func (f *fakeTransport) Respond(id RPCID, result any) error     { return nil }
func (f *fakeTransport) RespondError(id RPCID, rpcErr RPCError) error {
	return nil
}
func (f *fakeTransport) Close() error { return nil }

// probeClient 构造用 fakeTransport 的 backend 并完成初始化。
func probeClient(t *testing.T, ft *fakeTransport) *ClientBackend {
	t.Helper()
	if ft.handlers == nil {
		ft.handlers = map[string]func(any, any) error{}
	}
	ft.handlers[MethodInitialize] = func(params, result any) error {
		r := result.(*InitializeResult)
		r.ProtocolVersion = protocolVersion
		return nil
	}
	b := NewClientBackend(ClientConfig{
		ProtocolVersion: protocolVersion,
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return ft, nil
		},
	})
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return b
}

func drainConfigEvent(t *testing.T, b *ClientBackend) *ConfigOptionUpdateEvent {
	t.Helper()
	select {
	case ev := <-b.Events():
		e, ok := ev.(*ConfigOptionUpdateEvent)
		if !ok {
			t.Fatalf("expected ConfigOptionUpdateEvent, got %T", ev)
		}
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ConfigOptionUpdateEvent")
		return nil
	}
}

// set 注册一个方法的 fake 处理。
func (f *fakeTransport) set(method string, h func(any, any) error) {
	if f.handlers == nil {
		f.handlers = map[string]func(any, any) error{}
	}
	f.handlers[method] = h
}

// TestSessionNewEmitsResponseConfigOptions 验证 session/new 响应中的
// configOptions 被推为 ConfigOptionUpdateEvent（alkaid0 不另行广播初始配置）。
func TestSessionNewEmitsResponseConfigOptions(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionNew, func(params, result any) error {
		r := result.(*SessionResult)
		r.SessionID = "sess-1"
		r.ConfigOptions = []ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "unset"}}
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()

	s, err := b.NewSession(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if s.ID() != "sess-1" {
		t.Errorf("session id = %q, want sess-1", s.ID())
	}
	ev := drainConfigEvent(t, b)
	if len(ev.Options) != 1 || ev.Options[0].ConfigID != "thought_level" {
		t.Errorf("config event options = %#v", ev.Options)
	}
	if ev.SessionID != "sess-1" {
		t.Errorf("config event session = %q, want sess-1", ev.SessionID)
	}
}

// TestSessionResumeEmitsResponseConfigOptions 验证 session/resume 响应中的
// configOptions 同样被推送。
func TestSessionResumeEmitsResponseConfigOptions(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionResume, func(params, result any) error {
		r := result.(*SessionResult)
		r.ConfigOptions = []ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "high"}}
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()

	s, err := b.ResumeSession(context.Background(), "sess-2")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if s.ID() != "sess-2" {
		t.Errorf("session id = %q, want sess-2", s.ID())
	}
	ev := drainConfigEvent(t, b)
	if len(ev.Options) != 1 || ev.Options[0].CurrentValue != "high" {
		t.Errorf("config event options = %#v", ev.Options)
	}
}

// TestSetConfigOptionEmitsUpdateEvent 验证 SetConfigOption 发出
// session/set_config_option RPC，并把响应 configOptions 推为事件。
func TestSetConfigOptionEmitsUpdateEvent(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionNew, func(params, result any) error {
		r := result.(*SessionResult)
		r.SessionID = "sess-1"
		return nil
	})
	ft.set(MethodSessionSetConfig, func(params, result any) error {
		r := result.(*SessionSetConfigOptionResult)
		r.ConfigOptions = []ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "xhigh"}}
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()
	s, err := b.NewSession(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := s.SetConfigOption(context.Background(), "thought_level", "select", "xhigh"); err != nil {
		t.Fatalf("set config option: %v", err)
	}
	if len(ft.requests) < 2 || ft.requests[len(ft.requests)-1] != MethodSessionSetConfig {
		t.Fatalf("requests = %v, want last %s", ft.requests, MethodSessionSetConfig)
	}
	p := ft.params[len(ft.params)-1].(SessionSetConfigOptionParams)
	if p.SessionID != "sess-1" || p.ConfigID != "thought_level" || p.Type != "select" || p.Value != "xhigh" {
		t.Errorf("params = %#v", p)
	}
	ev := drainConfigEvent(t, b)
	if len(ev.Options) != 1 || ev.Options[0].CurrentValue != "xhigh" {
		t.Errorf("config event options = %#v", ev.Options)
	}
}

// TestListSessionsPagePropagatesCursor verifies cursor request/response propagation.
func TestListSessionsPagePropagatesCursor(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionList, func(params, result any) error {
		p := params.(SessionListParams)
		if p.Cursor != "next-page" {
			t.Fatalf("cursor = %q, want next-page", p.Cursor)
		}
		r := result.(*SessionListResult)
		r.Sessions = []*SessionInfo{{SessionID: "s2"}}
		r.NextCursor = "final-page"
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()
	page, err := b.ListSessionsPage(context.Background(), "next-page")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].SessionID != "s2" || page.NextCursor != "final-page" {
		t.Fatalf("page = %#v", page)
	}
}

// TestListSessionsIgnoresCWDNotInit 验证 alkaid0 在 cwd 未初始化（缺失
// .alkaid0）时返回的 -32099 "cwd not inited" 在列出会话时被静默忽略，
// 按空会话列表处理，而不是把错误抛给 UI。
func TestListSessionsIgnoresCWDNotInit(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionList, func(params, result any) error {
		return &RPCRemoteError{Method: MethodSessionList, RPCError: RPCError{Code: -32099, Message: "cwd not inited"}}
	})
	b := probeClient(t, ft)
	defer b.Close()

	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions with -32099 cwd not inited: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("sessions = %#v, want empty", sessions)
	}
}

// TestListSessionsPropagatesOtherErrors 验证非 cwd-not-init 的远程错误照常透传。
func TestListSessionsPropagatesOtherErrors(t *testing.T) {
	cases := []struct {
		name string
		code int
		msg  string
	}{
		{"other code", -32099, "internal failure"},
		{"other message", -32601, "cwd not inited"},
		{"not remote", -32099, "cwd not inited"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft := &fakeTransport{}
			if c.name != "not remote" {
				ft.set(MethodSessionList, func(params, result any) error {
					return &RPCRemoteError{Method: MethodSessionList, RPCError: RPCError{Code: c.code, Message: c.msg}}
				})
			} else {
				ft.set(MethodSessionList, func(params, result any) error {
					return errors.New("transport down")
				})
			}
			b := probeClient(t, ft)
			defer b.Close()
			if _, err := b.ListSessions(context.Background()); err == nil {
				t.Error("expected error to propagate, got nil")
			}
		})
	}
}

// TestDeleteSessionSendsMethod 验证 DeleteSession 发出 session/delete RPC，
// 且参数只带 sessionId（无 cwd），成功响应（空 result）不报错。
func TestDeleteSessionSendsMethod(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionDelete, func(params, result any) error {
		p, ok := params.(SessionDeleteParams)
		if !ok {
			t.Fatalf("params type = %T, want SessionDeleteParams", params)
		}
		if p.SessionID != "sess-9" {
			t.Errorf("params.SessionID = %q, want sess-9", p.SessionID)
		}
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()

	if err := b.DeleteSession(context.Background(), "sess-9"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if len(ft.requests) == 0 || ft.requests[len(ft.requests)-1] != MethodSessionDelete {
		t.Errorf("last request method = %v, want %s", ft.requests, MethodSessionDelete)
	}
}

// TestDeleteSessionEmptyID 验证空 id 直接报错，不发出 session/delete RPC。
func TestDeleteSessionEmptyID(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	defer b.Close()
	if err := b.DeleteSession(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty session id")
	}
	for _, m := range ft.requests {
		if m == MethodSessionDelete {
			t.Errorf("session/delete should not be sent for empty id; requests = %v", ft.requests)
		}
	}
}

// TestGetConfig 验证 config/get 发出正确方法并解析响应 config 字段。
func TestGetConfig(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodConfigGet, func(params, result any) error {
		r := result.(*ConfigGetResult)
		r.Config = json.RawMessage(`{"Version":1}`)
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()

	cfg, err := b.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if string(cfg) != `{"Version":1}` {
		t.Errorf("config = %s, want {\"Version\":1}", cfg)
	}
	if len(ft.requests) == 0 || ft.requests[len(ft.requests)-1] != MethodConfigGet {
		t.Errorf("last request method = %v, want %s", ft.requests, MethodConfigGet)
	}
}

// TestGetConfigMissingField 验证 config/get 响应缺少 config 字段时报错。
func TestGetConfigMissingField(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodConfigGet, func(params, result any) error { return nil })
	b := probeClient(t, ft)
	defer b.Close()

	if _, err := b.GetConfig(context.Background()); err == nil {
		t.Error("expected error for missing config field")
	}
}

// TestSetConfig 验证 config/set 发出部分更新 patch 且成功响应（null）不报错。
func TestSetConfig(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodConfigSet, func(params, result any) error {
		p, ok := params.(ConfigSetParams)
		if !ok {
			t.Fatalf("params type = %T, want ConfigSetParams", params)
		}
		if string(p.Config) != `{"Version":2}` {
			t.Errorf("patch = %s, want {\"Version\":2}", p.Config)
		}
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()

	if err := b.SetConfig(context.Background(), json.RawMessage(`{"Version":2}`)); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if len(ft.requests) == 0 || ft.requests[len(ft.requests)-1] != MethodConfigSet {
		t.Errorf("last request method = %v, want %s", ft.requests, MethodConfigSet)
	}
}
