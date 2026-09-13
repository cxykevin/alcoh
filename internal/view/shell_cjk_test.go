package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestShellPreviewKeepsCJKAtWrapBoundary 回归：CJK 按 2 列计算换行，
// 行末刚好放不下时整字换行，不会出现单字消失；任何一行都不超过框宽。
func TestShellPreviewKeepsCJKAtWrapBoundary(t *testing.T) {
	useEnglish(t)
	const w, h = 120, 20
	text := strings.Repeat("中文测试", 30)
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/1", Command: "echo", Status: "running", Content: text})
	m.ShellSelected = 0

	b := renderer.NewBuffer(w, h)
	p := &ShellPanel{Theme: renderer.DefaultTheme()}
	p.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)

	left := w * 3 / 5
	innerX := left + 2
	innerW := w - 1 - innerX - 1 // 末尾一列是滚动条
	var sb strings.Builder
	for y := 2; y < h-2; y++ {
		line := make([]rune, 0, innerW)
		width := 0
		for x := innerX; x < innerX+innerW; x++ {
			cell := b.Get(x, y)
			if cell.Width == 0 {
				continue
			}
			if cell.R != ' ' {
				width += renderer.RuneWidth(cell.R)
			}
			line = append(line, cell.R)
		}
		if width > innerW {
			t.Fatalf("row %d overflows the preview box: %d columns > %d", y, width, innerW)
		}
		sb.WriteString(strings.TrimRight(string(line), " "))
	}
	got := sb.String()
	if !strings.HasPrefix(text, got) {
		t.Fatalf("preview text is not a prefix of the content (chars lost/reordered):\n got=%q", got)
	}
	// 首屏应能完整展示前面若干行（内容远长于一屏），且不出现省略号。
	if strings.Contains(got, "…") {
		t.Fatalf("preview must wrap, not truncate: %q", got)
	}
	if len([]rune(got))%2 != 0 {
		t.Fatalf("CJK characters must not be split: %q", got)
	}
}
