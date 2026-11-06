package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestSubagentContentAttribution 验证子代理归属的来源：
//   - 消息取 alg.cxykevin.top/agent_status；
//   - 工具调用优先取归属消息（回放时消息晚到也不影响），拿不到时退回创建
//     当时"最近一条消息声明的子代理"快照；
//   - 终端取服务端给的 agentId；
//   - 主代理的空串能覆盖掉过期的快照。
func TestSubagentContentAttribution(t *testing.T) {
	s := NewSession("s1", "test")

	// 主代理用户消息（空串 = 主代理）。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_1",
		IsUser: true, Text: "帮我做", AgentID: new("")})
	if got := s.Messages[0].AgentID; got != "" {
		t.Fatalf("main user message AgentID = %q, want empty", got)
	}

	// 子代理思考 → 记录子代理代码。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_2",
		IsThought: true, Text: "先看看", AgentID: new("coder-agent")})
	if got := s.Messages[1].AgentID; got != "coder-agent" {
		t.Fatalf("subagent thought AgentID = %q", got)
	}

	// 直播的工具调用：创建时按当时的子代理快照归属。
	status := acp.ToolCompleted
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_2_0", Status: &status})
	if got := s.ToolCalls["call_1_2_0"].AgentID; got != "coder-agent" {
		t.Fatalf("tool call AgentID = %q, want coder-agent", got)
	}
	if got := s.ItemAgentID(s.timelineIndex["tool:call_1_2_0"]); got != "coder-agent" {
		t.Fatalf("ItemAgentID(tool) = %q, want coder-agent", got)
	}

	// 归属消息在本地时以消息为准：主代理消息下的工具必须归主代理，
	// 即使创建时的快照仍是子代理。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_1_0", Status: &status})
	if got := s.ItemAgentID(s.timelineIndex["tool:call_1_1_0"]); got != "" {
		t.Fatalf("ItemAgentID(tool under main message) = %q, want empty", got)
	}

	// 回到主代理：之后新建的工具调用归主代理。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_3",
		Text: "收尾", AgentID: new("")})
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_3_0", Status: &status})
	if got := s.ToolCalls["call_1_3_0"].AgentID; got != "" {
		t.Fatalf("tool call after deactivate AgentID = %q, want empty", got)
	}

	// 终端：服务端 agentId。
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "t1", Status: "running", AgentID: "coder-agent"})
	if got := s.ItemAgentID(s.timelineIndex["terminal:t1"]); got != "coder-agent" {
		t.Fatalf("ItemAgentID(terminal) = %q, want coder-agent", got)
	}

	// 未提供 agent 字段（nil）时保持原值不动。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "msg_4",
		Text: "other agent", AgentID: nil})
	if got := s.Messages[len(s.Messages)-1].AgentID; got != "" {
		t.Fatalf("nil agent status should keep previous value, got %q", got)
	}
}

// TestAgentLifecycleToolHintsAttribution 验证直播流里 tool_call_update 不带
// agent_status 时的兜底：activate_agent / deactivate_agent 的 calling_info
// 能切换后续新建工具调用的归属。
func TestAgentLifecycleToolHintsAttribution(t *testing.T) {
	s := NewSession("s1", "test")
	status := acp.ToolCompleted
	callingInfo := func(name, args string) []acp.ToolCallContent {
		return []acp.ToolCallContent{{Type: acp.ToolCallingInfoType, Name: name, Args: json.RawMessage(args)}}
	}

	// 主代理激活子代理（此前没有任何子代理消息）。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_1_0",
		Status: &status, ContentSet: true,
		Content: callingInfo("activate_agent", `{"name":"coder-agent","prompt":"go"}`)})
	// 子代理的第一个动作就是工具调用：应归到 coder-agent。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_2_1", Status: &status})
	if got := s.ToolCalls["call_1_2_1"].AgentID; got != "coder-agent" {
		t.Fatalf("tool after activate AgentID = %q, want coder-agent", got)
	}

	// 停用后回到主代理，之后的工具调用归主代理。
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_3_2",
		Status: &status, ContentSet: true,
		Content: callingInfo("deactivate_agent", `{"prompt":"done"}`)})
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "call_1_4_3", Status: &status})
	if got := s.ToolCalls["call_1_4_3"].AgentID; got != "" {
		t.Fatalf("tool after deactivate AgentID = %q, want empty", got)
	}
	if got := s.ToolCalls["call_1_3_2"].AgentID; got != "coder-agent" {
		t.Fatalf("deactivate tool itself AgentID = %q, want coder-agent", got)
	}
}

// TestToggleSubagentGroup 验证内容组的展开态按分组键独立保存、默认折叠。
func TestToggleSubagentGroup(t *testing.T) {
	s := NewSession("s1", "test")
	if s.SubagentExpanded["coder-agent#1"] {
		t.Fatal("group should default to collapsed")
	}
	s.ToggleSubagentGroup("coder-agent#1")
	if !s.SubagentExpanded["coder-agent#1"] {
		t.Fatal("group should be expanded after toggle")
	}
	if s.SubagentExpanded["coder-agent#2"] {
		t.Fatal("other group should stay collapsed")
	}
	s.ToggleSubagentGroup("coder-agent#1")
	if s.SubagentExpanded["coder-agent#1"] {
		t.Fatal("group should be collapsed after second toggle")
	}
}
