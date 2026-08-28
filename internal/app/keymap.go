package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/view"
	"github.com/cxykevin/alcoh/internal/widget"
)

// dispatchKey 分发按键事件（插件按键拦截优先，再按模态/视图）。
func (a *App) dispatchKey(ke input.KeyEvent) {
	// 插件 key hooks：仅当命中插件声明的按键绑定时才发起 IPC；返回 handled
	// 时该键被插件消费，不再进入默认分发。
	if a.pluginKeyHook(ke) {
		return
	}
	m := a.model
	switch m.Modal {
	case model.ModalPermission:
		a.permissionKey(ke)
		return
	case model.ModalElicitation:
		a.elicitationKey(ke)
		return
	case model.ModalHelp:
		if ke.Type == input.KeyEsc || ke.Type == input.KeyRune && ke.Rune == '?' {
			m.SetModal(model.NoModal)
		}
		return
	case model.ModalExitConfirm:
		if ke.Type == input.KeyRune && ke.Rune == 'y' {
			m.Quitting = true
		} else if ke.Type == input.KeyRune && ke.Rune == 'n' || ke.Type == input.KeyEsc {
			m.SetModal(model.NoModal)
		}
		return
	case model.ModalSettings:
		a.settingsKey(ke)
		return
	case model.ModalEffort:
		a.effortKey(ke)
		return
	case model.ModalModel:
		a.modelKey(ke)
		return
	case model.ModalServer:
		a.serverConfigKey(ke)
		return
	case model.ModalPlugins:
		a.pluginsConfigKey(ke)
		return
	case model.ModalOnboarding:
		a.onboardingKey(ke)
		return
	case model.ModalConnect:
		a.connectKey(ke)
		return
	}

	if ke.IsCtrl() && ke.Rune == 'q' {
		m.SetModal(model.ModalExitConfirm)
		return
	}
	if ke.IsCtrl() && ke.Rune == 'l' {
		a.render()
		return
	}
	if ke.IsCtrl() && ke.Rune == ',' {
		m.OpenSettings()
		return
	}
	if ke.IsCtrl() && ke.Rune == 'c' {
		a.handleCtrlC()
		return
	}
	if ke.IsCtrl() && ke.Rune == 'o' {
		a.expandAll()
		return
	}
	// ? 帮助：仅当输入框为空时触发；输入框有内容时 "?" 按普通字符输入。
	if ke.Type == input.KeyRune && ke.Rune == '?' && m.InputEmpty() {
		m.SetModal(model.ModalHelp)
		return
	}

	switch m.View {
	case model.ViewHome:
		a.homeKey(ke)
	case model.ViewSession:
		a.sessionKey(ke)
	}
}

func (a *App) homeKey(ke input.KeyEvent) {
	m := a.model
	// 列表聚焦模式：上下移动选中，Enter 恢复，d 删除，Esc/右键返回输入框。
	if m.HomeListFocused {
		switch {
		case ke.Type == input.KeyUp && m.HomeSelected > 0:
			m.HomeSelected--
		case ke.Type == input.KeyDown && m.HomeSelected < len(m.Sessions)-1:
			m.HomeSelected++
			a.maybeLoadMoreSessions()
		case ke.Type == input.KeyEnter:
			m.HomeListFocused = false
			if len(m.Sessions) > 0 {
				sel := m.HomeSelected
				if sel < 0 {
					sel = 0
				}
				if sel < len(m.Sessions) {
					a.resumeSession(m.Sessions[sel].SessionID)
				}
			}
		case ke.Type == input.KeyRune && ke.Rune == 'r':
			a.refreshSessionsLocked()
		case ke.Type == input.KeyRune && ke.Rune == 'd':
			if m.HomeSelected >= 0 && m.HomeSelected < len(m.Sessions) {
				a.deleteSession(m.Sessions[m.HomeSelected].SessionID)
			}
		case ke.Type == input.KeyEsc || ke.Type == input.KeyRight:
			m.HomeListFocused = false
		}
		return
	}

	// 命令输入面板：输入 / 时弹出命令建议列表，Enter/Tab 补全并执行本地命令。
	if m.SlashOpen {
		switch ke.Type {
		case input.KeyUp:
			m.SlashMove(-1)
			return
		case input.KeyDown:
			m.SlashMove(1)
			return
		case input.KeyTab:
			a.completeSlash()
			return
		case input.KeyEnter:
			if a.slashTokenMatchesSelection() {
				m.CloseSlash()
				if a.tryLocalSlashCommand() {
					return
				}
				if text := m.SubmitHomeInput(); text != "" {
					if !a.usePreSession(text) {
						m.PendingInitialPrompt = text
						a.createSession()
					}
				}
				return
			}
			a.completeSlash()
			return
		case input.KeyEsc:
			m.CloseSlash()
			return
		}
	}

	// 输入框内按左键（光标在行首时）：显示并全屏聚焦会话列表。
	if !m.HomeListFocused && !m.SlashOpen && ke.Type == input.KeyLeft && !ke.IsAlt() && !ke.IsCtrl() && m.Input.CX == 0 && m.Input.CY == 0 {
		m.HomeListFocused = true
		if m.HomeSelected < 0 && len(m.Sessions) > 0 {
			m.HomeSelected = 0
		}
		a.maybeLoadMoreSessions()
		return
	}

	// 输入框为空时 d 为删除会话快捷键（选中项为当前列表选中，未选中时忽略）。
	// 必须在 InputBox.OnKey 之前处理：否则 d 会被当作普通字符插入输入框。
	if !m.SlashOpen && ke.Type == input.KeyRune && ke.Rune == 'd' && m.Input.Text() == "" {
		if m.HomeSelected >= 0 && m.HomeSelected < len(m.Sessions) {
			a.deleteSession(m.Sessions[m.HomeSelected].SessionID)
		}
		return
	}

	ib := &widget.InputBox{Buf: m.Input, Style: a.view.Theme.Style(a.view.Theme.Text)}
	if ib.OnKey(ke) {
		m.UpdateSlashState()
		return
	}
	switch {
	case ke.Type == input.KeyUp:
		if m.Input.Text() == "" && m.HomeSelected > 0 {
			m.HomeSelected--
		}
	case ke.Type == input.KeyDown:
		if m.Input.Text() == "" && m.HomeSelected < len(m.Sessions)-1 {
			m.HomeSelected++
			a.maybeLoadMoreSessions()
		}
	case ke.Type == input.KeyEnter:
		if a.tryLocalSlashCommand() {
			return
		}
		if text := m.SubmitHomeInput(); text != "" {
			// 正常输入 prompt：直接复用主页预创建会话，不删除也不新建。
			if !a.usePreSession(text) {
				m.PendingInitialPrompt = text
				a.createSession()
			}
			return
		}
		if len(m.Sessions) > 0 {
			sel := m.HomeSelected
			if sel < 0 {
				sel = 0
			}
			if sel < len(m.Sessions) {
				a.resumeSession(m.Sessions[sel].SessionID)
			}
		}
	case ke.Type == input.KeyEsc:
		m.Quitting = true
	}
}

