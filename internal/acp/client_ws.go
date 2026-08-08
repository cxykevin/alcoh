//go:build !js && !wasip1

package acp

import (
	"context"
	"time"
)

// WSClientConfig 描述通过 WebSocket 连接的远程 ACP agent。
type WSClientConfig struct {
	// URL 形如 ws://127.0.0.1:7433/acp?token=xxx；token 等认证参数并入 query string。
	URL          string
	ClientInfo   ClientInfo
	Capabilities ClientCapabilities
	// CWD 是 session/list 与 session/resume 使用的默认工作目录。
	CWD string
	// DialTimeout 覆盖 WebSocket 握手超时；<=0 时由 transport 决定。
	DialTimeout time.Duration
	// ReadLimit 限制单个 JSON-RPC 帧大小；<=0 时由 transport 决定。
	ReadLimit int64
}

// NewWSClientBackend 创建通过 WebSocket 连接远程 ACP agent 的 backend。
// 生命周期与 stdio 版本一致：调用方需在退出前 Close 以回收连接。
func NewWSClientBackend(config WSClientConfig) *ClientBackend {
	wsConfig := WSConfig{URL: config.URL, DialTimeout: config.DialTimeout, ReadLimit: config.ReadLimit}
	return NewClientBackend(ClientConfig{
		ClientInfo:      config.ClientInfo,
		Capabilities:    config.Capabilities,
		ProtocolVersion: protocolVersion,
		CWD:             config.CWD,
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return NewWSTransport(ctx, wsConfig, handler, onError)
		},
	})
}
