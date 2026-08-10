package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/provider"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// drawConnect 渲染 /connect 向导并返回各行文本。
func drawConnect(cs *model.ConnectState, w, h int) []string {
	b := renderer.NewBuffer(w, h)
	canv := renderer.NewCanvas(b)
	cc := &ConnectContent{Theme: renderer.DefaultTheme(), Cs: cs}
	cc.Draw(canv, renderer.NewRect(0, 0, w, h))
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = effortRowText(b, y, w)
	}
	return rows
}

// TestConnectContentDraw 验证各步骤绘制：标题、模板列表、表单（密钥掩码）、
// 模型列表与完成页都在屏幕上正确出现，且任意步骤都不 panic。
func TestConnectContentDraw(t *testing.T) {
	const w, h = 80, 20

	steps := []struct {
		name string
		cs   *model.ConnectState
		want []string
	}{
		{"provider", &model.ConnectState{Step: model.ConnectStepProvider, ProviderSel: 1}, []string{"连接模型服务商", "OpenAI"}},
		{"form-masked", &model.ConnectState{
			Step:     model.ConnectStepForm,
			BaseURL:  "https://api.deepseek.com/v1",
			Key:      "sk-very-secret-key",
			FormFocus: 1,
		}, []string{"base_url", "sk-v**************"}},
		{"form-fetching", &model.ConnectState{Step: model.ConnectStepForm, Fetching: true}, []string{"正在获取模型列表"}},
		{"select", &model.ConnectState{
			Step:    model.ConnectStepSelect,
			ModelSel: 1,
			Models:  []provider.Model{{ID: "deepseek-chat"}, {ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", TokenLimit: 65536}},
		}, []string{"deepseek-reasoner", "65536"}},
		{"done", &model.ConnectState{Step: model.ConnectStepDone, Result: "模型已添加并设为默认模型"}, []string{"模型已添加并设为默认模型", "Enter / Esc 关闭"}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			rows := drawConnect(s.cs, w, h)
			joined := strings.Join(rows, "\n")
			for _, want := range s.want {
				if !strings.Contains(joined, want) {
					t.Errorf("missing %q in render:\n%s", want, joined)
				}
			}
		})
	}
}
