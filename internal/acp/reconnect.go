package acp

import (
	"context"
	"fmt"
	"time"
)

// ReconnectConfig 描述 transport 意外断开后的自动重连策略（见 ClientConfig.Reconnect）。
// 重连按指数退避尝试：首次等待 MinDelay，之后每次翻倍，直到 MaxDelay。
type ReconnectConfig struct {
	// MinDelay 是首次重试前的等待；<=0 时使用 1 秒。
	MinDelay time.Duration
	// MaxDelay 是退避上限；<=0 时使用 30 秒。
	MaxDelay time.Duration
	// HandshakeTimeout 是单次重连后 initialize 握手的超时；<=0 时使用 10 秒。
	HandshakeTimeout time.Duration
	// MaxAttempts 是最大尝试次数；0 表示无限重试。
	MaxAttempts int
}

const (
	defaultReconnectMinDelay         = time.Second
	defaultReconnectMaxDelay         = 30 * time.Second
	defaultReconnectHandshakeTimeout = 10 * time.Second
)

func (c *ReconnectConfig) retryDelay() time.Duration {
	if c == nil || c.MinDelay <= 0 {
		return defaultReconnectMinDelay
	}
	return c.MinDelay
}

func (c *ReconnectConfig) nextDelay(d time.Duration) time.Duration {
	maxDelay := defaultReconnectMaxDelay
	if c != nil && c.MaxDelay > 0 {
		maxDelay = c.MaxDelay
	}
	if d <= 0 {
		d = defaultReconnectMinDelay
	}
	d *= 2
	if d > maxDelay {
		d = maxDelay
	}
	return d
}

func (c *ReconnectConfig) handshakeTimeout() time.Duration {
	if c == nil || c.HandshakeTimeout <= 0 {
		return defaultReconnectHandshakeTimeout
	}
	return c.HandshakeTimeout
}

// handshake 在 transport 上完成 initialize / initialized 握手。
// timeout > 0 时给整次握手加超时（重连路径用它避免卡死在半开的连接上）。
func (b *ClientBackend) handshake(ctx context.Context, t Transport, timeout time.Duration) (InitializeResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var result InitializeResult
	err := t.Request(ctx, MethodInitialize, InitializeParams{
		ProtocolVersion: b.config.ProtocolVersion,
		Capabilities:    b.config.Capabilities,
		Info:            b.config.ClientInfo,
	}, &result)
	if err == nil && result.ProtocolVersion != b.config.ProtocolVersion {
		err = fmt.Errorf("ACP protocol version mismatch: agent=%d client=%d", result.ProtocolVersion, b.config.ProtocolVersion)
	}
	if err == nil {
		err = t.Notify(MethodInitialized, InitializedParams{})
	}
	return result, err
}

// reconnectLoop 在 transport 意外断开后按 ReconnectConfig 退避重连，并在新连接上
// 重新完成 initialize 握手。成功时把新 transport 装回 backend 并广播
// ReconnectSucceeded；调用方据此重新绑定活动会话（见 SessionReattacher）。
func (b *ClientBackend) reconnectLoop(old Transport) {
	policy := b.config.Reconnect
	if policy == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if b.state != backendReconnecting {
		b.mu.Unlock()
		cancel()
		return
	}
	b.reconnectCancel = cancel
	b.mu.Unlock()
	defer func() {
		cancel()
		b.mu.Lock()
		b.reconnectCancel = nil
		b.mu.Unlock()
	}()

	// 旧 transport 已进入终态；Close 等待其读循环退出，保证它此后不会再触发
	// handleTransportError 干扰新连接。
	if old != nil {
		_ = old.Close()
	}

	delay := policy.retryDelay()
	for attempt := 1; ; attempt++ {
		if !sleepContext(ctx, delay) {
			return
		}
		t, err := b.config.TransportFactory(ctx, b.handleIncoming, b.handleTransportError)
		if err == nil {
			var result InitializeResult
			result, err = b.handshake(ctx, t, policy.handshakeTimeout())
			if err == nil {
				b.mu.Lock()
				if b.state != backendReconnecting {
					b.mu.Unlock()
					_ = t.Close()
					return
				}
				b.transport = t
				b.agentCaps = result.Capabilities
				b.agentInfo = result.Info
				b.state = backendReady
				b.mu.Unlock()
				b.emit(&ReconnectEvent{Phase: ReconnectSucceeded, Attempt: attempt})
				return
			}
			// 握手失败：Close 等读循环退出后再重试（见上面的注释）。
			_ = t.Close()
		}
		if ctx.Err() != nil {
			return
		}
		b.emit(&ReconnectEvent{Phase: ReconnectAttemptFailed, Attempt: attempt, Err: err})
		if policy.MaxAttempts > 0 && attempt >= policy.MaxAttempts {
			b.emit(&ReconnectEvent{Phase: ReconnectGiveUp, Attempt: attempt, Err: err})
			b.shutdown(true, fmt.Errorf("ACP transport reconnect failed after %d attempt(s): %w", attempt, err))
			return
		}
		delay = policy.nextDelay(delay)
	}
}

// sleepContext 等待 d；ctx 取消时返回 false。
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
