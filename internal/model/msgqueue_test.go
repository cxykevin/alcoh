package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

func msgQueueCaps(v06, queue bool) acp.AgentCapabilities {
	raw := map[string]json.RawMessage{}
	if v06 {
		raw[acp.Alkaid0CapabilityV06] = json.RawMessage(`{}`)
	}
	if queue {
		raw[acp.Alkaid0CapabilityMsgQueue] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	return acp.AgentCapabilities{Raw: data}
}

// TestSupportsMsgQueue 验证队列能力门控：必须同时声明 alkaid0 v0.6 与
// msgqueue，只声明其中一个都不启用队列交互。
func TestSupportsMsgQueue(t *testing.T) {
	cases := []struct {
		name string
		v06  bool
		q    bool
		want bool
	}{
		{"both", true, true, true},
		{"only v0.6", true, false, false},
		{"only msgqueue", false, true, false},
		{"neither", false, false, false},
	}
	for _, c := range cases {
		m := New()
		m.AgentCaps = msgQueueCaps(c.v06, c.q)
		if got := m.SupportsMsgQueue(); got != c.want {
			t.Errorf("%s: SupportsMsgQueue = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestQueueAvailableNeedsActiveSession 验证 /queue 面板要求能力与活动会话同时具备。
func TestQueueAvailableNeedsActiveSession(t *testing.T) {
	m := New()
	m.AgentCaps = msgQueueCaps(true, true)
	if m.QueueAvailable() {
		t.Fatal("queue must be unavailable without an active session")
	}
	m.ActivateSession("s1", "会话")
	if !m.QueueAvailable() {
		t.Fatal("queue should be available with capability and active session")
	}
}

// TestApplyMsgQueueRevision 验证全量快照按 revision 单调应用：旧版本被丢弃、
// 等值版本可重复应用，并且快照被拷贝而不是共享底层数组。
func TestApplyMsgQueueRevision(t *testing.T) {
	s := NewSession("s1", "会话")
	if !s.ApplyMsgQueue(2, []acp.MsgQueueItem{{ID: "q1", Text: "one"}}) {
		t.Fatal("first snapshot should apply")
	}
	if len(s.MsgQueue) != 1 || s.MsgQueue[0].ID != "q1" {
		t.Fatalf("queue = %v, want [q1]", s.MsgQueue)
	}
	// 旧 revision：丢弃，队列保持。
	if s.ApplyMsgQueue(1, []acp.MsgQueueItem{{ID: "old"}}) {
		t.Fatal("older revision must be rejected")
	}
	if len(s.MsgQueue) != 1 || s.MsgQueue[0].ID != "q1" {
		t.Fatalf("stale snapshot must not override queue, got %v", s.MsgQueue)
	}
	// 等值 revision：允许重复应用（内容应与当前一致）。
	items := []acp.MsgQueueItem{{ID: "q2", Text: "two"}}
	if !s.ApplyMsgQueue(2, items) {
		t.Fatal("same revision should be applied")
	}
	items[0].ID = "mutated"
	if s.MsgQueue[0].ID != "q2" {
		t.Fatalf("queue snapshot should be copied, got %v", s.MsgQueue)
	}
}

// TestQueueInlineFocus 验证输入框上方的排队框焦点：↑ 进入时选中最后一条，
// ↑ 到顶返回 false（交还输入历史），↓ 到底同样返回 false。
func TestQueueInlineFocus(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "会话")
	m.Active.MsgQueue = []acp.MsgQueueItem{
		{ID: "q1", Text: "one"},
		{ID: "q2", Text: "two"},
		{ID: "q3", Text: "three"},
	}
	if !m.FocusQueue() {
		t.Fatal("FocusQueue should succeed with queued items")
	}
	if !m.QueueFocused || m.QueueSelect != 2 {
		t.Fatalf("focus = %v select = %d, want true/2 (bottom)", m.QueueFocused, m.QueueSelect)
	}
	if !m.QueueFocusMove(-1) || m.QueueSelect != 1 {
		t.Fatalf("move up = %v select = %d, want true/1", m.QueueFocused, m.QueueSelect)
	}
	if !m.QueueFocusMove(-1) || m.QueueSelect != 0 {
		t.Fatalf("move up to top = %v select = %d, want true/0", m.QueueFocused, m.QueueSelect)
	}
	if m.QueueFocusMove(-1) {
		t.Fatal("up at top should return false (hand over to input history)")
	}
	if !m.QueueFocusMove(1) || m.QueueSelect != 1 {
		t.Fatalf("move down = %v select = %d", m.QueueFocused, m.QueueSelect)
	}
	m.QueueSelect = 2
	if m.QueueFocusMove(1) {
		t.Fatal("down at bottom should return false")
	}
	m.BlurQueue()
	if m.QueueFocused {
		t.Fatal("BlurQueue should clear focus")
	}
	// 队列空时不能进入焦点。
	m.Active.MsgQueue = nil
	if m.FocusQueue() {
		t.Fatal("FocusQueue should fail on an empty queue")
	}
}

// TestRemoveQueueItemClampsFocus 验证删除排队项会收敛选中索引，删除最后一项
// 时同时退出排队框焦点。
func TestRemoveQueueItemClampsFocus(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "会话")
	m.Active.MsgQueue = []acp.MsgQueueItem{{ID: "q1"}, {ID: "q2"}}
	m.FocusQueue()
	if !m.RemoveQueueItem("q2") {
		t.Fatal("RemoveQueueItem should succeed")
	}
	if len(m.QueueItems()) != 1 || m.QueueSelect != 0 || !m.QueueFocused {
		t.Fatalf("after remove: n=%d select=%d focused=%v", len(m.QueueItems()), m.QueueSelect, m.QueueFocused)
	}
	if !m.RemoveQueueItem("q1") {
		t.Fatal("RemoveQueueItem should succeed")
	}
	if m.QueueFocused {
		t.Fatal("focus should drop when the queue becomes empty")
	}
	if m.RemoveQueueItem("missing") {
		t.Fatal("RemoveQueueItem should report a missing id")
	}
}

// TestQueueItemInputText 验证放回输入框的文本：prompt 用原文，command 还原成
// 完整命令；命令/参数都为空时退回 text。
func TestQueueItemInputText(t *testing.T) {
	cases := []struct {
		name string
		item acp.MsgQueueItem
		want string
	}{
		{"prompt", acp.MsgQueueItem{Kind: MsgQueueKindPrompt, Text: "hello"}, "hello"},
		{"command", acp.MsgQueueItem{Kind: MsgQueueKindCommand, Command: "/compact", Args: "--now", Text: "/compact --now"}, "/compact --now"},
		{"command no args", acp.MsgQueueItem{Kind: MsgQueueKindCommand, Command: "/compact", Text: "/compact"}, "/compact"},
		{"command fallback", acp.MsgQueueItem{Kind: MsgQueueKindCommand, Text: "raw"}, "raw"},
	}
	for _, c := range cases {
		if got := QueueItemInputText(c.item); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestQueueSelection 验证面板选中项的移动（带环绕）、按 ID 查找与越界收敛。
func TestQueueSelection(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "会话")
	m.Active.MsgQueue = []acp.MsgQueueItem{
		{ID: "q1", Text: "one"},
		{ID: "q2", Text: "two"},
	}
	if _, ok := m.QueueItem("q2"); !ok {
		t.Fatal("QueueItem should find q2")
	}
	m.QueueSelect = 0
	m.QueueMove(-1)
	if m.QueueSelect != 1 {
		t.Errorf("QueueMove(-1) from first = %d, want 1 (wrap)", m.QueueSelect)
	}
	m.QueueMove(1)
	if m.QueueSelect != 0 {
		t.Errorf("QueueMove(1) from last = %d, want 0 (wrap)", m.QueueSelect)
	}
	// 队列变短：QueueSelected 回退到最后一条而不是越界。
	m.SetModal(ModalMsgQueue)
	m.QueueSelect = 5
	if item, ok := m.QueueSelected(); !ok || item.ID != "q2" {
		t.Errorf("QueueSelected with out-of-range index = %v/%v, want q2", item.ID, ok)
	}
}
