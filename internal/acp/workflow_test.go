package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestDecodeWorkflowSnapshotUpdate 验证 workflow 快照通知（workflow/snapshot）
// 被解码成 WorkflowEvent：kind 为 snapshot，图 / workflow / agentState 齐备，
// runId 与 terminalId 缺一时互相补齐。
func TestDecodeWorkflowSnapshotUpdate(t *testing.T) {
	raw := []byte(`{"sessionUpdate":"alk.cxykevin.top/session/terminal/workflow/snapshot","runId":"@temp/run/3",` +
		`"workflow":{"workflowId":"w1","status":"running","currentNode":"collect","lastSequence":7},` +
		`"graph":{"nodes":{"collect":{"name":"采集"},"merge":{"name":"汇总"}},"edges":{"collect":["merge"]},"start":["collect"]},` +
		`"agentState":{"type":"agent","nodeId":"collect","state":"running"}}`)
	ev, err := DecodeSessionUpdatePayload("s1", raw)
	if err != nil {
		t.Fatal(err)
	}
	wf, ok := ev.(*WorkflowEvent)
	if !ok {
		t.Fatalf("event = %T, want *WorkflowEvent", ev)
	}
	if wf.Kind != WorkflowKindSnapshot || wf.SessionID != "s1" {
		t.Fatalf("event = %#v", wf)
	}
	// terminalId 缺失时用 runId 补齐（两者同值）。
	if wf.RunID != "@temp/run/3" || wf.TerminalID != "@temp/run/3" {
		t.Fatalf("run/terminal id = %q/%q", wf.RunID, wf.TerminalID)
	}
	if wf.Workflow == nil || wf.Workflow.WorkflowID != "w1" || wf.Workflow.CurrentNode != "collect" || wf.Workflow.LastSequence != 7 {
		t.Fatalf("workflow info = %#v", wf.Workflow)
	}
	if wf.Graph == nil || len(wf.Graph.Nodes) != 2 || wf.Graph.Nodes["merge"].Name != "汇总" {
		t.Fatalf("graph = %#v", wf.Graph)
	}
	if targets := wf.Graph.Edges["collect"]; len(targets) != 1 || targets[0] != "merge" {
		t.Fatalf("edges = %#v", wf.Graph.Edges)
	}
	if wf.AgentState == nil || wf.AgentState.NodeID != "collect" || wf.AgentState.State != "running" {
		t.Fatalf("agent state = %#v", wf.AgentState)
	}
	if len(wf.Raw) == 0 {
		t.Fatal("raw payload must be kept")
	}
}

