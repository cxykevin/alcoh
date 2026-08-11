package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/widget"
)

// alkaid0Caps 是声明了 alkaid0 扩展能力 v0.4 的能力对象，供测试构造。
func alkaid0Caps() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
}

func TestSlashSelectionUsesFilteredCommands(t *testing.T) {
	m := New()
	// /server 需要服务端声明 alkaid0 扩展能力才会出现在命令列表中。
	m.SetAgentInfo(acp.AgentInfo{Name: "alkaid0", Version: "test"}, alkaid0Caps())
	m.Input = widget.NewInputBuffer()
	for _, r := range "/se" {
		m.Input.InsertRune(r)
	}
	m.UpdateSlashState()

	if !m.SlashOpen {
		t.Fatal("slash panel should open")
	}
	if got := m.SlashSelectedCommand(); got != "/settings" {
		t.Fatalf("selected command = %q, want /settings", got)
	}
	m.SlashMove(1)
	if got := m.SlashSelectedCommand(); got != "/server" {
		t.Fatalf("selected command after move = %q, want /server", got)
	}
}

func TestSlashCommandsUsePriorityOrder(t *testing.T) {
	m := New()
	m.LocalCommands = []string{"/z-local", "/alcoh_help"}
	m.Active = NewSession("s", "")
	m.Active.Commands = []acp.AvailableCommand{{Name: "agent"}, {Name: "tool"}, {Name: "other"}}
	got := m.SlashCommands()
	want := []string{"/alcoh_help", "/z-local", "/agent", "/tool", "/other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}
func TestPermissionQueueSerializesRequests(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	desc1 := "first"
	desc2 := "second"
	m.ApplyEvent(&acp.PermissionRequestEvent{SessionID: "s1", Request: acp.PermissionRequest{RequestID: "r1", SessionID: "s1", Title: "one", Description: &desc1, Options: []acp.PermissionOption{{OptionID: "a", Name: "allow", Kind: acp.AllowOnce}}}})
	m.ApplyEvent(&acp.PermissionRequestEvent{SessionID: "s1", Request: acp.PermissionRequest{RequestID: "r2", SessionID: "s1", Title: "two", Description: &desc2, Options: []acp.PermissionOption{{OptionID: "a", Name: "allow", Kind: acp.AllowOnce}}}})
	if m.Permission == nil || m.Permission.RequestID != "r1" {
		t.Fatalf("first permission should be active, got %#v", m.Permission)
	}
	if m.PendingPermissionCount() != 1 {
		t.Fatalf("pending count = %d, want 1", m.PendingPermissionCount())
	}
	outcome, _ := m.ApproveSelection()
	if outcome != acp.OutcomeSelected {
		t.Fatalf("outcome = %v", outcome)
	}
	if m.Permission == nil || m.Permission.RequestID != "r2" {
		t.Fatalf("second permission should surface after first, got %#v", m.Permission)
	}
	m.CancelPermission()
	if m.Permission != nil || m.Modal == ModalPermission {
		t.Fatalf("queue should drain: perm=%#v modal=%v", m.Permission, m.Modal)
	}
}

func TestNonTextContentBlockPlaceholder(t *testing.T) {
	mime := "image/png"
	uri := "file:///tmp/a.png"
	name := "diagram"
	blk := acp.ContentBlock{Type: "image", MimeType: &mime, URI: &uri, Name: &name}
	got := nonTextBlockPlaceholder(blk)
	for _, want := range []string{"image", "diagram", "image/png", uri} {
		if !containsAll(got, want) {
			t.Fatalf("placeholder %q missing %q", got, want)
		}
	}
}

func containsAll(text, substr string) bool { return len(substr) == 0 || indexOf(text, substr) >= 0 }

func indexOf(text, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	for i := 0; i+len(substr) <= len(text); i++ {
		if text[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestSlashCompletionGhostText(t *testing.T) {
	m := New()
	for _, r := range "/se" {
		m.Input.InsertRune(r)
	}
	m.UpdateSlashState()
	ghost, _ := m.SlashCompletion()
	if ghost != "ttings" {
		t.Fatalf("ghost = %q, want ttings", ghost)
	}
	// 光标不在命令 token 内时不显示 ghost
	m.Input.InsertRune(' ')
	m.Input.InsertRune('x')
	m.UpdateSlashState()
	if g, _ := m.SlashCompletion(); g != "" {
		t.Fatalf("ghost should be empty when cursor past token, got %q", g)
	}
	if m.SlashOpen {
		t.Fatal("slash panel should close once cursor moved past token")
	}
}

func TestSlashCommandTokenFiltering(t *testing.T) {
	m := New()
	for _, r := range "/se arg" {
		m.Input.InsertRune(r)
	}
	m.Input.CX = 3 // 光标停在 "/se" 结尾
	m.UpdateSlashState()
	if !m.SlashOpen {
		t.Fatal("slash panel should be open when cursor in command token")
	}
	commands, _ := m.FilteredSlashCommands()
	for _, name := range commands {
		if len(name) < 3 || name[:3] != "/se" {
			t.Fatalf("filtered command %q not matching /se token", name)
		}
	}
}

func TestReplaceFirstTokenPreservesArgs(t *testing.T) {
	buf := widget.NewInputBuffer()
	for _, r := range "/se arg1 arg2" {
		buf.InsertRune(r)
	}
	buf.ReplaceFirstToken("/clear")
	if got := buf.Text(); got != "/clear arg1 arg2" {
		t.Fatalf("text after replace = %q, want /clear arg1 arg2", got)
	}
	if buf.CX != len("/clear") {
		t.Fatalf("cursor = %d, want %d", buf.CX, len("/clear"))
	}
}

func TestSettingsStateChanges(t *testing.T) {
	m := New()
	m.OpenSettings()
	if m.Modal != ModalSettings || m.SettingsSelected != 0 {
		t.Fatalf("settings should open at first row: modal=%v selected=%d", m.Modal, m.SettingsSelected)
	}
	if !m.CycleColorMode(1) || m.Settings.ColorMode != "mono" {
		t.Fatalf("color mode = %q, want mono", m.Settings.ColorMode)
	}
	m.MoveSettings(1, 3)
	if !m.ToggleSetting() || !m.Settings.ThinkingExpanded {
		t.Fatal("thinking setting should toggle on")
	}
	m.OpenServer()
	if m.Modal != ModalServer {
		t.Fatalf("server modal = %v, want %v", m.Modal, ModalServer)
	}
}

func TestSettingsLanguageCycling(t *testing.T) {
	m := New()
	m.OpenSettings()
	m.MoveSettings(1, 4)
	m.MoveSettings(1, 4)
	m.MoveSettings(1, 4) // 第 3 行 = 语言
	if m.SettingsSelected != 3 {
		t.Fatalf("language row selected = %d, want 3", m.SettingsSelected)
	}
	if !m.CycleLanguage(1) || m.Settings.Language != "en" {
		t.Fatalf("language = %q, want en", m.Settings.Language)
	}
	if m.CycleColorMode(1) {
		t.Fatal("CycleColorMode should be a no-op on the language row")
	}
	m.SettingsSelected = 3
	if !m.CycleLanguage(-1) || m.Settings.Language != "zh" {
		t.Fatalf("language = %q, want zh after cycling back", m.Settings.Language)
	}
}

func TestServerCommandRequiresAlkaid0Capability(t *testing.T) {
	m := New()
	if containsString(m.SlashCommands(), "/server") {
		t.Fatal("/server should not appear without alkaid0 capability")
	}

	m.SetAgentInfo(acp.AgentInfo{Name: "alkaid0", Version: "test"}, alkaid0Caps())
	if !m.SupportsAlkaid0() {
		t.Fatal("SupportsAlkaid0 should be true with v0.4 capability")
	}
	if !containsString(m.SlashCommands(), "/server") {
		t.Fatal("/server should appear when alkaid0 capability is declared")
	}

	// 与 alkaid0 无关的扩展标记不应触发 /server。
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/other/v1":{}}`)})
	if m.SupportsAlkaid0() {
		t.Fatal("SupportsAlkaid0 should be false for unrelated capability")
	}
	if containsString(m.SlashCommands(), "/server") {
		t.Fatal("/server should not appear for unrelated capability")
	}
}

// TestSupportsSessionDelete 验证 session/delete 能力门控：声明 capabilities
// .session.delete 才支持删除会话。
func TestSupportsSessionDelete(t *testing.T) {
	m := New()
	if m.SupportsSessionDelete() {
		t.Fatal("SupportsSessionDelete should be false without capability")
	}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"session":{"delete":{}}}`)})
	if !m.SupportsSessionDelete() {
		t.Fatal("SupportsSessionDelete should be true when session.delete is declared")
	}
}

// TestRemoveSession 验证从首页会话列表移除项并调整选中索引，删除活动会话时
// 回到首页。
func TestRemoveSession(t *testing.T) {
	m := New()
	m.Sessions = []*acp.SessionInfo{
		{SessionID: "s0", Title: "t0"},
		{SessionID: "s1", Title: "t1"},
		{SessionID: "s2", Title: "t2"},
	}
	m.HomeSelected = 1

	// 删除选中项之后的项（s2）：选中索引不变。
	if !m.RemoveSession("s2") {
		t.Fatal("RemoveSession(s2) should remove")
	}
	if len(m.Sessions) != 2 || m.HomeSelected != 1 {
		t.Fatalf("after removing s2: sessions=%d selected=%d", len(m.Sessions), m.HomeSelected)
	}

	// 删除选中项之前的项（s0）：选中索引前移。
	if !m.RemoveSession("s0") {
		t.Fatal("RemoveSession(s0) should remove")
	}
	if len(m.Sessions) != 1 || m.Sessions[0].SessionID != "s1" || m.HomeSelected != 0 {
		t.Fatalf("after removing s0: sessions=%#v selected=%d", m.Sessions, m.HomeSelected)
	}

	// 删除不存在的项：返回 false 且状态不变。
	if m.RemoveSession("nope") {
		t.Fatal("RemoveSession(nope) should not remove")
	}

	// 删除最后一项：列表空，选中置 -1。
	if !m.RemoveSession("s1") {
		t.Fatal("RemoveSession(s1) should remove")
	}
	if len(m.Sessions) != 0 || m.HomeSelected != -1 {
		t.Fatalf("after removing all: sessions=%d selected=%d", len(m.Sessions), m.HomeSelected)
	}
}

// TestRemoveSessionActiveGoesHome 验证删除的会话恰为活动会话时回到首页。
func TestRemoveSessionActiveGoesHome(t *testing.T) {
	m := New()
	m.Sessions = []*acp.SessionInfo{{SessionID: "s1", Title: "t1"}}
	m.ActivateSession("s1", "t1")
	if !m.HasActive() {
		t.Fatal("should be in session view before delete")
	}
	if !m.RemoveSession("s1") {
		t.Fatal("RemoveSession(s1) should remove")
	}
	if m.View != ViewHome || m.Active != nil {
		t.Fatal("deleting active session should return to home")
	}
}

func TestUnknownSessionUpdateAppendsTimelineNotice(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	raw := []byte(`{"sessionUpdate":"future_update","value":true}`)
	m.ApplyEvent(&acp.UnknownSessionUpdateEvent{SessionID: "s1", Discriminator: "future_update", Raw: raw})
	if len(m.Active.ProtocolUpdates) != 1 {
		t.Fatalf("protocol updates = %d, want 1", len(m.Active.ProtocolUpdates))
	}
	if len(m.Active.Timeline) != 1 || m.Active.Timeline[0].Kind != TimelineSystemNotice {
		t.Fatalf("timeline = %#v", m.Active.Timeline)
	}
	if !indexOfAny(m.Active.Timeline[0].Notice, "future_update") {
		t.Fatalf("notice = %q missing discriminator", m.Active.Timeline[0].Notice)
	}
	// 相同 discriminator 再次到达时不重复插入行。
	m.ApplyEvent(&acp.UnknownSessionUpdateEvent{SessionID: "s1", Discriminator: "future_update", Raw: raw})
	if len(m.Active.Timeline) != 1 {
		t.Fatalf("duplicate notice must reuse row, got %d", len(m.Active.Timeline))
	}
	// 不同 discriminator 追加新行。
	m.ApplyEvent(&acp.UnknownSessionUpdateEvent{SessionID: "s1", Discriminator: "another", Raw: raw})
	if len(m.Active.Timeline) != 2 {
		t.Fatalf("distinct discriminators must produce separate rows, got %d", len(m.Active.Timeline))
	}
}

func indexOfAny(text, substr string) bool { return indexOf(text, substr) >= 0 }

func TestApplyAgentConfigUpsertSemantics(t *testing.T) {
	s := NewSession("s1", "")
	// 首批 patch：configId 与 name 混合。
	s.applyAgentConfig([]acp.ConfigOption{
		{ConfigID: "temp", Name: "Temperature", Type: "number"},
		{Name: "Verbose", Type: "boolean"},
	})
	if len(s.AgentConfig) != 2 {
		t.Fatalf("initial config = %#v", s.AgentConfig)
	}
	// 按 configId 更新，顺序保持不变。
	s.applyAgentConfig([]acp.ConfigOption{
		{ConfigID: "temp", Name: "Temperature (updated)", Type: "number"},
	})
	if len(s.AgentConfig) != 2 || s.AgentConfig[0].Name != "Temperature (updated)" {
		t.Fatalf("upsert by configId failed: %#v", s.AgentConfig)
	}
	// 按 name 更新。
	s.applyAgentConfig([]acp.ConfigOption{
		{Name: "Verbose", Type: "string"},
	})
	if s.AgentConfig[1].Type != "string" {
		t.Fatalf("upsert by name failed: %#v", s.AgentConfig)
	}
	// 新 configId 追加到末尾。
	s.applyAgentConfig([]acp.ConfigOption{
		{ConfigID: "seed", Name: "Seed"},
	})
	if len(s.AgentConfig) != 3 || s.AgentConfig[2].ConfigID != "seed" {
		t.Fatalf("append new configId failed: %#v", s.AgentConfig)
	}
	// 无 configId / 无 name 的整批 patch 走整体替换语义。
	s.applyAgentConfig([]acp.ConfigOption{
		{Type: "opaque"},
	})
	if len(s.AgentConfig) != 1 || s.AgentConfig[0].Type != "opaque" {
		t.Fatalf("replace-all fallback failed: %#v", s.AgentConfig)
	}
}

func TestCommandsCachedUntilSessionActivated(t *testing.T) {
	// alkaid0 在 session/new 响应返回前就广播 available_commands_update；
	// 此刻会话尚未激活，命令必须缓存并在激活后重放，不能丢失。
	m := New()
	m.ApplyEvent(&acp.CommandsUpdateEvent{
		SessionID: "sess-1",
		Commands: []acp.AvailableCommand{
			{Name: "explain", Description: "解释实现"},
			{Name: "tests"},
		},
	})
	m.ActivateSession("sess-1", "t")
	var got []string
	for _, c := range m.SlashCommands() {
		if c == "/explain" || c == "/tests" {
			got = append(got, c)
		}
	}
	if len(got) != 2 {
		t.Errorf("agent commands should be available after activation, got %v", got)
	}
}

func TestCommandsOnlyApplyToOwnSession(t *testing.T) {
	m := New()
	m.ActivateSession("sess-a", "a")
	// 后台会话 B 的命令到达：不得污染活动会话 A。
	m.ApplyEvent(&acp.CommandsUpdateEvent{
		SessionID: "sess-b",
		Commands:  []acp.AvailableCommand{{Name: "b-cmd"}},
	})
	for _, c := range m.SlashCommands() {
		if c == "/b-cmd" {
			t.Errorf("background session command leaked into active session")
		}
	}
	// 激活 B 后命令可用。
	m.ActivateSession("sess-b", "b")
	found := false
	for _, c := range m.SlashCommands() {
		if c == "/b-cmd" {
			found = true
		}
	}
	if !found {
		t.Errorf("session B command should be available after activation")
	}
}

func TestConfigOptionUpdateEventPersistsSnapshotAndProtocol(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	raw := []byte(`{"sessionUpdate":"config_option_update","configOptions":[{"configId":"temp","name":"Temperature","type":"number"}]}`)
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options:   []acp.ConfigOption{{ConfigID: "temp", Name: "Temperature", Type: "number"}},
		Raw:       raw,
	})
	if len(m.Active.AgentConfig) != 1 || m.Active.AgentConfig[0].ConfigID != "temp" {
		t.Fatalf("agent config not applied: %#v", m.Active.AgentConfig)
	}
	if len(m.Active.ProtocolUpdates) != 1 {
		t.Fatalf("protocol updates = %d, want 1", len(m.Active.ProtocolUpdates))
	}
}

func TestActivateSessionIdempotent(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	// 会话激活后应用配置。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "medium"}},
	})
	if got := len(m.Active.AgentConfig); got != 1 {
		t.Fatalf("agent config after apply = %d, want 1", got)
	}
	// 重复激活同一会话（命令完成信号与后端事件并发到达）不得重建状态。
	m.ActivateSession("s1", "")
	if got := len(m.Active.AgentConfig); got != 1 {
		t.Errorf("idempotent activate must not reset agent config, got %d", got)
	}
	if !m.SupportsEffort() {
		t.Error("SupportsEffort should survive idempotent activation")
	}
	// 回首页后再激活同一会话：全新状态。
	m.GoHome()
	m.ActivateSession("s1", "")
	if got := len(m.Active.AgentConfig); got != 0 {
		t.Errorf("re-entry should create fresh session state, got %d configs", got)
	}
}

func TestEffortOnlyEnabledWhenThoughtLevelSupported(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	// 无 thought_level 配置 → /effort 不可用。
	for _, c := range m.SlashCommands() {
		if c == "/effort" {
			t.Fatalf("/effort should not be available without thought_level config")
		}
	}
	if m.SupportsEffort() {
		t.Fatal("SupportsEffort should be false")
	}
	// 服务端公布 thought_level → /effort 可用，且值始终是客户端硬编码候选。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options: []acp.ConfigOption{
			{ConfigID: "thought_level", Name: "Thought Level", Type: "select", CurrentValue: "high"},
			{ConfigID: "model", Name: "Model", Type: "select", CurrentValue: "0/foo"},
		},
	})
	if !m.SupportsEffort() {
		t.Fatal("SupportsEffort should be true when thought_level present")
	}
	found := false
	for _, c := range m.SlashCommands() {
		if c == "/effort" {
			found = true
		}
	}
	if !found {
		t.Fatalf("/effort should be available when thought_level present: %v", m.SlashCommands())
	}
	if got := m.CurrentEffort(); got != "high" {
		t.Errorf("CurrentEffort = %q, want high", got)
	}
}

func TestEffortModalSlider(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "xhigh"}},
	})
	m.OpenEffortModal()
	if m.Modal != ModalEffort {
		t.Fatalf("modal should be ModalEffort, got %v", m.Modal)
	}
	// 选中项初始化为服务端当前值。
	if m.EffortSelect != 4 { // unset low medium high xhigh max → xhigh 索引 4
		t.Errorf("initial select = %d, want 4 (xhigh)", m.EffortSelect)
	}
	// 左右移动（带环绕）。
	m.EffortMove(-1)
	if got := m.EffortSelectedValue(); got != "high" {
		t.Errorf("after left: %q, want high", got)
	}
	if m.Modal != NoModal {
		t.Errorf("confirm should close modal")
	}
	// 环绕：从 unset 向左到 max。
	m.OpenEffortModal()
	m.EffortSelect = 0
	m.EffortMove(-1)
	if m.EffortSelect != len(EffortLevels())-1 {
		t.Errorf("wrap left should land on max, got %d", m.EffortSelect)
	}
	m.CancelEffort()
	if m.Modal != NoModal {
		t.Errorf("cancel should close modal")
	}
}

func TestEffortValueValidation(t *testing.T) {
	m := New()
	for _, v := range []string{"unset", "low", "medium", "high", "xhigh", "max"} {
		if !m.ValidEffortValue(v) {
			t.Errorf("ValidEffortValue(%q) should be true", v)
		}
	}
	for _, v := range []string{"", "HIGH", "ultra", "medium "} {
		if m.ValidEffortValue(v) {
			t.Errorf("ValidEffortValue(%q) should be false", v)
		}
	}
}

func TestModelOnlyEnabledWhenConfigPublished(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	// 无 model 配置 → /model 不可用。
	for _, c := range m.SlashCommands() {
		if c == "/model" {
			t.Fatalf("/model should not be available without model config")
		}
	}
	if m.SupportsModel() {
		t.Fatal("SupportsModel should be false")
	}
	// 服务端公布 category="model" 配置 → /model 可用。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options: []acp.ConfigOption{
			{ConfigID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "0/foo",
				Options: []acp.ConfigOptionValue{{Value: "0/foo", Name: "foo"}, {Value: "1/bar", Name: "bar"}}},
		},
	})
	if !m.SupportsModel() {
		t.Fatal("SupportsModel should be true when model config present")
	}
	found := false
	for _, c := range m.SlashCommands() {
		if c == "/model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("/model should be available when model config present: %v", m.SlashCommands())
	}
	if got := m.CurrentModel(); got != "0/foo" {
		t.Errorf("CurrentModel = %q, want 0/foo", got)
	}
	if !m.ValidModelValue("1/bar") || m.ValidModelValue("nope") {
		t.Errorf("ValidModelValue should accept candidates only")
	}
	// 仅 configId="model"（无 category）也可识别。
	m2 := New()
	m2.ActivateSession("s2", "")
	m2.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s2",
		Options:   []acp.ConfigOption{{ConfigID: "model", Type: "select", CurrentValue: "x"}},
	})
	if !m2.SupportsModel() {
		t.Error("SupportsModel should match configId=model without category")
	}
}

func TestModelModalSelection(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "")
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options: []acp.ConfigOption{
			{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "demo-go-2",
				Options: []acp.ConfigOptionValue{
					{Value: "demo-go-1", Name: "Demo Go 1"},
					{Value: "demo-go-2", Name: "Demo Go 2"},
					{Value: "demo-go-3", Name: "Demo Go 3"},
				}},
		},
	})
	m.OpenModelModal()
	if m.Modal != ModalModel {
		t.Fatalf("modal should be ModalModel, got %v", m.Modal)
	}
	// 选中项初始化为服务端当前值。
	if m.ModelSelect != 1 {
		t.Errorf("initial select = %d, want 1 (demo-go-2)", m.ModelSelect)
	}
	// 上下移动（带环绕）。
	m.ModelMove(-1)
	if got := m.ModelSelectedValue(); got != "demo-go-1" {
		t.Errorf("after up: %q, want demo-go-1", got)
	}
	if m.Modal != NoModal {
		t.Errorf("confirm should close modal")
	}
	// 环绕：从最后一个向下回到第一个。
	m.OpenModelModal()
	m.ModelSelect = len(m.ModelOptions()) - 1
	m.ModelMove(1)
	if m.ModelSelect != 0 {
		t.Errorf("wrap down should land on 0, got %d", m.ModelSelect)
	}
	m.CancelModel()
	if m.Modal != NoModal {
		t.Errorf("cancel should close modal")
	}
}

func TestModelLabelFallback(t *testing.T) {
	// 无 session-info model，仅 model config → 回退到 currentValue 显示名。
	s := NewSession("s1", "")
	s.applyAgentConfig([]acp.ConfigOption{
		{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "1/bar",
			Options: []acp.ConfigOptionValue{{Value: "0/foo", Name: "Foo"}, {Value: "1/bar", Name: "Bar"}}},
	})
	if got := s.ModelLabel(); got != "Bar" {
		t.Errorf("ModelLabel = %q, want Bar", got)
	}
	// currentValue 不在候选内 → 原样返回。
	s2 := NewSession("s2", "")
	s2.applyAgentConfig([]acp.ConfigOption{
		{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "0/unknown"},
	})
	if got := s2.ModelLabel(); got != "0/unknown" {
		t.Errorf("ModelLabel = %q, want 0/unknown", got)
	}
	// session-info model 字段优先。
	s3 := NewSession("s3", "")
	s3.ModelName = "meta-model"
	s3.applyAgentConfig([]acp.ConfigOption{
		{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "1/bar"},
	})
	if got := s3.ModelLabel(); got != "meta-model" {
		t.Errorf("ModelLabel = %q, want meta-model", got)
	}
	// 两者皆无 → 空串。
	s4 := NewSession("s4", "")
	if got := s4.ModelLabel(); got != "" {
		t.Errorf("ModelLabel = %q, want empty", got)
	}
}

func TestHomeListDefaultsToUnfocused(t *testing.T) {
	// Task 5：首页会话列表默认不聚焦，HomeSelected 初始为 -1。
	m := New()
	if m.HomeSelected != -1 {
		t.Errorf("New().HomeSelected = %d, want -1", m.HomeSelected)
	}
	if m.HomeListFocused {
		t.Error("New().HomeListFocused = true, want false")
	}
	// 聚焦后调用 GoHome 应重置为未聚焦。
	m.HomeListFocused = true
	m.HomeSelected = 2
	m.GoHome()
	if m.HomeSelected != -1 {
		t.Errorf("GoHome().HomeSelected = %d, want -1", m.HomeSelected)
	}
	if m.HomeListFocused {
		t.Error("GoHome().HomeListFocused = true, want false")
	}
}

func TestShowErrorAutoExpires(t *testing.T) {
	m := New()
	m.ShowError("boom")
	if m.Error != "boom" {
		t.Fatalf("ShowError Error = %q, want %q", m.Error, "boom")
	}
	if m.ErrorInfo {
		t.Error("ShowError should set ErrorInfo = false")
	}
	if m.ErrorExpires.IsZero() {
		t.Fatal("ShowError should set ErrorExpires")
	}
	if !m.ErrorExpires.After(time.Now()) {
		t.Fatal("ErrorExpires should be in the future")
	}

	// timeout 前不消失。
	if m.ExpireError(time.Now().Add(ErrorTimeout / 2)) {
		t.Error("ExpireError cleared error before timeout")
	}
	if m.Error != "boom" {
		t.Fatalf("error cleared early: %q", m.Error)
	}

	// timeout 后自动消失，并报告状态变化。
	if !m.ExpireError(time.Now().Add(ErrorTimeout + time.Second)) {
		t.Error("ExpireError did not report change after timeout")
	}
	if m.Error != "" {
		t.Fatalf("error = %q, want empty after timeout", m.Error)
	}
	// 已空时再次调用不再变化。
	if m.ExpireError(time.Now().Add(time.Hour)) {
		t.Error("ExpireError reported change on already-clear error")
	}
}

func TestShowInfoUsesInfoStyle(t *testing.T) {
	m := New()
	m.ShowInfo("已复制 3 个字符")
	if m.Error != "已复制 3 个字符" {
		t.Fatalf("ShowInfo Error = %q, want %q", m.Error, "已复制 3 个字符")
	}
	if !m.ErrorInfo {
		t.Error("ShowInfo should set ErrorInfo = true")
	}
	if m.ErrorExpires.IsZero() {
		t.Fatal("ShowInfo should set ErrorExpires")
	}

	// ShowError 应覆盖信息样式为错误样式。
	m.ShowError("real error")
	if m.ErrorInfo {
		t.Error("ShowError after ShowInfo should clear ErrorInfo")
	}
	// ShowInfo 应覆盖错误样式为信息样式。
	m.ShowInfo("again")
	if !m.ErrorInfo {
		t.Error("ShowInfo after ShowError should set ErrorInfo")
	}

	// 过期清除时样式一并复位。
	if !m.ExpireError(time.Now().Add(ErrorTimeout + time.Second)) {
		t.Error("ExpireError did not report change after timeout")
	}
	if m.Error != "" || m.ErrorInfo {
		t.Errorf("after expiry: Error=%q ErrorInfo=%v, want empty/false", m.Error, m.ErrorInfo)
	}
}

func TestClearErrorResetsExpiry(t *testing.T) {
	m := New()
	m.ShowError("boom")
	m.ClearError()
	if m.Error != "" {
		t.Fatalf("ClearError Error = %q, want empty", m.Error)
	}
	if m.ErrorInfo {
		t.Error("ClearError should reset ErrorInfo")
	}
	if !m.ErrorExpires.IsZero() {
		t.Fatal("ClearError should reset ErrorExpires")
	}
	if m.ExpireError(time.Now().Add(time.Hour)) {
		t.Error("ExpireError reported change after ClearError")
	}
}
