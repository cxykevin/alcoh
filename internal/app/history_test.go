package app

// 历史回放分页（alkaid0 v0.6）在 app 层的接线测试：滚到正文顶部触发取更早一页、
// 加载中不重复请求、命令完成后解除加载状态并按新增行数补偿滚动偏移。
// 相关实现见 keymap.go 的 messageKey/loadEarlierHistory 与 app.go 的
// applyCommandResult（commandHistoryPage 分支）。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// earlierBackend 包装 demo backend，统计"取更早一页"的请求次数。
type earlierBackend struct {
	*demo.Backend
	mu    sync.Mutex
	loads int
}

func (b *earlierBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &earlierSession{Session: s, b: b}, nil
}

func (b *earlierBackend) loadCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loads
}

// earlierSession 透传 HistoryPager 并记录调用，用于断言"不重复发起请求"。
type earlierSession struct {
	acp.Session
	b *earlierBackend
}

func (s *earlierSession) MoreHistory() bool {
	pager, ok := s.Session.(acp.HistoryPager)
	return ok && pager.MoreHistory()
}

func (s *earlierSession) LoadEarlierHistory(ctx context.Context) (bool, error) {
	s.b.mu.Lock()
	s.b.loads++
	s.b.mu.Unlock()
	return s.Session.(acp.HistoryPager).LoadEarlierHistory(ctx)
}

// waitLoadCount 等待后台 goroutine 真正发出第 n 次请求。
func waitLoadCount(t *testing.T, b *earlierBackend, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b.loadCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("earlier history requests = %d, want at least %d", b.loadCount(), want)
}

// waitHistoryCommand 等待历史分页命令完成。
func waitHistoryCommand(t *testing.T, a *App) commandResult {
	t.Helper()
	select {
	case res := <-a.commands:
		if res.kind != commandHistoryPage {
			t.Fatalf("command kind = %v, want commandHistoryPage", res.kind)
		}
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the earlier-history command")
		return commandResult{}
	}
}

// TestScrollToTopLoadsEarlierHistory 验证滚到正文顶部触发更早一页的加载：
// 加载中显示提示并阻止重复请求；命令完成后解除加载状态、历史消息按 messageId
// 归位到正文之前，并据新增行数补偿滚动偏移（视口停在原处）；已翻到活跃分支
// 最早一条后不再请求。
func TestScrollToTopLoadsEarlierHistory(t *testing.T) {
	ft := newFakeTerm()
	b := &earlierBackend{Backend: demo.New(true)}
	defer b.Close()
	a := New(ft, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.runCtx = ctx

	s, err := b.ResumeSession(ctx, "demo-0")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	a.sess = s
	a.model.ActivateSession(s.ID(), s.Title())
	a.drain(b.Events())

	active := a.model.Active
	active.FollowBottom = false
	active.Scroll = 0
	active.Rows = 60
	active.MaxScroll = 20 // 渲染层回写：正文确实超出一屏

	// 用户在正文顶部继续上滚 → 取更早的一页。
	a.messageKey(input.SimpleKey(input.KeyUp))
	if !a.model.Active.HistoryLoading {
		t.Fatal("loading flag should be set after reaching the top")
	}
	waitLoadCount(t, b, 1)
	// 加载中再按几次不会重复发起请求。
	for i := 0; i < 3; i++ {
		a.messageKey(input.SimpleKey(input.KeyUp))
	}
	if got := b.loadCount(); got != 1 {
		t.Fatalf("earlier history requests = %d, want 1 while loading", got)
	}

	// 命令完成：回放的消息由服务端经 session/update 推送（与首屏同一路径）。
	res := waitHistoryCommand(t, a)
	a.drain(b.Events())
	a.applyCommandResult(res)

	if a.model.Active.HistoryLoading {
		t.Error("loading flag should be cleared when the page arrives")
	}
	messages := a.model.Active.Messages
	if len(messages) == 0 || messages[0].MessageID != "msg_91" {
		t.Fatalf("first message = %v, want msg_91 (the replayed page goes before known history)", messages)
	}
	for i, id := range []string{"msg_91", "msg_92", "msg_93", "msg_94"} {
		if messages[i].MessageID != id {
			t.Fatalf("messages[%d] = %s, want %s", i, messages[i].MessageID, id)
		}
	}

	// 渲染层按新增行数补偿：这一页前面多了 20 行，视口从 0 补到 20 后停在原处。
	a.model.Active.Rows = 80
	a.model.Active.AdjustScrollForHistoryAnchor(80)
	if got := a.model.Active.Scroll; got != 20 {
		t.Errorf("scroll = %d, want 20 after 20 prepended lines", got)
	}

	// 已到活跃分支最早一条（demo 只带一页更早的历史）：再滚到顶也不请求。
	a.model.Active.Scroll = 0
	a.messageKey(input.SimpleKey(input.KeyUp))
	if got := b.loadCount(); got != 1 {
		t.Errorf("earlier history requests = %d, want 1 after the last page", got)
	}
	if a.model.Active.HistoryLoading {
		t.Error("no request should leave the loading flag set")
	}
}
