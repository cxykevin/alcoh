package app

// 会话消息队列的交互测试：运行中提交直接入队（不再先停止）、/queue 面板的
// 列表/取消/清空/编辑请求。

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

// msgQueueBackend 包装 demo backend，声明 alkaid0 v0.6 + msgqueue 能力，并记录
// prompt 发送、Cancel 与队列管理调用。
type msgQueueBackend struct {
	*demo.Backend
	mu       sync.Mutex
	prompts  []string
	cancels  int
	queue    []acp.MsgQueueItem
	revision uint64
	cancelsQ []string
	updates  []msgQueueUpdate
	clears   int
	listCall int
}

type msgQueueUpdate struct {
	ID   string
	Text *string
	Args *string
}

func (b *msgQueueBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.6":{},"alk.cxykevin.top/msgqueue":{}}`)}
}

func (b *msgQueueBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &msgQueueSession{Session: s, b: b}, nil
}

func (b *msgQueueBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &msgQueueSession{Session: s, b: b}, nil
}

func (b *msgQueueBackend) sent() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.prompts...)
}

func (b *msgQueueBackend) cancelCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancels
}

// msgQueueSession 记录 prompt/Cancel，并实现 acp.MsgQueueControl。
type msgQueueSession struct {
	acp.Session
	b *msgQueueBackend
}

func (s *msgQueueSession) SendPrompt(ctx context.Context, text string) error {
	s.b.mu.Lock()
	s.b.prompts = append(s.b.prompts, text)
	s.b.mu.Unlock()
	return s.Session.SendPrompt(ctx, text)
}

func (s *msgQueueSession) Cancel(ctx context.Context) error {
	s.b.mu.Lock()
	s.b.cancels++
	s.b.mu.Unlock()
	return s.Session.Cancel(ctx)
}

func (s *msgQueueSession) ListMsgQueue(ctx context.Context) (acp.MsgQueueSnapshot, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.listCall++
	return acp.MsgQueueSnapshot{Revision: s.b.revision, Items: append([]acp.MsgQueueItem(nil), s.b.queue...)}, nil
}

func (s *msgQueueSession) CancelMsgQueueItem(ctx context.Context, itemID string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.cancelsQ = append(s.b.cancelsQ, itemID)
	for i, it := range s.b.queue {
		if it.ID == itemID {
			s.b.queue = append(s.b.queue[:i], s.b.queue[i+1:]...)
			break
		}
	}
	return nil
}

func (s *msgQueueSession) UpdateMsgQueueItem(ctx context.Context, itemID string, text, args *string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.updates = append(s.b.updates, msgQueueUpdate{ID: itemID, Text: text, Args: args})
	for i := range s.b.queue {
		if s.b.queue[i].ID != itemID {
			continue
		}
		if text != nil {
			s.b.queue[i].Text = *text
		}
		if args != nil {
			s.b.queue[i].Args = *args
		}
	}
	return nil
}

func (s *msgQueueSession) ClearMsgQueue(ctx context.Context) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.clears++
	s.b.queue = nil
	return nil
}

// inlineQueueSession 只实现排队框交互用到的 ID 与取消 RPC；其它会话方法经
// 内嵌的 nil 接口提供（测试不会调用）。
type inlineQueueSession struct {
	acp.Session
	id string
	b  *msgQueueBackend
}

func (s *inlineQueueSession) ID() string    { return s.id }
func (s *inlineQueueSession) Title() string { return "inline" }

func (s *inlineQueueSession) ListMsgQueue(ctx context.Context) (acp.MsgQueueSnapshot, error) {
	return acp.MsgQueueSnapshot{}, nil
}

func (s *inlineQueueSession) CancelMsgQueueItem(ctx context.Context, itemID string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.cancelsQ = append(s.b.cancelsQ, itemID)
	return nil
}

func (s *inlineQueueSession) UpdateMsgQueueItem(ctx context.Context, itemID string, text, args *string) error {
	return nil
}

func (s *inlineQueueSession) ClearMsgQueue(ctx context.Context) error { return nil }

