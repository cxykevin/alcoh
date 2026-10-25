package model

// /tasks 面板的纯状态测试：能力门控、命令面板接线，以及滚动窗口在列表
// 变长/变短时的收敛。

import (
	"slices"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// tasksPlan 构造 n 条任务（状态按 完成/进行中/未开始 循环）。
func tasksPlan(n int) *acp.Plan {
	statuses := []acp.PlanEntryStatus{acp.PlanCompleted, acp.PlanInProgress, acp.PlanPending}
	entries := make([]acp.PlanEntry, 0, n)
	for i := range n {
		entries = append(entries, acp.PlanEntry{Content: "任务", Status: statuses[i%len(statuses)]})
	}
	return &acp.Plan{Type: "items", PlanID: "task", Entries: entries}
}

func newTasksModel() *AppModel {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{Name: "alkaid0", Version: "test"}, alkaid0Caps())
	m.ActivateSession("s1", "会话")
	return m
}

// TestTasksAvailability 验证 /tasks 需要 alkaid0 能力与活动会话同时具备，
// 且命令只在两者都满足时进入命令面板。
func TestTasksAvailability(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "会话")
	if m.TasksAvailable() {
		t.Fatal("tasks must be unavailable without the alkaid0 capability")
	}
	if slices.Contains(m.SlashCommands(), "/tasks") {
		t.Fatal("/tasks should not appear without the alkaid0 capability")
	}
	if m.OpenTasksModal() {
		t.Fatal("OpenTasksModal should fail without the alkaid0 capability")
	}
	m.SetAgentInfo(acp.AgentInfo{Name: "alkaid0", Version: "test"}, alkaid0Caps())
	if !m.TasksAvailable() {
		t.Fatal("tasks should be available with alkaid0 capability and active session")
	}
	if !slices.Contains(m.SlashCommands(), "/tasks") {
		t.Fatal("/tasks should appear in the command palette")
	}
}

// TestTasksModalScroll 验证面板打开时回到顶部、上下/翻页/首尾滚动的
// 边界收敛，以及任务列表变短后窗口自动拉回。
func TestTasksModalScroll(t *testing.T) {
	m := newTasksModel()
	m.Active.Plan = tasksPlan(20)
	m.TasksRows = 5

	if !m.OpenTasksModal() {
		t.Fatal("OpenTasksModal should succeed")
	}
	if m.Modal != ModalTasks || m.TasksScroll != 0 {
		t.Fatalf("modal = %v scroll = %d, want ModalTasks/0", m.Modal, m.TasksScroll)
	}
	if got := m.TasksPageRows(); got != 5 {
		t.Fatalf("TasksPageRows = %d, want 5", got)
	}

	m.TasksScrollBy(3)
	if m.TasksScroll != 3 {
		t.Errorf("scroll after +3 = %d, want 3", m.TasksScroll)
	}
	m.TasksScrollBy(-10)
	if m.TasksScroll != 0 {
		t.Errorf("scroll after -10 = %d, want 0", m.TasksScroll)
	}
	m.TasksScrollBottom()
	if m.TasksScroll != 15 {
		t.Errorf("scroll to bottom = %d, want 15 (20-5)", m.TasksScroll)
	}
	m.TasksScrollBy(10)
	if m.TasksScroll != 15 {
		t.Errorf("scroll past bottom = %d, want 15", m.TasksScroll)
	}
	m.TasksScrollTop()
	if m.TasksScroll != 0 {
		t.Errorf("scroll to top = %d, want 0", m.TasksScroll)
	}

	// 任务列表变短：最大偏移随之收敛，窗口不会停在空白区。
	m.TasksScrollBottom()
	m.Active.Plan = tasksPlan(6)
	m.TasksScrollBy(10)
	if m.TasksScroll != 1 {
		t.Errorf("scroll after list shrank = %d, want 1 (6-5)", m.TasksScroll)
	}
	m.TasksScrollBottom()
	if m.TasksScroll != 1 {
		t.Errorf("bottom after list shrank = %d, want 1", m.TasksScroll)
	}

	// 内容不足一屏时不能滚动。
	m.Active.Plan = tasksPlan(3)
	m.TasksScrollBy(-1)
	if m.TasksScroll != 0 {
		t.Errorf("scroll on short list = %d, want 0", m.TasksScroll)
	}

	m.CancelTasksModal()
	if m.Modal != NoModal {
		t.Fatal("CancelTasksModal should close the modal")
	}
}

// TestTasksPageRowsFallback 验证视图尚未写入视口行数时翻页步长回退为 1。
func TestTasksPageRowsFallback(t *testing.T) {
	m := New()
	if got := m.TasksPageRows(); got != 1 {
		t.Fatalf("TasksPageRows without viewport = %d, want 1", got)
	}
	m.TasksRows = 7
	if got := m.TasksPageRows(); got != 7 {
		t.Fatalf("TasksPageRows = %d, want 7", got)
	}
}
