package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// setThreshold 修改默认模型的压缩阈值（带参路径）：config/get 定位默认模型后
// config/set 写回 CompressSize。后台执行，结果经 applyCommandResult 处理。
func (a *App) setThreshold(value int) {
	a.startCommand(commandResult{kind: commandThresholdSet}, func(ctx context.Context) (acp.Session, error) {
		return nil, a.applyThreshold(ctx, value)
	})
}

// applyThreshold 定位默认模型并写回其 CompressSize；返回错误时配置未改动。
func (a *App) applyThreshold(ctx context.Context, value int) error {
	cfg, err := a.backend.GetConfig(ctx)
	if err != nil {
		return err
	}
	info, err := model.ThresholdTarget(cfg)
	if err != nil {
		return err
	}
	patch := map[string]any{
		"Model": map[string]any{
			"Models": map[string]any{
				info.Key: map[string]any{"CompressSize": value},
			},
		},
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return a.backend.SetConfig(ctx, b)
}

// openThreshold 打开 /threshold 弹窗并异步拉取当前默认模型的压缩阈值预填。
func (a *App) openThreshold() {
	a.model.OpenThreshold()
	a.startThresholdGet()
}

// startThresholdGet 异步拉取默认模型的当前压缩阈值，结果经 applyCommandResult
// 的 commandThresholdGet 预填输入框。
func (a *App) startThresholdGet() {
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Add(1)
	go func() {
		defer a.commandWG.Done()
		info, err := func() (*model.ThresholdInfo, error) {
			cfg, err := a.backend.GetConfig(ctx)
			if err != nil {
				return nil, err
			}
			return model.ThresholdTarget(cfg)
		}()
		select {
		case a.commands <- commandResult{kind: commandThresholdGet, threshold: info, err: err}:
		case <-ctx.Done():
		}
	}()
}

// thresholdKey 处理 /threshold 弹窗按键：输入数字、退格删除、Enter 提交、
// Esc 取消。拉取当前值期间仅 Esc 可关闭。弹窗中也允许 Ctrl+q 退出确认。
func (a *App) thresholdKey(ke input.KeyEvent) {
	m := a.model
	ts := m.Threshold
	if ts == nil {
		return
	}
	if ke.IsCtrl() && ke.Rune == 'q' {
		m.SetModal(model.ModalExitConfirm)
		return
	}
	if ts.Loading {
		if ke.Type == input.KeyEsc {
			m.CloseThreshold()
		}
		return
	}
	switch {
	case ke.Type == input.KeyEsc:
		m.CloseThreshold()
	case ke.Type == input.KeyEnter:
		a.submitThreshold()
	case ke.Type == input.KeyBackspace:
		if len(ts.Input) > 0 {
			ts.Input = ts.Input[:len(ts.Input)-1]
		}
		ts.Error = ""
	case ke.Type == input.KeyRune:
		ts.Input += string(ke.Rune)
		ts.Error = ""
	}
}

// submitThreshold 校验弹窗输入（正整数）并写回压缩阈值。
func (a *App) submitThreshold() {
	ts := a.model.Threshold
	if ts == nil || ts.ModelKey == "" {
		ts.Error = i18n.T("目标模型未定位，请重试")
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(ts.Input))
	if err != nil || n <= 0 {
		ts.Error = i18n.T("压缩阈值需为正整数")
		return
	}
	a.setThreshold(n)
}
