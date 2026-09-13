package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestTerminalHistoryRequiresV06 验证历史查询在缺少 v0.6 能力时不发请求。
func TestTerminalHistoryRequiresV06(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.TerminalHistory(context.Background(), ""); err == nil {
		t.Fatal("expected error without v0.6 capability")
	}
	if len(ft.requests) != 1 { // 仅 initialize
		t.Fatalf("requests = %v", ft.requests)
	}
}

// TestTerminalHistorySendsSessionAndBroadcasts 验证历史查询带 sessionId、解析
// 返回内容，并把结果以 TerminalHistoryEvent 广播给 UI。
func TestTerminalHistorySendsSessionAndBroadcasts(t *testing.T) {
	ft := &fakeTransport{}
	ft.handlers = map[string]func(any, any) error{
		MethodTerminalHistory: func(params, result any) error {
			p := params.(TerminalHistoryParams)
			if p.SessionID != "s1" || p.TerminalID != "" {
				t.Fatalf("params = %#v", p)
			}
			r := result.(*TerminalHistoryResult)
			r.Terminals = []TerminalInfo{{TerminalID: "@temp/run/1", Status: "finished", Content: "done\n", Restored: true}}
			return nil
		},
	}
	b := probeClient(t, ft)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: json.RawMessage("{\"alk.cxykevin.top/alkaid0/v0.6\":{}}")}
	b.state = backendReady
	b.mu.Unlock()

	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	got, err := sess.TerminalHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("TerminalHistory: %v", err)
	}
	if len(got) != 1 || got[0].TerminalID != "@temp/run/1" || !got[0].Restored || got[0].Content != "done\n" {
		t.Fatalf("terminals = %#v", got)
	}
	if got[0].SessionID != "s1" {
		t.Fatalf("session id must be filled in: %#v", got[0])
	}
	select {
	case ev := <-b.Events():
		e, ok := ev.(*TerminalHistoryEvent)
		if !ok {
			t.Fatalf("event = %T", ev)
		}
		if e.SessionID != "s1" || len(e.Terminals) != 1 {
			t.Fatalf("event = %#v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TerminalHistoryEvent")
	}
	// 请求顺序：initialize → session/terminal/history。
	if len(ft.requests) != 2 || ft.requests[1] != MethodTerminalHistory {
		t.Fatalf("requests = %v", ft.requests)
	}
}

// TestToolCallUpdateCarriesTerminalID 验证 v0.6 的 terminal_id 被解析。
func TestToolCallUpdateCarriesTerminalID(t *testing.T) {
	raw := []byte("{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"completed\",\"alk.cxykevin.top/terminal_id\":\"@temp/run/7\"}")
	ev, err := DecodeSessionUpdatePayload("s1", raw)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := ev.(*ToolCallUpdateEvent)
	if !ok || e.TerminalID != "@temp/run/7" {
		t.Fatalf("event = %#v", ev)
	}
}
