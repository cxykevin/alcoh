package view

import (
	"encoding/json"
	"fmt"
	"slices"
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

// TestShellListRowsAndTop 验证列表按行铺开与窗口起点：每条 shell 两行（reason/状态
// 与命令），历史段前插入一行分节标题；装得下时停在顶部，装不下时把选中项贴到窗口
// 底部（它的两行都要露出来）。
func TestShellListRowsAndTop(t *testing.T) {
	rows := shellListRows(4, 2)
	want := []shellListRow{
		{index: 0}, {index: 0, line: 1},
		{index: 1}, {index: 1, line: 1},
		{index: -1},
		{index: 2}, {index: 2, line: 1},
		{index: 3}, {index: 3, line: 1},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	// 没有历史段时不插分节行。
	if got := shellListRows(2, 2); len(got) != 4 {
		t.Fatalf("rows without history = %+v", got)
	}
	// 装得下：窗口停在顶部（列表照旧从第一条画起）。
	if got := shellListTop(rows, 3, 20); got != 0 {
		t.Fatalf("top with room = %d, want 0", got)
	}
	// 装不下：选中项落在窗口最后两行。
	if got := shellListTop(rows, 3, 4); got != 5 {
		t.Fatalf("top scrolled = %d, want 5", got)
	}
	// 选中项缺失时不滚动。
	if got := shellListTop(rows, 9, 4); got != 0 {
		t.Fatalf("top for unknown selection = %d, want 0", got)
	}
}

// TestShellPanelListScrollsToSelection 验证终端列表超出一屏时跟随选中项滚动：
// 选中的那条（含命令那行）始终完整可见，列表最右一列随之出现滚动条。
func TestShellPanelListScrollsToSelection(t *testing.T) {
	useEnglish(t)
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	for i := range 20 {
		m.Active.ApplyTerminalInfo(acp.TerminalInfo{
			TerminalID: fmt.Sprintf("@temp/run/%d", i),
			Reason:     fmt.Sprintf("task %d", i),
			Command:    fmt.Sprintf("cmd %d", i),
			Status:     "running",
		})
	}
	// 活动段"新的在上"：下标 19 是最早创建的 @temp/run/0，也就是最后一条。
	m.ShellSelected = 19
	b := renderer.NewBuffer(100, 20)
	p := &ShellPanel{Theme: theme}
	p.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, 100, 20), m)
	lines := bufferText(b)

	// 列表可视区为第 3~19 行（17 行）：窗口随选中项下移，选中项贴住底部。
	if !strings.Contains(lines[17], "> task 0") || !strings.Contains(lines[17], "running") {
		t.Fatalf("selected terminal line 1 = %q", lines[17])
	}
	if !strings.Contains(lines[18], "cmd 0") {
		t.Fatalf("selected terminal line 2 = %q", lines[18])
	}
	// 顶部的终端已被滚出窗口，且窗口起点落在整条上（不会只露半条）。
	if joined := strings.Join(lines[2:19], "\n"); strings.Contains(joined, "task 19") {
		t.Fatalf("scrolled-out terminal must not be visible:\n%s", joined)
	}
	// 滚动条贴在列表最右一列（列表宽 3/5 = 60，预览框从第 62 列起）；滚到底部时
	// 滑块落在最后几行。
	if got := b.Get(59, 18).R; got != '█' {
		t.Fatalf("scrollbar thumb at the bottom = %q, want '█'", got)
	}
	if got := b.Get(59, 2).R; got != '│' {
		t.Fatalf("scrollbar track at the top = %q, want '│'", got)
	}
	// 滚回第一条：窗口回到顶部，滚出窗口的终端重新出现。
	m.ShellSelected = 0
	p.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, 100, 20), m)
	if !strings.Contains(bufferText(b)[2], "> task 19") {
		t.Fatalf("scrolling back must show the top of the list: %q", bufferText(b)[2])
	}
}

// TestShellPanelListNoScrollbarWhenFits 验证列表装得下一屏时不画滚动条：
// 右侧不多出一条线，内容照旧从第一行画起。
func TestShellPanelListNoScrollbarWhenFits(t *testing.T) {
	useEnglish(t)
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	for i := range 3 {
		m.Active.ApplyTerminalInfo(acp.TerminalInfo{
			TerminalID: fmt.Sprintf("@temp/run/%d", i),
			Reason:     fmt.Sprintf("task %d", i),
			Command:    fmt.Sprintf("cmd %d", i),
			Status:     "running",
		})
	}
	m.ShellSelected = 1
	b := renderer.NewBuffer(100, 20)
	p := &ShellPanel{Theme: theme}
	p.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, 100, 20), m)

	lines := bufferText(b)
	// 列表从第一条画起：最新的 @temp/run/2 在最上面，选中的 @temp/run/1 紧随其后。
	if !strings.Contains(lines[2], "task 2") || !strings.Contains(lines[4], "> task 1") {
		t.Fatalf("list must start at the top:\n%s", strings.Join(lines[2:8], "\n"))
	}
	for y := 2; y < 19; y++ {
		if got := b.Get(59, y).R; got != ' ' {
			t.Fatalf("no scrollbar expected at (59,%d): %q", y, got)
		}
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
