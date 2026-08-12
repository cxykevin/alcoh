package app

// 插件宿主接线：插件启动/关闭、plugin → host 事件应用、以及各处 hook 注入。

import (
	"context"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/plugin"
	"github.com/cxykevin/alcoh/product"
)

const (
	// promptHookTimeout 是 prompt hooks 的最长总等待时间（提交前同步调用）。
	promptHookTimeout = 2 * time.Second
	// keyHookTimeout 是 key hook 的最长等待时间。仅命中插件声明的按键时才
	// 发起 IPC，普通按键零开销。
	keyHookTimeout = 200 * time.Millisecond
	// commandHookTimeout 是插件斜杠命令的最长等待时间。
	commandHookTimeout = 2 * time.Second
)

// startPlugins 启动插件进程并完成握手，随后把插件注册的斜杠命令同步到模型
// 命令面板。任一插件失败不致命：错误会作为插件事件在事件循环中展示。
func (a *App) startPlugins() {
	if a.plugins == nil {
		return
	}
	a.plugins.SetHostInfo("alcoh", product.Version, a.workdir)
	_ = a.plugins.Start(a.runCtx)
	a.syncPluginCommands()
}

// syncPluginCommands 把已握手插件的斜杠命令与描述同步到模型。
func (a *App) syncPluginCommands() {
	if a.plugins == nil || a.model == nil {
		return
	}
	names := a.plugins.CommandNames()
	a.model.SetPluginCommands(names)
	info := make(map[string]model.SlashCommandInfo, len(names))
	for _, name := range names {
		desc, hint, ok := a.plugins.CommandInfo(name)
		if !ok {
			continue
		}
		info[name] = model.SlashCommandInfo{Name: name, Description: desc, ArgsHint: hint}
	}
	a.model.SetPluginCommandInfo(info)
}

// applyPluginEvent 应用插件 → 宿主事件（事件循环内、modelMu 持有中调用）。
func (a *App) applyPluginEvent(ev plugin.UIEvent) {
	switch ev.Kind {
	case plugin.EventNotify:
		if ev.IsErr {
			a.model.ShowError(ev.Text)
		} else {
			a.model.ShowInfo(ev.Text)
		}
	case plugin.EventStatus:
		a.model.SetPluginStatus(ev.Plugin, ev.Text)
	case plugin.EventFailed:
		a.model.ShowError(ev.Text)
	}
}

// promptHook 在提交 prompt 前依次调用插件 hooks。返回最终文本与是否被拦截；
// 拦截或改写为空时输入框不被消费（调用方应原样保留输入）。
func (a *App) promptHook(sessionID, text string) (string, bool) {
	if a.plugins == nil || a.runCtx == nil {
		return text, false
	}
	ctx, cancel := context.WithTimeout(a.runCtx, promptHookTimeout)
	defer cancel()
	out, blocked, reason, err := a.plugins.PromptHook(ctx, sessionID, text)
	if err != nil {
		a.model.ShowError(i18n.T("插件 prompt hook 失败: %s", err.Error()))
		return text, true
	}
	if blocked {
		if reason == "" {
			reason = i18n.T("prompt 已被插件拦截")
		}
		a.model.ShowError(reason)
		return text, true
	}
	if out == "" {
		a.model.ShowError(i18n.T("插件把 prompt 改写为空，已取消发送"))
		return text, true
	}
	return out, false
}

// pluginKeyHook 在按键分发前调用命中插件绑定的 key hooks；返回是否已被消费。
func (a *App) pluginKeyHook(ke input.KeyEvent) bool {
	if a.plugins == nil || a.runCtx == nil || !a.plugins.WantsKey(ke) {
		return false
	}
	m := a.model
	view := "home"
	if m.View == model.ViewSession {
		view = "session"
	}
	input := ""
	if m.Input != nil {
		input = m.Input.Text()
	}
	ctx, cancel := context.WithTimeout(a.runCtx, keyHookTimeout)
	defer cancel()
	return a.plugins.KeyHook(ctx, ke, view, modalName(m.Modal), input == "", input)
}

// runPluginCommand 把斜杠命令交给注册该命令的插件执行；返回是否已处理。
func (a *App) runPluginCommand(name, rest string) bool {
	if a.plugins == nil || a.runCtx == nil {
		return false
	}
	sessionID := ""
	if a.sess != nil {
		sessionID = a.sess.ID()
	}
	ctx, cancel := context.WithTimeout(a.runCtx, commandHookTimeout)
	handled := a.plugins.RunCommand(ctx, name, rest, sessionID)
	cancel()
	if handled {
		a.model.Input.Clear()
		a.model.CloseSlash()
	}
	return handled
}

// modalName 把模态枚举映射为协议按键上下文中的 modal 名。
func modalName(k model.ModalKind) string {
	switch k {
	case model.ModalPermission:
		return "permission"
	case model.ModalElicitation:
		return "elicitation"
	case model.ModalHelp:
		return "help"
	case model.ModalExitConfirm:
		return "exit_confirm"
	case model.ModalSettings:
		return "settings"
	case model.ModalServer:
		return "server"
	case model.ModalEffort:
		return "effort"
	case model.ModalModel:
		return "model"
	case model.ModalOnboarding:
		return "onboarding"
	case model.ModalConnect:
		return "connect"
	default:
		return "none"
	}
}

// sendPrompt 是 SendPrompt 的统一入口：先经插件 prompt hooks（可改写/拦截）
// 再发送。home 回车与 One Shot 初始消息路径都经此函数。
func (a *App) sendPrompt(session acp.Session, text string) {
	if session == nil || text == "" {
		return
	}
	out, blocked := a.promptHook(session.ID(), text)
	if blocked || out == "" {
		return
	}
	a.sendPromptRaw(session, out)
}

// sendPromptRaw 直接把已裁决的文本发送给会话（不再次触发 hooks；会话视图
// 提交路径在 submitPrompt 中先调用 promptHook 消费输入框，因此走这里）。
func (a *App) sendPromptRaw(session acp.Session, text string) {
	if session == nil || text == "" {
		return
	}
	s := session
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: s.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, s.SendPrompt(ctx, text)
	})
}