func (a *App) killSelectedShell() {
	s := a.model.SelectedShell()
	if s == nil || a.sess == nil {
		return
	}
	control, ok := a.sess.(acp.TerminalControl)
	if !ok {
		a.model.ShowError("alkaid0 v0.5 terminal control unavailable")
		return
	}
	id := s.ID
	a.startCommand(commandResult{kind: commandTerminalStop, sessionID: a.sess.ID(), terminalID: id}, func(ctx context.Context) (acp.Session, error) {
		return nil, control.StopTerminal(ctx, id)
	})
}

func (a *App) sessionKey(ke input.KeyEvent) {
	m := a.model
	if m.SlashOpen {
		switch ke.Type {
		case input.KeyUp:
			m.SlashMove(-1)
			return
		case input.KeyDown:
			m.SlashMove(1)
			return
		case input.KeyTab:
			a.completeSlash()
			return
		case input.KeyEnter:
			// Enter 与 Tab 一致做命令补全；若当前 token 已精确匹配选中命令，
			// 则直接执行本地命令 / 提交 prompt，避免要求用户再按一次 Enter。
			if a.slashTokenMatchesSelection() {
				m.CloseSlash()
				if a.tryLocalSlashCommand() {
					return
				}
				a.submitPrompt()
				return
			}
			a.completeSlash()
			return
		case input.KeyEsc:
			m.CloseSlash()
			return
		}
	}
	// 按 Esc 打断正在进行的 AI 响应。无论焦点在输入框还是消息区都生效；
	// 会话空闲时不动作（避免误触清空输入或误退出）。
	if ke.Type == input.KeyEsc {
		if m.HasActive() && m.Active.Running() {
			a.cancelCurrent()
		}
		return
	}
	if m.ShellPanel {
		switch ke.Type {
		case input.KeyEsc:
			if m.ShellFullscreen {
				m.ShellFullscreen = false
			} else {
				m.CloseShellPanel()
			}
			return
		case input.KeyEnter:
			m.ShellFullscreen = true
			return
		case input.KeyUp:
			if m.ShellSelected > 0 {
				m.ShellSelected--
			}
			return
		case input.KeyDown:
			if m.ShellSelected < len(m.Shells())-1 {
				m.ShellSelected++
			}
			return
		case input.KeyRune:
			if ke.Rune == 'x' {
				a.killSelectedShell()
				return
			}
		}
		return
	}
	if ke.Type == input.KeyTab {
		m.Focus = model.FocusMessage - m.Focus
		return
	}
	// PgUp/PgDn 始终滚动消息区，即便焦点在输入框；
	// 输入框内的 MoveBufferStart/End 由 Ctrl+Home/End 或滚轮补齐。
	if ke.Type == input.KeyPageUp || ke.Type == input.KeyPageDown {
		_, h := a.term.Size()
		page := h / 2
		if page < 1 {
			page = 1
		}
		if ke.Type == input.KeyPageUp {
			m.ScrollUp(page)
		} else {
			m.ScrollDown(page)
		}
		return
	}
	if ke.Type == input.KeyDown && m.Input.HistPos < 0 && len(m.Shells()) > 0 {
		m.OpenShellPanel()
		return
	}
	if m.Focus == model.FocusMessage {
		a.messageKey(ke)
		return
	}
	ib := &widget.InputBox{Buf: m.Input, Style: a.view.Theme.Style(a.view.Theme.Text)}
	if ib.OnKey(ke) {
		m.UpdateSlashState()
		return
	}
	switch {
	case ke.Type == input.KeyEnter:
		if a.tryLocalSlashCommand() {
			return
		}
		a.submitPrompt()
	case ke.Type == input.KeyRune && ke.Rune == 'x' && m.Input.Text() == "":
		a.goHome()
	}
}

// slashTokenMatchesSelection 判断输入框首个 token 是否已与选中命令一致（含末尾空格情形）。
func (a *App) slashTokenMatchesSelection() bool {
	sel := a.model.SlashSelectedCommand()
	if sel == "" {
		return false
	}
	name, _ := splitFirstToken(a.model.Input.Text())
	return name == sel
}

// completeSlash 将当前选中命令写入输入框首个 token，保留后续参数。
func (a *App) completeSlash() {
	command := a.model.SlashSelectedCommand()
	if command == "" {
		return
	}
	a.model.Input.ReplaceFirstToken(command)
	a.model.UpdateSlashState()
}

// tryLocalSlashCommand 若首个 token 恰好匹配本地命令则执行，返回是否已处理。
func (a *App) tryLocalSlashCommand() bool {
	m := a.model
	text := m.Input.Text()
	if len(text) == 0 || text[0] != '/' {
		return false
	}
	name, rest := splitFirstToken(text)
	switch name {
	case "/alcoh_help":
		m.Input.Clear()
		m.CloseSlash()
		m.SetModal(model.ModalHelp)
	case "/effort":
		m.Input.Clear()
		m.CloseSlash()
		if !m.SupportsEffort() {
			m.ShowError(i18n.T("服务端未公布 thought_level 配置，/effort 不可用"))
			return true
		}
		if rest == "" {
			// 无参数：打开水平滑条弹窗。
			m.OpenEffortModal()
			return true
		}
		// 带参数：校验后直接设置。
		if !m.ValidEffortValue(rest) {
			m.ShowError(i18n.T("无效的 effort 值: %s（可选: unset/low/medium/high/xhigh/max）", rest))
			return true
		}
		a.setEffort(rest)
	case "/model":
		m.Input.Clear()
		m.CloseSlash()
		if !m.SupportsModel() {
			m.ShowError(i18n.T("服务端未公布 model 配置，/model 不可用"))
			return true
		}
		if rest == "" {
			// 无参数：打开模型选择菜单。
			m.OpenModelModal()
			return true
		}
		// 带参数：校验后直接设置。
		if !m.ValidModelValue(rest) {
			m.ShowError(i18n.T("无效的 model 值: %s", rest))
			return true
		}
		a.setModel(rest)
	case "/clear":
		m.Input.Clear()
		m.CloseSlash()
		if rest == "on" {
			// /clear on：不取消正在运行的会话，直接返回会话列表。
			a.goHome()
			return true
		}
		// 默认：若会话仍在运行（未被取消），先停掉再返回会话列表。
		if m.Active != nil && m.Active.Running() {
			a.cancelCurrent()
		}
		a.goHome()
	case "/settings":
		m.Input.Clear()
		m.CloseSlash()
		m.OpenSettings()
	case "/server":
		m.Input.Clear()
		m.CloseSlash()
		if !m.SupportsAlkaid0() {
			m.ShowError(i18n.T("服务端未声明 alkaid0 扩展能力，/server 不可用"))
			return true
		}
		a.openServerEditor()
	case "/connect":
		m.Input.Clear()
		m.CloseSlash()
		if !m.SupportsAlkaid0() {
			m.ShowError(i18n.T("服务端未声明 alkaid0 扩展能力，/connect 不可用"))
			return true
		}
		a.openConnect()
	case "/plugins":
		m.Input.Clear()
		m.CloseSlash()
		a.openPluginsEditor()
	default:
		// 插件注册的斜杠命令（/xxx 开头的 token 未命中本地命令时尝试插件）。
		return a.runPluginCommand(name, rest)
	}
	return true
}

