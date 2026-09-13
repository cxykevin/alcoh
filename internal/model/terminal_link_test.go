package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestToolCallLinksTerminalMetadata 验证 run 工具调用（含 session/resume 的历史回放）
// 用 content 里的 alk.cxykevin.top/calling_info.args（alkaid0 不发 rawInput）
// 复原终端条目的 reason 与命令，而不是只剩 @temp/run/<n>。
func TestToolCallLinksTerminalMetadata(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{},\"alk.cxykevin.top/alkaid0/v0.6\":{}}")})
	m.ActivateSession("s", "session")

	// 回放里已结束的 run 工具调用：terminal_id + calling_info 参数块（无 rawInput），
	// 且没有 terminal_update 推送。
	done := acp.ToolCompleted
	m.ApplyEvent(&acp.ToolCallUpdateEvent{
		SessionID: "s", ToolCallID: "call_1", Status: &done, TerminalID: "@temp/run/7",
		Content: []acp.ToolCallContent{{
			Type: acp.ToolCallingInfoType,
			Name: "run",
			Args: json.RawMessage("{\"type\":\"shell\",\"reason\":\"run the tests\",\"command\":\"go test ./...\"}"),
		}},
	})
	history := m.ShellHistory()
	if len(history) != 1 || history[0].ID != "@temp/run/7" {
		t.Fatalf("history = %v", terminalIDs(history))
	}
	if history[0].Reason != "run the tests" || history[0].Command != "go test ./..." || history[0].Kind != "shell" {
		t.Fatalf("linked terminal = %#v", history[0])
	}
	// 之后 terminal/history 只补内容，元数据不被清空。
	m.ApplyEvent(&acp.TerminalHistoryEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/7", Status: "finished", Content: "ok\n", Restored: true},
	}})
	got := m.ShellHistory()
	if len(got) != 1 || got[0].Transcript != "ok\n" || got[0].Reason != "run the tests" || got[0].Command != "go test ./..." {
		t.Fatalf("after history merge = %#v", got)
	}

	// 回放里仍在执行的 run 工具调用（后台任务）先进活动段；
	// terminal/list 确认仍在运行后保持不动。
	running := acp.ToolInProgress
	m.ApplyEvent(&acp.ToolCallUpdateEvent{
		SessionID: "s", ToolCallID: "call_2", Status: &running, TerminalID: "@temp/run/8",
		Content: []acp.ToolCallContent{{
			Type: acp.ToolCallingInfoType,
			Name: "run",
			Args: json.RawMessage("{\"type\":\"shell\",\"reason\":\"serve the app\",\"command\":\"npm run dev\",\"background\":true}"),
		}},
	})
	active := m.ActiveShells()
	if len(active) != 1 || active[0].ID != "@temp/run/8" || active[0].Reason != "serve the app" {
		t.Fatalf("active shells = %#v", active)
	}
	m.ApplyEvent(&acp.TerminalListEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/8", Status: "running", Command: "npm run dev", Reason: "serve the app"},
	}})
	if active = m.ActiveShells(); len(active) != 1 || active[0].Reason != "serve the app" {
		t.Fatalf("active after list = %#v", active)
	}
}

// TestToolCallLinkMovesFinishedTerminal 验证已在活动段的终端被回放标记为结束后，
// 由 history 查询搬到历史段（不会两段同时出现）。
func TestToolCallLinkMovesFinishedTerminal(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{},\"alk.cxykevin.top/alkaid0/v0.6\":{}}")})
	m.ActivateSession("s", "session")
	m.ApplyEvent(&acp.TerminalListEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/9", Status: "running", Command: "sleep 99"},
	}})
	if len(m.ActiveShells()) != 1 {
		t.Fatalf("active = %v", terminalIDs(m.ActiveShells()))
	}
	m.ApplyEvent(&acp.TerminalHistoryEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/9", Status: "finished", Content: "done\n"},
	}})
	if n := len(m.ActiveShells()); n != 0 {
		t.Fatalf("finished terminal stayed active: %v", terminalIDs(m.ActiveShells()))
	}
	if h := m.ShellHistory(); len(h) != 1 || h[0].ID != "@temp/run/9" || h[0].Transcript != "done\n" {
		t.Fatalf("history = %#v", h)
	}
}
