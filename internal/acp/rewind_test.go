package acp

import (
	"encoding/json"
	"testing"
)

// TestDecodeRewindUpdate 验证 alk.cxykevin.top/session/rewind 被解析为
// RewindEvent：成功带目标消息 ID，失败带服务端给出的原因（见 extension.md §2.7）。
func TestDecodeRewindUpdate(t *testing.T) {
	ev, err := DecodeSessionUpdatePayload("s1", json.RawMessage(
		`{"sessionUpdate":"alk.cxykevin.top/session/rewind","messageId":"msg_7","success":true}`))
	if err != nil {
		t.Fatalf("DecodeSessionUpdatePayload: %v", err)
	}
	ok, isRewind := ev.(*RewindEvent)
	if !isRewind {
		t.Fatalf("event = %T, want *RewindEvent", ev)
	}
	if ok.SessionID != "s1" || ok.MessageID != "msg_7" || !ok.Success || ok.Error != "" {
		t.Fatalf("event = %#v", ok)
	}

	failed, err := DecodeSessionUpdatePayload("s1", json.RawMessage(
		`{"sessionUpdate":"alk.cxykevin.top/session/rewind","messageId":"msg_1","success":false,"error":"cannot rewind the first message"}`))
	if err != nil {
		t.Fatalf("DecodeSessionUpdatePayload: %v", err)
	}
	failure, isRewind := failed.(*RewindEvent)
	if !isRewind {
		t.Fatalf("event = %T, want *RewindEvent", failed)
	}
	if failure.Success || failure.MessageID != "msg_1" || failure.Error != "cannot rewind the first message" {
		t.Fatalf("event = %#v", failure)
	}
}

// TestDecodeRewindEnvelope 验证 session/update 信封（sessionId + update）里的
// rewind 通知同样能解出，且携带会话归属——模型据此把它路由到活动会话。
func TestDecodeRewindEnvelope(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(
		`{"sessionId":"s9","update":{"sessionUpdate":"alk.cxykevin.top/session/rewind","messageId":"msg_2","success":true}}`))
	if err != nil {
		t.Fatalf("DecodeSessionUpdate: %v", err)
	}
	re, ok := ev.(*RewindEvent)
	if !ok {
		t.Fatalf("event = %T, want *RewindEvent", ev)
	}
	if re.SessionID != "s9" || re.MessageID != "msg_2" || !re.Success {
		t.Fatalf("event = %#v", re)
	}
}
