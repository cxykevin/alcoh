package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/provider"
)

// openConnect 打开 /connect 向导。
func (a *App) openConnect() {
	a.model.OpenConnect()
}

// connectKey 处理 /connect 向导按键：按步骤分发。
func (a *App) connectKey(ke input.KeyEvent) {
	m := a.model
	cs := m.Connect
	if cs == nil {
		return
	}
	// 向导中也允许 Ctrl+q 退出（走退出确认弹窗）。
	if ke.IsCtrl() && ke.Rune == 'q' {
		m.SetModal(model.ModalExitConfirm)
		return
	}
	switch cs.Step {
	case model.ConnectStepProvider:
		a.connectProviderKey(ke, cs)
	case model.ConnectStepForm:
		a.connectFormKey(ke, cs)
	case model.ConnectStepSelect:
		a.connectSelectKey(ke, cs)
	case model.ConnectStepManual:
		a.connectManualKey(ke, cs)
	case model.ConnectStepDone:
		a.connectDoneKey(ke, cs)
	}
}

// connectDoneKey 完成步骤：Enter 关闭；向导由新手引导触发时，Enter 进入引导
// 剩余步骤（选推理强度），Esc 跳过引导直接完成。
func (a *App) connectDoneKey(ke input.KeyEvent, cs *model.ConnectState) {
	if ke.Type != input.KeyEnter && ke.Type != input.KeyEsc {
		return
	}
	if !cs.FromOnboarding {
		a.model.CloseConnect()
		return
	}
	if ke.Type == input.KeyEnter {
		a.model.CloseConnect()
		a.model.OpenOnboardingEffort()
		return
	}
	a.finishOnboarding()
}

// connectProviderKey 服务商模板步骤：↑↓ 选择、Enter 预填 base_url 进入表单、
// Esc 取消（引导触发时 Esc 跳过整个引导）。
func (a *App) connectProviderKey(ke input.KeyEvent, cs *model.ConnectState) {
	templates := model.ConnectTemplates()
	switch ke.Type {
	case input.KeyUp:
		if cs.ProviderSel > 0 {
			cs.ProviderSel--
		}
	case input.KeyDown:
		if cs.ProviderSel < len(templates)-1 {
			cs.ProviderSel++
		}
	case input.KeyEnter:
		if cs.ProviderSel >= 0 && cs.ProviderSel < len(templates) {
			cs.ConnectSetForm(templates[cs.ProviderSel].BaseURL)
			cs.Step = model.ConnectStepForm
		}
	case input.KeyEsc:
		if cs.FromOnboarding {
			a.finishOnboarding()
			return
		}
		a.model.CloseConnect()
	}
}

// connectFormKey base_url/key 表单步骤：↑↓/Tab 切换字段、输入字符、退格删除、
// Enter 提交拉取模型、Esc 返回服务商步骤。拉取中阻塞编辑。
func (a *App) connectFormKey(ke input.KeyEvent, cs *model.ConnectState) {
	if cs.Fetching {
		return
	}
	switch {
	case ke.Type == input.KeyEsc:
		cs.Step = model.ConnectStepProvider
		cs.FormError = ""
	case ke.Type == input.KeyUp, ke.Type == input.KeyDown, ke.Type == input.KeyTab:
		cs.FormFocus = 1 - cs.FormFocus
	case ke.Type == input.KeyEnter:
		a.submitConnectForm()
	case ke.Type == input.KeyBackspace:
		a.connectBackspace(cs)
	case ke.Type == input.KeyRune:
		if cs.FormFocus == 0 {
			cs.BaseURL += string(ke.Rune)
		} else {
			cs.Key += string(ke.Rune)
		}
		cs.FormError = ""
	}
}

// connectBackspace 删除当前聚焦字段的最后一个字符。
func (a *App) connectBackspace(cs *model.ConnectState) {
	if cs.FormFocus == 0 {
		if len(cs.BaseURL) > 0 {
			cs.BaseURL = cs.BaseURL[:len(cs.BaseURL)-1]
		}
	} else {
		if len(cs.Key) > 0 {
			cs.Key = cs.Key[:len(cs.Key)-1]
		}
	}
	cs.FormError = ""
}

