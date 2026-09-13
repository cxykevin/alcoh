package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/term"
)

// TestVTScreenUsedRows 验证 VT 屏幕的内容行数统计（滚动条与滚动范围的基础）。
func TestVTScreenUsedRows(t *testing.T) {
	scr := term.NewVTScreen(40, 10)
	if got := scr.UsedRows(); got != 1 {
		t.Fatalf("empty screen rows = %d, want 1", got)
	}
	scr.Feed(strings.Repeat("line\n", 100))
	if got := scr.UsedRows(); got < 100 {
		t.Fatalf("used rows = %d, want >= 100", got)
	}
	if scr.Scrolled == 0 {
		t.Fatal("screen must report scrolled lines")
	}
}

// TestShellPreviewScrollableRange 验证长内容下预览可滚动：ScreenRows 大于视口、
// 滚动条出现滑块、PgUp 后窗口上移且不越界。
func TestShellPreviewScrollableRange(t *testing.T) {
	useEnglish(t)
	const w, h = 120, 20
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s1", "s")
	m.ShellPanel = true
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString(fmt.Sprintf("row-%03d-", i))
		sb.WriteString(strings.Repeat("z", 20))
		sb.WriteString("\n")
	}
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/1", Command: "make", Status: "running", Content: sb.String()})
	m.ShellSelected = 0

	render := func() []string {
		b := renderer.NewBuffer(w, h)
		p := &ShellPanel{Theme: renderer.DefaultTheme()}
		p.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)
		return bufferText(b)
	}
	before := render()
	s := m.SelectedShell()
	t.Logf("screenRows=%d screenH=%d view=%d", s.ScreenRows, s.Screen.Height, m.ShellPreviewHeight())
	if s.ScreenRows <= m.ShellPreviewHeight() {
		t.Fatalf("content must exceed the viewport: rows=%d view=%d", s.ScreenRows, m.ShellPreviewHeight())
	}
	m.ScrollShellPreview(m.ShellPreviewHeight())
	after := render()
	if before[2] == after[2] {
		t.Fatalf("PgUp must change the visible window (row=%q)", before[2])
	}
	if s.Scroll == 0 {
		t.Fatal("scroll offset must be non-zero after PgUp")
	}
	// 继续翻到底也不能出现空白（窗口不会越过屏幕保留范围）。
	for i := 0; i < 50; i++ {
		m.ScrollShellPreview(m.ShellPreviewHeight())
		render()
	}
	top := render()
	if strings.TrimSpace(top[2]) == "" && strings.TrimSpace(top[len(top)-3]) == "" {
		t.Fatalf("scrolling too far shows blank window:\n%s", strings.Join(top, "\n"))
	}
}
