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
	"github.com/cxykevin/alcoh/internal/model"
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

// waitForPrependedPage 轮询拉取后端事件，直到更早的一页真正落到正文（首条消息
// 变成 want）。服务端的推送与加载响应来自两条后台路径，"先渲染、后收到响应"是
// 正常时序，这里刻意不消费命令结果，用于复现该时序。
func waitForPrependedPage(t *testing.T, a *App, b *earlierBackend, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.drain(b.Events())
		if msgs := a.model.Active.Messages; len(msgs) > 0 && msgs[0].MessageID == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("first message = %v, want %s before the response arrives", a.model.Active.Messages, want)
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

// preparePagedSession 准备一个"正文超出一屏、视口停在顶部"的活动会话，
// 用于断言各个滚动入口滚到顶部时都会续取更早一页（见 maybeLoadEarlierHistory）。
func preparePagedSession(t *testing.T, b *earlierBackend) *App {
	t.Helper()
	ft := newFakeTerm()
	a := New(ft, b)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a.runCtx = ctx

	s, err := b.ResumeSession(ctx, "demo-0")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	a.sess = s
	a.model.ActivateSession(s.ID(), s.Title())
	a.model.View = model.ViewSession
	a.drain(b.Events())

	active := a.model.Active
	active.FollowBottom = false
	active.Scroll = 0
	active.Rows = 60
	active.MaxScroll = 20 // 渲染层回写：正文确实超出一屏
	return a
}

// TestPageUpAtTopLoadsEarlierHistory 验证焦点留在输入框时，PgUp 滚到正文顶部
// 也会续取更早一页（不必先 Tab 切到消息区）。
func TestPageUpAtTopLoadsEarlierHistory(t *testing.T) {
	b := &earlierBackend{Backend: demo.New(true)}
	defer b.Close()
	a := preparePagedSession(t, b)

	a.sessionKey(input.SimpleKey(input.KeyPageUp))
	if !a.model.Active.HistoryLoading {
		t.Fatal("PgUp at the top should start loading earlier history")
	}
	waitLoadCount(t, b, 1)

	res := waitHistoryCommand(t, a)
	a.drain(b.Events())
	a.applyCommandResult(res)
	if a.model.Active.HistoryLoading {
		t.Error("loading flag should be cleared when the page arrives")
	}
}

// TestWheelAtTopLoadsEarlierHistory 验证鼠标滚轮滚到正文顶部也会续取更早一页。
func TestWheelAtTopLoadsEarlierHistory(t *testing.T) {
	b := &earlierBackend{Backend: demo.New(true)}
	defer b.Close()
	a := preparePagedSession(t, b)

	a.dispatchMouse(input.MouseEvent{Button: input.MouseWheelUp, Action: input.MousePress, X: 50, Y: 10})
	if !a.model.Active.HistoryLoading {
		t.Fatal("wheel up at the top should start loading earlier history")
	}
	waitLoadCount(t, b, 1)

	res := waitHistoryCommand(t, a)
	a.drain(b.Events())
	a.applyCommandResult(res)
	if got := a.model.Active.Messages; len(got) == 0 || got[0].MessageID != "msg_91" {
		t.Fatalf("first message = %v, want msg_91 (earlier page prepended)", got)
	}
}

// TestScrollTopWithoutOverflowKeepsIdle 验证正文不满一屏（MaxScroll == 0）时
// PgUp 与滚轮都不会发起翻页请求：没有更上面的内容可翻。
func TestScrollTopWithoutOverflowKeepsIdle(t *testing.T) {
	b := &earlierBackend{Backend: demo.New(true)}
	defer b.Close()
	a := preparePagedSession(t, b)
	a.model.Active.MaxScroll = 0

	a.sessionKey(input.SimpleKey(input.KeyPageUp))
	a.dispatchMouse(input.MouseEvent{Button: input.MouseWheelUp, Action: input.MousePress, X: 50, Y: 10})
	if got := b.loadCount(); got != 0 {
		t.Fatalf("earlier history requests = %d, want 0 without overflow", got)
	}
	if a.model.Active.HistoryLoading {
		t.Error("no request should leave the loading flag set")
	}
}

// TestEarlierHistoryPageRenderedBeforeResponseKeepsAnchor 验证"这一页先被推过来
// 渲染、加载响应随后才到"的时序：锚点在开始加载时就已记录，新行提前画出来也
// 不影响补偿，视口仍停在用户原本看到的位置；响应到达只解除加载状态，不再动偏移。
// 回归：曾把锚点记录在加载结束时，此时行数已涨上去，行数差归零、补偿失效，
// 视口直接跳到最早一条。
func TestEarlierHistoryPageRenderedBeforeResponseKeepsAnchor(t *testing.T) {
	b := &earlierBackend{Backend: demo.New(true)}
	defer b.Close()
	a := preparePagedSession(t, b)

	a.messageKey(input.SimpleKey(input.KeyUp))
	if !a.model.Active.HistoryLoading {
		t.Fatal("loading flag should be set after reaching the top")
	}
	waitLoadCount(t, b, 1)

	// 服务端在这一页的响应到达前就推送了消息（推送与响应来自两条后台路径，
	// 这里只等推送落到模型，不消费命令结果），渲染层随即回写新的行数。
	active := a.model.Active
	waitForPrependedPage(t, a, b, "msg_91")
	active.Rows = 80 // 前面多了 20 行
	active.AdjustScrollForHistoryAnchor(active.Rows)
	if got := active.Scroll; got != 20 {
		t.Fatalf("scroll = %d, want 20 while the page renders before the response", got)
	}

	// 响应到达：解除加载状态，视口保持不动（锚点已消费，不再重复补偿）。
	res := waitHistoryCommand(t, a)
	a.applyCommandResult(res)
	if active.HistoryLoading {
		t.Error("loading flag should be cleared when the page arrives")
	}
	active.AdjustScrollForHistoryAnchor(active.Rows)
	if got := active.Scroll; got != 20 {
		t.Errorf("scroll = %d, want 20 after the response", got)
	}
}
