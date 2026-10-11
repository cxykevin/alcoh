package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

func TestAlkaid0MessageOrdering(t *testing.T) {
	s := NewSession("s1", "test")
	s.SetAlkaid0MessageOrdering(true)
	for _, id := range []string{"msg_12", "msg_2", "msg_10"} {
		s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: id, Text: id})
	}
	got := []string{s.Messages[0].MessageID, s.Messages[1].MessageID, s.Messages[2].MessageID}
	want := []string{"msg_2", "msg_10", "msg_12"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("messages = %v, want %v", got, want)
		}
	}
	for i, item := range s.Timeline {
		if item.Message != nil && item.Message.MessageID != want[i] {
			t.Fatalf("timeline = %v, want message %s at %d", s.Timeline, want[i], i)
		}
	}
}

func TestAlkaid0CommandMessagesStayAtReceivePosition(t *testing.T) {
	// 斜杠命令的命令文本与命令回复用合成 ID cmd_<chatID>_<seq>：不入库、没有可与
	// dbID 比较的序号。它们不能因为"ID 不可比较"而一律沉到正文底部，而要停在收到
	// 时的位置；同时 msg_<dbID> 仍按 ID 排序。
	s := NewSession("s1", "test")
	s.SetAlkaid0MessageOrdering(true)
	for _, ev := range []struct {
		id   string
		user bool
	}{
		{"msg_1", true},
		{"msg_2", false},
		{"cmd_7_1", true},  // 命令文本
		{"cmd_7_2", false}, // 命令回复
		{"msg_3", false},
	} {
		s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: ev.id, IsUser: ev.user, Text: ev.id})
	}
	want := []string{"msg_1", "msg_2", "cmd_7_1", "cmd_7_2", "msg_3"}
	assertMessageOrder(t, s, want)

	// 乱序到达的 msg_<dbID> 仍按 ID 归位：命令消息停在收到时的槽位，前后的正文
	// 消息按 ID 排序填回其余槽位（cmd_7_1 收到时在 msg_12 之后、msg_2 之前）。
	shuffled := NewSession("s2", "test")
	shuffled.SetAlkaid0MessageOrdering(true)
	for _, id := range []string{"msg_12", "cmd_7_1", "msg_2"} {
		shuffled.AppendChunk(&acp.MessageChunkEvent{SessionID: "s2", MessageID: id, Text: id})
	}
	assertMessageOrder(t, shuffled, []string{"msg_2", "cmd_7_1", "msg_12"})
}

// assertMessageOrder 校验正文消息与时间线里消息项的顺序一致。
func assertMessageOrder(t *testing.T, s *SessionState, want []string) {
	t.Helper()
	if len(s.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d", len(s.Messages), len(want))
	}
	got := make([]string, len(s.Messages))
	for i, m := range s.Messages {
		got[i] = m.MessageID
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("messages = %v, want %v", got, want)
		}
	}
	pos := 0
	for i, item := range s.Timeline {
		if item.Message == nil {
			continue
		}
		if item.Message.MessageID != want[pos] {
			t.Fatalf("timeline[%d] = %s, want %s", i, item.Message.MessageID, want[pos])
		}
		pos++
	}
	if pos != len(want) {
		t.Fatalf("timeline has %d message items, want %d", pos, len(want))
	}
}

func TestMessageOrderingDisabledByDefault(t *testing.T) {
	s := NewSession("s1", "test")
	for _, id := range []string{"msg_12", "msg_2"} {
		s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: id})
	}
	if s.Messages[0].MessageID != "msg_12" {
		t.Fatalf("default order changed: %#v", s.Messages)
	}
}

func TestChunkAppend(t *testing.T) {
	s := NewSession("s1", "test")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "Hello "})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "world"})
	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Text != "Hello world" {
		t.Errorf("chunk append: %q, want %q", s.Messages[0].Text, "Hello world")
	}
}

func TestMessageUpdateReplace(t *testing.T) {
	s := NewSession("s1", "test")
	// 先 chunk 累积
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "partial"})
	// 整体替换
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1",
		Message: acp.Message{
			MessageID:  "m1",
			ContentSet: true,
			Content: []acp.ContentBlock{
				{Type: "text", Text: new("full")},
			},
		},
	})
	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Text != "full" {
		t.Errorf("replace: %q, want %q", s.Messages[0].Text, "full")
	}
	if !s.Messages[0].Done {
		t.Errorf("replace should mark done")
	}
}

