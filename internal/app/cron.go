package app

// 会话定时任务（alk.cxykevin.top/cron）的交互接线：/cron 面板的打开、刷新与
// 删除。任务由 AI 的 cron 工具创建，客户端只提供查看与删除（见
// ../alkaid0/docs/acp/extension.md §3.11）。

import (
	"context"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
)

// openCron 打开 /cron 面板并拉取一次任务列表。服务端需声明 cron 能力；
// 没有活动会话时面板无意义（任务归属会话）。
func (a *App) openCron() {
	m := a.model
	if !m.SupportsCron() {
		m.ShowError(i18n.T("服务端未声明 cron 能力，/cron 不可用"))
		return
	}
	if !m.HasActive() {
		m.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	if !m.OpenCronModal() {
		return
	}
	// 任务列表不主动推送：打开面板时补一次全量查询，断线重连或刚进入会话
	// 也能立即看到任务。
	a.refreshCron()
}

// refreshCron 查询当前会话的定时任务（cron/list）。结果经 applyCommandResult
// 写入模型。
func (a *App) refreshCron() {
	if a.sess == nil || !a.model.SupportsCron() {
		return
	}
	control, ok := a.sess.(acp.CronControl)
	if !ok {
		return
	}
	session := a.sess
	a.startCronList(commandResult{kind: commandCronList, sessionID: session.ID()}, func(ctx context.Context) ([]acp.CronJob, error) {
		return control.ListCrons(ctx)
	})
}

// startCronList 与 startCommand 同构，但把 list 返回的任务列表随结果带回
// 事件循环（startCommand 只能回传 session/error）。
func (a *App) startCronList(result commandResult, fn func(context.Context) ([]acp.CronJob, error)) {
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Go(func() {
		jobs, err := fn(ctx)
		result.crons = jobs
		result.err = err
		select {
		case a.commands <- result:
		case <-ctx.Done():
		}
	})
}

// cronKey 处理 /cron 面板按键：↑↓ 选择、d/x 删除选中任务、r 刷新、Esc 关闭。
func (a *App) cronKey(ke input.KeyEvent) {
	m := a.model
	switch ke.Type {
	case input.KeyUp:
		m.CronMove(-1)
		return
	case input.KeyDown:
		m.CronMove(1)
		return
	case input.KeyEsc:
		m.CancelCronModal()
		return
	case input.KeyRune:
	default:
		return
	}
	if ke.IsCtrl() || ke.IsAlt() {
		return
	}
	switch ke.Rune {
	case 'd', 'x':
		a.deleteSelectedCron()
	case 'r':
		a.refreshCron()
	}
}

// deleteSelectedCron 删除面板当前选中的定时任务。本地先移除让面板立即响应；
// 服务端失败时由 applyCommandResult 重新拉取列表恢复。
func (a *App) deleteSelectedCron() {
	job, ok := a.model.CronSelected()
	if !ok || a.sess == nil {
		return
	}
	control, ok := a.sess.(acp.CronControl)
	if !ok {
		return
	}
	session := a.sess
	id := job.ID
	a.model.RemoveCron(id)
	a.startCommand(commandResult{kind: commandCronDelete, sessionID: session.ID(), cronID: id}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.DeleteCron(ctx, id)
	})
}
