package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// bufferText 把渲染缓冲按行拼成文本（跳过宽字符续列）。
func bufferText(b *renderer.Buffer) []string {
	out := make([]string, b.H)
	for y := range b.H {
		var sb strings.Builder
		for x := range b.W {
			cell := b.Get(x, y)
			if cell.Width == 0 {
				continue
			}
			sb.WriteRune(cell.R)
		}
		out[y] = strings.TrimRight(sb.String(), " ")
	}
	return out
}

// useEnglish 固定界面语言为英文，便于断言文案（测试结束恢复）。
func useEnglish(t *testing.T) {
	t.Helper()
	old := i18n.Current()
	i18n.SetLang(i18n.En)
	t.Cleanup(func() { i18n.SetLang(old) })
}

// TestShellPanelListsActiveThenHistory 验证 shells 面板：活动 shell 在上、
// 历史 shell 在下方独立分段；每条 shell 占两行（reason + 右对齐状态 / 命令），
// 两段内部都是"新的在上"。
func TestShellPanelListsActiveThenHistory(t *testing.T) {
	useEnglish(t)
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{},\"alk.cxykevin.top/alkaid0/v0.6\":{}}")})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/2", Reason: "serve the app", Command: "npm run dev", Status: "running", Content: "listening on :8080\n"})
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/1", Reason: "run the tests", Command: "go test ./...", Status: "running", Content: "ok\n"})
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: "@temp/run/0", Reason: "build the project", Command: "make build", Status: "running", Content: "old\n"})
	m.Active.ArchiveTerminal("@temp/run/0")
	m.Active.ArchiveTerminal("@temp/run/1")

	// 选中历史项：列表顺序为 [活跃..., 历史最新在前]，下标 1 即最近结束的历史项。
	m.ShellSelected = 1
	b := renderer.NewBuffer(100, 20)
	canv := renderer.NewCanvas(b)
	p := &ShellPanel{Theme: theme}
	p.Draw(canv, renderer.NewRect(0, 0, 100, 20), m)
	lines := bufferText(b)

	// 标题里的计数只统计活动 shell，历史数量单独标注。
	if !strings.Contains(lines[0], "shells") || !strings.Contains(lines[0], "1  · history (2)") {
		t.Fatalf("header = %q", lines[0])
	}
	// 活动项：第一行 reason + 右对齐状态，第二行命令。
	if !strings.Contains(lines[2], "serve the app") || !strings.Contains(lines[2], "running") {
		t.Fatalf("active row line 1 = %q", lines[2])
	}
	// 状态右对齐：列表列（左侧 60 列）去掉右侧空白后以状态结尾。
	leftColumn := strings.TrimRight(lines[2][:60], " ")
	if !strings.HasSuffix(leftColumn, "running") {
		t.Fatalf("status must be right aligned in the list column: %q", leftColumn)
	}
	if !strings.Contains(lines[3], "npm run dev") {
		t.Fatalf("active row line 2 = %q", lines[3])
	}
	if !strings.Contains(lines[4], "history (2)") {
		t.Fatalf("history separator = %q", lines[4])
	}
	// 最近结束的历史项排在最上面，同样两行。
	if !strings.Contains(lines[5], "> run the tests") || !strings.Contains(lines[5], "finished") {
		t.Fatalf("newest history row line 1 = %q", lines[5])
	}
	if !strings.Contains(lines[6], "go test ./...") {
		t.Fatalf("newest history row line 2 = %q", lines[6])
	}
	if !strings.Contains(lines[7], "build the project") {
		t.Fatalf("older history row line 1 = %q", lines[7])
	}
	// 预览区显示历史 shell 的内容（terminal: 标题 + 输出）。
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "terminal: go test ./...") {
		t.Fatalf("preview title missing:\n%s", joined)
	}
	if !strings.Contains(joined, "ok") {
		t.Fatalf("preview content missing:\n%s", joined)
	}
}

// TestShellPanelStatusColors 验证状态文案按状态取色：运行中、已完成、已终止各自不同。
func TestShellPanelStatusColors(t *testing.T) {
	useEnglish(t)
	theme := renderer.DefaultTheme()
	cases := []struct {
		status string
		want   string
		color  renderer.Color
	}{
		{"running", "running", theme.ToolRunning},
		{"finished", "finished", theme.ToolDone},
		{"killed", "killed", theme.Warning},
	}
	for _, c := range cases {
		s := &model.TerminalState{ID: "@temp/run/1", Status: c.status}
		if got := shellStatusLabel(s); got != c.want {
			t.Fatalf("status %q label = %q, want %q", c.status, got, c.want)
		}
		if got := shellStatusColor(theme, s); got != c.color {
			t.Fatalf("status %q color = %v, want %v", c.status, got, c.color)
		}
	}
}
