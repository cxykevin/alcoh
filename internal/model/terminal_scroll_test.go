package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// newScrollSession 构造一个带长输出的终端会话（活动终端）。
func newScrollSession(t *testing.T, lines int) (*AppModel, *TerminalState) {
	t.Helper()
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s", "session")
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		sb.WriteString("line-")
		sb.WriteString(strings.Repeat("x", 10))
		sb.WriteString("\n")
	}
	m.ApplyEvent(&acp.TerminalListEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/1", Status: "running", Command: "npm run dev", Content: sb.String()},
	}})
	s := m.SelectedShell()
	if s == nil {
		t.Fatal("terminal must be listed")
	}
	return m, s
}

// TestShellPreviewScrollKeepsSticky 验证活动终端的滚动粘滞：停在底部时新输出继续
// 跟随（Scroll 保持 0），用户向上翻页后不再自动跳回。
func TestShellPreviewScrollKeepsSticky(t *testing.T) {
	m, s := newScrollSession(t, 120)
	m.ShellPreviewRows = 10
	s.ScreenRows = 120
	if !s.FollowBottom || s.Scroll != 0 {
		t.Fatalf("new terminal must follow bottom: scroll=%d follow=%v", s.Scroll, s.FollowBottom)
	}
	// 底部 + 新输出：仍停在底部。
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/1", Status: "running", Output: "more\n"})
	s.ScreenRows = 200
	m.ScrollShellPreview(0)
	if s.Scroll != 0 || !s.FollowBottom {
		t.Fatalf("sticky bottom broken: scroll=%d follow=%v", s.Scroll, s.FollowBottom)
	}
	// 向上翻一页：停止跟随。
	m.ScrollShellPreview(m.ShellPreviewHeight())
	if s.Scroll != 10 || s.FollowBottom {
		t.Fatalf("page up: scroll=%d follow=%v", s.Scroll, s.FollowBottom)
	}
	// 又来了新输出：位置不动（由视图按 Scroll 计算窗口，这里只验证状态不变）。
	m.ApplyEvent(&acp.TerminalUpdateEvent{SessionID: "s", TerminalID: "@temp/run/1", Status: "running", Output: "tail\n"})
	if s.Scroll != 10 {
		t.Fatalf("scroll must not jump while not following: %d", s.Scroll)
	}
	// 滚回底部：恢复粘滞。
	m.ScrollShellPreview(-100)
	if s.Scroll != 0 || !s.FollowBottom {
		t.Fatalf("back to bottom: scroll=%d follow=%v", s.Scroll, s.FollowBottom)
	}
}

// TestResetShellPreviewScrollOnSwitch 验证切换到某个终端时一律回到最底部。
func TestResetShellPreviewScrollOnSwitch(t *testing.T) {
	m, first := newScrollSession(t, 120)
	m.ShellPreviewRows = 10
	first.ScreenRows = 120
	m.ApplyEvent(&acp.TerminalListEvent{SessionID: "s", Terminals: []acp.TerminalInfo{
		{TerminalID: "@temp/run/2", Status: "running", Command: "go test", Content: "x\n"},
	}})
	// 新终端排在最前（下标 0），选回第一个终端再向上翻滚。
	m.ShellSelected = 1
	if s := m.SelectedShell(); s != first {
		t.Fatalf("selected = %#v, want the first terminal", s)
	}
	m.ScrollShellPreview(50)
	if first.Scroll == 0 {
		t.Fatal("scroll must move up")
	}
	first.FollowBottom = false
	// 切到另一个终端再切回来：每次都从底部开始。
	m.ShellSelected = 1
	m.ResetShellPreviewScroll()
	if s := m.SelectedShell(); s == nil || s.Scroll != 0 || !s.FollowBottom {
		t.Fatalf("switched terminal must start at bottom: %#v", s)
	}
	m.ShellSelected = 0
	m.ResetShellPreviewScroll()
	if first.Scroll != 0 || !first.FollowBottom {
		t.Fatalf("switching back must reset scroll: scroll=%d follow=%v", first.Scroll, first.FollowBottom)
	}
}
