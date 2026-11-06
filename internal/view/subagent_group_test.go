package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// subagentSession 构造一段带子代理内容的会话：
//
//	主代理用户消息 → 子代理思考 → 子代理正文 → 子代理工具调用 → 主代理正文
func subagentSession() *model.SessionState {
	s := model.NewSession("s1", "test")
	mainAgent := new("")
	sub := new("coder-agent")
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", IsUser: true, AgentID: mainAgent,
		Message: acp.Message{MessageID: "msg_1", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("帮我改代码")}}}})
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", IsThought: true, AgentID: sub,
		Message: acp.Message{MessageID: "msg_2", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("先看看")}}}})
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", AgentID: sub,
		Message: acp.Message{MessageID: "msg_3", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("改好了")}}}})
	status := acp.ToolCompleted
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_3_0",
		Status: &status, Title: new("read_file")})
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", AgentID: mainAgent,
		Message: acp.Message{MessageID: "msg_4", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("收到")}}}})
	return s
}

func drawBody(t *testing.T, s *model.SessionState) *MessageList {
	t.Helper()
	ml := &MessageList{Theme: renderer.DefaultTheme()}
	ml.Draw(renderer.NewCanvas(renderer.NewBuffer(60, 20)), renderer.NewRect(0, 0, 60, 20), s)
	return ml
}

// TestSubagentContentAutoCollapses 验证同一子代理的连续内容自动合并成一行
// 组标题：默认折叠时正文只多出这一行，组内的思考/正文/工具不再单独占行。
func TestSubagentContentAutoCollapses(t *testing.T) {
	s := subagentSession()
	ml := drawBody(t, s)

	// 主代理用户消息 1 行 + 子代理组标题 1 行 + 主代理回复 1 行。
	if ml.Rows != 3 {
		t.Fatalf("rows = %d, want 3 (main, group header, main)", ml.Rows)
	}
	ref, ok := ml.Toggles[1]
	if !ok || ref.Kind != ToggleSubagent || ref.ID == "" {
		t.Fatalf("group header at row 1 should be a subagent toggle, got %+v ok=%v", ref, ok)
	}
	if _, ok := ml.Toggles[0]; ok {
		t.Error("user message row should not be clickable")
	}
	if _, ok := ml.Toggles[2]; ok {
		t.Error("main assistant row should not be clickable")
	}

	// 组标题带项数（组内 3 个块）。
	var header string
	for _, b := range ml.Body {
		if b.Start == 1 {
			header = b.Raw
		}
	}
	if !strings.HasPrefix(header, "subagent coder-agent") {
		t.Fatalf("group raw = %q, want subagent prefix", header)
	}
}

// TestSubagentGroupExpands 验证点击组标题展开后按原顺序显示组内全部内容，
// 并保留组内思考/工具标题的点击行。
func TestSubagentGroupExpands(t *testing.T) {
	s := subagentSession()
	ml := drawBody(t, s)
	ref := ml.Toggles[1]
	s.ToggleSubagentGroup(ref.ID)
	ml = drawBody(t, s)

	// user 1 + 组标题 1 + 思考 1 + 正文 1 + 工具 1 + 主代理正文 1
	if ml.Rows != 6 {
		t.Fatalf("rows after expand = %d, want 6", ml.Rows)
	}
	if got, ok := ml.Toggles[1]; !ok || got.Kind != ToggleSubagent || got.ID != ref.ID {
		t.Fatalf("group header should stay clickable after expand, got %+v ok=%v", got, ok)
	}
	if got, ok := ml.Toggles[2]; !ok || got.Kind != ToggleThought || got.ID != "msg_2" {
		t.Fatalf("thought header inside group = %+v ok=%v", got, ok)
	}
	if got, ok := ml.Toggles[4]; !ok || got.Kind != ToggleTool || got.ID != "call_1_3_0" {
		t.Fatalf("tool header inside group = %+v ok=%v", got, ok)
	}

	// 组整体折叠回去后，组内点击行一并消失。
	s.ToggleSubagentGroup(ref.ID)
	ml = drawBody(t, s)
	if ml.Rows != 3 {
		t.Fatalf("rows after collapse = %d, want 3", ml.Rows)
	}
	for row := range ml.Toggles {
		if row != 1 {
			t.Fatalf("collapsed group should only expose its header, got toggle at row %d", row)
		}
	}
}

// TestSubagentGoalCheckerDisplayName 验证内置目标校验代理的实例 ID
// "__goal_check__" 在组标题里单独显示为 "Goal Checker"，其余子代理沿用实例名。
func TestSubagentGoalCheckerDisplayName(t *testing.T) {
	ml := &MessageList{Theme: renderer.DefaultTheme()}
	s := model.NewSession("s1", "test")
	inner := []*block{{lines: [][]Span{{{Text: "hi"}}}}}

	blk := ml.subagentBlock(s, "__goal_check__#1", "__goal_check__", inner, false)
	if got := blk.lines[0][0].Text; got != "▸ subagent Goal Checker  1 items" {
		t.Fatalf("goal-check header = %q", got)
	}
	if !strings.HasPrefix(blk.raw, "subagent Goal Checker") {
		t.Fatalf("goal-check raw header = %q", blk.raw)
	}
	if strings.Contains(blk.raw, "__goal_check__") {
		t.Fatalf("raw should not expose the internal id, got %q", blk.raw)
	}

	normal := ml.subagentBlock(s, "coder-agent#1", "coder-agent", inner, false)
	if got := normal.lines[0][0].Text; got != "▸ subagent coder-agent  1 items" {
		t.Fatalf("normal agent header = %q", got)
	}
}

// TestSubagentGroupsSplitByMainContent 验证主代理内容会把同一子代理的两段
// 活动分成两个组，各自独立折叠；组键互不相同。
func TestSubagentGroupsSplitByMainContent(t *testing.T) {
	s := subagentSession()
	sub := new("coder-agent")
	main := new("")
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", AgentID: sub,
		Message: acp.Message{MessageID: "msg_5", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("第二个任务")}}}})
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", AgentID: main,
		Message: acp.Message{MessageID: "msg_6", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("收工")}}}})

	ml := drawBody(t, s)
	// user 1 + 组1 1 + 主代理正文 1 + 组2 1 + 主代理正文 1 = 5
	if ml.Rows != 5 {
		t.Fatalf("rows = %d, want 5", ml.Rows)
	}
	keys := map[string]bool{}
	for _, ref := range ml.Toggles {
		if ref.Kind == ToggleSubagent {
			keys[ref.ID] = true
		}
	}
	if len(keys) != 2 {
		t.Fatalf("want two distinct subagent groups, got %v", keys)
	}
}
