//go:build !js && !wasip1

package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// WSConfig 描述 ACP WebSocket agent 连接。
// URL 直接作为 ws:// 地址传给 dialer，token 等认证参数应已并入 query string。
type WSConfig struct {
	URL string
	// DialTimeout 覆盖默认的 WebSocket 握手超时；<=0 时使用 5 秒。
	DialTimeout time.Duration
	// ReadLimit 限制单个 JSON-RPC 帧的最大字节数；<=0 时使用默认值。
	ReadLimit int64
	// Heartbeat 控制心跳间隔；<=0 时禁用主动 ping。
	Heartbeat time.Duration
}

// WSTransport 是基于 gorilla/websocket 的 ACP JSON-RPC transport。
// 生命周期语义与 StdioTransport 一致：Close 后所有 pending 请求以明确错误解除，
// Done 关闭表示连接已完整回收。
type WSTransport struct {
	conn *websocket.Conn

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]rpcPending
	nextID  atomic.Uint64
	closed  bool
	termErr error

	handle  IncomingHandler
	onError TransportErrorHandler

	terminateOnce sync.Once
}

// NewWSTransport 连接 ws:// URL 并开始读取服务端 JSON-RPC 帧。
func NewWSTransport(parent context.Context, config WSConfig, handler IncomingHandler, onError TransportErrorHandler) (*WSTransport, error) {
	if strings.TrimSpace(config.URL) == "" {
		return nil, errors.New("ACP WebSocket URL is empty")
	}
	u, err := url.Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse ACP WebSocket URL: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("ACP WebSocket URL must use ws:// or wss://, got %q", u.Scheme)
	}
	dialTimeout := config.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: dialTimeout,
	}
	ctx, cancel := context.WithCancel(parent)
	conn, resp, err := dialer.DialContext(ctx, config.URL, nil)
	if err != nil {
		cancel()
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("connect ACP WebSocket (%d): %w", status, err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	readLimit := config.ReadLimit
	if readLimit <= 0 {
		readLimit = defaultMaxRPCLine
	}
	conn.SetReadLimit(readLimit)

	t := &WSTransport{
		conn: conn, ctx: ctx, cancel: cancel, pending: make(map[string]rpcPending),
		handle: handler, onError: onError, done: make(chan struct{}),
	}
	t.wg.Add(2)
	go t.readLoop()
	go t.watchParent(parent)
	go func() {
		t.wg.Wait()
		close(t.done)
	}()
	return t, nil
}

// Request 发出 JSON-RPC request，并等待对应 response 或 context 取消。
func (t *WSTransport) Request(ctx context.Context, method string, params any, result any) error {
	id := NewRPCID([]byte(fmt.Sprintf("%d", t.nextID.Add(1))))
	key := id.Key()
	pending := rpcPending{method: method, result: make(chan rpcResult, 1)}
	if err := t.addPending(key, pending); err != nil {
		return err
	}
	line, err := MarshalRequest(id, method, params)
	if err != nil {
		t.removePending(key)
		return err
	}
	if err := t.write(line); err != nil {
		t.removePending(key)
		t.terminate(err, true)
		return err
	}
	select {
	case received := <-pending.result:
		if received.err != nil {
			return received.err
		}
		if result == nil || len(received.result) == 0 || string(received.result) == "null" {
			return nil
		}
		if err := json.Unmarshal(received.result, result); err != nil {
			return fmt.Errorf("decode RPC %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		t.removePending(key)
		return ctx.Err()
	case <-t.ctx.Done():
		return t.terminalError(method)
	}
}

// Notify 发出不期待 response 的 JSON-RPC notification。
func (t *WSTransport) Notify(method string, params any) error {
	line, err := MarshalNotification(method, params)
	if err != nil {
		return err
	}
	if err := t.write(line); err != nil {
		t.terminate(err, true)
		return err
	}
	return nil
}

// Respond 以 agent 发来的原始 request ID 回复成功结果。
func (t *WSTransport) Respond(id RPCID, result any) error {
	line, err := MarshalResult(id, result)
	if err != nil {
		return err
	}
	if err := t.write(line); err != nil {
		t.terminate(err, true)
		return err
	}
	return nil
}

// RespondError 以 agent 发来的原始 request ID 回复 JSON-RPC error。
func (t *WSTransport) RespondError(id RPCID, rpcErr RPCError) error {
	line, err := MarshalError(id, rpcErr)
	if err != nil {
		return err
	}
	if err := t.write(line); err != nil {
		t.terminate(err, true)
		return err
	}
	return nil
}

func (t *WSTransport) addPending(key string, pending rpcPending) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return t.terminalErrorLocked("request")
	}
	t.pending[key] = pending
	return nil
}

func (t *WSTransport) write(line []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.mu.Lock()
	closed := t.closed
	err := t.termErr
	t.mu.Unlock()
	if closed {
		if err != nil {
			return err
		}
		return errors.New("ACP WebSocket transport is closed")
	}
	if t.conn == nil {
		return errors.New("ACP WebSocket transport has no connection")
	}
	if err := t.conn.WriteMessage(websocket.TextMessage, line); err != nil {
		return fmt.Errorf("write ACP message: %w", err)
	}
	return nil
}

func (t *WSTransport) readLoop() {
	defer t.wg.Done()
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}
		_, message, err := t.conn.ReadMessage()
		if err != nil {
			t.readError(err)
			return
		}
		incoming, err := DecodeIncoming(message)
		if err != nil {
			t.terminate(err, true)
			return
		}
		if incoming.Response != nil {
			t.deliverResponse(incoming.Response)
			continue
		}
		if t.handle != nil {
			t.handle(incoming)
		}
	}
}

