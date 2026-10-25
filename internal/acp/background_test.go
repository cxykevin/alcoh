package acp

// 会话后台运行模式（alk.cxykevin.top/session/get_background）的协议测试：
// 能力门控、请求参数形状与响应解码。

import (
	"context"
	"encoding/json"
	"testing"
)

// TestGetBackgroundRPC 验证 GetBackground 发出 get_background 请求并解析
// background 布尔值。
func TestGetBackgroundRPC(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionNew, func(params, result any) error {
		r := result.(*SessionResult)
		r.SessionID = "sess-1"
		return nil
	})
	var gotParams SessionGetBackgroundParams
	ft.set(MethodSessionGetBackground, func(params, result any) error {
		gotParams = params.(SessionGetBackgroundParams)
		result.(*SessionGetBackgroundResult).Background = true
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()
	// get_background 只在服务端声明 alkaid0 扩展能力时可用。
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
	b.mu.Unlock()

	s, err := b.NewSession(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	control, ok := s.(BackgroundControl)
	if !ok {
		t.Fatalf("session does not implement BackgroundControl: %T", s)
	}
	background, err := control.GetBackground(context.Background())
	if err != nil {
		t.Fatalf("get background: %v", err)
	}
	if !background {
		t.Error("background = false, want true")
	}
	if gotParams.SessionID != "sess-1" {
		t.Errorf("sessionId = %q, want sess-1", gotParams.SessionID)
	}
}

// TestGetBackgroundRequiresAlkaid0 验证未声明 alkaid0 扩展能力时不发请求。
func TestGetBackgroundRequiresAlkaid0(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodSessionNew, func(params, result any) error {
		result.(*SessionResult).SessionID = "sess-1"
		return nil
	})
	b := probeClient(t, ft)
	defer b.Close()
	s, err := b.NewSession(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if _, err := s.(BackgroundControl).GetBackground(context.Background()); err == nil {
		t.Fatal("expected an error without alkaid0 capability")
	}
	for _, method := range ft.requests {
		if method == MethodSessionGetBackground {
			t.Fatal("get_background must not be sent without alkaid0 capability")
		}
	}
}
