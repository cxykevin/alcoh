package app

import (
	"context"
	"encoding/json"
	"errors"
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

// connectSelectKey 模型选择步骤：↑↓ 选择、Enter 确认写入（所选模型按规则自动
// 计算压缩阈值，见 connectCompressForLimit；未公布上下文长度进入手动输入步骤）、
// Esc 返回表单。
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
			if picked := cs.Models[cs.ModelSel]; picked.ID == "deepseek-v4-flash" {
				// deepseek-v4-flash：上下文已知 1M，压缩阈值固定 140000，
				// 全自动无需手动输入。
				a.submitConnectModel(picked, 1000000, 140000)
			} else if picked := cs.Models[cs.ModelSel]; picked.TokenLimit > 0 {
				// 拉取到上下文长度：按规则自动计算压缩阈值（见 connectCompressForLimit）。
				a.submitConnectModel(picked, picked.TokenLimit, connectCompressForLimit(picked, picked.TokenLimit))
			} else {
				// 未拉取到：手动输入上下文长度与压缩阈值。
				cs.Step = model.ConnectStepManual
				cs.ManualTokenLimit = ""
				cs.ManualCompress = ""
				cs.ManualCompressTouched = false
				cs.ManualError = ""
			}
		}
	case input.KeyEsc:
		cs.Step = model.ConnectStepForm
	}
}

// connectCompressForLimit 按规则计算模型的自动压缩阈值：
//   - deepseek-v4-flash：固定 140000（保持原值）；
//   - 模型名（ID/Name）包含 "gemini"：80000；
//   - 上下文长度 >= 1M 且模型名不含 "claude"：200000；
//   - 其余：上下文长度的 80%。
func connectCompressForLimit(p provider.Model, tokenLimit int) int {
	if p.ID == "deepseek-v4-flash" {
		return 140000
	}
	name := strings.ToLower(p.ID + p.Name)
	if strings.Contains(name, "gemini") {
		return 80000
	}
	if tokenLimit >= 1000000 && !strings.Contains(name, "claude") {
		return 200000
	}
	return tokenLimit * 80 / 100
}

// connectManualKey 手动输入步骤：↑↓/Tab 切换字段（0=上下文长度 1=压缩阈值）、
// 输入数字、退格删除、Enter 提交、Esc 返回模型选择。压缩阈值未手动修改过时
// 随上下文长度按规则联动预填（见 connectCompressForLimit）。
func (a *App) connectManualKey(ke input.KeyEvent, cs *model.ConnectState) {
	switch {
	case ke.Type == input.KeyEsc:
		cs.Step = model.ConnectStepSelect
		cs.ManualError = ""
	case ke.Type == input.KeyUp, ke.Type == input.KeyDown, ke.Type == input.KeyTab:
		cs.ManualFocus = 1 - cs.ManualFocus
	case ke.Type == input.KeyEnter:
		a.submitConnectManualInputs()
	case ke.Type == input.KeyBackspace:
		a.manualBackspace(cs)
	case ke.Type == input.KeyRune:
		if cs.ManualFocus == 0 {
			cs.ManualTokenLimit += string(ke.Rune)
			a.manualSyncCompress(cs)
		} else {
			// 压缩阈值字段首次输入：清空联动预填值，以用户输入为准。
			if !cs.ManualCompressTouched {
				cs.ManualCompress = ""
				cs.ManualCompressTouched = true
			}
			cs.ManualCompress += string(ke.Rune)
		}
		cs.ManualError = ""
	}
}

// manualBackspace 删除当前聚焦字段的最后一个字符（压缩阈值字段标记为已手动
// 修改，停止联动）。
func (a *App) manualBackspace(cs *model.ConnectState) {
	if cs.ManualFocus == 0 {
		if len(cs.ManualTokenLimit) > 0 {
			cs.ManualTokenLimit = cs.ManualTokenLimit[:len(cs.ManualTokenLimit)-1]
			a.manualSyncCompress(cs)
		}
	} else {
		if len(cs.ManualCompress) > 0 {
			cs.ManualCompress = cs.ManualCompress[:len(cs.ManualCompress)-1]
		}
		cs.ManualCompressTouched = true
	}
	cs.ManualError = ""
}

// manualSyncCompress 在上下文长度变化时按规则联动压缩阈值（见
// connectCompressForLimit）；压缩阈值已被手动修改过或上下文长度非法时不联动。
func (a *App) manualSyncCompress(cs *model.ConnectState) {
	if cs.ManualCompressTouched {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(cs.ManualTokenLimit))
	if err != nil || n <= 0 {
		cs.ManualCompress = ""
		return
	}
	if cs.ModelSel >= 0 && cs.ModelSel < len(cs.Models) {
		cs.ManualCompress = strconv.Itoa(connectCompressForLimit(cs.Models[cs.ModelSel], n))
	}
}

// submitConnectManualInputs 校验手动输入的上下文长度与压缩阈值（均需正整数）
// 并提交写入。
func (a *App) submitConnectManualInputs() {
	cs := a.model.Connect
	if cs == nil || cs.ModelSel < 0 || cs.ModelSel >= len(cs.Models) {
		return
	}
	tl, err := strconv.Atoi(strings.TrimSpace(cs.ManualTokenLimit))
	if err != nil || tl <= 0 {
		cs.ManualError = i18n.T("上下文长度需为正整数")
		return
	}
	compress, err := strconv.Atoi(strings.TrimSpace(cs.ManualCompress))
	if err != nil || compress <= 0 {
		cs.ManualError = i18n.T("压缩阈值需为正整数")
		return
	}
	a.submitConnectModel(cs.Models[cs.ModelSel], tl, compress)
}

// ensureCompressForModel 在切换模型后生效的压缩阈值规则：模型为
// deepseek-v4-flash 时，静默把服务端配置中该模型的 CompressSize 更新为
// 140000。模型不在服务端配置中或写回失败时静默跳过，不打扰用户。
func (a *App) ensureCompressForModel(modelID string) {
	if modelID != "deepseek-v4-flash" {
		return
	}
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Add(1)
	go func() {
		defer a.commandWG.Done()
		_ = a.applyCompressForModel(ctx, modelID, 140000)
	}()
}

// applyCompressForModel 定位服务端配置中 ModelID 匹配的模型并写回其
// CompressSize。模型不存在时返回错误。
func (a *App) applyCompressForModel(ctx context.Context, modelID string, compress int) error {
	cfg, err := a.backend.GetConfig(ctx)
	if err != nil {
		return err
	}
	key, ok := model.FindModelKeyByID(cfg, modelID)
	if !ok {
		return errors.New("model not found in server config")
	}
	patch := map[string]any{
		"Model": map[string]any{
			"Models": map[string]any{
				key: map[string]any{"CompressSize": compress},
			},
		},
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return a.backend.SetConfig(ctx, b)
}

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
