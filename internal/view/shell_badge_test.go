package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestSessionShellBadgeCountsActiveOnly 验证输入框下方的 "<n> shell" 徽标只统计
// 活动 shell：已结束（历史段）的 shell 不再计入。
func TestSessionShellBadgeCountsActiveOnly(t *testing.T) {
	useEnglish(t)
	m := model.New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s1", "会话")
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/1", Status: "running", Content: "a\n"})
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/2", Status: "running", Content: "b\n"})
	m.Active.ArchiveTerminal("@temp/run/2")
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/3", Status: "running", Content: "c\n"})
	m.Active.ArchiveTerminal("@temp/run/3")

	const w, h = 100, 24
	b := renderer.NewBuffer(w, h)
	v := NewAppView(renderer.DefaultTheme())
	v.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)
	joined := strings.Join(bufferText(b), "\n")
	if !strings.Contains(joined, "1 shell") {
		t.Fatalf("badge should count only the active shell:\n%s", joined)
	}
	if strings.Contains(joined, "3 shell") || strings.Contains(joined, "2 shell") {
		t.Fatalf("badge must not count history shells:\n%s", joined)
	}
	// 两个历史 shell 都留在历史段（面板下半段仍可查看）。
	if n := len(m.ShellHistory()); n != 2 {
		t.Fatalf("history shells = %d", n)
	}
}
