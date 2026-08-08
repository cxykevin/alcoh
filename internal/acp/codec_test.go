package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeIncomingDistinguishesMessageKinds(t *testing.T) {
	request, err := DecodeIncoming([]byte(`{"jsonrpc":"2.0","id":12,"method":"session/request_permission","params":{}}`))
	if err != nil || request.Request == nil || request.Request.ID.String() != "12" {
		t.Fatalf("request = %#v, err = %v", request, err)
	}
	notification, err := DecodeIncoming([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{}}`))
	if err != nil || notification.Notification == nil {
		t.Fatalf("notification = %#v, err = %v", notification, err)
	}
	response, err := DecodeIncoming([]byte(`{"jsonrpc":"2.0","id":"abc","result":{}}`))
	if err != nil || response.Response == nil || response.Response.ID.String() != `"abc"` {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
}

func TestDecodeIncomingRejectsInvalidResponse(t *testing.T) {
	_, err := DecodeIncoming([]byte(`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":1,"message":"bad"}}`))
	if err == nil || !strings.Contains(err.Error(), "both result and error") {
		t.Fatalf("err = %v, want result/error protocol failure", err)
	}
}

func TestPermissionResponsePreservesNumericRequestID(t *testing.T) {
	out, err := MarshalResult(NewRPCID(json.RawMessage(`42`)), PermissionResponse{Outcome: OutcomeCancelled})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"id":42`) {
		t.Fatalf("response must retain numeric id, got %s", out)
	}
}

func TestDecodeSessionUpdateMessagePatchAndRawContent(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"agent_message","messageId":"m1","content":[{"type":"text","text":"hello"},{"type":"future","value":2}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	message, ok := ev.(*MessageUpdateEvent)
	if !ok || !message.Message.ContentSet || len(message.Message.Content) != 2 {
		t.Fatalf("event = %#v", ev)
	}
	if string(message.Message.Content[1].Raw) == "" {
		t.Fatal("unknown content block raw JSON was not retained")
	}
}

func TestDecodeSessionUpdateUnknownVariantIsForwardCompatible(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"future_update","value":true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	unknown, ok := ev.(*UnknownSessionUpdateEvent)
	if !ok || unknown.Discriminator != "future_update" || len(unknown.Raw) == 0 {
		t.Fatalf("event = %#v", ev)
	}
}

func TestClientCapabilitiesRawMerge(t *testing.T) {
	fs := json.RawMessage(`{"enabled":true}`)
	out, err := json.Marshal(ClientCapabilities{
		FileSystem: &fs,
		Raw:        json.RawMessage(`{"future":{"enabled":true},"fs":{"enabled":false}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"future":{"enabled":true}`) || !strings.Contains(string(out), `"fs":{"enabled":true}`) {
		t.Fatalf("capability merge = %s", out)
	}
}

func TestInitializeResultPreservesRawAndCapabilities(t *testing.T) {
	original := []byte(`{"protocolVersion":2,"capabilities":{"loadSession":false,"future":1},"info":{"name":"agent","version":"1"},"authMethods":[],"futureTop":true}`)
	var result InitializeResult
	if err := json.Unmarshal(original, &result); err != nil {
		t.Fatal(err)
	}
	if result.Capabilities.LoadSession == nil || *result.Capabilities.LoadSession {
		t.Fatalf("loadSession = %#v, want false", result.Capabilities.LoadSession)
	}
	if !strings.Contains(string(result.Raw), `"futureTop":true`) || !strings.Contains(string(result.Capabilities.Raw), `"future":1`) {
		t.Fatalf("raw result=%s caps=%s", result.Raw, result.Capabilities.Raw)
	}
	original[0] = '['
	if !json.Valid(result.Raw) || !json.Valid(result.Capabilities.Raw) {
		t.Fatal("raw protocol data must be copied, not aliased")
	}
}

func TestDecodeSessionUpdateToolCallPreservesLocations(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","locations":[{"path":"main.go","line":7}],"content":[]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := ev.(*ToolCallUpdateEvent)
	if !ok || len(tool.Locations) != 1 || tool.Locations[0].Path != "main.go" {
		t.Fatalf("event = %#v", ev)
	}
}

func TestDecodeSessionUpdateUserThoughtChunk(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"user_thought_chunk","messageId":"m1","text":"why?"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	chunk, ok := ev.(*MessageChunkEvent)
	if !ok || !chunk.IsUser || !chunk.IsThought || chunk.Text != "why?" {
		t.Fatalf("chunk = %#v", ev)
	}
}

func TestDecodeSessionUpdateCommandsPreservesRaw(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"do","description":"desc","input":{"type":"string","hint":"arg"},"futureField":true}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cmd, ok := ev.(*CommandsUpdateEvent)
	if !ok || len(cmd.Commands) != 1 || !strings.Contains(string(cmd.Commands[0].Raw), "futureField") {
		t.Fatalf("commands = %#v", ev)
	}
	if len(cmd.Commands[0].Input) == 0 {
		t.Fatalf("command input missing: %s", cmd.Commands[0].Raw)
	}
}

func TestDecodeSessionUpdateConfigOptionStructured(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"config_option_update","configOptions":[
			{"configId":"temperature","name":"Temperature","description":"sampling","category":"model","type":"number","currentValue":"0.7","options":[{"value":"0.7","name":"0.7"}],"future":true},
			{"name":"legacy","type":"boolean","currentValue":"true"}
		]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := ev.(*ConfigOptionUpdateEvent)
	if !ok || len(cfg.Options) != 2 {
		t.Fatalf("config event = %#v", ev)
	}
	first := cfg.Options[0]
	if first.ConfigID != "temperature" || first.Name != "Temperature" || first.Type != "number" || first.CurrentValue != "0.7" {
		t.Fatalf("first option = %#v", first)
	}
	if len(first.Options) != 1 || first.Options[0].Value != "0.7" {
		t.Fatalf("first option choices = %#v", first.Options)
	}
	if !strings.Contains(string(first.Raw), "future") {
		t.Fatalf("first option raw missing future field: %s", first.Raw)
	}
	if cfg.Options[1].ConfigID != "" || cfg.Options[1].Name != "legacy" {
		t.Fatalf("second option = %#v", cfg.Options[1])
	}
	if len(cfg.Raw) == 0 {
		t.Fatal("envelope raw missing")
	}
}

func TestDecodeSessionUpdateConfigOptionSingularShape(t *testing.T) {
	ev, err := DecodeSessionUpdate(json.RawMessage(`{
		"sessionId":"s1",
		"update":{"sessionUpdate":"config_option_update","option":{"configId":"model","name":"Model","type":"string","currentValue":"gpt"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := ev.(*ConfigOptionUpdateEvent)
	if !ok || len(cfg.Options) != 1 || cfg.Options[0].ConfigID != "model" {
		t.Fatalf("config event = %#v", ev)
	}
	if cfg.Options[0].CurrentValue != "gpt" {
		t.Fatalf("currentValue = %q", cfg.Options[0].CurrentValue)
	}
}

func TestAgentCapabilitiesHas(t *testing.T) {
	caps := AgentCapabilities{Raw: json.RawMessage(`{"session":{"delete":{}},"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
	if !caps.Has(Alkaid0CapabilityV04) {
		t.Fatal("should detect the alkaid0 extension capability")
	}
	if caps.Has("alk.cxykevin.top/alkaid0/v0.5") {
		t.Fatal("should not report a missing capability")
	}
	if caps.Has("") {
		t.Fatal("empty marker should not match")
	}
	if (AgentCapabilities{}).Has(Alkaid0CapabilityV04) {
		t.Fatal("empty raw should not match any capability")
	}
	bad := AgentCapabilities{Raw: json.RawMessage(`not-json`)}
	if bad.Has(Alkaid0CapabilityV04) {
		t.Fatal("malformed raw should not match")
	}
}

// TestAgentCapabilitiesSupportsSessionDelete 验证 session.delete 能力解析：
// capabilities.session.delete 存在（非 null）→ 支持；缺失或为 null → 不支持。
func TestAgentCapabilitiesSupportsSessionDelete(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"declared empty object", `{"session":{"delete":{}}}`, true},
		{"declared with value", `{"session":{"delete":{"soft":true}}}`, true},
		{"missing session", `{"loadSession":true}`, false},
		{"session without delete", `{"session":{"loadSession":true}}`, false},
		{"delete null", `{"session":{"delete":null}}`, false},
		{"empty raw", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var caps AgentCapabilities
			if c.raw != "" {
				if err := json.Unmarshal([]byte(c.raw), &caps); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
			}
			if got := caps.SupportsSessionDelete(); got != c.want {
				t.Errorf("SupportsSessionDelete() = %v, want %v", got, c.want)
			}
		})
	}
}
