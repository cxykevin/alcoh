//go:build js || wasip1

package acp

import (
	"context"
	"errors"

	"github.com/cxykevin/alcoh/internal/i18n"

	"github.com/cxykevin/alcoh/product"
)

// WSClientConfig 在 WASM/WASI 上保留类型使 cmd 可跨平台编译。
type WSClientConfig struct {
	URL          string
	ClientInfo   ClientInfo
	Capabilities ClientCapabilities
	CWD          string
}

// ErrWSUnavailable 表示 WASM/WASI 目标不支持 WebSocket transport。
var ErrWSUnavailable = errors.New(i18n.T("ACP WebSocket transport 仅支持桌面平台；WASM/WASI 请使用 --demo"))

// NewWSClientBackend 在 WASM/WASI 上返回一个 Initialize 即失败的 backend，
// 避免调用方 nil 解引用；错误信息在握手时以明确错误呈现。
func NewWSClientBackend(WSClientConfig) *ClientBackend {
	return NewClientBackend(ClientConfig{
		ClientInfo: ClientInfo{Name: "alcoh", Version: product.Version},
		TransportFactory: func(ctx context.Context, handler IncomingHandler, onError TransportErrorHandler) (Transport, error) {
			return nil, ErrWSUnavailable
		},
	})
}
