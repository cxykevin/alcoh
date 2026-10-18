package acp

// 会话消息队列（alk.cxykevin.top/msgqueue）的协议测试：能力门控、RPC 参数
// 形状与 msgqueue_update 事件解码。

import (
	"context"
	"encoding/json"
	"testing"
)

func msgQueueTestCaps(t *testing.T, b *ClientBackend, v06, queue bool) {
	t.Helper()
	raw := map[string]json.RawMessage{}
	if v06 {
		raw[Alkaid0CapabilityV06] = json.RawMessage(`{}`)
	}
	if queue {
		raw[Alkaid0CapabilityMsgQueue] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: data}
	b.state = backendReady
	b.mu.Unlock()
}

// TestMsgQueueRequiresBothCapabilities 验证队列 RPC 只在同时声明 v0.6 与
// msgqueue 时可用（只声明 msgqueue 不把输入交给看不到的队列）。
func TestMsgQueueRequiresBothCapabilities(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.ListMsgQueue(context.Background()); err == nil {
		t.Fatal("expected error without msgqueue capabilities")
	}
	msgQueueTestCaps(t, b, true, false)
	if _, err := sess.ListMsgQueue(context.Background()); err == nil {
		t.Fatal("expected error with only v0.6")
	}
	msgQueueTestCaps(t, b, false, true)
	if _, err := sess.ListMsgQueue(context.Background()); err == nil {
		t.Fatal("expected error with only msgqueue")
	}
	if len(ft.requests) != 1 {
		t.Fatalf("no RPC should be sent before capability gate, requests = %v", ft.requests)
	}
}

// TestMsgQueueListDecodesSnapshot 验证 list 带 sessionId、解析全量快照，并把
// nil items 归一为空数组。
func TestMsgQueueListDecodesSnapshot(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodMsgQueueList, func(params, result any) error {
		p := params.(MsgQueueListParams)
		if p.SessionID != "s1" {
			t.Fatalf("params = %#v", p)
		}
		r := result.(*MsgQueueSnapshot)
		r.Revision = 9
		r.Items = []MsgQueueItem{{ID: "q1", Kind: "prompt", Text: "排队中"}}
		return nil
	})
	b := probeClient(t, ft)
	msgQueueTestCaps(t, b, true, true)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}

	snap, err := sess.ListMsgQueue(context.Background())
	if err != nil {
		t.Fatalf("ListMsgQueue: %v", err)
	}
	if snap.Revision != 9 || len(snap.Items) != 1 || snap.Items[0].ID != "q1" || snap.Items[0].Text != "排队中" {
		t.Fatalf("snapshot = %#v", snap)
	}

	// nil items 归一为空数组，方便 UI 直接使用。
	ft.handlers[MethodMsgQueueList] = func(params, result any) error {
		result.(*MsgQueueSnapshot).Items = nil
		return nil
	}
	snap, err = sess.ListMsgQueue(context.Background())
	if err != nil {
		t.Fatalf("ListMsgQueue: %v", err)
	}
	if snap.Items == nil {
		t.Fatal("nil items should be normalized to an empty slice")
	}
}

// TestMsgQueueUpdateParams 验证 update 只发送非 nil 字段（prompt 改 text，
// command 改 args）。
func TestMsgQueueUpdateParams(t *testing.T) {
	ft := &fakeTransport{}
	var got []MsgQueueUpdateParams
	ft.set(MethodMsgQueueUpdate, func(params, result any) error {
		got = append(got, params.(MsgQueueUpdateParams))
		return nil
	})
	b := probeClient(t, ft)
	msgQueueTestCaps(t, b, true, true)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}

	text := "新内容"
	if err := sess.UpdateMsgQueueItem(context.Background(), "q1", &text, nil); err != nil {
		t.Fatalf("update text: %v", err)
	}
	args := "--flag"
	if err := sess.UpdateMsgQueueItem(context.Background(), "q2", nil, &args); err != nil {
		t.Fatalf("update args: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("updates = %#v", got)
	}
	if got[0].SessionID != "s1" || got[0].ItemID != "q1" || got[0].Text == nil || *got[0].Text != "新内容" || got[0].Args != nil {
		t.Errorf("text update = %#v", got[0])
	}
	if got[1].ItemID != "q2" || got[1].Text != nil || got[1].Args == nil || *got[1].Args != "--flag" {
		t.Errorf("args update = %#v", got[1])
	}
}

// TestDecodeMsgQueueUpdateEvent 验证 msgqueue_update 解码：revision / reason /
// 全量 items / 单个 item，并把 nil items 归一为空数组。
func TestDecodeMsgQueueUpdateEvent(t *testing.T) {
	raw := []byte(`{"sessionUpdate":"alk.cxykevin.top/msgqueue_update","revision":4,"reason":"enqueued","items":[{"id":"q1","kind":"prompt","text":"排队中"}],"item":{"id":"q1","kind":"prompt","text":"排队中"}}`)
	ev, err := DecodeSessionUpdatePayload("s1", raw)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := ev.(*MsgQueueUpdateEvent)
	if !ok {
		t.Fatalf("event = %T", ev)
	}
	if e.SessionID != "s1" || e.Revision != 4 || e.Reason != "enqueued" {
		t.Fatalf("event = %#v", e)
	}
	if len(e.Items) != 1 || e.Items[0].ID != "q1" || e.Item == nil || e.Item.Text != "排队中" {
		t.Fatalf("items = %#v item = %#v", e.Items, e.Item)
	}
	if len(e.Raw) == 0 {
		t.Error("event should keep raw payload")
	}

	ev, err = DecodeSessionUpdatePayload("s1", []byte(`{"sessionUpdate":"alk.cxykevin.top/msgqueue_update","revision":5,"reason":"cleared"}`))
	if err != nil {
		t.Fatal(err)
	}
	e = ev.(*MsgQueueUpdateEvent)
	if e.Items == nil {
		t.Fatal("nil items should be normalized to an empty slice")
	}
}

// TestClientCapabilitiesDeclaresMsgQueue 验证客户端在 initialize capabilities
// 顶层声明 msgqueue（服务端据此推送队列事件）。
func TestClientCapabilitiesDeclaresMsgQueue(t *testing.T) {
	data, err := json.Marshal(ClientCapabilities{MsgQueue: true})
	if err != nil {
		t.Fatal(err)
	}
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(data, &caps); err != nil {
		t.Fatal(err)
	}
	if _, ok := caps[Alkaid0CapabilityMsgQueue]; !ok {
		t.Fatalf("capabilities = %s, want msgqueue key", data)
	}
}
