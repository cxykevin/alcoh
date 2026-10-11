package app

// /rewind 命令的交互测试：打开对话选择器、运行中先停止再打开、确认后发送
// 隐藏命令、结果到达后裁剪正文并回填输入框。相关实现见 keymap.go 的
// startRewind / confirmRewind / applyRewindOutcome 与 app.go 的 pendingRewindCh。

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

// rewindSession 是只记录调用的 acp.Session 假实现：测试关注 App 发出的隐藏
// 命令与 Cancel 请求，不需要真实后端。
type rewindSession struct {
	mu      sync.Mutex
	id      string
	prompts []string
	cancels int
}

func (s *rewindSession) ID() string    { return s.id }
func (s *rewindSession) Title() string { return "" }

func (s *rewindSession) SendPrompt(_ context.Context, text string) error {
	s.mu.Lock()
	s.prompts = append(s.prompts, text)
	s.mu.Unlock()
	return nil
}

func (s *rewindSession) Cancel(context.Context) error {
	s.mu.Lock()
	s.cancels++
	s.mu.Unlock()
	return nil
}

func (s *rewindSession) Close(context.Context) error { return nil }

func (s *rewindSession) ApprovePermission(context.Context, string, acp.PermissionOutcome, *string) error {
	return nil
}

func (s *rewindSession) SetConfigOption(context.Context, string, string, string) error { return nil }

func (s *rewindSession) RespondElicitation(context.Context, acp.RPCID, acp.ElicitationAction, json.RawMessage) error {
	return nil
}

func (s *rewindSession) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.prompts...)
}

func (s *rewindSession) cancelCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancels
}

// newRewindApp 构造一个已进入会话视图、声明 alkaid0 v0.6 能力且带用户消息的
// App。不启动事件循环：runCtx 直接给出，使异步命令仍会真正发送。
func newRewindApp(t *testing.T) (*App, *rewindSession) {
	t.Helper()
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	a.runCtx = context.Background()
	a.model.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.6":{}}`),
	})
	s := &rewindSession{id: "s1"}
	a.sess = s
	a.model.ActivateSession("s1", "test")
	for _, ev := range []struct {
		id   string
		user bool
		text string
	}{
		{"msg_1", true, "第一条"},
		{"msg_2", false, "回复一"},
		{"msg_3", true, "第二条"},
	} {
		a.model.ApplyEvent(&acp.MessageChunkEvent{SessionID: "s1", MessageID: ev.id, IsUser: ev.user, Text: ev.text})
	}
	return a, s
}