func splitFirstToken(text string) (name, rest string) {
	end := len(text)
	for i, r := range text {
		if r == ' ' || r == '\t' {
			end = i
			break
		}
	}
	name = text[:end]
	if end < len(text) {
		rest = text[end+1:]
	}
	return name, rest
}

func (a *App) settingsKey(ke input.KeyEvent) {
	m := a.model
	switch ke.Type {
	case input.KeyUp:
		m.MoveSettings(-1, 4)
	case input.KeyDown:
		m.MoveSettings(1, 4)
	case input.KeyLeft:
		if m.CycleColorMode(-1) || m.CycleLanguage(-1) {
			a.saveSettings()
		}
	case input.KeyRight:
		if m.CycleColorMode(1) || m.CycleLanguage(1) {
			a.saveSettings()
		}
	case input.KeyEnter:
		if m.ToggleSetting() {
			a.saveSettings()
		}
	case input.KeyEsc:
		m.SetModal(model.NoModal)
	}
}

// effortKey 处理推理强度滑条弹窗按键：左右移动、回车确认、Esc 取消。
func (a *App) effortKey(ke input.KeyEvent) {
	m := a.model
	switch ke.Type {
	case input.KeyLeft:
		m.EffortMove(-1)
	case input.KeyRight:
		m.EffortMove(1)
	case input.KeyEnter:
		a.setEffort(m.EffortSelectedValue())
	case input.KeyEsc:
		m.CancelEffort()
	}
}

// setEffort 通过 session/set_config_option 设置推理强度（thought_level）。
func (a *App) setEffort(value string) {
	if value == "" || a.sess == nil {
		return
	}
	session := a.sess
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, session.SetConfigOption(ctx, "thought_level", "select", value)
	})
}

// modelKey 处理模型选择菜单按键：上下移动、回车确认、Esc 取消。
func (a *App) modelKey(ke input.KeyEvent) {
	m := a.model
	switch ke.Type {
	case input.KeyUp:
		m.ModelMove(-1)
	case input.KeyDown:
		m.ModelMove(1)
	case input.KeyEnter:
		a.setModel(m.ModelSelectedValue())
	case input.KeyEsc:
		m.CancelModel()
	}
}

// setModel 通过 session/set_config_option 设置模型。configId 取服务端公布的
// model 配置项；ACP v2 中 select 类配置使用 type="id"。
func (a *App) setModel(value string) {
	if value == "" || a.sess == nil {
		return
	}
	opt := a.model.ActiveModelConfig()
	if opt == nil {
		return
	}
	session := a.sess
	configID := opt.ConfigID
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, session.SetConfigOption(ctx, configID, "id", value)
	})
}

// openServerEditor 打开服务端配置编辑器并异步拉取配置（config/get）。
func (a *App) openServerEditor() {
	m := a.model
	if !m.SupportsAlkaid0() {
		m.ShowError(i18n.T("服务端未声明 alkaid0 扩展能力"))
		return
	}
	m.OpenServer()
	a.serverCfgFocus = nil // 每次打开重置新增项重定向目标
	a.startConfigGet()
}

// startConfigGet 异步调用 alk.cxykevin.top/config/get，结果经 applyCommandResult
// 写入模型配置树（commandConfigGet）。每次发起递增 cfgGetSeq，结果携带该序号，
// 晚回的旧序号结果会被 applyCommandResult 丢弃。
func (a *App) startConfigGet() {
	if a.runCtx == nil {
		return
	}
	a.cfgGetSeq++
	seq := a.cfgGetSeq
	ctx := a.runCtx
	a.commandWG.Add(1)
	go func() {
		defer a.commandWG.Done()
		cfg, err := a.backend.GetConfig(ctx)
		select {
		case a.commands <- commandResult{kind: commandConfigGet, cfgSeq: seq, config: cfg, err: err}:
		case <-ctx.Done():
		}
	}()
}

// applyConfigSet 通过 alk.cxykevin.top/config/set 部分更新服务端配置并持久化。
// 写回按发出顺序串行：有 set 在途时 patch 排队，前一个完成后再发送下一个，
// 避免并发 config/set 乱序导致旧的完整对象 patch 覆盖紧接着的字段编辑。
// 写回开始即置 Saving，直到全部写回与随后的全量重载完成才解除（期间阻塞改动）。
func (a *App) applyConfigSet(patch json.RawMessage) {
	if len(patch) == 0 {
		return
	}
	if ed := a.model.ServerCfg; ed != nil {
		ed.Saving = true
	}
	if a.cfgWriteBusy {
		a.cfgWriteQueue = append(a.cfgWriteQueue, patch)
		return
	}
	a.sendConfigSet(patch)
}

// sendConfigSet 实际发送一个 config/set，并标记写回在途。
func (a *App) sendConfigSet(patch json.RawMessage) {
	a.cfgWriteBusy = true
	a.startCommand(commandResult{kind: commandConfigSet}, func(ctx context.Context) (acp.Session, error) {
		return nil, a.backend.SetConfig(ctx, patch)
	})
}

// nextConfigSet 在当前 config/set 完成后调用：发送队列中的下一个 patch，
// 全部写回完成后触发整配置重载（config/get 重建，展示以服务端为准）。
// 重载完成（applyCommandResult 应用）后解除 Saving 阻塞。
func (a *App) nextConfigSet() {
	if len(a.cfgWriteQueue) > 0 {
		next := a.cfgWriteQueue[0]
		a.cfgWriteQueue = a.cfgWriteQueue[1:]
		a.sendConfigSet(next)
		return
	}
	a.cfgWriteBusy = false
	a.startConfigGet()
}

