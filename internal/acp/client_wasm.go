//go:build js || wasip1

package acp

import (
	"context"
	"errors"
)

// NewDesktopClientBackend 在 WASM/WASI 上明确拒绝本地子进程 transport。
func NewDesktopClientBackend(context.Context, ClientConfig) (*ClientBackend, error) {
	return nil, errors.New("真实 ACP agent 子进程仅支持桌面平台；WASM/WASI 请使用 --demo")
}
