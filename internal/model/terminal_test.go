package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestReplaceTerminalsArchivesStaleAndFinished 验证全量快照语义：
// 快照内仍活动的终端保留元数据，快照里已结束或不再出现的终端移入历史段
// （内容保留），而不是被丢弃。
func TestReplaceTerminalsArchivesStaleAndFinished(t *testing.T) {
	s := NewSession("s1", "")
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "old", Kind: "shell", AgentID: "a", Command: "old", Content: "old\n"})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "keep", Kind: "shell", Command: "run", Content: "first\n"})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "keep", Status: "running", Content: "chunk\n"})
	full := []acp.TerminalInfo{
		{TerminalID: "live", Kind: "shell", Command: "serve", Status: "running", Content: "up\n"},
		{TerminalID: "keep", Status: "completed", Content: "snapshot\n"},
	}
	s.ReplaceTerminals(full)

	// 快照里仍活动的终端留在活动段。
	if got := s.Terminal("live"); got == nil || got.Command != "serve" || got.Transcript != "up\n" {
		t.Fatalf("live terminal = %#v", got)
	}
	// 不再出现（old）与已结束（keep）的终端都进历史段，内容保留。
	if s.Terminal("old") != nil {
		t.Fatal("stale terminal must leave the active list")
	}
	old := s.TerminalHistoryByID("old")
	if old == nil || old.Transcript != "old\n" || old.Command != "old" {
		t.Fatalf("archived stale terminal = %#v", old)
	}
	keep := s.TerminalHistoryByID("keep")
	if keep == nil || keep.Kind != "shell" || keep.Command != "run" || keep.Transcript != "snapshot\n" {
		t.Fatalf("archived finished terminal = %#v", keep)
	}
	if !keep.Finished() {
		t.Fatalf("finished terminal status = %q", keep.Status)
	}
}

// TestTerminalStopArchivesWithContent 验证增量 stop 推送把终端移入历史段并保留内容。
func TestTerminalStopArchivesWithContent(t *testing.T) {
	s := NewSession("s1", "")
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "run/1", Kind: "background", Command: "go test ./...", Status: "running", Content: "ok\n"})
	s.ArchiveTerminal("run/1")
	if s.Terminal("run/1") != nil {
		t.Fatal("stopped terminal must leave the active list")
	}
	got := s.TerminalHistoryByID("run/1")
	if got == nil || got.Transcript != "ok\n" || got.Command != "go test ./..." || !got.Finished() {
		t.Fatalf("history terminal = %#v", got)
	}
	if len(s.Terminals()) != 0 || len(s.TerminalHistory()) != 1 {
		t.Fatalf("active=%d history=%d", len(s.Terminals()), len(s.TerminalHistory()))
	}
}

// TestReplaceTerminalsHistorySnapshotKeepsActive 验证 history 查询附带的全量推送
// （条目全部已结束）只更新历史段，不清空活动列表。
func TestReplaceTerminalsHistorySnapshotKeepsActive(t *testing.T) {
	s := NewSession("s1", "")
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/2", Status: "running", Content: "live\n"})
	s.ReplaceTerminals([]acp.TerminalInfo{
		{TerminalID: "@temp/run/1", Status: "finished", Content: "done\n"},
	})
	if got := s.Terminal("@temp/run/2"); got == nil || got.Transcript != "live\n" {
		t.Fatalf("active terminal lost: %#v", got)
	}
	if got := s.TerminalHistoryByID("@temp/run/1"); got == nil || got.Transcript != "done\n" {
		t.Fatalf("history terminal = %#v", got)
	}
}