// serverConfigKey 处理服务端配置编辑器（ModalServer）按键。
// 编辑即自动保存：修改标量、增删集合项后立即构造部分更新 patch 写回。
func (a *App) serverConfigKey(ke input.KeyEvent) {
	m := a.model
	ed := m.ServerCfg
	if ed == nil {
		// 配置尚未加载：仅 Esc 可关闭。
		if ke.Type == input.KeyEsc {
			a.closeServerEditor()
		}
		return
	}
	if ed.Saving {
		// 写回/全量重载进行中：阻塞新改动，仅 Esc 可关闭（在途写回仍在后台完成，
		// 其余排队写回放弃，重载结果因编辑器已关闭而被丢弃）。
		if ke.Type == input.KeyEsc {
			a.closeServerEditor()
			a.serverCfgFocus = nil
			a.cfgWriteQueue = nil
			a.cfgWriteBusy = false
		}
		return
	}
	if a.configEditorKey(ke, ed, func() {
		a.startConfigGet() // 重新拉取，丢弃本地未写回状态
	}) {
		return
	}
	if ke.Type == input.KeyEsc {
		a.closeServerEditor()
	}
}

// pluginsConfigKey 处理本地配置编辑器（ModalPlugins）按键，与 serverConfigKey
// 共用同一套导航/编辑逻辑（configEditorKey），仅写回目标为本地 config.json。
func (a *App) pluginsConfigKey(ke input.KeyEvent) {
	m := a.model
	ed := m.PluginsCfg
	if ed == nil {
		// 配置尚未加载：仅 Esc 可关闭。
		if ke.Type == input.KeyEsc {
			m.ClosePlugins()
		}
		return
	}
	if a.configEditorKey(ke, ed, func() {
		a.reloadPluginsConfig() // 重新读取本地配置，丢弃未保存改动
	}) {
		return
	}
	if ke.Type == input.KeyEsc {
		m.ClosePlugins()
	}
}

// configEditorKey 处理配置树编辑器的公共按键（导航/编辑/新增键输入）。
// onRefresh 是 "r" 重新加载的钩子（服务端重新 config/get，本地重新读盘）。
// 返回 true 表示按键已处理；Esc 关闭由调用方决定。
func (a *App) configEditorKey(ke input.KeyEvent, ed *model.ConfigEditor, onRefresh func()) bool {
	if ed.CopyingKey {
		switch {
		case ke.Type == input.KeyEsc:
			ed.CancelCopyKey()
		case ke.Type == input.KeyEnter:
			a.confirmConfigCopyKey(ed)
		default:
			(&widget.InputBox{Buf: ed.CopyInput, Style: a.view.Theme.Style(a.view.Theme.Text)}).OnKey(ke)
		}
		return true
	}
	if ed.RenamingKey {
		switch {
		case ke.Type == input.KeyEsc:
			ed.CancelRenameKey()
		case ke.Type == input.KeyEnter:
			a.confirmConfigRenameKey(ed)
		default:
			(&widget.InputBox{Buf: ed.RenameInput, Style: a.view.Theme.Style(a.view.Theme.Text)}).OnKey(ke)
		}
		return true
	}
	if ed.Editing {
		switch {
		case ke.Type == input.KeyEsc:
			ed.CancelEdit()
		case ke.Type == input.KeyEnter:
			a.commitConfigEdit(ed)
		default:
			(&widget.InputBox{Buf: ed.EditInput, Style: a.view.Theme.Style(a.view.Theme.Text)}).OnKey(ke)
		}
		return true
	}
	if ed.AddingKey {
		switch {
		case ke.Type == input.KeyEsc:
			ed.CancelAddKey()
		case ke.Type == input.KeyEnter:
			a.confirmConfigAddKey(ed)
		default:
			(&widget.InputBox{Buf: ed.AddInput, Style: a.view.Theme.Style(a.view.Theme.Text)}).OnKey(ke)
		}
		return true
	}
	switch {
	case ke.Type == input.KeyUp:
		ed.Move(-1)
	case ke.Type == input.KeyDown:
		ed.Move(1)
	case ke.Type == input.KeyLeft:
		ed.Back() // 返回上一页
	case ke.Type == input.KeyRight:
		a.activateConfigRow(ed)
	case ke.Type == input.KeyEnter:
		a.activateConfigRow(ed)
	case ke.Type == input.KeyRune && ke.Rune == 'r':
		onRefresh()
	default:
		return false
	}
	return true
}

// onboardingKey 处理新手引导剩余步骤按键（模型配置由 /connect 向导完成）。
// Esc 在任何步骤都视为"跳过"（直接结束引导）。
func (a *App) onboardingKey(ke input.KeyEvent) {
	m := a.model
	ob := m.Onboarding
	if ob == nil {
		return
	}
	// 引导中也允许 Ctrl+q 退出（走退出确认弹窗）。
	if ke.IsCtrl() && ke.Rune == 'q' {
		m.SetModal(model.ModalExitConfirm)
		return
	}
	switch ob.Step {
	case model.OnboardStepEffort:
		n := len(model.OnboardEffortCandidates)
		switch {
		case ke.Type == input.KeyUp && ob.EffortSel > 0:
			ob.EffortSel--
		case ke.Type == input.KeyDown && ob.EffortSel < n-1:
			ob.EffortSel++
		case ke.Type == input.KeyEnter:
			a.applyOnboardingEffort(model.OnboardEffortCandidates[ob.EffortSel])
			ob.Step = model.OnboardStepTeaching
		case ke.Type == input.KeyEsc:
			a.finishOnboarding()
		}
	case model.OnboardStepTeaching:
		switch ke.Type {
		case input.KeyEnter, input.KeyEsc:
			a.finishOnboarding()
		}
	}
}

// finishOnboarding 结束新手引导进入主页：清除引导与 /connect 向导状态并返回
// 主页（goHome 会创建主页预创建会话，使 /effort 与 /model 在命令面板可用）。
func (a *App) finishOnboarding() {
	a.model.CloseConnect()
	a.model.CloseOnboarding()
	a.goHome()
}

// applyOnboardingEffort 保存用户第一个会话的推理强度到本地配置。首个会话激活
// 时经 applyFirstSessionEffort 应用并清除。
func (a *App) applyOnboardingEffort(value string) {
	if a.model.Settings.OnboardingEffort == value {
		return
	}
	a.model.Settings.OnboardingEffort = value
	if err := config.Save(a.model.Settings); err != nil {
		a.model.ShowError(i18n.T("保存本地配置失败: %s", err.Error()))
	}
}

// activateConfigRow 处理选中行的 Enter/→：「(新增)」行触发新增、「(删除该项)」
// 行删除其指向的集合项，对象/数组进入其子页面，布尔切换，其余标量进入值编辑。
func (a *App) activateConfigRow(ed *model.ConfigEditor) {
	if ed.OnAddRow() {
		a.activateAddRow(ed)
		return
	}
	if ed.OnRenameRow() {
		ed.BeginRenameKey()
		return
	}
	if ed.OnDeleteRow() {
		a.deleteConfigItem(ed)
		return
	}
	if ed.OnCopyRow() {
		ed.BeginCopyKey()
		return
	}
	n := ed.SelectedNode()
	if n == nil {
		return
	}
	switch n.Kind {
	case model.ConfigObject, model.ConfigArray:
		ed.Enter()
	case model.ConfigBool:
		a.applyEditorPatch(ed, ed.ToggleBool())
	default:
		ed.BeginEdit()
	}
}

