package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// promptRecord 记录一次 SendPrompt 调用。
type promptRecord struct {
	sessionID string
	text      string
}

// promptRecordingBackend 包装 demo backend，记录会话创建与 SendPrompt 调用，
// 用于验证 One Shot 模式启动后自动发送的消息。caps 覆盖 AgentCapabilities，
// 为空时回退 demo 默认（声明 session.delete）。
type promptRecordingBackend struct {
	*demo.Backend
	mu      sync.Mutex
	prompts []promptRecord
	created []string
	caps    acp.AgentCapabilities
}

func (b *promptRecordingBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.created = append(b.created, s.ID())
	b.mu.Unlock()
	return &promptRecordingSession{Session: s, b: b}, nil
}

func (b *promptRecordingBackend) AgentCapabilities() acp.AgentCapabilities {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.caps.Raw) > 0 {
		return b.caps
	}
	return b.Backend.AgentCapabilities()
}

func (b *promptRecordingBackend) record(id, text string) {
	b.mu.Lock()
	b.prompts = append(b.prompts, promptRecord{sessionID: id, text: text})
	b.mu.Unlock()
}

func (b *promptRecordingBackend) recordedPrompts() []promptRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]promptRecord(nil), b.prompts...)
}

func (b *promptRecordingBackend) createdCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.created)
}

func (b *promptRecordingBackend) firstCreated() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.created) == 0 {
		return ""
	}
	return b.created[0]
}

type promptRecordingSession struct {
	acp.Session
	b *promptRecordingBackend
}

func (s *promptRecordingSession) SendPrompt(ctx context.Context, text string) error {
	s.b.record(s.ID(), text)
	return s.Session.SendPrompt(ctx, text)
}

// TestInitialPromptAutoSendsOnStartup 验证 One Shot 模式：设置启动消息后，
// 应用启动即复用主页预创建会话进入会话视图并自动发送该消息，无需手动输入，
// 与主页输入 prompt 回车同一条路径（不新建、不删除预创建会话）。
func TestInitialPromptAutoSendsOnStartup(t *testing.T) {
	ft := newFakeTerm()
	b := &promptRecordingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	a.SetInitialPrompt("帮我实现一个 TCP 服务器")
	done := runApp(t, a)

	// 启动后应恰好收到一次 SendPrompt，文本与启动消息一致。
	waitCondition(t, "initial prompt sent", func() bool { return len(b.recordedPrompts()) == 1 })
	// 应自动进入会话视图，预创建会话被提升为活动会话且已开始运行。
	waitCondition(t, "session view with running active", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.View == model.ViewSession && a.model.Active != nil && a.model.Active.Running()
	})

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if got := b.recordedPrompts(); len(got) != 1 || got[0].text != "帮我实现一个 TCP 服务器" {
		t.Errorf("prompts = %v, want exactly [帮我实现一个 TCP 服务器]", got)
	}
	// 复用预创建会话：只创建过一次（预创建本身），活动会话即该会话。
	if got := b.createdCount(); got != 1 {
		t.Errorf("sessions created %d times, want 1 (pre-session reused)", got)
	}
	preID := b.firstCreated()
	if a.model.Active == nil || a.model.Active.ID != preID {
		t.Errorf("active session = %v, want pre-session %q", a.model.Active, preID)
	}
	if len(b.recordedPrompts()) != 1 || b.recordedPrompts()[0].sessionID != preID {
		t.Errorf("prompt sent to session %v, want %q", b.recordedPrompts()[0].sessionID, preID)
	}
}

// TestInitialPromptCreatesNewWithoutPreSession 验证主页没有预创建会话（服务端未
// 声明 session.delete）时，One Shot 模式新建会话并发送消息。
func TestInitialPromptCreatesNewWithoutPreSession(t *testing.T) {
	ft := newFakeTerm()
	b := &promptRecordingBackend{Backend: demo.New(true)}
	b.caps = acp.AgentCapabilities{Raw: json.RawMessage(`{}`)}
	a := New(ft, b)
	a.SetInitialPrompt("你好")
	done := runApp(t, a)

	waitCondition(t, "initial prompt sent", func() bool { return len(b.recordedPrompts()) == 1 })
	waitCondition(t, "session view with active", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.View == model.ViewSession && a.model.Active != nil
	})

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if got := b.recordedPrompts(); len(got) != 1 || got[0].text != "你好" {
		t.Errorf("prompts = %v, want exactly [你好]", got)
	}
	if a.model.Active == nil {
		t.Error("active session should be set after new-session path")
	}
}

// TestInitialPromptEmptyDoesNothing 验证未设置启动消息时启动流程与普通交互一致：
// 停留在主页，不发送任何消息。
func TestInitialPromptEmptyDoesNothing(t *testing.T) {
	ft := newFakeTerm()
	b := &promptRecordingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	waitCondition(t, "home ready", func() bool { return homeCommandsReady(a) })
	time.Sleep(100 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if got := b.recordedPrompts(); len(got) != 0 {
		t.Errorf("prompts = %v, want none without initial message", got)
	}
}