func TestThoughtAutoCollapse(t *testing.T) {
	s := NewSession("s1", "test")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "t1", IsThought: true, Text: "thinking..."})
	// 思考消息在 chunk 流式时 expanded 应为 true（展示中）
	if !s.Messages[0].Expanded {
		t.Errorf("streaming thought should be expanded")
	}
	// 完整消息标记完成（真实 wire 中 thought 完整块是 agent_thought，IsThought=true）
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1",
		Message: acp.Message{
			MessageID:  "t1",
			ContentSet: true,
			Content:    []acp.ContentBlock{{Type: "text", Text: new("done")}},
		},
		IsThought: true,
	})
	if !s.Messages[0].Done {
		t.Errorf("thought should be done")
	}
	// idle → 自动折叠
	s.CollapseThoughts()
	if !s.Messages[0].Collapsed() {
		t.Errorf("thought should be collapsed after idle")
	}
	// 手动展开
	s.ToggleMessage("t1")
	if s.Messages[0].Collapsed() {
		t.Errorf("manual toggle should expand")
	}
}

func TestExpandAll(t *testing.T) {
	s := NewSession("s1", "test")
	// 两条思考：完整块后标记完成，再自动折叠。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "t1", IsThought: true, Text: "想 1"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "t2", IsThought: true, Text: "想 2"})
	for _, id := range []string{"t1", "t2"} {
		s.ApplyMessage(&acp.MessageUpdateEvent{
			SessionID: "s1", IsThought: true,
			Message: acp.Message{MessageID: id, ContentSet: true,
				Content: []acp.ContentBlock{{Type: "text", Text: new("想")}}},
		})
	}
	s.CollapseThoughts()
	// 一条展开、一条折叠的工具调用。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "tc1", Title: new("tool 1")})
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "tc2", Title: new("tool 2")})
	s.ToggleToolCall("tc2")
	for _, m := range s.Messages {
		if m.Kind != MsgThought || !m.Collapsed() {
			t.Fatalf("thought should be collapsed before ExpandAll")
		}
	}
	if !s.ToolCalls["tc1"].Expanded || s.ToolCalls["tc2"].Expanded {
		t.Fatalf("tool call expansion state wrong before ExpandAll")
	}

	s.ExpandAll()

	for _, m := range s.Messages {
		if m.Kind == MsgThought && !m.Expanded {
			t.Errorf("thought %s should be expanded after ExpandAll", m.MessageID)
		}
	}
	for id, tc := range s.ToolCalls {
		if !tc.Expanded {
			t.Errorf("tool call %s should be expanded after ExpandAll", id)
		}
	}
	if !s.AllExpanded() {
		t.Error("AllExpanded should be true after ExpandAll")
	}

	// 再次按 Ctrl+O：收回（全部折叠）。
	s.CollapseAll()
	for _, m := range s.Messages {
		if m.Kind == MsgThought && m.Expanded {
			t.Errorf("thought %s should be collapsed after CollapseAll", m.MessageID)
		}
	}
	for id, tc := range s.ToolCalls {
		if tc.Expanded {
			t.Errorf("tool call %s should be collapsed after CollapseAll", id)
		}
	}
	if s.AllExpanded() {
		t.Error("AllExpanded should be false after CollapseAll")
	}
	if !s.HasCollapsible() {
		t.Error("HasCollapsible should be true with thoughts and tool calls")
	}
}

func TestThoughtCollapsesWhenBodyStarts(t *testing.T) {
	// 思考结束后立即折叠，而不是等整个 turn 的 idle。
	s := NewSession("s1", "test")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", IsThought: true, Text: "思考中..."})
	if s.Messages[0].Collapsed() {
		t.Fatal("streaming thought should stay expanded")
	}
	// 正文 chunk 开始 → 思考流结束，立即折叠。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "正"})
	if !s.Messages[0].Done {
		t.Errorf("thought should be done when body starts")
	}
	if !s.Messages[0].Collapsed() {
		t.Errorf("thought should collapse immediately when body starts")
	}
	// 后续正文 chunk 不再影响已折叠的思考。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "文"})
	if !s.Messages[0].Collapsed() {
		t.Errorf("thought should stay collapsed")
	}
	// 完整块同理：thought 只有 chunk、正文是完整块时也立即折叠。
	s2 := NewSession("s2", "test")
	s2.AppendChunk(&acp.MessageChunkEvent{SessionID: "s2", MessageID: "x", IsThought: true, Text: "想"})
	s2.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s2", IsThought: false,
		Message: acp.Message{MessageID: "x", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("正文")}}},
	})
	if !s2.Messages[0].Done || !s2.Messages[0].Collapsed() {
		t.Errorf("thought should collapse when full body arrives, done=%v collapsed=%v",
			s2.Messages[0].Done, s2.Messages[0].Collapsed())
	}
}

