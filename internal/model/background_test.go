package model

// /background 客户端覆盖指令的状态测试：面板可用性、与服务端同名命令的
// 去重优先级，以及本地模式读写。

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

func TestBackgroundClientCommandOverridesServerEntry(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)})
	m.ActivateSession("s1", "会话")

	if !m.BackgroundAvailable() {
		t.Fatal("background command should be available with alkaid0 capability and an active session")
	}

	// 服务端也广告同名 /background：客户端覆盖指令应只出现一次，且排在前面
	// （本地命令优先级高于 agent 命令）。
	m.ApplyEvent(&acp.CommandsUpdateEvent{
		SessionID: "s1",
		Commands:  []acp.AvailableCommand{{Name: "background", Description: "Set background mode on/off — keep session alive after all clients disconnect"}},
	})
	var count int
	for _, name := range m.SlashCommands() {
		if name == "/background" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("/background appears %d times in %v, want exactly once", count, m.SlashCommands())
	}

	commands := m.SlashCommands()
	descriptions := m.SlashCommandDescriptions()
	idx := slices.Index(commands, "/background")
	if idx < 0 || idx >= len(descriptions) {
		t.Fatalf("missing /background in %v", commands)
	}
	if !strings.Contains(descriptions[idx], "后台运行模式") {
		t.Errorf("description = %q, want the client override description", descriptions[idx])
	}
	if strings.Contains(descriptions[idx], "keep session alive") {
		t.Errorf("description = %q, must not use the server-provided description", descriptions[idx])
	}

	// 本地模式读写。
	if m.Background() {
		t.Fatal("background should default to off")
	}
	m.SetBackground(true)
	if !m.Background() || !m.Active.Background {
		t.Fatal("SetBackground(true) should update the active session")
	}

	// 回到主页后不再可用。
	m.GoHome()
	if m.BackgroundAvailable() {
		t.Fatal("background command must not be available without an active session")
	}
	m.SetBackground(true) // 无活动会话时静默忽略
	if m.Background() {
		t.Fatal("background should be false without an active session")
	}
}