// newInlineQueueApp 构造带活动会话与排队项的 App（不启动事件循环），用于
// 直接驱动按键处理。
func newInlineQueueApp(t *testing.T, items []acp.MsgQueueItem) (*App, *msgQueueBackend) {
	t.Helper()
	b := &msgQueueBackend{}
	a := New(newFakeTerm(), demo.New(true))
	a.runCtx = context.Background()
	a.model.SetAgentInfo(acp.AgentInfo{}, (&msgQueueBackend{}).AgentCapabilities())
	a.model.ActivateSession("s1", "会话")
	a.model.Active.MsgQueue = items
	a.sess = &inlineQueueSession{id: "s1", b: b}
	return a, b
}

// TestQueueInlineUpFocusesList 验证输入框里按 ↑ 先聚焦排队框并选中最后一条；
// 在框内到顶后再按 ↑ 才交还输入历史。
func TestQueueInlineUpFocusesList(t *testing.T) {
	a, _ := newInlineQueueApp(t, []acp.MsgQueueItem{{ID: "q1", Kind: model.MsgQueueKindPrompt, Text: "排队内容"}})
	a.model.Input.History = []string{"older prompt"}

	a.dispatchKey(input.SimpleKey(input.KeyUp))
	if !a.model.QueueFocused {
		t.Fatal("↑ with queued items should focus the queue list")
	}
	// 已经在唯一一项（最顶上）：再按 ↑ 退出排队框并浏览输入历史。
	a.dispatchKey(input.SimpleKey(input.KeyUp))
	if a.model.QueueFocused {
		t.Fatal("↑ at queue top should hand focus back to the input box")
	}
	if got := a.model.Input.Text(); got != "older prompt" {
		t.Fatalf("input history = %q, want older prompt", got)
	}
}

// TestQueueInlineDeleteRestoresInput 验证 Backspace/Delete 删除排队项并把它的
// 内容无条件替换到输入框（覆盖原有草稿）。
func TestQueueInlineDeleteRestoresInput(t *testing.T) {
	for _, key := range []input.KeyType{input.KeyBackspace, input.KeyDelete} {
		a, b := newInlineQueueApp(t, []acp.MsgQueueItem{{ID: "q1", Kind: model.MsgQueueKindPrompt, Text: "排队内容"}})
		for _, r := range "draft" {
			a.model.Input.InsertRune(r)
		}
		a.dispatchKey(input.SimpleKey(input.KeyUp)) // 聚焦排队框
		a.dispatchKey(input.SimpleKey(key))
		if got := a.model.Input.Text(); got != "排队内容" {
			t.Errorf("key %v: input = %q, want queue content (draft overwritten)", key, got)
		}
		if len(a.model.QueueItems()) != 0 {
			t.Errorf("key %v: queue should drop the deleted item, got %v", key, a.model.QueueItems())
		}
		if a.model.QueueFocused {
			t.Errorf("key %v: focus should leave the empty queue", key)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			b.mu.Lock()
			n := len(b.cancelsQ)
			b.mu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		b.mu.Lock()
		cancelled := append([]string(nil), b.cancelsQ...)
		b.mu.Unlock()
		if len(cancelled) != 1 || cancelled[0] != "q1" {
			t.Errorf("key %v: cancelled = %v, want [q1]", key, cancelled)
		}
	}
}

// TestQueueInlineEditKeys 验证排队框上 Tab/空格/i 都进入编辑；命令项只把
// args 放回输入框，提交时由服务端重建命令文本。
func TestQueueInlineEditKeys(t *testing.T) {
	cases := []struct {
		name string
		key  input.KeyEvent
	}{
		{"tab", input.SimpleKey(input.KeyTab)},
		{"space", input.RuneKey(' ', input.ModNone)},
		{"i", input.RuneKey('i', input.ModNone)},
	}
	for _, c := range cases {
		a, _ := newInlineQueueApp(t, []acp.MsgQueueItem{{
			ID: "q1", Kind: model.MsgQueueKindCommand, Command: "/compact", Args: "--now", Text: "/compact --now",
		}})
		a.dispatchKey(input.SimpleKey(input.KeyUp))
		a.dispatchKey(c.key)
		if a.queueEdit == nil {
			t.Errorf("%s: should enter queue edit mode", c.name)
			continue
		}
		if a.model.QueueFocused {
			t.Errorf("%s: editing should leave queue focus", c.name)
		}
		if got := a.model.Input.Text(); got != "--now" {
			t.Errorf("%s: input = %q, want command args --now", c.name, got)
		}
	}
}

// TestSubmitWhileRunningEnqueues 验证服务端声明 msgqueue 能力时，agent loop
// 执行中提交的输入直接交给服务端排队：prompt 照常发送、不取消当前运行、
// 输入框被消费。
func TestSubmitWhileRunningEnqueues(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &msgQueueBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	enterRunning(t, ft, a, "first")

	typeText(ft, "second")
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := b.sent(); len(got) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := b.sent()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("prompts = %v, want [first second]", got)
	}
	if n := b.cancelCount(); n != 0 {
		t.Fatalf("running submission must not cancel the session, got %d cancel(s)", n)
	}

	quitApp(ft)
	waitRun(t, done)
	if text := a.model.Input.Text(); text != "" {
		t.Errorf("input should be consumed after enqueue, got %q", text)
	}
}

