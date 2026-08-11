package model

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

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
				{Type: "text", Text: strPtr("full")},
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
			Content:    []acp.ContentBlock{{Type: "text", Text: strPtr("done")}},
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
				Content: []acp.ContentBlock{{Type: "text", Text: strPtr("想")}}},
		})
	}
	s.CollapseThoughts()
	// 一条展开、一条折叠的工具调用。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "tc1", Title: strPtr("tool 1")})
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "tc2", Title: strPtr("tool 2")})
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
			Content: []acp.ContentBlock{{Type: "text", Text: strPtr("正文")}}},
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
			Content: []acp.ContentBlock{{Type: "text", Text: strPtr("完整思考")}}},
	})
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1", IsThought: false,
		Message: acp.Message{MessageID: "msg_21", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: strPtr("完整正文")}}},
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
		Title: strPtr("write_file"),
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

func strPtr(s string) *string { return &s }
