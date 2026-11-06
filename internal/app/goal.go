package app

// 会话 goal（alk.cxykevin.top/goal）的交互接线：/goal 看板、编辑/清除，
// 以及带参 /goal 命令。goal 是会话元数据，写入走 goal/set RPC，不产生
// 消息、不触发模型回合；服务端随后广播快照（见
// ../alkaid0/docs/acp/extension.md §2.9/§3.12）。

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/widget"
)

// openGoal 处理无参数 /goal：打开当前会话的 goal 看板。
func (a *App) openGoal() {
	m := a.model
	if !m.SupportsGoal() {
		m.ShowError(i18n.T("服务端未声明 goal 能力，/goal 不可用"))
		return
	}
	if !m.HasActive() {
		m.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	if !m.OpenGoalModal() {
		return
	}
}

// goalCommand 处理带参 /goal：clear 清除，其它文本设置/覆盖。参数校验在
// 本地完成（长度与空值），写入走 goal/set RPC；服务端推送快照后看板刷新。
func (a *App) goalCommand(rest string) {
	m := a.model
	if !m.SupportsGoal() {
		m.ShowError(i18n.T("服务端未声明 goal 能力，/goal 不可用"))
		return
	}
	if !m.HasActive() {
		m.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		a.openGoal()
		return
	}
	if strings.EqualFold(rest, "clear") {
		a.setGoal("")
		return
	}
	if n := utf8.RuneCountInString(rest); n > model.MaxGoalRunes {
		m.ShowError(i18n.T("会话目标过长：%d 个字符（最多 %d 个）", n, model.MaxGoalRunes))
		return
	}
	a.setGoal(rest)
}

// setGoal 发起 goal/set 写入；空字符串清除目标。结果经 applyCommandResult
// 提示失败/成功，实际内容以服务端随后推送的 goal 快照为准。
func (a *App) setGoal(goal string) {
	if a.sess == nil {
		a.model.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	control, ok := a.sess.(acp.GoalControl)
	if !ok {
		a.model.ShowError(i18n.T("当前会话不支持写入 goal"))
		return
	}
	session := a.sess
	a.startCommand(commandResult{kind: commandGoalSet, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.SetGoal(ctx, goal)
	})
}

// startGoalEdit 进入看板的编辑态，输入缓冲以当前 goal 为初始内容。
func (a *App) startGoalEdit() {
	a.model.BeginGoalEdit()
}

// commitGoalEdit 提交编辑态草稿：空白与超长在本地拦截，成功后走 goal/set。
func (a *App) commitGoalEdit() {
	m := a.model
	text, err := m.CommitGoalEdit()
	if err != nil {
		if errors.Is(err, model.ErrGoalEmpty) {
			m.ShowError(i18n.T("目标不能为空；清空请使用 /goal clear"))
			return
		}
		m.ShowError(i18n.T("设置会话目标失败: %s", err.Error()))
		return
	}
	a.setGoal(text)
}

// clearGoal 清除当前会话的 goal（/goal clear 的看板快捷方式）。
func (a *App) clearGoal() {
	a.setGoal("")
}

// goalKey 处理 /goal 看板按键：只读态 e/Enter 编辑、d/x 清除、Esc 关闭；
// 编辑态 Enter 保存、Shift+Enter 换行、Esc 取消，其余按键交给输入框。
func (a *App) goalKey(ke input.KeyEvent) {
	m := a.model
	if m.GoalEditing {
		switch {
		case ke.Type == input.KeyEsc:
			m.CancelGoalEdit()
			return
		case ke.Type == input.KeyEnter && !ke.IsShift():
			a.commitGoalEdit()
			return
		}
		if m.GoalInput == nil {
			return
		}
		ib := &widget.InputBox{Buf: m.GoalInput, Style: a.view.Theme.Style(a.view.Theme.Text)}
		ib.OnKey(ke)
		return
	}
	switch ke.Type {
	case input.KeyEsc:
		m.CancelGoalModal()
		return
	case input.KeyEnter:
		a.startGoalEdit()
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
		a.startGoalEdit()
	case 'd', 'x':
		a.clearGoal()
	}
}
