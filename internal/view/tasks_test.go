package view

// /tasks 面板的排版测试：完整列表逐行展示，状态着色（完成绿、进行中蓝、
// 未完成紫、取消红），超过一屏时按滚动偏移取窗口并绘制滚动条。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// tasksTestModel 构造带任务列表且已打开 /tasks 面板的模型。
func tasksTestModel(entries []acp.PlanEntry) *model.AppModel {
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.Plan = &acp.Plan{Type: "items", PlanID: "task", Entries: entries}
	m.SetModal(model.ModalTasks)
	return m
}

// TestTasksContentDraw 验证摘要、四类状态的行符号与颜色，以及底部提示。
func TestTasksContentDraw(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := tasksTestModel([]acp.PlanEntry{
		{Content: "已完成任务", Status: acp.PlanCompleted},
		{Content: "  子任务进行中", Status: acp.PlanInProgress},
		{Content: "未开始任务", Status: acp.PlanPending},
		{Content: "已取消任务", Status: acp.PlanCancelled},
	})

	modalH := tasksModalHeight(m)
	if modalH != 4+5 {
		t.Fatalf("tasksModalHeight = %d, want 9", modalH)
	}
	const w = 70
	contentH := modalH - 1 // DrawSheet 的标题行占 1 行
	b := renderer.NewBuffer(w, contentH)
	tasksContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))

	if summary := effortRowText(b, 0, w); !strings.Contains(summary, "任务 1/4 完成") || !strings.Contains(summary, "1 进行中") {
		t.Errorf("summary row = %q", summary)
	}
	if m.TasksRows != 5 {
		t.Errorf("TasksRows = %d, want 5 (content rows minus header/blank/footer)", m.TasksRows)
	}

	cases := []struct {
		row  int
		sym  rune
		fg   renderer.Color
		text string
	}{
		{2, '✓', theme.ToolDone, "已完成任务"},
		{3, '●', theme.ToolRunning, "  子任务进行中"},
		{4, '○', theme.Secondary, "未开始任务"}, // 未完成：紫色
		{5, '✗', theme.ToolFailed, "已取消任务"},
	}
	for _, c := range cases {
		cell := b.Get(1, c.row) // 行格式 " ✓ 任务名称"，符号在第 1 列
		if cell.R != c.sym {
			t.Errorf("row %d symbol = %q, want %q", c.row, cell.R, c.sym)
		}
		if cell.Style.Fg != c.fg {
			t.Errorf("row %d color = %v, want %v", c.row, cell.Style.Fg, c.fg)
		}
		if text := effortRowText(b, c.row, w); !strings.Contains(text, c.text) {
			t.Errorf("row %d text = %q, want contains %q", c.row, text, c.text)
		}
	}
	if footer := effortRowText(b, contentH-1, w); !strings.Contains(footer, "PgUp/PgDn") || !strings.Contains(footer, "Esc 关闭") {
		t.Errorf("footer row = %q", footer)
	}
}

// TestTasksContentScrollWindow 验证超过一屏时按 Scroll 取窗口，并在最右列
// 绘制滚动条（任务行让出一列）。
func TestTasksContentScrollWindow(t *testing.T) {
	theme := renderer.DefaultTheme()
	entries := make([]acp.PlanEntry, 30)
	for i := range entries {
		entries[i] = acp.PlanEntry{Content: fmt.Sprintf("任务 %02d", i), Status: acp.PlanPending}
	}
	m := tasksTestModel(entries)
	m.TasksScroll = 10

	modalH := tasksModalHeight(m)
	if modalH != tasksMaxRows+5 {
		t.Fatalf("tasksModalHeight = %d, want %d", modalH, tasksMaxRows+5)
	}
	const w = 32
	contentH := modalH - 1
	b := renderer.NewBuffer(w, contentH)
	tasksContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))

	if m.TasksRows != 15 {
		t.Fatalf("TasksRows = %d, want 15 (content rows minus 3)", m.TasksRows)
	}
	if first := effortRowText(b, 2, w); !strings.Contains(first, "任务 10") {
		t.Errorf("first visible row = %q, want 任务 10", first)
	}
	// 滚动条独占最右一列（此处窗口顶部在 10/15 处，滑块在下方）。
	if cell := b.Get(w-1, 2); cell.R != '│' && cell.R != '█' {
		t.Errorf("scrollbar cell = %q, want track or thumb", cell.R)
	}
	if last := effortRowText(b, contentH-2, w); !strings.Contains(last, "任务 24") {
		t.Errorf("last visible row = %q, want 任务 24", last)
	}
}

// TestTasksContentEmptyState 验证没有任务时显示空状态与说明，高度只占一行。
func TestTasksContentEmptyState(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := tasksTestModel(nil)

	modalH := tasksModalHeight(m)
	if modalH != 6 {
		t.Fatalf("empty tasksModalHeight = %d, want 6", modalH)
	}
	const w = 60
	contentH := modalH - 1
	b := renderer.NewBuffer(w, contentH)
	tasksContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))

	if row := effortRowText(b, 0, w); !strings.Contains(row, "本会话还没有 AI 任务列表") {
		t.Errorf("empty title row = %q", row)
	}
	if row := effortRowText(b, 2, w); !strings.Contains(row, "task 工具") {
		t.Errorf("empty hint row = %q", row)
	}
	if footer := effortRowText(b, contentH-1, w); !strings.Contains(footer, "Esc 关闭") {
		t.Errorf("footer row = %q", footer)
	}
}
