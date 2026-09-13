package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestTerminalHistoryNewestFirst 验证历史段顺序：最后结束的终端排最前，
// 面板里最近的历史紧接在活动 shell 下方。
func TestTerminalHistoryNewestFirst(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage("{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s", "session")
	for _, id := range []string{"@temp/run/1", "@temp/run/2", "@temp/run/3"} {
		m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: id, Status: "running", Output: id})
	}
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/1", Status: "stop"})
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/2", Status: "stop"})
	history := m.ShellHistory()
	if len(history) != 2 {
		t.Fatalf("history = %d entries", len(history))
	}
	if history[0].ID != "@temp/run/2" || history[1].ID != "@temp/run/1" {
		t.Fatalf("history order = %s, %s (want run/2 then run/1)", history[0].ID, history[1].ID)
	}
	// 服务端 terminal/history 按 createdAt 升序返回，客户端展示为最新在前。
	m.ApplyEvent(&acp.TerminalHistoryEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/5", Status: "finished", Content: "old"},
		{TerminalID: "@temp/run/6", Status: "finished", Content: "new"},
	}})
	history = m.ShellHistory()
	if len(history) != 4 || history[0].ID != "@temp/run/6" || history[1].ID != "@temp/run/5" {
		t.Fatalf("history after server merge = %v", terminalIDs(history))
	}
}

func terminalIDs(xs []*TerminalState) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.ID)
	}
	return out
}