func TestSharedMessageIDThoughtAndBodyStaySeparate(t *testing.T) {
	// alkaid0 流式期间 agent_thought_chunk 与 agent_message_chunk 共享同一 messageId
	// （都来自 resp.MsgID）。正文 chunk 绝不能追加进思维链，反之亦然。
	s := NewSession("s1", "test")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_20", IsThought: true, Text: "我先想一下。"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_20", Text: "这是"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_20", Text: "正文"})
	if len(s.Messages) != 2 {
		t.Fatalf("expected 2 separate messages (thought + body), got %d", len(s.Messages))
	}
	thought, body := s.Messages[0], s.Messages[1]
	if thought.Kind != MsgThought {
		t.Errorf("first should be thought, got %d", thought.Kind)
	}
	if thought.Text != "我先想一下。" {
		t.Errorf("thought text = %q, body leaked into thinking chain", thought.Text)
	}
	if body.Kind != MsgAssistant {
		t.Errorf("second should be assistant body, got %d", body.Kind)
	}
	if body.Text != "这是正文" {
		t.Errorf("body text = %q, want %q", body.Text, "这是正文")
	}
	// 完整块同理：agent_thought 与 agent_message 共享 id 时各归其位。
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1", IsThought: true,
		Message: acp.Message{MessageID: "msg_21", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("完整思考")}}},
	})
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1", IsThought: false,
		Message: acp.Message{MessageID: "msg_21", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("完整正文")}}},
	})
	if len(s.Messages) != 4 {
		t.Fatalf("expected 4 messages after full blocks, got %d", len(s.Messages))
	}
	if s.Messages[2].Text != "完整思考" || s.Messages[3].Text != "完整正文" {
		t.Errorf("full blocks misrouted: %q / %q", s.Messages[2].Text, s.Messages[3].Text)
	}
	// 时间线同样两条独立项。
	if len(s.Timeline) != 4 {
		t.Fatalf("expected 4 timeline items, got %d", len(s.Timeline))
	}
	for i, item := range s.Timeline {
		want := TimelineThought
		if i%2 == 1 {
			want = TimelineAssistantMessage
		}
		if item.Kind != want {
			t.Errorf("timeline[%d] kind = %d, want %d", i, item.Kind, want)
		}
	}
}

func TestToolCallUpsert(t *testing.T) {
	s := NewSession("s1", "test")
	status := acp.ToolPending
	kind := acp.KindWrite
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{
		SessionID: "s1", ToolCallID: "c1", Status: &status, Kind: &kind,
		Title: new("write_file"),
	})
	if len(s.ToolOrder) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(s.ToolOrder))
	}
	tc := s.ToolCalls["c1"]
	if tc.Title != "write_file" {
		t.Errorf("title: %q, want write_file", tc.Title)
	}
	// upsert 状态更新
	done := acp.ToolCompleted
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "c1", Status: &done})
	if s.ToolCalls["c1"].Status != acp.ToolCompleted {
		t.Errorf("status should update to completed")
	}
	// 数量不变
	if len(s.ToolOrder) != 1 {
		t.Errorf("upsert should not add duplicate, got %d", len(s.ToolOrder))
	}
}

func TestPlanReplace(t *testing.T) {
	s := NewSession("s1", "test")
	p1 := acp.Plan{Type: "items", PlanID: "p1", Entries: []acp.PlanEntry{
		{Content: "step1", Status: acp.PlanPending, Priority: acp.PriorityHigh},
	}}
	s.ApplyPlan(&acp.PlanUpdateEvent{SessionID: "s1", Plan: p1})
	if s.Plan == nil || len(s.Plan.Entries) != 1 {
		t.Fatalf("plan should be set")
	}
	// 整体替换
	p2 := acp.Plan{Type: "items", PlanID: "p1", Entries: []acp.PlanEntry{
		{Content: "step1", Status: acp.PlanCompleted, Priority: acp.PriorityHigh},
		{Content: "step2", Status: acp.PlanInProgress, Priority: acp.PriorityMedium},
	}}
	s.ApplyPlan(&acp.PlanUpdateEvent{SessionID: "s1", Plan: p2})
	if len(s.Plan.Entries) != 2 {
		t.Errorf("plan entries should be fully replaced, got %d", len(s.Plan.Entries))
	}
}

