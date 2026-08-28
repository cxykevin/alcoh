package model

import (
	"github.com/cxykevin/alcoh/internal/acp"
	"testing"
)

func TestReplaceTerminalsPreservesMetadataAndRemovesStale(t *testing.T) {
	s := NewSession("s1", "")
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "old", Kind: "shell", AgentID: "a", Command: "old", Content: "old\n"})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "keep", Kind: "shell", Command: "run", Content: "first\n"})
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "keep", Status: "running", Content: "chunk\n"})
	s.ReplaceTerminals([]acp.TerminalInfo{{TerminalID: "keep", Status: "completed", Content: "snapshot\n"}})
	if s.Terminal("old") != nil {
		t.Fatal("stale terminal remains")
	}
	got := s.Terminal("keep")
	if got == nil || got.Kind != "shell" || got.Command != "run" || got.Transcript != "snapshot\n" {
		t.Fatalf("terminal = %#v", got)
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
