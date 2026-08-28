package acp

import "testing"

func TestDecodeAlkaid0TerminalUpdate(t *testing.T) {
	ev, err := DecodeSessionUpdatePayload("s", []byte(`{"sessionUpdate":"alk.cxykevin.top/terminal_update","updateType":"full","terminals":[{"terminalId":"t","command":"echo hi","status":"running","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	x, ok := ev.(*TerminalUpdateEvent)
	if !ok || x.UpdateType != "full" || len(x.Terminals) != 1 || x.Terminals[0].Command != "echo hi" {
		t.Fatalf("bad event %#v", ev)
	}
}
func TestGenericTerminalUpdateIgnored(t *testing.T) {
	ev, err := DecodeSessionUpdatePayload("s", []byte(`{"sessionUpdate":"terminal_update","terminalId":"t"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ev.(*UnknownSessionUpdateEvent); !ok {
		t.Fatalf("got %T", ev)
	}
}
