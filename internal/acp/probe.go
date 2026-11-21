//go:build !js && !wasip1

package acp

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ProbeWSAuth 尝试与 WS 端点建立一次 WebSocket 握手（不发送任何 ACP 消息），
// 返回 HTTP 状态码。握手成功返回 101；认证失败通常是 401。
//
// 用途：TUI 启动前用保存的密钥做一次轻量预检，只有认证失败才重新询问用户，
// 避免因网络不可达而反复要求输入密钥。err 为 nil 表示握手成功。
func ProbeWSAuth(ctx context.Context, url string, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := websocket.Dialer{HandshakeTimeout: timeout}
	conn, resp, err := dialer.DialContext(ctx, url, nil)
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if conn != nil {
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = conn.Close()
	}
	return status, err
}

// IsWSAuthFailure 报告预检状态码是否属于"密钥不对"（需要重新询问用户）。
func IsWSAuthFailure(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}
