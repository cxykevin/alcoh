//go:build !js && !wasip1

package acp

import "context"

// NewDesktopClientBackend 创建可启动本地 ACP agent 的真实 backend。
// 该构造器位于桌面 build tag 下，使 WASM/WASI 调用方不会意外依赖 os/exec。
func NewDesktopClientBackend(ctx context.Context, config ClientConfig) (*ClientBackend, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return NewClientBackend(config), nil
}