// TestQueuePanelListCancelClear 验证 /queue 打开面板并拉取快照，d 取消选中项、
// c 清空队列都发往对应 RPC。
func TestQueuePanelListCancelClear(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &msgQueueBackend{Backend: demo.New(true)}
	b.queue = []acp.MsgQueueItem{
		{ID: "q1", Kind: model.MsgQueueKindPrompt, Text: "第一条"},
		{ID: "q2", Kind: model.MsgQueueKindPrompt, Text: "第二条"},
	}
	b.revision = 7
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	for _, r := range "/queue" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.Modal == model.ModalMsgQueue && len(s.QueueItems) == 2 })

	// d：取消选中第一条；c：清空剩余队列。
	ft.sendKey(input.RuneKey('d', input.ModNone))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('c', input.ModNone))
	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEsc)) // 关闭面板后才能 Ctrl+Q 退出
	time.Sleep(50 * time.Millisecond)

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	cancelled := append([]string(nil), b.cancelsQ...)
	clears := b.clears
	items := append([]acp.MsgQueueItem(nil), b.queue...)
	b.mu.Unlock()
	if len(cancelled) != 1 || cancelled[0] != "q1" {
		t.Errorf("cancelled items = %v, want [q1]", cancelled)
	}
	if clears != 1 {
		t.Errorf("clear calls = %d, want 1", clears)
	}
	if len(items) != 0 {
		t.Errorf("queue should be empty after clear, got %v", items)
	}
}

// TestQueueEditUpdatesItem 验证 e 编辑选中排队项：内容回填输入框，回车走
// msgqueue/update（而不是发送新 prompt）。
func TestQueueEditUpdatesItem(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &msgQueueBackend{Backend: demo.New(true)}
	b.queue = []acp.MsgQueueItem{{ID: "q1", Kind: model.MsgQueueKindPrompt, Text: "旧内容"}}
	b.revision = 3
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	for _, r := range "/queue" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.Modal == model.ModalMsgQueue && len(s.QueueItems) == 1 })

	// Enter/e 进入编辑：输入框回填旧内容，面板关闭。
	ft.sendKey(input.RuneKey('e', input.ModNone))
	time.Sleep(100 * time.Millisecond)

	// 清掉旧内容输入新内容后回车保存。
	ft.sendKey(input.RuneKey('u', input.ModCtrl))
	typeText(ft, "新内容")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		n := len(b.updates)
		b.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	updates := append([]msgQueueUpdate(nil), b.updates...)
	prompts := append([]string(nil), b.prompts...)
	b.mu.Unlock()
	if len(updates) != 1 {
		t.Fatalf("updates = %v, want 1", updates)
	}
	if updates[0].ID != "q1" || updates[0].Text == nil || *updates[0].Text != "新内容" {
		t.Errorf("update = %+v, want q1 text 新内容", updates[0])
	}
	if len(prompts) != 0 {
		t.Errorf("queue edit must not send a prompt, got %v", prompts)
	}
}
