package app

// 会话消息队列（alk.cxykevin.top/msgqueue）的交互接线：/queue 面板、排队项的
// 取消/清空/编辑，以及运行中提交直接入队。

import (
	"context"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/widget"
)

// openMsgQueue 打开 /queue 面板并拉取一次队列快照。服务端需同时声明 alkaid0
// v0.6 与 msgqueue；没有活动会话时队列面板无意义（队列属于会话）。
func (a *App) openMsgQueue() {
	m := a.model
	if !m.SupportsMsgQueue() {
		m.ShowError(i18n.T("服务端未声明 msgqueue 能力，/queue 不可用"))
		return
	}
	if !m.HasActive() {
		m.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	if !m.OpenQueueModal() {
		return
	}
	// 平时靠服务端推送 msgqueue_update 维护快照；打开面板时补一次全量查询，
	// 断线重连或刚进入会话也能立即看到队列内容。
	a.refreshMsgQueue()
}

// refreshMsgQueue 查询当前会话的完整队列快照（msgqueue/list）。结果经
// applyCommandResult 写入模型；revision 较旧的快照会被模型丢弃。
func (a *App) refreshMsgQueue() {
	if a.sess == nil || !a.model.SupportsMsgQueue() {
		return
	}
	control, ok := a.sess.(acp.MsgQueueControl)
	if !ok {
		return
	}
	session := a.sess
	a.startMsgQueueList(commandResult{kind: commandMsgQueueList, sessionID: session.ID()}, func(ctx context.Context) (acp.MsgQueueSnapshot, error) {
		return control.ListMsgQueue(ctx)
	})
}

// startMsgQueueList 与 startCommand 同构，但把 list 返回的全量快照随结果带回
// 事件循环（startCommand 只能回传 session/error）。
func (a *App) startMsgQueueList(result commandResult, fn func(context.Context) (acp.MsgQueueSnapshot, error)) {
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Go(func() {
		snap, err := fn(ctx)
		result.queue = &snap
		result.err = err
		select {
		case a.commands <- result:
		case <-ctx.Done():
		}
	})
}

// queueKey 处理 /queue 面板按键：↑↓ 选择、Enter/e 编辑、d/x 取消选中项、
// c 清空、r 刷新、Esc 关闭。
func (a *App) queueKey(ke input.KeyEvent) {
	m := a.model
	switch ke.Type {
	case input.KeyUp:
		m.QueueMove(-1)
		return
	case input.KeyDown:
		m.QueueMove(1)
		return
	case input.KeyEnter:
		a.startQueueEdit()
		return
	case input.KeyEsc:
		m.CancelQueueModal()
		return
	case input.KeyRune:
	default:
		return
	}
	if ke.IsCtrl() || ke.IsAlt() {
		return
	}
	switch ke.Rune {
	case 'e':
		a.startQueueEdit()
	case 'd', 'x':
		a.cancelSelectedQueueItem()
	case 'c':
		a.clearMsgQueue()
	case 'r':
		a.refreshMsgQueue()
	}
}

// startQueueEdit 把选中排队项的内容填回输入框并进入编辑状态：面板关闭，
// 回车提交时走 msgqueue/update（见 submitQueueEdit），而不是发送新 prompt。
func (a *App) startQueueEdit() {
	m := a.model
	item, ok := m.QueueSelected()
	if !ok {
		return
	}
	if !m.HasActive() {
		return
	}
	text := item.Text
	if item.Kind == model.MsgQueueKindCommand {
		// 命令项编辑的是参数：服务端据 command + args 重建 text。参数为空时
		// 输入框也为空，避免把完整的 command 文本再当作参数提交一次。
		text = item.Args
	}
	a.queueEdit = &msgQueueEdit{
		SessionID: m.Active.ID,
		ItemID:    item.ID,
		Kind:      item.Kind,
		Command:   item.Command,
	}
	m.CancelQueueModal()
	m.BlurQueue()
	a.setInputText(text)
	m.UpdateSlashState()
	m.ShowInfo(i18n.T("编辑排队消息：Enter 保存，Esc 取消"))
}

// setInputText 用 text 替换输入框内容（光标停在末尾）。排队项编辑/删除都会
// 用它回填，其中删除是无条件覆盖：原有草稿会被排队项内容顶掉。
func (a *App) setInputText(text string) {
	lines := widget.SplitLines(text)
	a.model.Input.Lines = lines
	a.model.Input.CY = len(lines) - 1
	a.model.Input.CX = len(lines[a.model.Input.CY])
	a.model.Input.HistPos = -1
}

// deleteFocusedQueueItem 删除排队框当前选中的排队项，并把它的内容放回输入框
// （无条件覆盖现有草稿）。本地先移除该项让界面立即响应；发送失败时由
// applyCommandResult 重新拉取队列恢复。
func (a *App) deleteFocusedQueueItem() {
	item, ok := a.model.QueueSelected()
	if !ok || a.sess == nil {
		return
	}
	control, ok := a.sess.(acp.MsgQueueControl)
	if !ok {
		return
	}
	text := model.QueueItemInputText(item)
	a.setInputText(text)
	a.model.RemoveQueueItem(item.ID)
	a.model.BlurQueue()
	a.model.UpdateSlashState()
	a.model.ShowInfo(i18n.T("已删除排队消息，内容已放回输入框"))
	session := a.sess
	id := item.ID
	a.startCommand(commandResult{kind: commandMsgQueueCancel, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.CancelMsgQueueItem(ctx, id)
	})
}

// submitQueueEdit 提交正在编辑的排队项：prompt 改 text，command 改 args。
// 会话已切换或编辑目标已不在队列里时放弃提交。
func (a *App) submitQueueEdit() {
	ed := a.queueEdit
	if ed == nil {
		return
	}
	m := a.model
	text := m.Input.Text()
	if strings.TrimSpace(text) == "" {
		m.ShowError(i18n.T("排队消息内容不能为空"))
		return
	}
	a.queueEdit = nil
	m.Input.Clear()
	m.UpdateSlashState()
	if a.sess == nil || a.sess.ID() != ed.SessionID {
		m.ShowInfo(i18n.T("会话已切换，已放弃编辑排队消息"))
		return
	}
	control, ok := a.sess.(acp.MsgQueueControl)
	if !ok {
		return
	}
	session := a.sess
	itemID := ed.ItemID
	if ed.Kind == model.MsgQueueKindCommand {
		args := text
		a.startCommand(commandResult{kind: commandMsgQueueUpdate, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
			return nil, control.UpdateMsgQueueItem(ctx, itemID, nil, &args)
		})
		return
	}
	a.startCommand(commandResult{kind: commandMsgQueueUpdate, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.UpdateMsgQueueItem(ctx, itemID, &text, nil)
	})
}

// cancelQueueEdit 放弃正在进行的排队项编辑并清空输入框（内容属于排队项，
// 不当作新 prompt 发送）。
func (a *App) cancelQueueEdit() {
	if a.queueEdit == nil {
		return
	}
	a.queueEdit = nil
	a.model.Input.Clear()
	a.model.UpdateSlashState()
	a.model.ShowInfo(i18n.T("已取消编辑排队消息"))
}

// cancelSelectedQueueItem 取消（删除）面板当前选中的排队项。
func (a *App) cancelSelectedQueueItem() {
	item, ok := a.model.QueueSelected()
	if !ok {
		return
	}
	if a.sess == nil {
		return
	}
	control, ok := a.sess.(acp.MsgQueueControl)
	if !ok {
		return
	}
	session := a.sess
	id := item.ID
	a.startCommand(commandResult{kind: commandMsgQueueCancel, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.CancelMsgQueueItem(ctx, id)
	})
}

// clearMsgQueue 清空当前会话的全部排队项。
func (a *App) clearMsgQueue() {
	if a.sess == nil || len(a.model.QueueItems()) == 0 {
		return
	}
	control, ok := a.sess.(acp.MsgQueueControl)
	if !ok {
		return
	}
	session := a.sess
	a.startCommand(commandResult{kind: commandMsgQueueClear, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.ClearMsgQueue(ctx)
	})
}
