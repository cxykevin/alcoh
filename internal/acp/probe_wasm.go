//go:build js || wasip1

package acp

import (
	"context"
	"errors"
	"time"
)

// ProbeWSAuth 在 wasm 构建里不可用：浏览器/WASI 环境没有可用的 WebSocket
// 握手预检。远端连接的调用方会直接拿到错误，而不是反复索要密钥。
func ProbeWSAuth(context.Context, string, time.Duration) (int, error) {
	return 0, errors.New("WebSocket precheck is not supported in the wasm build")
}

// IsWSAuthFailure 在 wasm 构建里恒为 false（预检不会返回认证失败状态码）。
func IsWSAuthFailure(int) bool { return false }
