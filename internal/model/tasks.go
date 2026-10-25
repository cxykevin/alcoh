package model

// AI 任务列表（/tasks 面板）的纯状态逻辑。
//
// 任务来自 alkaid0 的 task 工具：AI 通过 edit 编辑虚拟对象 @task，服务端把任务
// markdown 解析成 ACP plan entries，经 plan_update 全量推送给客户端（见
// ../alkaid0/tools/tools/task）。模型把整份列表保存在 SessionState.Plan，本文件
// 只维护面板的滚动位置；面板只读，任务由 AI 维护。

import "github.com/cxykevin/alcoh/internal/acp"

// TasksAvailable 报告 /tasks 面板是否可用：需要 alkaid0 扩展能力与活动会话。
func (m *AppModel) TasksAvailable() bool {
	return m.Active != nil && m.SupportsAlkaid0()
}

// TaskEntries 返回当前会话的完整任务列表（按任务 markdown 顺序；嵌套层级已用
// 两个空格缩进编码在 content 里）。没有任务或没有活动会话时为空。
func (m *AppModel) TaskEntries() []acp.PlanEntry {
	if m.Active == nil || m.Active.Plan == nil {
		return nil
	}
	return m.Active.Plan.Entries
}

// OpenTasksModal 打开任务列表面板（列表为空时同样打开以显示空状态）。
// 返回是否成功打开。
func (m *AppModel) OpenTasksModal() bool {
	if !m.TasksAvailable() {
		return false
	}
	m.CloseSlash()
	m.TasksScroll = 0
	m.SetModal(ModalTasks)
	return true
}

// CancelTasksModal 关闭任务列表面板且不做任何修改。
func (m *AppModel) CancelTasksModal() {
	if m.Modal == ModalTasks {
		m.SetModal(NoModal)
	}
}

// TasksPageRows 返回一次翻页移动的条目数（至少 1）。视口行数由视图绘制时写入
// TasksRows，首帧之前回退为 1。
func (m *AppModel) TasksPageRows() int {
	if m.TasksRows < 1 {
		return 1
	}
	return m.TasksRows
}

// TasksScrollBy 按 delta 条滚动列表，并收敛到 [0, 最大偏移]。
func (m *AppModel) TasksScrollBy(delta int) {
	m.TasksScroll = m.clampTasksScroll(m.TasksScroll + delta)
}

// TasksScrollTop 滚动到列表顶部。
func (m *AppModel) TasksScrollTop() { m.TasksScroll = 0 }

// TasksScrollBottom 滚动到列表底部（最后一行贴窗口底部）。
func (m *AppModel) TasksScrollBottom() { m.TasksScroll = m.clampTasksScroll(len(m.TaskEntries())) }

// clampTasksScroll 把滚动偏移收敛到当前列表与视口允许的范围：AI 重写任务列表
// 后内容变短时把窗口拉回，避免停在空白区。
func (m *AppModel) clampTasksScroll(scroll int) int {
	if scroll < 0 {
		return 0
	}
	maxScroll := len(m.TaskEntries()) - m.TasksPageRows()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		return maxScroll
	}
	return scroll
}