// waitPrompts 等待异步发送的 prompt 达到期望序列。
func waitPrompts(t *testing.T, s *rewindSession, want []string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := s.sent()
		if len(got) == len(want) {
			matched := true
			for i := range want {
				if got[i] != want[i] {
					matched = false
					break
				}
			}
			if matched {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("prompts = %v, want %v", s.sent(), want)
}

// waitCancels 等待异步发出的 Cancel 请求到达假会话。
func waitCancels(t *testing.T, s *rewindSession, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.cancelCount() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cancels = %d, want >= %d", s.cancelCount(), want)
}

// TestRewindOpensPickerAndSendsHiddenCommand 验证会话空闲时 /rewind 直接打开对话
// 选择器（不请求停止），回车确认后发出隐藏命令 /__alk_rewind <messageId>。
func TestRewindOpensPickerAndSendsHiddenCommand(t *testing.T) {
	a, s := newRewindApp(t)

	a.startRewind()
	if a.model.Modal != model.ModalRewind {
		t.Fatalf("modal = %v, want ModalRewind", a.model.Modal)
	}
	if a.pendingRewindOpen || a.pendingRewindCh != nil {
		t.Fatal("空闲会话不应挂起 /rewind")
	}
	if s.cancelCount() != 0 {
		t.Fatalf("cancels = %d, want 0", s.cancelCount())
	}

	a.rewindKey(input.SimpleKey(input.KeyEnter))
	// 第一条 DB 消息（msg_1）不能作为目标，选择器只有 msg_3。
	waitPrompts(t, s, []string{"/__alk_rewind msg_3"})
	if a.model.Modal != model.NoModal {
		t.Fatalf("modal = %v, want NoModal（确认后关闭选择器）", a.model.Modal)
	}
}

// TestRewindWhileRunningStopsThenOpens 验证运行中触发 /rewind 时先请求停止当前
// 运行，等会话停稳后才打开对话选择器（与服务端命令一样等 cancelHandoffWait）。
func TestRewindWhileRunningStopsThenOpens(t *testing.T) {
	a, s := newRewindApp(t)
	a.model.Active.State = acp.StateRunning

	a.startRewind()
	if a.model.Modal == model.ModalRewind {
		t.Fatal("运行中不应立即打开选择器")
	}
	if !a.pendingRewindOpen || a.pendingRewindCh == nil {
		t.Fatal("应挂起 /rewind 等会话停稳")
	}
	waitCancels(t, s, 1)
	waitPrompts(t, s, nil)

	// 会话停稳：下一轮主循环把挂起的选择器打开。
	a.model.Active.State = acp.StateIdle
	a.flushPendingRewind()
	if a.model.Modal != model.ModalRewind {
		t.Fatalf("modal = %v, want ModalRewind", a.model.Modal)
	}
	if a.pendingRewindOpen || a.pendingRewindCh != nil {
		t.Fatal("打开选择器后应清空挂起状态")
	}
	if got := s.sent(); len(got) != 0 {
		t.Fatalf("prompts = %v, want none before confirming", got)
	}
}

// TestRewindWhileRunningNotStoppedDrops 验证等待窗口内没停稳就放弃本次撤回：
// 不打开选择器，并给出提示。
func TestRewindWhileRunningNotStoppedDrops(t *testing.T) {
	a, _ := newRewindApp(t)
	a.model.Active.State = acp.StateRunning

	a.startRewind()
	a.dropPendingRewind()
	if a.model.Modal == model.ModalRewind {
		t.Fatal("超时放弃时不应打开选择器")
	}
	if a.pendingRewindOpen || a.pendingRewindCh != nil {
		t.Fatal("放弃后应清空挂起状态")
	}
	if a.model.Error == "" {
		t.Fatal("放弃时应给出提示")
	}
}

// TestRewindOutcomeFillsInputAndTruncates 验证 rewind 结果到达后：正文裁到目标
// 之前，被撤回消息的原文回填输入框（用户改完可直接重发），结果只消费一次。
func TestRewindOutcomeFillsInputAndTruncates(t *testing.T) {
	a, _ := newRewindApp(t)

	a.model.ApplyEvent(&acp.RewindEvent{SessionID: "s1", MessageID: "msg_3", Success: true})
	a.applyRewindOutcome()

	if got := a.model.Input.Text(); got != "第二条" {
		t.Fatalf("input = %q, want %q", got, "第二条")
	}
	if n := len(a.model.Active.Messages); n != 2 {
		t.Fatalf("messages = %d, want 2", n)
	}
	if a.model.TakeRewindOutcome() != nil {
		t.Fatal("结果应已被消费")
	}
}

// TestRewindOutcomeFailureShowsReason 验证失败的 rewind 只提示服务端原因，
// 不裁剪正文、不动输入框。
func TestRewindOutcomeFailureShowsReason(t *testing.T) {
	a, _ := newRewindApp(t)

	a.model.ApplyEvent(&acp.RewindEvent{SessionID: "s1", MessageID: "msg_3", Success: false, Error: "目标已被撤回"})
	a.applyRewindOutcome()

	if a.model.Error == "" {
		t.Fatal("失败时应给出提示")
	}
	if n := len(a.model.Active.Messages); n != 3 {
		t.Fatalf("messages = %d, want 3（失败不改动正文）", n)
	}
	if got := a.model.Input.Text(); got != "" {
		t.Fatalf("input = %q, want empty", got)
	}
}
