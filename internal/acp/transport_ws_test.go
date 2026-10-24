//go:build !js && !wasip1

package acp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWSTransportReconnectsAfterServerClose 用真实 WebSocket 服务端验证断线重连：
// 服务端关闭第一条连接后，backend 通过 WSTransport 的 URL 重新拨号、重新握手，
// 事件流保持打开。
func TestWSTransportReconnectsAfterServerClose(t *testing.T) {
	var mu sync.Mutex
	var conns []*websocket.Conn
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			incoming, err := DecodeIncoming(message)
			if err != nil {
				return
			}
			if incoming.Request == nil || incoming.Request.Method != MethodInitialize {
				continue
			}
			reply, err := MarshalResult(incoming.Request.ID, InitializeResult{ProtocolVersion: protocolVersion})
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, reply); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	b := NewClientBackend(ClientConfig{
		ProtocolVersion: protocolVersion,
		CWD:             "/tmp",
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return NewWSTransport(ctx, WSConfig{URL: wsURL}, handler, onError)
		},
		Reconnect: &ReconnectConfig{
			MinDelay:         5 * time.Millisecond,
			MaxDelay:         20 * time.Millisecond,
			HandshakeTimeout: 2 * time.Second,
		},
	})
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer b.Close()

	mu.Lock()
	first := conns[0]
	mu.Unlock()
	_ = first.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	_ = first.Close()

	waitReconnectPhase(t, b, ReconnectSucceeded, 3*time.Second)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(conns)
		mu.Unlock()
		if n >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not accept a reconnect")
}