func (t *WSTransport) readError(err error) {
	select {
	case <-t.ctx.Done():
	default:
		// 服务端主动关闭连接视为非预期终止，除非自身正在关闭。
		t.terminate(err, true)
	}
}

func (t *WSTransport) watchParent(parent context.Context) {
	defer t.wg.Done()
	select {
	case <-parent.Done():
		t.terminate(parent.Err(), true)
	case <-t.ctx.Done():
	}
}

func (t *WSTransport) deliverResponse(response *RPCResponse) {
	key := response.ID.Key()
	t.mu.Lock()
	pending, ok := t.pending[key]
	if ok {
		delete(t.pending, key)
	}
	t.mu.Unlock()
	if !ok {
		return // 已取消或重复 response；不影响其它请求。
	}
	if response.Error != nil {
		pending.result <- rpcResult{err: &RPCRemoteError{Method: pending.method, RPCError: *response.Error}}
		return
	}
	pending.result <- rpcResult{result: append([]byte(nil), response.Result...)}
}

func (t *WSTransport) removePending(key string) {
	t.mu.Lock()
	delete(t.pending, key)
	t.mu.Unlock()
}

// terminate 是唯一的终态入口。unexpected 决定是否上报 transport 错误。
func (t *WSTransport) terminate(err error, unexpected bool) {
	t.terminateOnce.Do(func() {
		if err == nil {
			err = errors.New("ACP WebSocket transport closed")
		}
		t.mu.Lock()
		t.closed = true
		t.termErr = err
		pending := t.pending
		t.pending = make(map[string]rpcPending)
		t.mu.Unlock()

		t.cancel()
		if t.conn != nil {
			_ = t.conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			_ = t.conn.Close()
		}
		for _, p := range pending {
			p.result <- rpcResult{err: err}
		}
		if unexpected && t.onError != nil {
			t.onError(err)
		}
	})
}

func (t *WSTransport) terminalError(method string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminalErrorLocked(method)
}

func (t *WSTransport) terminalErrorLocked(method string) error {
	if t.termErr != nil {
		return fmt.Errorf("ACP WebSocket transport closed while waiting for %s: %w", method, t.termErr)
	}
	return errors.New("ACP WebSocket transport is closed")
}

// Close 停止 transport 并回收连接与后台 goroutine。可重复调用。
func (t *WSTransport) Close() error {
	t.terminate(errors.New("ACP WebSocket transport closed"), false)
	<-t.done
	return nil
}

// Done 在 transport 完整回收后关闭。
func (t *WSTransport) Done() <-chan struct{} { return t.done }
