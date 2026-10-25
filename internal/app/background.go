package app

// 会话后台运行模式（alk.cxykevin.top/session/get_background）的查询接线。
// 模式由服务端命令 /background on|off 修改，服务端不推送变化，客户端在进入
// 会话、重连重绑与状态切换时补一次查询，供状态栏工作指示器着色（见
// ../alkaid0/docs/acp/extension.md §3.7）。

import (
	"context"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
)

// refreshBackgroundMode 查询当前会话的后台运行模式。服务端未声明 alkaid0
// 扩展能力、会话句柄不支持该查询或没有活动会话时静默跳过；查询失败也不提示
// ——它只影响指示器颜色，不应为旧服务端打扰用户。
func (a *App) refreshBackgroundMode() {
	if a.sess == nil || !a.model.SupportsAlkaid0() {
		return
	}
	control, ok := a.sess.(acp.BackgroundControl)
	if !ok {
		return
	}
	sessionID := a.sess.ID()
	a.startBackgroundGet(commandResult{kind: commandBackgroundGet, sessionID: sessionID}, func(ctx context.Context) (bool, error) {
		return control.GetBackground(ctx)
	})
}

// startBackgroundGet 与 startCommand 同构，但把布尔查询结果随结果带回事件循环。
func (a *App) startBackgroundGet(result commandResult, fn func(context.Context) (bool, error)) {
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Go(func() {
		background, err := fn(ctx)
		result.background = background
		result.err = err
		select {
		case a.commands <- result:
		case <-ctx.Done():
		}
	})
}

// applyBackgroundResult 把 get_background 的结果写入模型。会话已切换时丢弃；
// 查询失败时保持原值（指示器继续用默认蓝色），不弹提示。
func (a *App) applyBackgroundResult(result commandResult) {
	if a.sess == nil || a.sess.ID() != result.sessionID {
		return
	}
	if result.err != nil {
		return
	}
	if a.model.HasActive() && a.model.Active.ID == result.sessionID {
		a.model.Active.Background = result.background
	}
}

// backgroundCommand 处理客户端覆盖指令 /background。它由本地命令分发直接
// 调用，因此始终绕过消息队列：不经过 submitPrompt 的"运行中入队"分支，
// 不取消当前运行，也不触发插件 prompt hooks；只是把服务端命令原样发出去。
//
// 无参数时只显示当前模式（本地状态，不打扰服务端）；on|off 立即发送并先在
// 本地更新指示器，随后服务端广播状态切换会再经 get_background 校正。
func (a *App) backgroundCommand(rest string) {
	m := a.model
	if !m.HasActive() || a.sess == nil {
		m.ShowError(i18n.T("当前没有活动会话"))
		return
	}
	if !m.SupportsAlkaid0() {
		m.ShowError(i18n.T("服务端未声明 alkaid0 扩展能力，/background 不可用"))
		return
	}
	arg := strings.ToLower(strings.TrimSpace(rest))
	switch arg {
	case "":
		label := i18n.T("关闭")
		if m.Background() {
			label = i18n.T("开启")
		}
		m.ShowInfo(i18n.T("后台运行模式：%s", label))
		return
	case "on", "off":
	default:
		m.ShowError(i18n.T("用法: /background on|off"))
		return
	}
	// 本地先行更新，指示器立即变色；服务端响应后由查询结果校正（见
	// refreshBackgroundMode）。
	m.SetBackground(arg == "on")
	a.sendPromptRaw(a.sess, "/background "+arg)
}
