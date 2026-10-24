package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
)

// reattachRecordingSession 记录 Reattach 调用次数，模拟实现了 SessionReattacher
// 的真实会话（clientSession）。
type reattachRecordingSession struct {
	acp.Session
	calls atomic.Int32
}

func (s *reattachRecordingSession) Reattach(context.Context) error {
	s.calls.Add(1)
	return nil
}

// TestReconnectSuccessReattachesActiveSession 验证重连成功的应用层接线：
// 更新界面状态，并把当前会话重新绑定到新连接（只发一次），会话句柄不变。
func TestReconnectSuccessReattachesActiveSession(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.runCtx = ctx

	base, err := b.NewSession(ctx, ".")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer base.Close(ctx)
	s := &reattachRecordingSession{Session: base}
	a.sess = s
	a.withModel(func() { a.model.ApplyEvent(&acp.NewSessionEvent{Session: s}) })

	a.withModel(func() { a.applyEvent(&acp.ReconnectEvent{Phase: acp.ReconnectStarted}) })
	if !a.model.Reconnecting {
		t.Fatal("model should report reconnecting after ReconnectStarted")
	}

	a.withModel(func() { a.applyEvent(&acp.ReconnectEvent{Phase: acp.ReconnectSucceeded}) })

	// 重新绑定以异步命令执行：等待它的结果（同时排除会话列表刷新等其它结果）。
	deadline := time.After(2 * time.Second)
	for {
		select {
		case res := <-a.commands:
			a.withModel(func() { a.applyCommandResult(res) })
			if res.kind == commandSessionAction {
				if res.err != nil {
					t.Fatalf("reattach command failed: %v", res.err)
				}
				if got := s.calls.Load(); got != 1 {
					t.Fatalf("Reattach calls = %d, want 1", got)
				}
				if a.sess != s {
					t.Fatal("reattach must keep the existing session handle")
				}
				if a.model.Reconnecting {
					t.Fatal("model should leave the reconnecting state")
				}
				return
			}
		case <-deadline:
			t.Fatal("reattach command was not issued")
		}
	}
}
