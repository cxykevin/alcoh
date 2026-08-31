package model

import (
	"encoding/json"
	"github.com/cxykevin/alcoh/internal/acp"
	"testing"
)

func TestShellsRequireAlkaid0V05(t *testing.T) {
	m := New()
	m.ActivateSession("s", "session")
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)})
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "1", Output: "x"})
	if len(m.Shells()) != 0 {
		t.Fatal("shells must be disabled without v0.5")
	}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.5":{}}`)})
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "1", Output: "x"})
	if len(m.Shells()) != 1 {
		t.Fatal("shells must be enabled with v0.5")
	}
}
