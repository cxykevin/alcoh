package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestTerminalListEventMergesActive 验证 terminal/list 结果并入活动段：
// 前台 run 不推送 start/running，正在运行的终端只能靠这次查询列出来；
// 未出现在列表里的活动终端不被移除（结束仍由 stop 推送与历史查询驱动）。
func TestTerminalListEventMergesActive(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage("{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s", "session")
	// 推送驱动的终端（后台任务）与列表驱动的终端（前台 run）并存。
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/8", Status: "running", Output: "bg\n"})
	m.ApplyEvent(&acp.TerminalListEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/8", Status: "running", Command: "npm run dev", Kind: "shell"},
		{TerminalID: "@temp/run/9", Status: "running", Command: "go build ./...", Kind: "shell"},
	}})
	// 活动段"新的在上"：后并入的 run/9 排最前，push 驱动的 run/8 保留已收内容。
	active := m.ActiveShells()
	if len(active) != 2 {
		t.Fatalf("active shells = %v, want 2 (list result must be merged)", terminalIDs(active))
	}
	if active[0].ID != "@temp/run/9" || active[0].Command != "go build ./..." {
		t.Fatalf("active[0] = %#v", active[0])
	}
	if active[1].ID != "@temp/run/8" || active[1].Command != "npm run dev" || active[1].Transcript != "bg\n" {
		t.Fatalf("active[1] = %#v", active[1])
	}
	// 列表里已结束的条目直接进历史段。
	m.ApplyEvent(&acp.TerminalListEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/9", Status: "finished", Command: "go build ./...", Content: "ok\n"},
	}})
	if got := m.ActiveShells(); len(got) != 1 || got[0].ID != "@temp/run/8" {
		t.Fatalf("active after finished entry = %v", terminalIDs(got))
	}
	if h := m.ShellHistory(); len(h) != 1 || h[0].ID != "@temp/run/9" || h[0].Transcript != "ok\n" {
		t.Fatalf("history = %#v", h)
	}
}