// activateAddRow 处理「(新增)」行：Model.Models 直接分配数字键新增空模型；
// Context.Phrase.Phrases 数组直接追加元素；本地配置 plugins 数组追加插件条目
// （仅入内存，编辑该条目字段时才写回，见 AddPluginsItem）；本地配置根页无
// plugins 段时直接新建该段并追加首条目（AddPluginsArray）；名称键 map
// （Agent.Agents、Context.LSP.LanguageServers）进入新增键名输入。
func (a *App) activateAddRow(ed *model.ConfigEditor) {
	if ed.IsModels() {
		patch, ok := ed.AddModelsItem()
		if ok {
			// 新增后本地已进入新项子页；记录其路径，写回成功即整配置重载并
			// 重定向回该页（展示以服务端真实返回为准，避免本地硬编码偏差）。
			a.serverCfgFocus = append([]string(nil), ed.Current().Path...)
			a.applyEditorPatch(ed, patch)
		}
		return
	}
	if cur := ed.Current(); cur != nil {
		if cur.Kind == model.ConfigArray {
			if cur.Key == "plugins" {
				// 本地插件数组：仅在内存中追加条目并进入其子页，编辑该条目
				// 任一字段时才随整体数组写回（避免把空条目凭空持久化）。
				ed.AddPluginsItem()
				return
			}
			patch, ok := ed.AddPhrasesItem()
			if ok {
				a.serverCfgFocus = append([]string(nil), ed.Current().Path...)
				a.applyEditorPatch(ed, patch)
			}
			return
		}
		if cur.Parent == nil && ed.IsLocalConfig() && !ed.HasPluginsArray() {
			// 本地配置根页且尚无 plugins 段：直接新建插件数组 + 首条目
			// （同样延迟写回，编辑条目字段时才保存）。
			ed.AddPluginsArray()
			return
		}
	}
	ed.BeginAddKey()
}

// commitConfigEdit 解析编辑输入并按活动编辑器写回（服务端 config/set 或
// 本地 config.json）。
func (a *App) commitConfigEdit(ed *model.ConfigEditor) {
	patch, ok, errMsg := ed.CommitEdit()
	if !ok {
		a.model.ShowError(i18n.T("编辑失败: %s", errMsg))
		return
	}
	a.applyEditorPatch(ed, patch)
}

// confirmConfigCopyKey confirms a copied key and writes the new entry.
func (a *App) confirmConfigCopyKey(ed *model.ConfigEditor) {
	patch, ok, errMsg := ed.ConfirmCopyKey()
	if !ok {
		if errMsg != "" {
			a.model.ShowError(i18n.T("复制失败: %s", errMsg))
		}
		return
	}
	a.applyEditorPatch(ed, patch)
}

// confirmConfigRenameKey confirms a key rename and writes the replacement patch.
func (a *App) confirmConfigRenameKey(ed *model.ConfigEditor) {
	patch, ok, errMsg := ed.ConfirmRenameKey()
	if !ok {
		if errMsg != "" {
			a.model.ShowError(i18n.T("重命名失败: %s", errMsg))
		}
		return
	}
	a.applyEditorPatch(ed, patch)
}

// confirmConfigAddKey 确认新增 Agent.Agents 键并写回（该键置空对象）。
func (a *App) confirmConfigAddKey(ed *model.ConfigEditor) {
	patch, ok, errMsg := ed.ConfirmAddKey()
	if !ok {
		a.model.ShowError(i18n.T("添加失败: %s", errMsg))
		return
	}
	// 记录新项路径：写回成功后整配置重载并重定向到新项子页。
	a.serverCfgFocus = append([]string(nil), ed.Current().Path...)
	a.applyEditorPatch(ed, patch)
}

// deleteConfigItem 删除当前页面本身（模型项或子代理项），并返回其父页面。
// 对象键以 null 键写回，服务端 config/set 对 map 字段的 null 键真正删除；
// 数组（本地 plugins）整体替换写回。
func (a *App) deleteConfigItem(ed *model.ConfigEditor) {
	patch, ok := ed.DeleteItem()
	if !ok {
		return
	}
	a.applyEditorPatch(ed, patch)
}

func (a *App) saveSettings() {
	if err := config.Save(a.model.Settings); err != nil {
		a.model.ShowError(i18n.T("保存本地配置失败: %s", err.Error()))
		return
	}
	// 语言变更立即生效：之后所有渲染文本按新语言输出。
	i18n.SetLang(i18n.Detect(a.model.Settings.Language))
	a.mode = colorMode(a.model.Settings.ColorMode)
	a.model.ClearError()
}

func (a *App) messageKey(ke input.KeyEvent) {
	m := a.model
	_, h := a.term.Size()
	page := h / 2
	if page < 1 {
		page = 1
	}
	switch {
	case ke.Type == input.KeyUp:
		m.ScrollUp(1)
	case ke.Type == input.KeyDown:
		m.ScrollDown(1)
	case ke.Type == input.KeyPageUp:
		m.ScrollUp(page)
	case ke.Type == input.KeyPageDown:
		m.ScrollDown(page)
	case ke.Type == input.KeyHome:
		m.ScrollTop()
	case ke.Type == input.KeyEnd:
		m.ScrollBottom()
	case ke.Type == input.KeyEnter:
		m.ToggleFocusItem()
	}
}

