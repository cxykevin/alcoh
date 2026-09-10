package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

func TestStatusBarStates(t *testing.T) {
	theme := renderer.DefaultTheme()
	cases := []struct {
		state acp.SessionState
		want  string
	}{
		{acp.StateIdle, "● idle"},
		{acp.StateRequiresAction, "? action"},
		{acp.StateRunning, "coding"},
	}
	for _, c := range cases {
		s := model.NewSession("s1", "会话1")
		s.State = c.state
		m := &model.AppModel{}
		m.Active = s
		b := renderer.NewBuffer(40, 1)
		canv := renderer.NewCanvas(b)
		sb := &StatusBar{Theme: theme, SpinFrame: 0}
		sb.Draw(canv, renderer.NewRect(0, 0, 40, 1), m)
		var txt []rune
		for x := range 40 {
			cell := b.Get(x, 0)
			if cell.Width == 0 {
				continue
			}
			txt = append(txt, cell.R)
		}
		got := strings.TrimRight(string(txt), " ")
		t.Logf("state=%v => %q", c.state, got)
		if !strings.Contains(got, c.want) {
			t.Errorf("state=%v: expected text containing %q, got %q", c.state, c.want, got)
		}
	}
}

// TestStatusBarModelShowsConfigFallback 验证状态栏 model 显示：
// 无 session-info model 字段时回退到 agent 公布的 model config 当前值。
func TestStatusBarModelShowsConfigFallback(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.ActivateSession("s1", "")
	// 仅 model config（category="model"），无 session-info model。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options: []acp.ConfigOption{
			{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "0/foo",
				Options: []acp.ConfigOptionValue{{Value: "0/foo", Name: "foo"}, {Value: "1/bar", Name: "bar"}}},
		},
	})

	b := renderer.NewBuffer(50, 1)
	sb := &StatusBar{Theme: theme, SpinFrame: 0}
	sb.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, 50, 1), m)
	var txt []rune
	for x := range 50 {
		cell := b.Get(x, 0)
		if cell.Width == 0 {
			continue
		}
		txt = append(txt, cell.R)
	}
	got := strings.TrimRight(string(txt), " ")
	if !strings.Contains(got, "model foo") {
		t.Errorf("status bar = %q, want containing 'model foo' (config fallback)", got)
	}
	if strings.Contains(got, "model —") {
		t.Errorf("status bar = %q, should not show placeholder 'model —' when config present", got)
	}
}

// TestRunningStateSweepsLeftToRight 验证运行状态动词使用蓝色主题，且高光从左向右扫过。
func TestRunningStateSweepsLeftToRight(t *testing.T) {
	theme := renderer.DefaultTheme()
	baseFg := theme.Style(theme.ToolRunning).Fg
	white := renderer.RGB(0xFF, 0xFF, 0xFF)

	// 固定短单词，便于断言高光前缘在连续帧中的位置。
	orig := runningWords
	runningWords = []string{"abc"}
	defer func() { runningWords = orig }()

	// 高光前缘（纯白加粗字符）应随帧号从左向右移动：帧 0/1/2 → 字符 0/1/2。
	for frame, want := range []int{0, 1, 2} {
		spans := runningState("", frame, theme)
		got := -1
		for i := 1; i < len(spans); i++ { // spans[0] 是 "spin "
			if spans[i].Style.Fg == white && spans[i].Style.Bold {
				got = i - 1
			}
		}
		if got != want {
			t.Fatalf("frame=%d: highlight head at rune %d, want %d", frame, got, want)
		}
	}

	// 帧 0 时，非高亮字符使用主题蓝基色（验证蓝色主题渲染）。
	spans := runningState("", 0, theme)
	if spans[2].Style.Fg != baseFg || spans[2].Style.Bold {
		t.Fatalf("frame=0: rune 1 should be base blue non-bold, got style %+v", spans[2].Style)
	}
}