func TestSubmitInput(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "test")
	m.Input.InsertRune('h')
	m.Input.InsertRune('i')
	text := m.SubmitInput()
	if text != "hi" {
		t.Errorf("SubmitInput: %q, want hi", text)
	}
	// 新语义：提交不回显用户消息；agent 反射的 user_message 事件才创建消息。
	if len(m.Active.Messages) != 0 {
		t.Fatalf("SubmitInput must not echo locally, got %d messages", len(m.Active.Messages))
	}
	if m.Input.Text() != "" {
		t.Errorf("input should be cleared after submit, got %q", m.Input.Text())
	}
	// agent 反射 user_message（真实 messageId），消息进入时间线且为 user kind。
	msg := m.Active.message("msg_9", false, true)
	msg.Text = "hi"
	msg.Done = true
	if msg.Kind != MsgUser {
		t.Errorf("reflected message should be user kind")
	}
	// 空输入不提交
	if got := m.SubmitInput(); got != "" {
		t.Errorf("empty submit should return empty, got %q", got)
	}
}

func TestMarkStreamingDoneOnIdle(t *testing.T) {
	s := NewSession("s1", "test")
	// agent 流式：只发 chunk，不补发完整块。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "hel"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "lo"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "t1", IsThought: true, Text: "think"})
	if s.Messages[0].Done {
		t.Fatal("streaming message should not be done before idle")
	}
	// idle：补齐完成标记，未折叠的 thought 随后折叠。
	s.MarkStreamingDone()
	s.CollapseThoughts()
	if !s.Messages[0].Done {
		t.Errorf("assistant message should be marked done on idle")
	}
	if !s.Messages[1].Done {
		t.Errorf("thought should be marked done on idle")
	}
	if s.Messages[1].Expanded {
		t.Errorf("completed thought should be collapsed")
	}
}

//go:fix inline
func strPtr(s string) *string { return new(s) }

// TestAlkaid0ToolCallFollowsOwnerMessage 验证工具项跟随归属消息归位。回放分页的
// 页边界可能切在 agent 消息与它的工具结果之间，工具调用于是先于归属消息到达、
// 占在正文最前面；更早的一页把归属消息补进来后，工具项必须排到它后面，不能
// 一直压在归属消息乃至更早的消息之上。
func TestAlkaid0ToolCallFollowsOwnerMessage(t *testing.T) {
	s := NewSession("s1", "test")
	s.SetAlkaid0MessageOrdering(true)

	// 首屏：工具项先到，它的 agent 消息（msg_100）还在更早的一页里。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_100_tool_run"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_101", IsUser: true, Text: "用户消息 101"})
	assertTimelineOrder(t, s, []string{"tool:call_1_100_tool_run", "msg:msg_101"})

	// 归属消息还不在本地：工具项保持原位，不提前移动。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_102", Text: "AI 回复 102"})
	assertTimelineOrder(t, s, []string{"tool:call_1_100_tool_run", "msg:msg_101", "msg:msg_102"})

	// 更早一页到达：工具项跟到归属消息之后，落在更早的消息下面。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_99", IsUser: true, Text: "用户消息 99"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_100", Text: "AI 回复 100"})
	assertTimelineOrder(t, s, []string{"msg:msg_99", "msg:msg_100", "tool:call_1_100_tool_run", "msg:msg_101", "msg:msg_102"})
}

// TestAlkaid0ToolCallsUnderSameOwnerKeepArrivalOrder 验证归属同一消息的多个工具项
// 按到达顺序挂在该消息之后。
func TestAlkaid0ToolCallsUnderSameOwnerKeepArrivalOrder(t *testing.T) {
	s := NewSession("s1", "test")
	s.SetAlkaid0MessageOrdering(true)
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_50_tool_a"})
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_50_tool_b"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_60", IsUser: true, Text: "用户消息"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_50", Text: "AI 回复"})
	assertTimelineOrder(t, s, []string{"msg:msg_50", "tool:call_1_50_tool_a", "tool:call_1_50_tool_b", "msg:msg_60"})
}

