package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestReconnectEventUpdatesState 验证重连进展映射到界面状态：重连期间保持
// Reconnecting，恢复后清除；放弃时显示错误原因。
func TestReconnectEventUpdatesState(t *testing.T) {
	m := New()

	m.ApplyEvent(&acp.ReconnectEvent{Phase: acp.ReconnectStarted, Err: errors.New("reset")})
	if !m.Reconnecting {
		t.Fatal("Reconnecting should be true after ReconnectStarted")
	}
	if m.Error == "" || !m.ErrorInfo {
		t.Fatalf("started notice = %q (info=%v), want an info hint", m.Error, m.ErrorInfo)
	}

	m.ApplyEvent(&acp.ReconnectEvent{Phase: acp.ReconnectAttemptFailed, Attempt: 3})
	if !m.Reconnecting {
		t.Fatal("Reconnecting should stay true during retries")
	}
	if !strings.Contains(m.Error, "3") {
		t.Fatalf("retry notice = %q, want the attempt number", m.Error)
	}

	m.ApplyEvent(&acp.ReconnectEvent{Phase: acp.ReconnectSucceeded})
	if m.Reconnecting {
		t.Fatal("Reconnecting should be false after ReconnectSucceeded")
	}
	if !m.ErrorInfo {
		t.Fatalf("restored notice should be an info hint, got error style: %q", m.Error)
	}

	m.ApplyEvent(&acp.ReconnectEvent{Phase: acp.ReconnectGiveUp, Err: errors.New("boom")})
	if m.Reconnecting {
		t.Fatal("Reconnecting should be false after ReconnectGiveUp")
	}
	if m.ErrorInfo {
		t.Fatal("give-up notice should use the error style")
	}
	if !strings.Contains(m.Error, "boom") {
		t.Fatalf("give-up notice = %q, want the underlying error", m.Error)
	}
}

// TestHistoryReloadResetsConversation 验证重连历史重载事件清空当前会话正文
// （消息、工具调用、计划、活动终端、滚动位置），但保留已结束终端与命令等会话
// 元数据，且不影响其它会话。
func TestHistoryReloadResetsConversation(t *testing.T) {
	m := New()
	s := NewSession("sess-1", "会话")
	m.Active = s
	m.View = ViewSession

	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "sess-1",
		IsUser:    true,
		Message: acp.Message{
			MessageID:  "msg_1",
			ContentSet: true,
			Content:    []acp.ContentBlock{{Type: "text", Text: strPtr("hi")}},
		},
	})
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "sess-1", ToolCallID: "call_1_1_x", Title: strPtr("tool")})
	s.ApplyPlan(&acp.PlanUpdateEvent{SessionID: "sess-1", Plan: acp.Plan{PlanID: "p1", Type: "items"}})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "run1", Status: "running"})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "run2", Status: "finished"})
	s.Commands = []acp.AvailableCommand{{Name: "cmd"}}
	s.AgentConfig = []acp.ConfigOption{{ConfigID: "thought_level", CurrentValue: "high"}}
	s.Scroll = 42
	s.MaxScroll = 100
	s.Rows = 200
	s.FollowBottom = false
	s.HistoryLoading = true

	// 其它会话的重载事件不应动当前会话。
	m.ApplyEvent(&acp.HistoryReloadEvent{SessionID: "other"})
	if len(s.Messages) == 0 {
		t.Fatal("HistoryReloadEvent for another session reset the active conversation")
	}

	m.ApplyEvent(&acp.HistoryReloadEvent{SessionID: "sess-1"})
	if len(s.Messages) != 0 || len(s.receiveMessages) != 0 || len(s.Timeline) != 0 || len(s.ToolCalls) != 0 {
		t.Fatalf("conversation not cleared: messages=%d timeline=%d tools=%d", len(s.Messages), len(s.Timeline), len(s.ToolCalls))
	}
	if s.Plan != nil || len(s.ToolOrder) != 0 {
		t.Fatalf("plan/tool order not cleared: plan=%v order=%v", s.Plan, s.ToolOrder)
	}
	if len(s.Terminals()) != 0 {
		t.Errorf("active terminals should be cleared for the server snapshot, got %d", len(s.Terminals()))
	}
	if len(s.TerminalHistory()) != 1 {
		t.Errorf("finished terminals are persistent and should be kept, got %d", len(s.TerminalHistory()))
	}
	if len(s.Commands) != 1 || len(s.AgentConfig) != 1 {
		t.Errorf("session metadata should be kept: commands=%d config=%d", len(s.Commands), len(s.AgentConfig))
	}
	if !s.FollowBottom || s.Scroll != 0 || s.MaxScroll != 0 || s.Rows != 0 || s.HistoryLoading {
		t.Errorf("scroll/history state not reset: follow=%v scroll=%d max=%d rows=%d loading=%v",
			s.FollowBottom, s.Scroll, s.MaxScroll, s.Rows, s.HistoryLoading)
	}
}