// TestDecodeWorkflowIncrementalUpdates 验证 update_* 增量事件的解码：类型来自
// sessionUpdate 前缀，载荷字段直接放在 update 顶层。
func TestDecodeWorkflowIncrementalUpdates(t *testing.T) {
	const prefix = "alk.cxykevin.top/session/terminal/workflow/update_"
	cases := []struct {
		name       string
		update     string
		body       string
		wantKind   WorkflowEventKind
		wantNodeID string
		check      func(t *testing.T, ev *WorkflowEvent)
	}{
		{
			name: "node", update: "node",
			body:       `{"runId":"@temp/run/4","workflow":"@temp/run/4","nodeId":"collect","state":"running","cached":true}`,
			wantKind:   WorkflowKindNode,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.State != "running" || !ev.Cached {
					t.Fatalf("node event = %#v", ev)
				}
			},
		},
		{
			name: "node_code", update: "node_code",
			body: `{"runId":"@temp/run/4","nodeId":"collect","name":"采集","code":"def collect(): ..."}`,
			// 显示名与源码只在 graph 未给名字时兜底，这里只验证字段被解析。
			wantKind:   WorkflowKindNodeCode,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.Name != "采集" || len(ev.Code) == 0 {
					t.Fatalf("node_code event = %#v", ev)
				}
			},
		},
		{
			name: "node_result_string", update: "node_result",
			body:       `{"runId":"@temp/run/4","nodeId":"collect","result":"42"}`,
			wantKind:   WorkflowKindNodeResult,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.Result != "42" {
					t.Fatalf("result = %q, want 42", ev.Result)
				}
			},
		},
		{
			name: "node_result_object", update: "node_result",
			body:       `{"runId":"@temp/run/4","nodeId":"collect","result":{"b":1,"a":[2,3]}}`,
			wantKind:   WorkflowKindNodeResult,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				// 非字符串结果紧凑序列化（保留原键序；不可 JSON 序列化的值
				// 服务端已转成字符串）。
				if ev.Result != `{"b":1,"a":[2,3]}` {
					t.Fatalf("result = %q", ev.Result)
				}
			},
		},
		{
			name: "agents_start", update: "agents_start",
			body:       `{"runId":"@temp/run/4","nodeId":"collect","count":2,"prompts":["甲","乙"]}`,
			wantKind:   WorkflowKindAgentsStart,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.Count != 2 || len(ev.Prompts) != 2 || ev.Prompts[1] != "乙" {
					t.Fatalf("agents_start event = %#v", ev)
				}
			},
		},
		{
			name: "agent", update: "agent",
			body:       `{"runId":"@temp/run/4","nodeId":"collect","agentIndex":1,"agentCount":3,"state":"success","prompt":"甲","attempt":2}`,
			wantKind:   WorkflowKindAgent,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.AgentIndex != 1 || ev.AgentCount != 3 || ev.State != "success" || ev.Attempt != 2 {
					t.Fatalf("agent event = %#v", ev)
				}
			},
		},
		{
			name: "node_log", update: "node_log",
			body:       `{"runId":"@temp/run/4","nodeId":"collect","message":"第一行"}`,
			wantKind:   WorkflowKindNodeLog,
			wantNodeID: "collect",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.Message != "第一行" {
					t.Fatalf("node_log event = %#v", ev)
				}
			},
		},
		{
			name: "graph", update: "graph",
			body:       `{"runId":"@temp/run/4","graph":{"nodes":{"a":{"name":"A"}},"start":["a"]}}`,
			wantKind:   WorkflowKindGraph,
			wantNodeID: "",
			check: func(t *testing.T, ev *WorkflowEvent) {
				if ev.Graph == nil || len(ev.Graph.Nodes) != 1 {
					t.Fatalf("graph event = %#v", ev)
				}
			},
		},
		{
			// 服务端不做白名单：未知类型同样按 workflow 事件保留，由 UI 忽略。
			name: "unknown", update: "brand_new_type",
			body:     `{"runId":"@temp/run/4","nodeId":"collect"}`,
			wantKind: WorkflowEventKind("brand_new_type"), wantNodeID: "collect",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := DecodeSessionUpdatePayload("s1", []byte(`{"sessionUpdate":"`+prefix+tc.update+`",`+tc.body[1:]))
			if err != nil {
				t.Fatal(err)
			}
			wf, ok := ev.(*WorkflowEvent)
			if !ok {
				t.Fatalf("event = %T, want *WorkflowEvent", ev)
			}
			if wf.Kind != tc.wantKind || wf.SessionID != "s1" {
				t.Fatalf("kind = %q (session %q), want %q", wf.Kind, wf.SessionID, tc.wantKind)
			}
			if wf.NodeID != tc.wantNodeID {
				t.Fatalf("nodeId = %q, want %q", wf.NodeID, tc.wantNodeID)
			}
			// terminalId 缺失时用 runId 补齐。
			if wf.TerminalID != "@temp/run/4" {
				t.Fatalf("terminalId = %q", wf.TerminalID)
			}
			if tc.check != nil {
				tc.check(t, wf)
			}
		})
	}
}