// dispatchMouse 处理鼠标事件：
//   - 左键：按下/拖拽/释放，维护框选（非模态时），Ctrl+C 复制；
//   - 滚轮：会话视图滚动消息区，首页滚动会话列表，权限/设置模态切换选项。
func (a *App) dispatchMouse(me input.MouseEvent) {
	// 左键用于文本选择；选择不依赖鼠标位置（所见即所得整屏框选）。
	if me.Button == input.MouseLeft {
		a.handleSelect(me)
		return
	}
	if !me.IsWheel() {
		return
	}
	if me.Action != input.MousePress {
		return
	}
	m := a.model
	step := 3
	if me.Mod&input.ModShift != 0 {
		// Shift+wheel 大步滚动（近似 PgUp/PgDn 的一页）。
		_, h := a.term.Size()
		step = h / 2
		if step < 1 {
			step = 1
		}
	}
	switch m.Modal {
	case model.ModalPermission:
		if m.Permission == nil {
			return
		}
		if me.Button == input.MouseWheelUp {
			m.PrevPermissionOption()
		} else if me.Button == input.MouseWheelDown {
			m.NextPermissionOption()
		}
		return
	case model.ModalElicitation:
		if m.Elicitation == nil {
			return
		}
		// 只有 Form 模式才处理滚轮
		if m.Elicitation.Request.Mode == acp.ElicitationModeForm {
			if me.Button == input.MouseWheelUp && m.Elicitation.FieldIndex > 0 {
				m.Elicitation.FieldIndex--
			} else if me.Button == input.MouseWheelDown && m.Elicitation.FieldIndex < len(m.Elicitation.FieldOrder)-1 {
				m.Elicitation.FieldIndex++
			}
		}
		return
	case model.ModalSettings:
		if me.Button == input.MouseWheelUp {
			m.MoveSettings(-1, 3)
		} else if me.Button == input.MouseWheelDown {
			m.MoveSettings(1, 3)
		}
		return
	case model.ModalModel:
		if me.Button == input.MouseWheelUp {
			m.ModelMove(-1)
		} else if me.Button == input.MouseWheelDown {
			m.ModelMove(1)
		}
		return
	case model.ModalServer:
		ed := m.ServerCfg
		if ed == nil || ed.Editing || ed.AddingKey {
			return
		}
		if me.Button == input.MouseWheelUp {
			ed.Move(-1)
		} else if me.Button == input.MouseWheelDown {
			ed.Move(1)
		}
		return
	case model.ModalPlugins:
		ed := m.PluginsCfg
		if ed == nil || ed.Editing || ed.AddingKey {
			return
		}
		if me.Button == input.MouseWheelUp {
			ed.Move(-1)
		} else if me.Button == input.MouseWheelDown {
			ed.Move(1)
		}
		return
	case model.ModalHelp, model.ModalExitConfirm, model.ModalEffort:
		return
	}
	switch m.View {
	case model.ViewHome:
		if m.HomeListFocused {
			if me.Button == input.MouseWheelUp && m.HomeSelected > 0 {
				m.HomeSelected--
			} else if me.Button == input.MouseWheelDown && m.HomeSelected < len(m.Sessions)-1 {
				m.HomeSelected++
			}
		}
	case model.ViewSession:
		if me.Button == input.MouseWheelUp {
			m.ScrollUp(step)
		} else if me.Button == input.MouseWheelDown {
			m.ScrollDown(step)
		}
	}
}

// handleSelect 处理左键按下/拖拽/释放，维护行选择状态。
// 选择只在会话正文区域（BodyRect）内生效；拖出正文时被裁剪到正文边界。
func (a *App) handleSelect(me input.MouseEvent) {
	m := a.model
	if m.Modal != model.NoModal || m.View != model.ViewSession {
		return
	}
	// 终端坐标 1-based → buffer 0-based，并 clamp 到屏幕范围（X/Y 分别处理）。
	x, y := me.X-1, me.Y-1
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	w, h := a.term.Size()
	if w > 0 && x >= w {
		x = w - 1
	}
	if h > 0 && y >= h {
		y = h - 1
	}
	rect := a.view.BodyRect
	switch me.Action {
	case input.MousePress:
		// 只在正文区域内建立选择。
		if rect.H <= 0 || y < rect.Y || y >= rect.Y+rect.H {
			m.Selection = nil
			return
		}
		// 左键点击思考/工具标题行：切换该单项展开/折叠，不进入框选。
		if a.clickBodyToggle(x, y, rect) {
			return
		}
		m.Selection = &model.Selection{AnchorX: x, AnchorY: y, CurX: x, CurY: y}
	case input.MouseMove:
		if m.Selection != nil {
			cy := y
			if cy < rect.Y {
				cy = rect.Y
			}
			if cy >= rect.Y+rect.H {
				cy = rect.Y + rect.H - 1
			}
			m.Selection.CurX, m.Selection.CurY = x, cy
		}
	case input.MouseRelease:
		if m.Selection != nil {
			// 无实际范围视为误点，清掉。
			if m.Selection.AnchorX == m.Selection.CurX && m.Selection.AnchorY == m.Selection.CurY {
				m.Selection = nil
			}
		}
	}
}

// clickBodyToggle 处理鼠标左键点击正文中思考/工具标题行：命中时切换该单项的
// 展开/折叠状态并返回 true（不进入框选）。坐标 x/y 为 0-based buffer 坐标。
func (a *App) clickBodyToggle(x, y int, rect renderer.Rect) bool {
	m := a.model
	if !m.HasActive() {
		return false
	}
	row := y - rect.Y + a.view.BodyScroll
	ref, ok := a.view.BodyToggles[row]
	if !ok {
		return false
	}
	s := m.Active
	switch ref.Kind {
	case view.ToggleThought:
		s.ToggleMessage(ref.ID)
	case view.ToggleTool:
		s.ToggleToolCall(ref.ID)
	default:
		return false
	}
	a.render()
	return true
}

// copySelection 提取当前选择区域的正文原始文本并复制到剪贴板。复制成功后清除选择。
// 无选择或提取为空时返回空串（调用方继续其它 Ctrl+C 语义）。
func (a *App) copySelection() string {
	m := a.model
	if m.Selection == nil {
		return ""
	}
	sel := m.Selection
	m.Selection = nil
	text := a.bodyText(sel)
	if text == "" {
		return ""
	}
	if err := a.term.CopyToClipboard(text); err != nil {
		m.ShowError(i18n.T("复制失败: %s", err.Error()))
		return text
	}
	m.ShowInfo(i18n.T("已复制 %d 个字符", len([]rune(text))))
	return text
}

// lineSelectionBounds 计算行选择下第 y 行覆盖的列区间 [lo, hi]。
// 行选择语义：首行从起点（宽字符对齐后）到行尾，末行从行首到终点，
// 中间行整行；单行从 min 列到 max 列。宽字符不切半：lo 若落在续列则回退
// 到该字符首列，hi 若落在宽字符首列则前进到续列。无区间时返回 hi=-1。
func lineSelectionBounds(buf *renderer.Buffer, sel *model.Selection, y int) (int, int) {
	var lo, hi int
	switch {
	case sel.AnchorY == sel.CurY:
		lo, hi = min(sel.AnchorX, sel.CurX), max(sel.AnchorX, sel.CurX)
	case y == sel.AnchorY:
		if sel.AnchorY < sel.CurY { // 正向：anchor 行在上方，从 anchor 到行尾
			lo, hi = sel.AnchorX, buf.W-1
		} else { // 反向：anchor 行在下方，从行首到 anchor
			lo, hi = 0, sel.AnchorX
		}
	case y == sel.CurY:
		if sel.AnchorY < sel.CurY { // 正向：cur 行在下方，从行首到 cur
			lo, hi = 0, sel.CurX
		} else { // 反向：cur 行在上方，从 cur 到行尾
			lo, hi = sel.CurX, buf.W-1
		}
	default:
		lo, hi = 0, buf.W-1
	}
	if lo < 0 {
		lo = 0
	}
	if hi >= buf.W {
		hi = buf.W - 1
	}
	if hi < 0 || lo > hi {
		return 0, -1
	}
	// 宽字符闭包：lo 落在续列时回退到该宽字符首列。
	for lo > 0 {
		i := buf.Index(lo, y)
		if i < 0 || buf.Cells[i].Width != 0 {
			break
		}
		lo--
	}
	// hi 落在宽字符首列时前进到续列，避免半字反显/复制。
	if hi < buf.W-1 {
		if i := buf.Index(hi, y); i >= 0 && buf.Cells[i].Width == 2 {
			hi++
		}
	}
	return lo, hi
}