// TestApplyTerminalHistoryMergesRestored 验证服务端持久化副本（restored）只带内容时
// 仍能进历史段，且不会被空元数据覆盖既有字段。
func TestApplyTerminalHistoryMergesRestored(t *testing.T) {
	s := NewSession("s1", "")
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/3", Command: "npm test", Status: "running", Content: "partial\n"})
	s.ApplyTerminalHistory([]acp.TerminalInfo{
		{TerminalID: "@temp/run/3", Status: "finished", Content: "final\n", Restored: true},
		{TerminalID: "@temp/run/1", Status: "finished", Content: "persisted\n", Restored: true},
	})
	restored := s.TerminalHistoryByID("@temp/run/1")
	if restored == nil || !restored.Restored || restored.Transcript != "persisted\n" || restored.Status != "finished" {
		t.Fatalf("restored terminal = %#v", restored)
	}
	moved := s.TerminalHistoryByID("@temp/run/3")
	if moved == nil || moved.Transcript != "final\n" || moved.Command != "npm test" {
		t.Fatalf("moved terminal = %#v (command must survive the merge)", moved)
	}
	if s.Terminal("@temp/run/3") != nil {
		t.Fatal("terminal must leave the active list")
	}
	if n := len(s.TerminalHistory()); n != 2 {
		t.Fatalf("history size = %d", n)
	}
}

// TestShellsListsActiveThenHistory 验证面板列表顺序：活跃在前、历史在后。
func TestShellsListsActiveThenHistory(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`)})
	m.ActivateSession("s", "session")
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/1", Status: "running", Output: "a"})
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/2", Status: "running", Output: "b"})
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/1", Status: "stop"})

	active := m.ActiveShells()
	history := m.ShellHistory()
	if len(active) != 1 || active[0].ID != "@temp/run/2" {
		t.Fatalf("active shells = %#v", active)
	}
	if len(history) != 1 || history[0].ID != "@temp/run/1" || history[0].Transcript != "a" {
		t.Fatalf("shell history = %#v", history)
	}
	all := m.Shells()
	if len(all) != 2 || all[0].ID != "@temp/run/2" || all[1].ID != "@temp/run/1" {
		t.Fatalf("panel shells order = %#v", all)
	}
}

// TestIncrementalUpdateMergesTerminalSnapshot 验证增量推送里 terminals 快照的
// 命令等元数据会被并入：服务端顶层只带 terminalId/status/content，
// 命令等细节只在快照条目里（否则面板只能显示终端 ID）。
func TestIncrementalUpdateMergesTerminalSnapshot(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`)})
	m.ActivateSession("s", "session")
	m.ApplyEvent(&acp.TerminalUpdateEvent{
		SessionID: "s", UpdateType: "incremental", TerminalID: "@temp/run/1", Status: "start",
		Terminals: []acp.TerminalInfo{{TerminalID: "@temp/run/1", Status: "running", Command: "npm run dev", Kind: "shell"}},
	})
	active := m.ActiveShells()
	if len(active) != 1 || active[0].Command != "npm run dev" || active[0].Kind != "shell" {
		t.Fatalf("active shells = %#v", active)
	}
	// 顶层 content 单独到达时也要落到同一终端。
	m.ApplyEvent(&acp.TerminalUpdateEvent{
		SessionID: "s", UpdateType: "incremental", TerminalID: "@temp/run/1", Status: "running",
		Output: "listening on :8080\n",
	})
	if got := m.ActiveShells(); len(got) != 1 || got[0].Transcript != "listening on :8080\n" {
		t.Fatalf("content not applied: %#v", got)
	}
}

func TestTerminalUpdateParsesFullAndMetadata(t *testing.T) {
	raw := []byte(`{"sessionUpdate":"alk.cxykevin.top/terminal_update","updateType":"full","terminals":[{"terminalId":"t1","kind":"shell","command":"go test","status":"running","content":"ok"}]}`)
	ev, err := acp.DecodeSessionUpdatePayload("s1", raw)
	if err != nil {
		t.Fatal(err)
	}
	e := ev.(*acp.TerminalUpdateEvent)
	if len(e.Terminals) != 1 || e.Terminals[0].Kind != "shell" || e.Terminals[0].Command != "go test" {
		t.Fatalf("event = %#v", e)
	}
}
