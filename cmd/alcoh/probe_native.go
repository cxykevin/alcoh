//go:build !js && !wasip1

package main

import (
	"context"

	"github.com/cxykevin/alcoh/internal/acp"
)

// probeWSAuth 在原生构建里做一次真实 WebSocket 握手预检（只在原生平台可用）。
func probeWSAuth(urlStr string) (int, error) {
	return acp.ProbeWSAuth(context.Background(), urlStr, wsProbeTimeout)
}

// isWSAuthFailure 报告预检状态码是否表示"密钥不对"。
func isWSAuthFailure(status int) bool { return acp.IsWSAuthFailure(status) }