// bodyText 返回行选择覆盖的正文原始文本。选择只作用于正文区域（BodyRect），
// 滚动位置通过 BodyScroll 映射到 contentY。
// 消息块走行级插针：取选中渲染行对应的原始 markdown 逻辑行（不保留 "❯"/缩进
// 等渲染前缀），同一逻辑行 wrap 拆分的续行只输出一次；其余块整块输出。
func (a *App) bodyText(sel *model.Selection) string {
	body := a.view.Body
	if len(body) == 0 {
		return ""
	}
	rect := a.view.BodyRect
	if rect.H <= 0 {
		return ""
	}
	y1, y2 := min(sel.AnchorY, sel.CurY), max(sel.AnchorY, sel.CurY)
	if y2 < rect.Y || y1 >= rect.Y+rect.H {
		return ""
	}
	if y1 < rect.Y {
		y1 = rect.Y
	}
	if y2 >= rect.Y+rect.H {
		y2 = rect.Y + rect.H - 1
	}
	c1 := y1 - rect.Y + a.view.BodyScroll
	c2 := y2 - rect.Y + a.view.BodyScroll
	var sb strings.Builder
	for _, blk := range body {
		if blk.End < c1 || blk.Start > c2 {
			continue
		}
		if len(blk.Src) > 0 {
			lo := max(c1, blk.Start)
			hi := min(c2, blk.End)
			prev := ""
			for r := lo; r <= hi; r++ {
				s := blk.Src[r-blk.Start]
				// 长逻辑行 wrap 拆分的续行（First=false）与上一行同源，去重。
				if !s.First && s.Text == prev {
					continue
				}
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(s.Text)
				prev = s.Text
			}
			continue
		}
		// 块级（工具/终端/思考等）：整块输出一次。
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(blk.Raw)
	}
	return sb.String()
}

// expandAll 切换当前会话全部思维链与工具调用的展开状态（Ctrl+O）：
// 全部已展开时再按一次收回（折叠全部），否则展开全部。
func (a *App) expandAll() {
	m := a.model
	if !m.HasActive() {
		return
	}
	s := m.Active
	if s.AllExpanded() && s.HasCollapsible() {
		s.CollapseAll()
		m.ShowInfo(i18n.T("已折叠全部思考与工具调用"))
	} else {
		s.ExpandAll()
		m.ShowInfo(i18n.T("已展开全部思考与工具调用"))
	}
	a.render()
}

// handleCtrlC 处理 Ctrl+C：
//   - 存在选择 → 复制选区文本到剪贴板并清除；
//   - 输入框非空 → 清空输入；
//   - 输入框为空 → 首次提示"再次按
//
// Ctrl+C 退出"，2 秒内再次按下才真正退出。
// 不再承担取消运行任务职责（取消统一由 Esc 承担）。
func (a *App) handleCtrlC() {
	m := a.model
	if a.copySelection() != "" {
		return
	}
	if m.Input != nil && m.Input.Text() != "" {
		m.Input.Clear()
		m.UpdateSlashState()
		a.lastCtrlCAt = time.Time{}
		m.ClearError()
		return
	}
	now := time.Now()
	if !a.lastCtrlCAt.IsZero() && now.Sub(a.lastCtrlCAt) <= 2*time.Second {
		m.Quitting = true
		return
	}
	a.lastCtrlCAt = now
	m.ShowInfo(i18n.T("再次按 Ctrl+C 退出"))
}

func (a *App) permissionKey(ke input.KeyEvent) {
	m := a.model
	reqID := ""
	if m.Permission != nil {
		reqID = m.Permission.RequestID
	}
	switch {
	case ke.Type == input.KeyUp:
		m.PrevPermissionOption()
	case ke.Type == input.KeyDown:
		m.NextPermissionOption()
	case ke.Type == input.KeyEnter:
		outcome, optID := m.ApproveSelection()
		a.respondPermission(reqID, outcome, &optID)
	case ke.Type == input.KeyRune && ke.Rune == 'a':
		if m.SelectPermissionByKind(acp.AllowOnce) || m.SelectPermissionByKind(acp.AllowAlways) {
			outcome, optID := m.ApproveSelection()
			a.respondPermission(reqID, outcome, &optID)
		}
	case ke.Type == input.KeyRune && ke.Rune == 'r':
		if m.SelectPermissionByKind(acp.RejectOnce) || m.SelectPermissionByKind(acp.RejectAlways) {
			outcome, optID := m.ApproveSelection()
			a.respondPermission(reqID, outcome, &optID)
		}
	case ke.Type == input.KeyEsc:
		a.cancelPermission()
	}
}

func (a *App) respondPermission(reqID string, outcome acp.PermissionOutcome, optID *string) {
	if a.sess == nil {
		return
	}
	session := a.sess
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, session.ApprovePermission(ctx, reqID, outcome, optID)
	})
}

func (a *App) cancelPermission() {
	reqID := ""
	if a.model.Permission != nil {
		reqID = a.model.Permission.RequestID
	}
	a.model.CancelPermission()
	if a.sess == nil {
		return
	}
	session := a.sess
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, session.ApprovePermission(ctx, reqID, acp.OutcomeCancelled, nil)
	})
}

func (a *App) submitPrompt() {
	if a.sess == nil || a.model.Input == nil {
		return
	}
	// 先经插件 hooks 裁决（可改写/拦截），通过后才消费输入框；拦截时输入
	// 框保留原文，便于用户修改后重试。
	text := a.model.Input.Text()
	if text == "" {
		return
	}
	out, blocked := a.promptHook(a.sess.ID(), text)
	if blocked {
		return
	}
	if a.model.SubmitInput() == "" {
		return
	}
	// 已裁决的文本直接发送，不再二次触发 hooks（见 sendPromptRaw）。
	a.sendPromptRaw(a.sess, out)
}

func (a *App) cancelCurrent() {
	if a.sess == nil {
		return
	}
	session := a.sess
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, session.Cancel(ctx)
	})
}

func (a *App) createSession() {
	// 用户新建真实会话，主页预创建的空会话已无用，先丢弃（异步删除）。
	a.discardPreSession()
	a.sessionOpID++
	opID := a.sessionOpID
	// 记录本次新建的请求序号：创建成功后若引导里选了 effort 则应用到该会话
	//（恢复旧会话不设置 firstSessionOpID，故不应用）。
	a.firstSessionOpID = opID
	a.startCommand(commandResult{kind: commandSession, opID: opID}, func(ctx context.Context) (acp.Session, error) {
		return a.backend.NewSession(ctx, a.sessionCWD())
	})
}

