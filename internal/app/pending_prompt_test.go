package app

// agent loop 执行期间提交输入的"先停止再发送"行为测试。相关实现见 app.go 的
// pendingPrompt / cancelHandoffWait 与 keymap.go 的 holdPromptUntilStopped。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// promptBackend 记录发往会话的 prompt 与 Cancel 调用；ignoreStop 为 true 时
// Cancel 不回 idle，模拟"服务端没在等待窗口内停下来"。
type promptBackend struct {
	*demo.Backend
	mu         sync.Mutex
	prompts    []string
	cancels    int
	ignoreStop bool
}

func (b *promptBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &promptSession{Session: s, b: b}, nil
}

func (b *promptBackend) sent() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.prompts...)
}

func (b *promptBackend) cancelCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancels
}

// promptSession 包装 acp.Session，记录 prompt 文本与 Cancel 次数。
type promptSession struct {
	acp.Session
	b *promptBackend
}

func (s *promptSession) SendPrompt(ctx context.Context, text string) error {
	s.b.mu.Lock()
	s.b.prompts = append(s.b.prompts, text)
	s.b.mu.Unlock()
	return s.Session.SendPrompt(ctx, text)
}

func (s *promptSession) Cancel(ctx context.Context) error {
	s.b.mu.Lock()
	s.b.cancels++
	ignore := s.b.ignoreStop
	s.b.mu.Unlock()
	if ignore {
		// 模拟服务端未响应取消：会话保持 running，不推 idle。
		return nil
	}
	return s.Session.Cancel(ctx)
}

// typeText 逐字符把文本敲进输入框。
func typeText(ft *fakeTerm, text string) {
	for _, r := range text {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
}

// enterRunning 在首页输入 prompt 并回车，等待会话进入 running。
func enterRunning(t *testing.T, ft *fakeTerm, a *App, text string) {
	t.Helper()
	typeText(ft, text)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap := a.snapshot(); snap.HasActive && snap.ActiveRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("session never entered running state")
}

// TestSubmitWhileRunningStopsThenSends 验证 agent loop 执行期间提交输入时：
// 先取消当前运行，会话停稳后把这段输入发出去，而不是像以前那样被后端丢弃。
func TestSubmitWhileRunningStopsThenSends(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	tb := &promptBackend{Backend: demo.New(true)}
	a := New(ft, tb)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	enterRunning(t, ft, a, "first")

	// 会话仍在 running：回车提交应转为"先停止再发送"。
	typeText(ft, "second")
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	time.Sleep(700 * time.Millisecond)
	quitApp(ft)
	waitRun(t, done)

	got := tb.sent()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("prompts = %v, want [first second]", got)
	}
	if tb.cancelCount() == 0 {
		t.Error("expected the running session to be cancelled before resending")
	}
}

// TestSubmitWhileRunningNotStoppedKeepsInput 验证会话没在 cancelHandoffWait 内
// 停下来时：输入不发送，输入框内容保持原样供用户重试。
func TestSubmitWhileRunningNotStoppedKeepsInput(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	tb := &promptBackend{Backend: demo.New(true), ignoreStop: true}
	a := New(ft, tb)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	enterRunning(t, ft, a, "first")

	typeText(ft, "second")
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	// 等待窗口（cancelHandoffWait = 200ms）过后仍不应发送。
	time.Sleep(500 * time.Millisecond)
	if got := tb.sent(); len(got) != 1 {
		t.Fatalf("prompts = %v, want only the first prompt", got)
	}
	if snap := a.snapshot(); !snap.ActiveRunning {
		t.Fatal("session should still be running while the server ignores cancel")
	}
	quitApp(ft)
	waitRun(t, done)

	if tb.cancelCount() == 0 {
		t.Error("expected a cancel request for the running session")
	}
	if text := a.model.Input.Text(); text != "second" {
		t.Errorf("input box = %q, want the submitted text preserved", text)
	}
}