// submitConnectForm 校验表单并发起模型列表拉取（后台 goroutine，结果经
// applyCommandResult 的 commandConnectFetch 回填）。
func (a *App) submitConnectForm() {
	cs := a.model.Connect
	if cs == nil {
		return
	}
	baseURL := strings.TrimSpace(cs.BaseURL)
	key := strings.TrimSpace(cs.Key)
	if baseURL == "" {
		cs.FormError = i18n.T("base_url 不能为空")
		return
	}
	if key == "" {
		cs.FormError = i18n.T("API key 不能为空")
		return
	}
	cs.Fetching = true
	cs.FormError = ""
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Add(1)
	go func() {
		defer a.commandWG.Done()
		models, err := provider.FetchModels(ctx, baseURL, key)
		select {
		case a.commands <- commandResult{kind: commandConnectFetch, models: models, err: err}:
		case <-ctx.Done():
		}
	}()
}

// connectSelectKey 模型选择步骤：↑↓ 选择、Enter 确认写入（所选模型公布上下文
// 长度时自动设压缩阈值=50%，否则进入手动输入步骤）、Esc 返回表单。
func (a *App) connectSelectKey(ke input.KeyEvent, cs *model.ConnectState) {
	switch ke.Type {
	case input.KeyUp:
		if cs.ModelSel > 0 {
			cs.ModelSel--
		}
	case input.KeyDown:
		if cs.ModelSel < len(cs.Models)-1 {
			cs.ModelSel++
		}
	case input.KeyEnter:
		if cs.ModelSel >= 0 && cs.ModelSel < len(cs.Models) {
			if picked := cs.Models[cs.ModelSel]; picked.TokenLimit > 0 {
				// 拉取到上下文长度：压缩阈值自动取其 50%。
				a.submitConnectModel(picked, picked.TokenLimit, picked.TokenLimit/2)
			} else {
				// 未拉取到：手动设置压缩阈值。
				cs.Step = model.ConnectStepManual
				cs.ManualCompress = ""
				cs.ManualError = ""
			}
		}
	case input.KeyEsc:
		cs.Step = model.ConnectStepForm
	}
}

// connectManualKey 手动设置压缩阈值步骤：输入数字、退格删除、Enter 提交、
// Esc 返回模型选择。
func (a *App) connectManualKey(ke input.KeyEvent, cs *model.ConnectState) {
	switch {
	case ke.Type == input.KeyEsc:
		cs.Step = model.ConnectStepSelect
		cs.ManualError = ""
	case ke.Type == input.KeyEnter:
		a.submitConnectManualCompress()
	case ke.Type == input.KeyBackspace:
		if len(cs.ManualCompress) > 0 {
			cs.ManualCompress = cs.ManualCompress[:len(cs.ManualCompress)-1]
		}
		cs.ManualError = ""
	case ke.Type == input.KeyRune:
		cs.ManualCompress += string(ke.Rune)
		cs.ManualError = ""
	}
}

// submitConnectManualCompress 校验手动输入的压缩阈值（正整数）并提交写入；
// TokenLimit 未拉取到，使用默认 128000。
func (a *App) submitConnectManualCompress() {
	cs := a.model.Connect
	if cs == nil || cs.ModelSel < 0 || cs.ModelSel >= len(cs.Models) {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(cs.ManualCompress))
	if err != nil || n <= 0 {
		cs.ManualError = i18n.T("压缩阈值需为正整数")
		return
	}
	a.submitConnectModel(cs.Models[cs.ModelSel], 128000, n)
}

// submitConnectModel 把选中的模型写入服务端配置：先 config/get 计算下一个
// 模型键，再 config/set 写入并设为默认（后台 goroutine，结果经
// applyCommandResult 的 commandConnectSubmit 回填）。
func (a *App) submitConnectModel(picked provider.Model, tokenLimit, compressSize int) {
	cs := a.model.Connect
	if cs == nil {
		return
	}
	baseURL := strings.TrimSpace(cs.BaseURL)
	key := strings.TrimSpace(cs.Key)
	a.startCommand(commandResult{kind: commandConnectSubmit}, func(ctx context.Context) (acp.Session, error) {
		cfg, err := a.backend.GetConfig(ctx)
		if err != nil {
			return nil, err
		}
		patch, err := model.ConnectModelPatch(cfg, picked, baseURL, key, tokenLimit, compressSize)
		if err != nil {
			return nil, err
		}
		return nil, a.backend.SetConfig(ctx, patch)
	})
}
