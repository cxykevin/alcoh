//go:build !js && !wasip1

package acp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newProbeServer 启动一个只做握手的测试服务：key 匹配时升级后立即关闭，
// 否则返回 401。
func newProbeServer(t *testing.T, wantKey string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestProbeWSAuth 验证预检能区分密钥正确（101）与密钥错误（401）。
func TestProbeWSAuth(t *testing.T) {
	srv := newProbeServer(t, "right")
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	status, err := ProbeWSAuth(context.Background(), wsURL+"?key=right", 3*time.Second)
	if err != nil {
		t.Fatalf("probe with right key error: %v", err)
	}
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", status)
	}
	if IsWSAuthFailure(status) {
		t.Fatal("101 must not be auth failure")
	}

	status, err = ProbeWSAuth(context.Background(), wsURL+"?key=wrong", 3*time.Second)
	if err == nil {
		t.Fatal("probe with wrong key should fail")
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if !IsWSAuthFailure(status) {
		t.Fatal("401 must be auth failure")
	}
}

// TestProbeWSAuthUnreachable 验证网络不可达时返回状态 0（不当作密钥错误）。
func TestProbeWSAuthUnreachable(t *testing.T) {
	status, err := ProbeWSAuth(context.Background(), "ws://127.0.0.1:1/acp", 300*time.Millisecond)
	if err == nil {
		t.Fatal("unreachable probe should fail")
	}
	if IsWSAuthFailure(status) {
		t.Fatalf("status %d must not be auth failure", status)
	}
}