// TestWorkflowUpdateSnapshotDiscriminatorIsNotIncremental 验证
// update_snapshot 不被当成增量事件（快照走 workflow/snapshot 判别值）。
func TestWorkflowUpdateSnapshotDiscriminatorIsNotIncremental(t *testing.T) {
	raw := []byte(`{"sessionUpdate":"alk.cxykevin.top/session/terminal/workflow/update_snapshot","runId":"@temp/run/4"}`)
	ev, err := DecodeSessionUpdatePayload("s1", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ev.(*WorkflowEvent); ok {
		t.Fatalf("update_snapshot must not decode as a workflow event: %#v", ev)
	}
	if _, ok := ev.(*UnknownSessionUpdateEvent); !ok {
		t.Fatalf("event = %T, want *UnknownSessionUpdateEvent", ev)
	}
}

// TestDecodeWorkflowPayload 验证 workflow/status 的 logs[].payload 解码：
// 有 type 的载荷还原成事件并用 runId 补齐终端标识，无 type 的载荷被忽略。
func TestDecodeWorkflowPayload(t *testing.T) {
	ev, ok := DecodeWorkflowPayload("@temp/run/9", json.RawMessage(
		`{"type":"node_log","nodeId":"collect","message":"hello"}`))
	if !ok {
		t.Fatal("payload with type must decode")
	}
	if ev.Kind != WorkflowKindNodeLog || ev.NodeID != "collect" || ev.Message != "hello" {
		t.Fatalf("event = %#v", ev)
	}
	if ev.RunID != "@temp/run/9" || ev.TerminalID != "@temp/run/9" {
		t.Fatalf("run/terminal id = %q/%q", ev.RunID, ev.TerminalID)
	}
	for _, raw := range []string{``, `{}`, `{"nodeId":"collect"}`, `null`} {
		if _, ok := DecodeWorkflowPayload("@temp/run/9", json.RawMessage(raw)); ok {
			t.Fatalf("payload %q must be ignored", raw)
		}
	}
}

// TestWorkflowStatusRequiresV05 验证缺少 v0.5 能力时不发请求。
func TestWorkflowStatusRequiresV05(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.WorkflowStatus(context.Background(), "@temp/run/5"); err == nil {
		t.Fatal("expected error without v0.5 capability")
	}
	if len(ft.requests) != 1 { // 仅 initialize
		t.Fatalf("requests = %v", ft.requests)
	}
}

// TestWorkflowStatusRejectsEmptyRunID 验证空 run id 不发请求。
func TestWorkflowStatusRejectsEmptyRunID(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: json.RawMessage("{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")}
	b.state = backendReady
	b.mu.Unlock()
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.WorkflowStatus(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty run id")
	}
	if len(ft.requests) != 1 {
		t.Fatalf("requests = %v", ft.requests)
	}
}

// TestWorkflowStatusSendsSessionAndBroadcasts 验证 workflow/status 带 sessionId /
// runId 发出，解析响应（含事件日志）并以 WorkflowStatusEvent 广播给 UI。
func TestWorkflowStatusSendsSessionAndBroadcasts(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodWorkflowStatus, func(params, result any) error {
		p, ok := params.(WorkflowStatusParams)
		if !ok {
			t.Fatalf("params = %#v", params)
		}
		if p.SessionID != "s1" || p.RunID != "@temp/run/5" {
			t.Fatalf("params = %#v", p)
		}
		r := result.(*WorkflowStatusResult)
		r.Status = "running"
		r.Workflow = WorkflowInfo{WorkflowID: "w1", Status: "running", CurrentNode: "collect"}
		r.Graph = &WorkflowGraph{Nodes: map[string]WorkflowGraphNode{"collect": {Name: "采集"}}, Start: []string{"collect"}}
		r.Logs = []WorkflowLogEntry{{Sequence: 1, Type: "node_log", NodeID: "collect",
			Payload: json.RawMessage(`{"type":"node_log","nodeId":"collect","message":"hi"}`)}}
		return nil
	})
	b := probeClient(t, ft)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: json.RawMessage("{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")}
	b.state = backendReady
	b.mu.Unlock()

	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	got, err := sess.WorkflowStatus(context.Background(), "@temp/run/5")
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	// 响应缺 runId/terminalId 时用入参补齐（两者同值）。
	if got.RunID != "@temp/run/5" || got.TerminalID != "@temp/run/5" {
		t.Fatalf("run/terminal id = %q/%q", got.RunID, got.TerminalID)
	}
	if got.Workflow.WorkflowID != "w1" || got.Graph == nil || len(got.Logs) != 1 {
		t.Fatalf("result = %#v", got)
	}
	select {
	case ev := <-b.Events():
		e, ok := ev.(*WorkflowStatusEvent)
		if !ok {
			t.Fatalf("event = %T", ev)
		}
		if e.SessionID != "s1" || e.Result.Workflow.WorkflowID != "w1" {
			t.Fatalf("event = %#v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WorkflowStatusEvent")
	}
	// 请求顺序：initialize → workflow/status。
	if len(ft.requests) != 2 || ft.requests[1] != MethodWorkflowStatus {
		t.Fatalf("requests = %v", ft.requests)
	}
}
