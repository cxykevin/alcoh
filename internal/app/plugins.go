package app

// 插件宿主接线：插件启动/关闭、plugin → host 事件应用、以及各处 hook 注入。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
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
	case model.ModalPlugins:
		return "plugins"
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

// applyEditorPatch 按活动编辑器把 patch 写回：/plugins 弹窗 → 本地
// config.json（applyLocalConfigPatch），/server 弹窗 → 服务端 config/set。
func (a *App) applyEditorPatch(ed *model.ConfigEditor, patch json.RawMessage) {
	if len(patch) == 0 {
		return
	}
	if ed != nil && ed == a.model.PluginsCfg {
		a.applyLocalConfigPatch(patch)
		return
	}
	a.applyConfigSet(patch)
}

// openPluginsEditor 打开本地配置编辑器（/plugins）：读取 config.json 构建
// 配置树并聚焦到 plugins 段。配置中没有 plugins 段时不凭空新建——停留在根页，
// 根页「(新增)」行可直接新建该段并追加插件条目（见 AddPluginsArray）。
func (a *App) openPluginsEditor() {
	m := a.model
	m.OpenPlugins()
	values, err := config.Load()
	if err != nil {
		m.ShowError(i18n.T("读取本地配置失败: %s", err.Error()))
		m.ClosePlugins()
		return
	}
	raw, err := json.Marshal(values)
	if err != nil {
		m.ShowError(i18n.T("序列化本地配置失败: %s", err.Error()))
		m.ClosePlugins()
		return
	}
	m.SetPluginsConfig(raw)
	if ed := m.PluginsCfg; ed != nil {
		ed.Focus([]string{"plugins"})
	}
}

// reloadPluginsConfig 重新读取本地配置重建 /plugins 配置树（"r" 刷新，
// 丢弃未保存的改动）。
func (a *App) reloadPluginsConfig() {
	if a.model.PluginsCfg == nil {
		return
	}
	a.openPluginsEditor()
}

// applyLocalConfigPatch 把本地配置编辑器的部分 patch 合并进 config.json 并
// 原子保存（/plugins 的"编辑即保存"）。数组/标量整体替换，对象递归合并，
// null 键删除。
func (a *App) applyLocalConfigPatch(patch json.RawMessage) {
	if len(patch) == 0 {
		return
	}
	values, err := config.Load()
	if err != nil {
		a.model.ShowError(i18n.T("读取本地配置失败: %s", err.Error()))
		return
	}
	root, err := valuesAsMap(values)
	if err != nil {
		a.model.ShowError(i18n.T("序列化本地配置失败: %s", err.Error()))
		return
	}
	var patchRoot map[string]any
	if err := json.Unmarshal(patch, &patchRoot); err != nil {
		a.model.ShowError(i18n.T("解析配置修改失败: %s", err.Error()))
		return
	}
	mergeJSON(root, patchRoot)
	merged, err := json.Marshal(root)
	if err != nil {
		a.model.ShowError(i18n.T("序列化配置失败: %s", err.Error()))
		return
	}
	var updated config.Values
	if err := json.Unmarshal(merged, &updated); err != nil {
		a.model.ShowError(i18n.T("配置类型不匹配: %s", err.Error()))
		return
	}
	if err := config.Save(updated); err != nil {
		a.model.ShowError(i18n.T("保存本地配置失败: %s", err.Error()))
		return
	}
	a.model.Settings = updated
	a.model.ShowInfo(i18n.T("配置已保存（插件改动重启 alcoh 后生效）"))
}

// valuesAsMap 把本地配置序列化为可合并的 JSON 对象。
func valuesAsMap(values config.Values) (map[string]any, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// mergeJSON 把 patch（部分更新对象）递归合并进 dst：对象递归，数组/标量
// 整体替换，null 值删除对应键（服务端 config/set 对 map 键的删除语义）。
func mergeJSON(dst map[string]any, patch map[string]any) {
	for k, pv := range patch {
		if pv == nil {
			delete(dst, k)
			continue
		}
		dv, ok := dst[k]
		pvMap, pvIsMap := pv.(map[string]any)
		dvMap, dvIsMap := dv.(map[string]any)
		if ok && pvIsMap && dvIsMap {
			mergeJSON(dvMap, pvMap)
			continue
		}
		dst[k] = pv
	}
}
