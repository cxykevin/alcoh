package app

// 后台运行模式指示器的交互测试：进入会话即经 get_background 同步一次状态，
// 状态切换（如 /background off 命令执行完毕回到 idle）后重新查询。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// backgroundBackend 包装 demo backend：声明 alkaid0 v0.4 能力，并记录
// get_background 的查询次数与当前返回值。
type backgroundBackend struct {
	*demo.Backend
	mu         sync.Mutex
	background bool
	calls      int
	prompts    []string
	cancels    int
	caps       json.RawMessage
}

func (b *backgroundBackend) AgentCapabilities() acp.AgentCapabilities {
	if len(b.caps) > 0 {
		return acp.AgentCapabilities{Raw: b.caps}
	}
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
}

func (b *backgroundBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &backgroundSession{Session: s, b: b}, nil
}

func (b *backgroundBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &backgroundSession{Session: s, b: b}, nil
}

// backgroundSession 实现 acp.BackgroundControl；状态保存在 backend 上。
type backgroundSession struct {
	acp.Session
	b *backgroundBackend
}

func (s *backgroundSession) GetBackground(ctx context.Context) (bool, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.calls++
	return s.b.background, nil
}

// SendPrompt 记录客户端发出的文本，并按服务端 /background 命令的语义更新模式；
// 不启动 demo 脚本，保持测试里的会话状态可控。
func (s *backgroundSession) SendPrompt(ctx context.Context, text string) error {
	s.b.mu.Lock()
	s.b.prompts = append(s.b.prompts, text)
	if arg, ok := strings.CutPrefix(text, "/background "); ok {
		s.b.background = strings.TrimSpace(arg) == "on"
	}
	s.b.mu.Unlock()
	return nil
}

// Cancel 记录取消请求：/background 覆盖指令不应取消当前运行。
func (s *backgroundSession) Cancel(ctx context.Context) error {
	s.b.mu.Lock()
	s.b.cancels++
	s.b.mu.Unlock()
	return nil
}

// TestBackgroundModeSyncOnEntryAndStateChange 验证进入会话时查询后台模式，
// 状态切换（/background off 执行完毕）后重新查询并更新指示器状态。
func TestBackgroundModeSyncOnEntryAndStateChange(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &backgroundBackend{Backend: demo.New(true), background: true}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.HasActive && s.Background
	})

	// 服务端执行 /background off 后，状态切换时客户端应重新查询。
	b.mu.Lock()
	b.background = false
	b.mu.Unlock()
	a.withModel(func() {
		a.applyEvent(&acp.StateChangeEvent{SessionID: a.model.Active.ID, State: acp.StateIdle})
	})
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.HasActive && !s.Background
	})

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	calls := b.calls
	b.mu.Unlock()
	if calls < 2 {
		t.Errorf("get_background calls = %d, want >= 2 (entry + state change)", calls)
	}
}

// TestBackgroundCommandBypassesQueue 验证 /background 是客户端覆盖指令：
// 会话运行中提交也直接发送服务端命令，不取消当前运行（因此不会走"先停止
// 再发送"或消息队列的路径）。
func TestBackgroundCommandBypassesQueue(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &backgroundBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.HasActive })

	// 模拟 agent loop 正在执行（状态 running）。
	a.withModel(func() {
		a.applyEvent(&acp.StateChangeEvent{SessionID: a.model.Active.ID, State: acp.StateRunning})
	})

	typeText(ft, "/background on")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.HasActive && s.Background
	})

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	prompts := append([]string(nil), b.prompts...)
	cancels := b.cancels
	b.mu.Unlock()
	if !slices.Contains(prompts, "/background on") {
		t.Fatalf("prompts = %v, want /background on sent directly", prompts)
	}
	if cancels != 0 {
		t.Errorf("cancels = %d, want 0 (override command must not stop the running turn)", cancels)
	}
}

// TestBackgroundModeSkippedWithoutCapability 验证服务端未声明 alkaid0 扩展能力时
// 不发送 get_background 查询（旧服务端/第三方 ACP 服务端）。
func TestBackgroundModeSkippedWithoutCapability(t *testing.T) {
	b := &backgroundBackend{Backend: demo.New(true), caps: json.RawMessage(`{"session":{"delete":{}}}`)}
	s, err := b.NewSession(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	a := New(newFakeTerm(), b)
	a.model.ActivateSession(s.ID(), "")
	a.sess = s

	a.refreshBackgroundMode()

	b.mu.Lock()
	calls := b.calls
	b.mu.Unlock()
	if calls != 0 {
		t.Errorf("get_background calls = %d, want 0 without alkaid0 capability", calls)
	}
}
