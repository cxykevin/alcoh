//go:build !js && !wasip1

package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultMaxRPCLine = 4 << 20

// CommandConfig 是本地 ACP agent 子进程配置。Command 和 Args 直接作为 argv
// 传给 os/exec，不经过 shell 解析。
type CommandConfig struct {
	Command         string
	Args            []string
	Dir             string
	Env             []string
	MaxLineBytes    int
	ShutdownTimeout time.Duration
}

// IncomingHandler 接收解析后的 agent 入站 JSON-RPC 消息。handler 不应阻塞；
// 若需耗时工作应自行转发到上层队列。
type IncomingHandler func(IncomingMessage)

// TransportErrorHandler 在 transport 非预期终止时最多调用一次。
type TransportErrorHandler func(error)

// StdioTransport 是 ACP agent 的 NDJSON JSON-RPC 2.0 transport。
// Done 关闭意味着子进程已 Wait、所有 reader/watcher 都已退出。
type StdioTransport struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

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

	maxLine int
	timeout time.Duration
	handle  IncomingHandler
	onError TransportErrorHandler

	terminateOnce sync.Once
}

// StartStdioTransport 启动 agent 并开始读取其 stdout 上的 NDJSON 协议流。
func StartStdioTransport(parent context.Context, config CommandConfig, handler IncomingHandler, onError TransportErrorHandler) (*StdioTransport, error) {
	if strings.TrimSpace(config.Command) == "" {
		return nil, errors.New("ACP agent command is empty")
	}
	if config.MaxLineBytes <= 0 {
		config.MaxLineBytes = defaultMaxRPCLine
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 2 * time.Second
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Dir = config.Dir
	if len(config.Env) > 0 {
		cmd.Env = append(cmd.Environ(), config.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create agent stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("create agent stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("create agent stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("start ACP agent: %w", err)
	}
	t := &StdioTransport{
		cmd: cmd, stdin: stdin, ctx: ctx, cancel: cancel, pending: make(map[string]rpcPending),
		maxLine: config.MaxLineBytes, timeout: config.ShutdownTimeout, handle: handler, onError: onError, done: make(chan struct{}),
	}
	// reader、stderr drainer、唯一 Wait 所有者和 parent context watcher。
	t.wg.Add(4)
	go t.readLoop(stdout)
	go t.drainStderr(stderr)
	go t.waitLoop()
	go t.watchParent(parent)
	go func() {
		t.wg.Wait()
		close(t.done)
	}()
	return t, nil
}

// Request 发出 JSON-RPC request，并等待对应 response 或 context 取消。
func (t *StdioTransport) Request(ctx context.Context, method string, params any, result any) error {
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
func (t *StdioTransport) Notify(method string, params any) error {
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
func (t *StdioTransport) Respond(id RPCID, result any) error {
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
func (t *StdioTransport) RespondError(id RPCID, rpcErr RPCError) error {
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

func (t *StdioTransport) addPending(key string, pending rpcPending) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return t.terminalErrorLocked("request")
	}
	t.pending[key] = pending
	return nil
}

func (t *StdioTransport) write(line []byte) error {
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
		return errors.New("ACP transport is closed")
	}
	line = append(append([]byte(nil), line...), '\n')
	if _, err := t.stdin.Write(line); err != nil {
		return fmt.Errorf("write ACP message: %w", err)
	}
	return nil
}

func (t *StdioTransport) readLoop(stdout io.Reader) {
	defer t.wg.Done()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), t.maxLine)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		incoming, err := DecodeIncoming(line)
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
	if err := scanner.Err(); err != nil {
		t.terminate(fmt.Errorf("read ACP stdout: %w", err), true)
		return
	}
	select {
	case <-t.ctx.Done():
	default:
		t.terminate(io.EOF, true)
	}
}

func (t *StdioTransport) drainStderr(stderr io.Reader) {
	defer t.wg.Done()
	// agent stderr 绝不能阻塞协议进程；不向 TUI 输出，避免污染 alternate screen。
	_, _ = io.Copy(io.Discard, stderr)
}

func (t *StdioTransport) waitLoop() {
	defer t.wg.Done()
	err := t.cmd.Wait()
	select {
	case <-t.ctx.Done():
		return
	default:
	}
	if err != nil {
		t.terminate(fmt.Errorf("ACP agent exited: %w", err), true)
	} else {
		t.terminate(io.EOF, true)
	}
}

func (t *StdioTransport) watchParent(parent context.Context) {
	defer t.wg.Done()
	select {
	case <-parent.Done():
		t.terminate(parent.Err(), true)
	case <-t.ctx.Done():
	}
}

func (t *StdioTransport) deliverResponse(response *RPCResponse) {
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

func (t *StdioTransport) removePending(key string) {
	t.mu.Lock()
	delete(t.pending, key)
	t.mu.Unlock()
}

// terminate 是唯一的终态入口。unexpected 决定是否上报 transport 错误。
func (t *StdioTransport) terminate(err error, unexpected bool) {
	t.terminateOnce.Do(func() {
		if err == nil {
			err = errors.New("ACP transport closed")
		}
		t.mu.Lock()
		t.closed = true
		t.termErr = err
		pending := t.pending
		t.pending = make(map[string]rpcPending)
		t.mu.Unlock()

		t.cancel()
		_ = t.stdin.Close()
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		for _, p := range pending {
			p.result <- rpcResult{err: err}
		}
		if unexpected && t.onError != nil {
			t.onError(err)
		}
	})
}

func (t *StdioTransport) terminalError(method string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminalErrorLocked(method)
}

func (t *StdioTransport) terminalErrorLocked(method string) error {
	if t.termErr != nil {
		return fmt.Errorf("ACP transport closed while waiting for %s: %w", method, t.termErr)
	}
	return errors.New("ACP transport is closed")
}

// Close 停止 transport，并回收 agent 子进程与后台 goroutine。可重复调用。
func (t *StdioTransport) Close() error {
	t.terminate(errors.New("ACP transport closed"), false)
	<-t.done
	return nil
}

// Done 在 transport 完整回收后关闭。
func (t *StdioTransport) Done() <-chan struct{} { return t.done }
