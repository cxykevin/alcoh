package app

// AI 任务列表（/tasks）的交互接线：打开/关闭面板与滚动。
//
// 任务由 alkaid0 的 task 工具维护：AI 编辑虚拟对象 @task 后，服务端把任务
// markdown 解析成 ACP plan entries，经 plan_update 全量推送到会话的所有客户端
// （见 ../alkaid0/tools/tools/task）。模型把整份列表保存在 SessionState.Plan，
// 面板直接读本地快照，无需额外 RPC；会话运行中任务更新时面板也会实时变化。

import (
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
)

// openTasks 打开 /tasks 任务列表面板。任务列表按会话归属，没有活动会话时面板
// 无意义；列表为空时同样打开（显示空状态与说明）。
func (a *App) openTasks() {
	m := a.model
	if m.Active == nil {
		m.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	if !m.TasksAvailable() {
		m.ShowError(i18n.T("服务端未声明 alkaid0 扩展能力，/tasks 不可用"))
		return
	}
	m.OpenTasksModal()
}

// tasksKey 处理 /tasks 面板按键：↑↓ 逐行滚动、PgUp/PgDn 翻页、Home/End
// 跳到首尾、Esc 关闭。
func (a *App) tasksKey(ke input.KeyEvent) {
	m := a.model
	switch ke.Type {
	case input.KeyUp:
		m.TasksScrollBy(-1)
	case input.KeyDown:
		m.TasksScrollBy(1)
	case input.KeyPageUp:
		m.TasksScrollBy(-m.TasksPageRows())
	case input.KeyPageDown:
		m.TasksScrollBy(m.TasksPageRows())
	case input.KeyHome:
		m.TasksScrollTop()
	case input.KeyEnd:
		m.TasksScrollBottom()
	case input.KeyEsc:
		m.CancelTasksModal()
	}
}
