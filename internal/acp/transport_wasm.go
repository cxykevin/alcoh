//go:build js || wasip1

package acp

import (
	"context"
	"errors"
	"time"
)

// CommandConfig 描述本地 agent 子进程。WASM/WASI 没有本地 os/exec 能力，
// 仅保留类型使调用方可跨平台编译。
type CommandConfig struct {
	Command         string
	Args            []string
	Dir             string
	Env             []string
	MaxLineBytes    int
	ShutdownTimeout time.Duration
}

type IncomingHandler func(IncomingMessage)
type TransportErrorHandler func(error)

// StdioTransport 在 WASM/WASI 上不可用。
type StdioTransport struct{}

func StartStdioTransport(context.Context, CommandConfig, IncomingHandler, TransportErrorHandler) (*StdioTransport, error) {
	return nil, errors.New("ACP stdio transport is unavailable on wasm targets")
}

func (*StdioTransport) Request(context.Context, string, any, any) error {
	return errors.New("ACP stdio transport is unavailable on wasm targets")
}

func (*StdioTransport) Notify(string, any) error {
	return errors.New("ACP stdio transport is unavailable on wasm targets")
}

func (*StdioTransport) Respond(RPCID, any) error {
	return errors.New("ACP stdio transport is unavailable on wasm targets")
}

func (*StdioTransport) RespondError(RPCID, RPCError) error {
	return errors.New("ACP stdio transport is unavailable on wasm targets")
}

func (*StdioTransport) Close() error { return nil }
func (*StdioTransport) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
