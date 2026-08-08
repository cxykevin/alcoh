package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// preSessionTrackingBackend 包装 demo backend，记录 NewSession 与 DeleteSession
// 的会话 id，用于验证主页预创建会话的创建与删除生命周期。
type preSessionTrackingBackend struct {
	*demo.Backend
	mu      sync.Mutex
	created []string
	deleted []string
}

func (b *preSessionTrackingBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.created = append(b.created, s.ID())
	b.mu.Unlock()
	return s, nil
}

func (b *preSessionTrackingBackend) DeleteSession(ctx context.Context, id string) error {
	b.mu.Lock()
	b.deleted = append(b.deleted, id)
	b.mu.Unlock()
	return b.Backend.DeleteSession(ctx, id)
}

func (b *preSessionTrackingBackend) createdCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.created)
}

func (b *preSessionTrackingBackend) firstCreated() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.created) == 0 {
		return ""
	}
	return b.created[0]
}

func (b *preSessionTrackingBackend) deletedIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.deleted...)
}

func contains(id string, ids []string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// waitCondition 轮询直到条件满足或超时。
func waitCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within 5s: %s", what)
}

// homeCommandsReady 报告主页命令面板 /effort 与 /model 是否可用（预创建会话的
// config 已应用）。经锁内读取，避免与 UI 主循环并发访问。
func homeCommandsReady(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.View == model.ViewHome && a.model.PreSession != nil &&
		a.model.SupportsEffort() && a.model.SupportsModel()
}

// TestPreSessionCreatedOnHomeAndDeletedOnExit 验证：进入主页即预创建一个会话，
// 其 config 使主页 /effort 与 /model 可用；程序退出时删除该空会话。
func TestPreSessionCreatedOnHomeAndDeletedOnExit(t *testing.T) {
	ft := newFakeTerm()
	b := &preSessionTrackingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	// 启动进入主页后应预创建一个会话。
	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })
	// 预创建会话的 config 广播使主页命令面板可用 /effort 与 /model。
	waitCondition(t, "home effort/model enabled", func() bool { return homeCommandsReady(a) })

	// 停在主页直接退出。
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	preID := b.firstCreated()
	if preID == "" {
		t.Fatal("no pre-session was created")
	}
	if !contains(preID, b.deletedIDs()) {
		t.Errorf("pre-session %q should be deleted on exit; deleted=%v", preID, b.deletedIDs())
	}
	if a.model.PreSession != nil {
		t.Error("model pre-session should be cleared after exit")
	}
}

// TestPreSessionReusedOnHomeInput 验证：主页正常输入 prompt 回车时直接复用预创建
// 会话——不删除、不新建，prompt 发送到该会话并进入会话视图。
func TestPreSessionReusedOnHomeInput(t *testing.T) {
	ft := newFakeTerm()
	b := &preSessionTrackingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })
	preID := b.firstCreated()

	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "pre-session promoted to active", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.View == model.ViewSession && a.model.Active != nil && a.model.Active.ID == preID
	})

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if got := b.createdCount(); got != 1 {
		t.Errorf("sessions created %d times, want 1 (pre-session reused, no new session)", got)
	}
	if len(b.deletedIDs()) != 0 {
		t.Errorf("reused pre-session should not be deleted, deleted=%v", b.deletedIDs())
	}
	if a.model.PreSession != nil {
		t.Error("model pre-session should be promoted to active, not remain")
	}
	if a.model.Active == nil || a.model.Active.ID != preID {
		t.Errorf("active session = %v, want pre-session %q", a.model.Active, preID)
	}
}

// TestPreSessionDeletedOnResume 验证：用户恢复旧会话时，主页预创建的空会话被丢弃
// （本地状态清空 + 后台删除），进入会话后 /effort 与 /model 作用于活动会话。
func TestPreSessionDeletedOnResume(t *testing.T) {
	ft := newFakeTerm()
	b := &preSessionTrackingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })
	preID := b.firstCreated()

	// 按 Enter 恢复 demo-0：预创建空会话应被丢弃。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(400 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.View != model.ViewSession {
		t.Fatalf("expected session view after resume, got %v", a.model.View)
	}
	if preID == "" {
		t.Fatal("no pre-session was created")
	}
	if !contains(preID, b.deletedIDs()) {
		t.Errorf("pre-session %q should be deleted after resume; deleted=%v", preID, b.deletedIDs())
	}
	if a.model.PreSession != nil {
		t.Error("model pre-session should be cleared after resume")
	}
}

// TestPreSessionRecreatedAfterGoHome 验证：从会话 /clear 返回主页后重新预创建
// 会话（主页命令面板 /effort 与 /model 再次可用），且两次预创建会话最终都被删除。
func TestPreSessionRecreatedAfterGoHome(t *testing.T) {
	ft := newFakeTerm()
	b := &preSessionTrackingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })

	// 进入会话再 /clear 回主页：应重新预创建会话。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)
	for _, r := range "/clear" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "pre-session recreated", func() bool { return b.createdCount() == 2 })
	waitCondition(t, "home commands ready after /clear", func() bool { return homeCommandsReady(a) })

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if got := b.createdCount(); got != 2 {
		t.Errorf("pre-session created %d times, want 2 (startup + after /clear)", got)
	}
	deleted := b.deletedIDs()
	if len(deleted) != 2 {
		t.Errorf("pre-session deleted %d times, want 2; deleted=%v", len(deleted), deleted)
	}
	if a.model.PreSession != nil {
		t.Error("model pre-session should be cleared after exit")
	}
}
