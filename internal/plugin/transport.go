// Package plugin 实现 alcoh 前端的插件宿主：以本地子进程方式启动插件，
// 经 NDJSON JSON-RPC 2.0（stdin/stdout）通信，消息 payload 用 protobuf
// 编码（schema 见 proto/plugin/v1/plugin.proto），向 TUI 的若干流程注入
// hooks（prompt 改写 / 按键拦截 / 事件观察 / 斜杠命令）。
package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
)

const (
	defaultMaxRPCLine = 4 << 20
	// defaultShutdownTimeout 是退出时等待插件进程自行退出的时长。
	defaultShutdownTimeout = 2 * time.Second
	// ProtocolVersion 是插件协议版本（InitializeRequest.protocol_version）。
	ProtocolVersion = 1
)

// pluginConfig 是单个插件进程的启动参数。
type pluginConfig struct {
	Name    string
	Command string
	Args    []string
	Dir     string
	Env     []string
}

// incomingHandler 接收解析后的插件入站 JSON-RPC 消息。handler 不应阻塞。
type incomingHandler func(acp.IncomingMessage)

// transportErrorHandler 在 transport 非预期终止时最多调用一次。
type transportErrorHandler func(error)

// pending 记录一个等待响应的 JSON-RPC 请求。
type pending struct {
	method string
	result chan rpcResult
}

type rpcResult struct {
	result []byte
	err    error
}

// transport 是插件进程的 NDJSON JSON-RPC 2.0 stdio transport。
// Done 关闭意味着子进程已 Wait、所有 reader/watcher 都已退出。
type transport struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]pending
	nextID  atomic.Uint64
	closed  bool
	termErr error

	handle  incomingHandler
	onError transportErrorHandler

	terminateOnce sync.Once
}

// startTransport 启动插件子进程并开始读取其 stdout 上的 NDJSON 协议流。
func startTransport(parent context.Context, cfg pluginConfig, handler incomingHandler, onError transportErrorHandler) (*transport, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, errors.New("plugin command is empty")
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create plugin stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("create plugin stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("create plugin stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	t := &transport{
		name: cfg.Name, cmd: cmd, stdin: stdin, ctx: ctx, cancel: cancel,
		pending: make(map[string]pending), handle: handler, onError: onError,
		done: make(chan struct{}),
	}
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

// request 发出 JSON-RPC request，并等待对应 response 或 context 取消。
func (t *transport) request(ctx context.Context, method string, params []byte) ([]byte, error) {
	id := acp.NewRPCID([]byte(fmt.Sprintf("%d", t.nextID.Add(1))))
	key := id.Key()
	p := pending{method: method, result: make(chan rpcResult, 1)}
	if err := t.addPending(key, p); err != nil {
		return nil, err
	}
	line, err := acp.MarshalRequest(id, method, envelope{Data: params})
	if err != nil {
		t.removePending(key)
		return nil, err
	}
	if err := t.write(line); err != nil {
		t.removePending(key)
		t.terminate(err, true)
		return nil, err
	}
	select {
	case received := <-p.result:
		if received.err != nil {
			return nil, received.err
		}
		var env envelope
		if len(received.result) > 0 && string(received.result) != "null" {
			if err := unmarshalEnvelope(received.result, &env); err != nil {
				return nil, fmt.Errorf("decode RPC %s result: %w", method, err)
			}
		}
		return env.Data, nil
	case <-ctx.Done():
		t.removePending(key)
		return nil, ctx.Err()
	case <-t.ctx.Done():
		return nil, t.terminalError(method)
	}
}

// notify 发出不期待 response 的 JSON-RPC notification。
func (t *transport) notify(method string, params []byte) error {
	line, err := acp.MarshalNotification(method, envelope{Data: params})
	if err != nil {
		return err
	}
	if err := t.write(line); err != nil {
		t.terminate(err, true)
		return err
	}
	return nil
}

// respond 以插件发来的原始 request ID 回复成功结果。
func (t *transport) respond(id acp.RPCID, result []byte) error {
	line, err := acp.MarshalResult(id, envelope{Data: result})
	if err != nil {
		return err
	}
	if err := t.write(line); err != nil {
		t.terminate(err, true)
		return err
	}
	return nil
}

// respondError 以插件发来的原始 request ID 回复 JSON-RPC error。
func (t *transport) respondError(id acp.RPCID, rpcErr acp.RPCError) error {
	line, err := acp.MarshalError(id, rpcErr)
	if err != nil {
		return err
	}
	if err := t.write(line); err != nil {
		t.terminate(err, true)
		return err
	}
	return nil
}

func (t *transport) addPending(key string, p pending) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return t.terminalErrorLocked("request")
	}
	t.pending[key] = p
	return nil
}