// TestAlkaid0ToolCallOwnerParsing 验证归属解析只认 alkaid0 的
// "call_<sessionID>_<messageID>_<toolID>"，其它 agent 的 toolCallId 不锚定。
func TestAlkaid0ToolCallOwnerParsing(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want string
	}{
		{"call_1_100_tool_run", "msg_100"},
		{"call_12_3456_tool_x", "msg_3456"},
		{"call_1_100_tool_run_2", "msg_100"},
		{"call_1", ""},
		{"call_x_100_tool", ""},
		{"call_1_y_tool", ""},
		{"tool_call_1", ""},
		{"", ""},
	} {
		if got := alkaid0ToolCallOwnerMessageID(tc.id); got != tc.want {
			t.Errorf("alkaid0ToolCallOwnerMessageID(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// assertTimelineOrder 断言时间线顺序：消息项写作 "msg:<id>"，工具项写作 "tool:<id>"。
func assertTimelineOrder(t *testing.T, s *SessionState, want []string) {
	t.Helper()
	got := make([]string, 0, len(s.Timeline))
	for _, item := range s.Timeline {
		switch {
		case item.Message != nil:
			got = append(got, "msg:"+item.Message.MessageID)
		case item.ToolCall != nil:
			got = append(got, "tool:"+item.ToolCall.ID)
		case item.Terminal != nil:
			got = append(got, "terminal:"+item.Terminal.ID)
		default:
			got = append(got, "other:"+item.Key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("timeline = %v, want %v", got, want)
		}
	}
}

// TestAlkaid0TerminalCardFollowsOwnerMessage 验证 run 工具的终端卡片跟随归属消息
// 归位：卡片与工具项一样可能先于归属消息到达（resume 的终端快照在连接注册后立即
// 推送，分页回放的消息随后按页到达），更早一页把归属消息补进来后，卡片必须和它
// 的工具项一起落到该 agent 消息之后，不能停在正文最前面。
func TestAlkaid0TerminalCardFollowsOwnerMessage(t *testing.T) {
	s := NewSession("s1", "test")
	s.SetAlkaid0MessageOrdering(true)

	// 首屏：run 工具调用与终端卡片先到，它们的 agent 消息（msg_100）还在更早一页。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_100_tool_run", TerminalID: "@temp/run/7"})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/7", Status: "running", Command: "go test ./..."})
	s.AnchorTerminalOwner("@temp/run/7", "call_1_100_tool_run")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_101", IsUser: true, Text: "用户消息"})
	assertTimelineOrder(t, s, []string{"tool:call_1_100_tool_run", "terminal:@temp/run/7", "msg:msg_101"})

	// 更早一页到达：工具项与终端卡片一起跟到归属消息之后。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_100", Text: "AI 回复"})
	assertTimelineOrder(t, s, []string{"msg:msg_100", "tool:call_1_100_tool_run", "terminal:@temp/run/7", "msg:msg_101"})
}

// TestAlkaid0TerminalCardAnchoredByToolCallEvent 验证事件接线：带 terminal_id 的
// run 工具调用事件会把终端卡片锚定到该 agent 消息（与工具项同一归属）。
func TestAlkaid0TerminalCardAnchoredByToolCallEvent(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{},\"alk.cxykevin.top/alkaid0/v0.6\":{}}")})
	m.ActivateSession("s1", "test")
	s := m.Active
	s.SetAlkaid0MessageOrdering(true)

	// 终端快照先到（resume 后立即推送），随后才是分页回放的消息与工具调用。
	m.ApplyEvent(&acp.TerminalUpdateEvent{
		SessionID: "s1", TerminalID: "@temp/run/7", UpdateType: "full",
		Terminals: []acp.TerminalInfo{{TerminalID: "@temp/run/7", Status: "running", Command: "go test ./..."}},
	})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_101", IsUser: true, Text: "用户消息"})
	m.ApplyEvent(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_100_tool_run", TerminalID: "@temp/run/7"})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_100", Text: "AI 回复"})
	assertTimelineOrder(t, s, []string{"msg:msg_100", "terminal:@temp/run/7", "tool:call_1_100_tool_run", "msg:msg_101"})
}