// usePreSession 复用主页预创建会话作为用户的新会话：不删除、不新建，直接把
// prompt（可空）发送到预创建会话并进入会话视图。返回是否成功复用；无预创建
// 会话时返回 false，调用方应走正常新建流程。
func (a *App) usePreSession(prompt string) bool {
	if a.preSession == nil || a.model.PreSession == nil {
		return false
	}
	s := a.preSession
	a.preSession = nil
	// 提升为活动会话：保留已应用的 config/commands；a.sess 仍指向该会话句柄。
	a.model.ActivateSession(s.ID(), s.Title())
	// 引导里选的 effort 应用到用户第一个会话（仅一次，此后由 /effort 管理）。
	a.applyFirstSessionEffort(s)
	if prompt != "" {
		// 经插件 prompt hooks（可改写/拦截）后发送。
		a.sendPrompt(s, prompt)
	}
	return true
}

func (a *App) resumeSession(id string) {
	// 用户恢复旧会话，主页预创建的空会话已无用，先丢弃（异步删除）。
	a.discardPreSession()
	a.sessionOpID++
	opID := a.sessionOpID
	a.startCommand(commandResult{kind: commandSession, opID: opID}, func(ctx context.Context) (acp.Session, error) {
		return a.backend.ResumeSession(ctx, id)
	})
}

// deleteSession 删除指定会话（首页按 d 触发）。删除在后台执行，成功回调
// applyCommandResult 移除本地列表项并提示。失败保留列表，仅提示错误。
func (a *App) deleteSession(id string) {
	if id == "" {
		return
	}
	if !a.model.SupportsSessionDelete() {
		a.model.ShowError(i18n.T("服务端未声明 session.delete 能力，无法删除会话"))
		return
	}
	a.startCommand(commandResult{kind: commandSessionDelete, sessionID: id}, func(ctx context.Context) (acp.Session, error) {
		return nil, a.backend.DeleteSession(ctx, id)
	})
}

func (a *App) elicitationKey(ke input.KeyEvent) {
	m := a.model
	if m.Elicitation == nil {
		return
	}

	// URL 模式只需要处理确认/取消
	if m.Elicitation.Request.Mode == acp.ElicitationModeURL {
		switch {
		case ke.Type == input.KeyEnter:
			a.respondElicitation(acp.ElicitationActionAccept, nil)
		case ke.Type == input.KeyEsc:
			a.respondElicitation(acp.ElicitationActionCancel, nil)
		case ke.Type == input.KeyRune && ke.Rune == 'd':
			a.respondElicitation(acp.ElicitationActionDecline, nil)
		}
		return
	}

	// Form 模式处理
	switch {
	case ke.Type == input.KeyUp:
		if m.Elicitation.FieldIndex > 0 {
			m.Elicitation.FieldIndex--
		}
	case ke.Type == input.KeyDown:
		if m.Elicitation.FieldIndex < len(m.Elicitation.FieldOrder)-1 {
			m.Elicitation.FieldIndex++
		}
	case ke.Type == input.KeyTab:
		// Tab 切换到下一个字段
		if m.Elicitation.FieldIndex < len(m.Elicitation.FieldOrder)-1 {
			m.Elicitation.FieldIndex++
		} else {
			m.Elicitation.FieldIndex = 0
		}
	case ke.Type == input.KeyEnter && ke.IsCtrl():
		// Ctrl+Enter 提交表单
		if err := a.validateAndSubmitElicitation(); err != nil {
			m.Elicitation.ErrorMessage = err.Error()
		}
	case ke.Type == input.KeyEsc:
		a.respondElicitation(acp.ElicitationActionCancel, nil)
	case ke.Type == input.KeyRune:
		// 输入字符到当前字段
		if m.Elicitation.FieldIndex < len(m.Elicitation.FieldOrder) {
			field := m.Elicitation.FieldOrder[m.Elicitation.FieldIndex]
			if m.ElicitationFormData == nil {
				m.ElicitationFormData = make(map[string]string)
			}
			m.ElicitationFormData[field] += string(ke.Rune)
			m.Elicitation.ErrorMessage = ""
		}
	case ke.Type == input.KeyBackspace:
		// 删除字符
		if m.Elicitation.FieldIndex < len(m.Elicitation.FieldOrder) {
			field := m.Elicitation.FieldOrder[m.Elicitation.FieldIndex]
			if val, ok := m.ElicitationFormData[field]; ok && len(val) > 0 {
				m.ElicitationFormData[field] = val[:len(val)-1]
			}
			m.Elicitation.ErrorMessage = ""
		}
	}
}

func (a *App) validateAndSubmitElicitation() error {
	m := a.model
	if m.Elicitation == nil || m.Elicitation.Request.Mode != acp.ElicitationModeForm {
		return nil
	}

	// 简单验证：检查必需字段
	if required, ok := m.Elicitation.Schema["required"].([]interface{}); ok {
		for _, r := range required {
			if fieldName, ok := r.(string); ok {
				if val, exists := m.ElicitationFormData[fieldName]; !exists || val == "" {
					return fmt.Errorf(i18n.T("字段 %s 是必需的"), fieldName)
				}
			}
		}
	}

	// 验证枚举值
	if props, ok := m.Elicitation.Schema["properties"].(map[string]interface{}); ok {
		for field, value := range m.ElicitationFormData {
			if propSchema, ok := props[field].(map[string]interface{}); ok {
				if enum, ok := propSchema["enum"].([]interface{}); ok && len(enum) > 0 {
					valid := false
					for _, e := range enum {
						if str, ok := e.(string); ok && str == value {
							valid = true
							break
						}
					}
					if !valid {
						return fmt.Errorf(i18n.T("字段 %s 的值必须是枚举值之一"), field)
					}
				}
			}
		}
	}

	// 序列化表单数据
	content, err := json.Marshal(m.ElicitationFormData)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("序列化表单数据失败"), err)
	}

	a.respondElicitation(acp.ElicitationActionAccept, content)
	return nil
}

func (a *App) respondElicitation(action acp.ElicitationAction, content json.RawMessage) {
	if a.sess == nil || a.model.Elicitation == nil {
		return
	}

	rpcID := a.model.ElicitationRPCID
	session := a.sess

	a.model.AdvanceElicitationQueue()

	if rpcID != nil {
		a.startCommand(commandResult{kind: commandSessionAction, sessionID: session.ID()}, func(ctx context.Context) (acp.Session, error) {
			return nil, session.RespondElicitation(ctx, rpcID, action, content)
		})
	}
}
