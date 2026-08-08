package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestPreSessionEnablesHomeEffortModel 验证主页预创建会话公布的 config 使
// SupportsEffort / SupportsModel 在无活动会话时为 true（主页命令面板 /effort
// 与 /model 可用），且 PreSession 不激活为会话视图。
func TestPreSessionEnablesHomeEffortModel(t *testing.T) {
	m := New()
	if m.SupportsEffort() || m.SupportsModel() {
		t.Fatal("without pre-session, home must not support effort/model")
	}
	// 事件先于 SetPreSession 到达：缓存在 pendingSessionEvents，登记时重放。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "pre-1",
		Options: []acp.ConfigOption{
			{ConfigID: "thought_level", Type: "select", CurrentValue: "medium"},
			{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "m1",
				Options: []acp.ConfigOptionValue{{Value: "m1", Name: "M1"}, {Value: "m2", Name: "M2"}}},
		},
	})
	m.SetPreSession("pre-1", "")
	if !m.SupportsEffort() {
		t.Fatal("SupportsEffort should be true via pre-session config")
	}
	if !m.SupportsModel() {
		t.Fatal("SupportsModel should be true via pre-session config")
	}
	if got := m.CurrentEffort(); got != "medium" {
		t.Errorf("CurrentEffort = %q, want medium", got)
	}
	if got := m.CurrentModel(); got != "m1" {
		t.Errorf("CurrentModel = %q, want m1", got)
	}
	if !m.ValidModelValue("m2") {
		t.Error("ValidModelValue(m2) should be true")
	}
	if m.HasActive() {
		t.Fatal("pre-session must not become the active session")
	}

	// 事件在 SetPreSession 之后到达：直接应用（不缓存）。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "pre-1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "high"}},
	})
	if got := m.CurrentEffort(); got != "high" {
		t.Errorf("CurrentEffort after post-register event = %q, want high", got)
	}
}

// TestPreSessionClearDisablesHomeCommands 验证清除预创建会话后主页命令不可用，
// 且其缓存事件一并丢弃。
func TestPreSessionClearDisablesHomeCommands(t *testing.T) {
	m := New()
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "pre-1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "medium"}},
	})
	m.SetPreSession("pre-1", "")
	if !m.SupportsEffort() {
		t.Fatal("precondition: effort should be supported")
	}
	m.ClearPreSession()
	if m.SupportsEffort() {
		t.Error("SupportsEffort should be false after clearing pre-session")
	}
	if m.PreSession != nil {
		t.Error("PreSession should be nil after clear")
	}
	if len(m.pendingSessionEvents) != 0 {
		t.Errorf("pending events = %d, want 0 after clear", len(m.pendingSessionEvents))
	}
}

// TestPreSessionEventsAppliedBeforeRegister 验证事件先于 SetPreSession 到达时被
// 缓存并在登记时重放，不会丢弃。
func TestPreSessionEventsAppliedBeforeRegister(t *testing.T) {
	m := New()
	m.ApplyEvent(&acp.CommandsUpdateEvent{
		SessionID: "pre-1",
		Commands:  []acp.AvailableCommand{{Name: "explain", Description: "解释当前实现"}},
	})
	if len(m.pendingSessionEvents) != 1 {
		t.Fatalf("pending events = %d, want 1", len(m.pendingSessionEvents))
	}
	m.SetPreSession("pre-1", "")
	if m.PreSession == nil {
		t.Fatal("PreSession should be set")
	}
	if len(m.PreSession.Commands) != 1 || m.PreSession.Commands[0].Name != "explain" {
		t.Errorf("replayed commands = %#v, want [explain]", m.PreSession.Commands)
	}
	if len(m.pendingSessionEvents) != 0 {
		t.Errorf("pending events after replay = %d, want 0", len(m.pendingSessionEvents))
	}
}

// TestHomeSlashCommandsIncludeServer 验证主页命令面板（无活动会话、无预创建会话）
// 在服务端声明 alkaid0 扩展能力时包含 /server；未声明时不包含。
func TestHomeSlashCommandsIncludeServer(t *testing.T) {
	m := New()
	// 未声明 alkaid0 能力：主页不出现 /server。
	if containsString(m.SlashCommands(), "/server") {
		t.Fatal("/server should be absent without alkaid0 capability")
	}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)})
	// 主页（无 Active、无 PreSession）也应显示 /server。
	if !containsString(m.SlashCommands(), "/server") {
		t.Fatalf("home slash commands should include /server when alkaid0 capability declared, got %v", m.SlashCommands())
	}
	if m.HasActive() {
		t.Fatal("precondition: must stay on home (no active session)")
	}
}

// TestPreSessionActiveConfigPriority 验证活动会话存在时 config 优先取自活动会话，
// 预创建会话仅作回退（进入真实会话后 /effort 与 /model 作用于活动会话）。
func TestPreSessionActiveConfigPriority(t *testing.T) {
	m := New()
	m.SetPreSession("pre-1", "")
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "pre-1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "high"}},
	})
	m.ActivateSession("s1", "")
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "low"}},
	})
	if got := m.CurrentEffort(); got != "low" {
		t.Errorf("CurrentEffort = %q, want active-session value low", got)
	}
	if m.PreSession == nil {
		t.Error("PreSession should remain set alongside active session")
	}
}