func (t *transport) write(line []byte) error {
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
		return errors.New("plugin transport is closed")
	}
	line = append(append([]byte(nil), line...), '\n')
	if _, err := t.stdin.Write(line); err != nil {
		return fmt.Errorf("write plugin message: %w", err)
	}
	return nil
}

func (t *transport) readLoop(stdout io.Reader) {
	defer t.wg.Done()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), defaultMaxRPCLine)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		incoming, err := acp.DecodeIncoming(line)
		if err != nil {
			t.terminate(fmt.Errorf("decode plugin %s message: %w", t.name, err), true)
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
		t.terminate(fmt.Errorf("read plugin %s stdout: %w", t.name, err), true)
		return
	}
	select {
	case <-t.ctx.Done():
	default:
		t.terminate(io.EOF, true)
	}
}

func (t *transport) drainStderr(stderr io.Reader) {
	defer t.wg.Done()
	// 插件 stderr 绝不能阻塞协议进程；直接丢弃，避免污染 alternate screen。
	_, _ = io.Copy(io.Discard, stderr)
}

func (t *transport) waitLoop() {
	defer t.wg.Done()
	err := t.cmd.Wait()
	select {
	case <-t.ctx.Done():
		return
	default:
	}
	if err != nil {
		t.terminate(fmt.Errorf("plugin %s exited: %w", t.name, err), true)
	} else {
		t.terminate(io.EOF, true)
	}
}

func (t *transport) watchParent(parent context.Context) {
	defer t.wg.Done()
	select {
	case <-parent.Done():
		t.terminate(parent.Err(), true)
	case <-t.ctx.Done():
	}
}

func (t *transport) deliverResponse(response *acp.RPCResponse) {
	key := response.ID.Key()
	t.mu.Lock()
	p, ok := t.pending[key]
	if ok {
		delete(t.pending, key)
	}
	t.mu.Unlock()
	if !ok {
		return // 已取消或重复 response；不影响其它请求。
	}
	if response.Error != nil {
		p.result <- rpcResult{err: &acp.RPCRemoteError{Method: p.method, RPCError: *response.Error}}
		return
	}
	p.result <- rpcResult{result: append([]byte(nil), response.Result...)}
}

func (t *transport) removePending(key string) {
	t.mu.Lock()
	delete(t.pending, key)
	t.mu.Unlock()
}

// terminate 是唯一的终态入口。unexpected 决定是否上报 transport 错误。
func (t *transport) terminate(err error, unexpected bool) {
	t.terminateOnce.Do(func() {
		if err == nil {
			err = errors.New("plugin transport closed")
		}
		t.mu.Lock()
		t.closed = true
		t.termErr = err
		pendings := t.pending
		t.pending = make(map[string]pending)
		t.mu.Unlock()

		t.cancel()
		_ = t.stdin.Close()
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		for _, p := range pendings {
			p.result <- rpcResult{err: err}
		}
		if unexpected && t.onError != nil {
			t.onError(err)
		}
	})
}

func (t *transport) terminalError(method string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminalErrorLocked(method)
}

func (t *transport) terminalErrorLocked(method string) error {
	if t.termErr != nil {
		return fmt.Errorf("plugin transport closed while waiting for %s: %w", method, t.termErr)
	}
	return errors.New("plugin transport is closed")
}

// shutdownGraceful 发送 shutdown notification 并等待插件自行退出，
// 超时后强制 kill。
func (t *transport) shutdownGraceful(timeout time.Duration) error {
	_ = t.notify("shutdown", nil)
	select {
	case <-t.done:
		return nil
	case <-time.After(timeout):
		t.terminate(fmt.Errorf("plugin %s did not exit in time", t.name), false)
		<-t.done
		return nil
	}
}

// close 停止 transport 并回收插件子进程与后台 goroutine。可重复调用。
func (t *transport) close() error {
	t.terminate(errors.New("plugin transport closed"), false)
	<-t.done
	return nil
}

// done 在 transport 完整回收后关闭。
func (t *transport) doneCh() <-chan struct{} { return t.done }
