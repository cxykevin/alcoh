package acp

import (
	"encoding/json"
	"testing"
)

// TestDecodeSessionUpdateAgentStatus 验证 alkaid0 的
// alk.cxykevin.top/agent_status 被解码到消息事件上：流式 chunk 与完整消息
// 都要保留子代理代码（空串表示主代理，字段缺失时为 nil）。
func TestDecodeSessionUpdateAgentStatus(t *testing.T) {
	cases := []struct {
		name   string
		update string
	}{
		{
			name: "chunk",
			update: `{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk",
				"messageId":"m1","text":"hi",
				"alk.cxykevin.top/agent_status":"coder-agent"}}`,
		},
		{
			name: "full message",
			update: `{"sessionId":"s1","update":{"sessionUpdate":"agent_message",
				"messageId":"m1","content":[{"type":"text","text":"hi"}],
				"alk.cxykevin.top/agent_status":"coder-agent"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := DecodeSessionUpdate(json.RawMessage(tc.update))
			if err != nil {
				t.Fatal(err)
			}
			var agent *string
			switch e := ev.(type) {
			case *MessageChunkEvent:
				agent = e.AgentID
			case *MessageUpdateEvent:
				agent = e.AgentID
			default:
				t.Fatalf("event = %#v, want a message event", ev)
			}
			if agent == nil || *agent != "coder-agent" {
				t.Fatalf("agent = %v, want coder-agent", agent)
			}
		})
	}

	// 主代理的空串要保留为"已提供且为空"，而字段缺失时区分成 nil。
	ev, err := DecodeSessionUpdate(json.RawMessage(`{"sessionId":"s1","update":{
		"sessionUpdate":"agent_message_chunk","messageId":"m1","text":"hi",
		"alk.cxykevin.top/agent_status":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	chunk, ok := ev.(*MessageChunkEvent)
	if !ok || chunk.AgentID == nil || *chunk.AgentID != "" {
		t.Fatalf("empty agent status should be preserved, got %#v", ev)
	}
	ev, err = DecodeSessionUpdate(json.RawMessage(`{"sessionId":"s1","update":{
		"sessionUpdate":"agent_message_chunk","messageId":"m2","text":"yo"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if chunk, ok := ev.(*MessageChunkEvent); !ok || chunk.AgentID != nil {
		t.Fatalf("missing agent status should be nil, got %#v", ev)
	}
}
