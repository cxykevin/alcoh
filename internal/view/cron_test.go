package view

// /cron 定时任务面板的排版测试：高度必须容纳每条任务的三行内容，列表展示
// id/表达式/类型、提示词与下次触发时间，底部保留操作提示。

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

func TestCronContentFitsItems(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.Crons = []acp.CronJob{
		{ID: 3, Cron: "0 9 * * *", Prompt: "总结昨天的提交", NextRuns: []string{"2026-10-25 09:00:00 GMT+8"}},
		{ID: 7, Cron: "*/30 * * * *", Prompt: "检查服务状态", RunOnce: true, NextRuns: []string{"2026-10-24 12:30:00 GMT+8"}},
	}
	m.CronSelect = 1
	m.SetModal(model.ModalCron)

	modalH := cronModalHeight(m)
	if modalH != 3*2+5 {
		t.Fatalf("cronModalHeight = %d, want 11", modalH)
	}
	const w = 64
	contentH := modalH - 1 // DrawSheet 的标题行占 1 行
	b := renderer.NewBuffer(w, contentH)
	cronContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))

	if hint := effortRowText(b, 0, w); !strings.Contains(hint, "定时任务 2 条") {
		t.Errorf("hint row = %q", hint)
	}
	first := effortRowText(b, 2, w)
	if !strings.HasPrefix(first, "  #3  0 9 * * *") || !strings.Contains(first, "周期") {
		t.Errorf("first title row = %q", first)
	}
	if prompt := effortRowText(b, 3, w); !strings.Contains(prompt, "总结昨天的提交") {
		t.Errorf("first prompt row = %q", prompt)
	}
	if next := effortRowText(b, 4, w); !strings.Contains(next, "2026-10-25 09:00:00") {
		t.Errorf("first next row = %q", next)
	}
	second := effortRowText(b, 5, w)
	if !strings.HasPrefix(second, "❯ #7  */30 * * * *") || !strings.Contains(second, "一次") {
		t.Errorf("second title row = %q", second)
	}
	if prompt := effortRowText(b, 6, w); !strings.Contains(prompt, "检查服务状态") {
		t.Errorf("second prompt row = %q", prompt)
	}
	if next := effortRowText(b, 7, w); !strings.Contains(next, "2026-10-24 12:30:00") {
		t.Errorf("second next row = %q", next)
	}
	if footer := effortRowText(b, contentH-1, w); !strings.Contains(footer, "取消定时任务") {
		t.Errorf("footer row = %q", footer)
	}
}

// TestCronContentEmptyState 验证没有任务时面板只占一行空状态，并显示空提示。
func TestCronContentEmptyState(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.SetModal(model.ModalCron)

	modalH := cronModalHeight(m)
	if modalH != 6 {
		t.Fatalf("empty cronModalHeight = %d, want 6", modalH)
	}
	const w = 48
	contentH := modalH - 1 // DrawSheet 的标题行占 1 行
	b := renderer.NewBuffer(w, contentH)
	cronContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))
	if row := effortRowText(b, 2, w); !strings.Contains(row, "（暂无定时任务）") {
		t.Errorf("empty state row = %q", row)
	}
}

// TestCronContentMissingNextRun 验证服务端未给出下次触发时间时的兜底文案。
func TestCronContentMissingNextRun(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.Crons = []acp.CronJob{{ID: 1, Cron: "not a cron", Prompt: "x"}}
	m.SetModal(model.ModalCron)

	const w = 64
	contentH := cronModalHeight(m) - 1 // DrawSheet 的标题行占 1 行
	b := renderer.NewBuffer(w, contentH)
	cronContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))
	if row := effortRowText(b, 4, w); !strings.Contains(row, "无法计算下次触发时间") {
		t.Errorf("next row = %q, want fallback text", row)
	}
}

// TestCronContentScrollsManyJobs 验证任务很多时只显示固定窗口（最多 4 条），
// 且窗口随选中项滚动：选中的任务始终在可视范围内。
func TestCronContentScrollsManyJobs(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := model.New()
	m.ActivateSession("s1", "会话")
	for i := 1; i <= 10; i++ {
		m.Active.Crons = append(m.Active.Crons, acp.CronJob{
			ID:     uint32(i),
			Cron:   "0 9 * * *",
			Prompt: "job",
		})
	}
	m.SetModal(model.ModalCron)

	const w = 64
	contentH := cronModalHeight(m) - 1 // DrawSheet 的标题行占 1 行
	titleRows := []int{2, 5, 8, 11}    // 提示 + 空行之后，每条任务 3 行

	draw := func(selected int) *renderer.Buffer {
		m.CronSelect = selected
		b := renderer.NewBuffer(w, contentH)
		cronContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))
		return b
	}
	titles := func(b *renderer.Buffer) []string {
		out := make([]string, 0, len(titleRows))
		for _, y := range titleRows {
			out = append(out, effortRowText(b, y, w))
		}
		return out
	}

	// 选中第一条：窗口从顶部开始。
	if got := titles(draw(0)); !strings.HasPrefix(got[0], "❯ #1") ||
		!strings.Contains(got[3], "#4") || strings.Contains(got[3], "#5") {
		t.Fatalf("selected 0 rows = %q", got)
	}
	// 选中中间一条：窗口以选中项为中心滚动，选中项保持可见。
	got := titles(draw(5))
	if !strings.Contains(got[0], "#4") || !strings.Contains(got[2], "❯ #6") ||
		!strings.Contains(got[3], "#7") {
		t.Fatalf("selected 5 rows = %q", got)
	}
	// 选中最后一条：窗口贴底，最后一条在可视范围内。
	got = titles(draw(9))
	if !strings.Contains(got[0], "#7") || !strings.HasPrefix(got[3], "❯ #10") {
		t.Fatalf("selected 9 rows = %q", got)
	}
}
