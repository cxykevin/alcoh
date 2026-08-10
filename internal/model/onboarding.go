package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cxykevin/alcoh/internal/i18n"
)

// OnboardingStep 是新手指南的步骤。
type OnboardingStep int

const (
	// OnboardStepWelcome 欢迎页：说明本次引导要做什么。
	OnboardStepWelcome OnboardingStep = iota
	// OnboardStepProvider 选择预设服务商（自动预填 ProviderURL）。
	OnboardStepProvider
	// OnboardStepForm 模型表单：六个字段全部必填，提交即写回 Model.Models.<0>。
	OnboardStepForm
	// OnboardStepResult 提交结果页：可选中按钮「打开 /server 详细配置」或「下一步」。
	OnboardStepResult
	// OnboardStepEffort 选择用户第一个会话的推理强度（thought_level）。
	OnboardStepEffort
	// OnboardStepTeaching 基本操作教学。
	OnboardStepTeaching
	// OnboardStepDone 引导完成（过渡用，不渲染）。
	OnboardStepDone
)

// OnboardProvider 是引导里可选的预设服务商。
type OnboardProvider struct {
	Name string
	URL  string
}

// OnboardFields 是模型表单的字段定义（顺序即表单展示与写回顺序）。
type OnboardField struct {
	Key    string // 写回服务端 config 的字段名
	Label  string // 界面显示名
	Secret bool   // 是否掩码显示（密钥）
	Number bool   // 是否为数字字段
}

// onboardProviders 是引导预设的服务商（选中后自动预填 ProviderURL）。
// 末尾的"自定义"URL 为空：不预填，由用户在表单里自行填写提供方 URL。
var onboardProviders = []OnboardProvider{
	{Name: "Deepseek", URL: "https://api.deepseek.com"},
	{Name: "OpenAI", URL: "https://api.openai.com/v1"},
	{Name: "S3AI Api", URL: "https://ai.furry.vg/v1"}, // 基于 newapi
	{Name: "自定义", URL: ""},
}

// onboardFormFields 是模型表单字段（六项全部必填）。
var onboardFormFields = []OnboardField{
	{Key: "ProviderURL", Label: "提供方 URL"},
	{Key: "ProviderKey", Label: "提供方密钥", Secret: true},
	{Key: "ModelName", Label: "模型名称"},
	{Key: "ModelID", Label: "模型 ID"},
	{Key: "TokenLimit", Label: "Token 上限", Number: true},
	{Key: "CompressSize", Label: "压缩阈值", Number: true},
}

// OnboardEffortCandidates 是引导里可选的首会话推理强度（不含 unset）。
var OnboardEffortCandidates = []string{"low", "medium", "high", "xhigh", "max"}

// OnboardingState 保存全屏新手指南的状态。
type OnboardingState struct {
	Step          OnboardingStep
	ProviderSel   int      // 服务商列表选中索引
	FormValues    []string // 与 OnboardFields 顺序一致
	FormFocus     int      // 表单当前输入字段索引
	FormError     string   // 表单校验/提交错误
	FormSubmitting bool    // 表单提交中（阻塞编辑）
	ResultSel     int      // 结果页按钮选中：0=打开 /server，1=下一步
	EffortSel     int      // effort 候选选中索引
}

// OpenOnboarding 进入全屏新手指南并重置状态。
func (m *AppModel) OpenOnboarding() {
	m.Onboarding = &OnboardingState{
		Step:       OnboardStepWelcome,
		FormValues: make([]string, len(onboardFormFields)),
		FormFocus:  0,
	}
	m.CloseSlash()
	m.ClearError()
	m.SetModal(ModalOnboarding)
}

// CloseOnboarding 关闭新手指南（返回主页）。不在此处触发主页预创建会话——
// 由 app 层在引导结束时调用 goHome 创建。
func (m *AppModel) CloseOnboarding() {
	m.Onboarding = nil
	m.SetModal(NoModal)
}

// OnboardFields 返回表单字段定义副本。
func OnboardFields() []OnboardField {
	return append([]OnboardField(nil), onboardFormFields...)
}

// OnboardProviders 返回预设服务商副本。
func OnboardProviders() []OnboardProvider {
	return append([]OnboardProvider(nil), onboardProviders...)
}

// SelectedProvider 返回当前选中的预设服务商。
func (ob *OnboardingState) SelectedProvider() (OnboardProvider, bool) {
	if ob.ProviderSel < 0 || ob.ProviderSel >= len(onboardProviders) {
		return OnboardProvider{}, false
	}
	return onboardProviders[ob.ProviderSel], true
}

// SetFormProvider 把服务商 URL 预填到表单第一个字段（提供方 URL）。
func (ob *OnboardingState) SetFormProvider(url string) {
	if url == "" || len(ob.FormValues) == 0 {
		return
	}
	ob.FormValues[0] = url
}

// ValidateForm 校验模型表单：六项全部必填，数字字段必须是正整数。
func (ob *OnboardingState) ValidateForm() error {
	for i, f := range onboardFormFields {
		v := strings.TrimSpace(ob.FormValues[i])
		if v == "" {
			return fmt.Errorf(i18n.T("字段「%s」是必需的"), f.Label)
		}
		if f.Number {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf(i18n.T("字段「%s」必须是正整数"), f.Label)
			}
		}
	}
	return nil
}

// ModelPatch 返回把表单写回服务端 Model.Models.<0> 并设为默认模型（DefaultModelID=0，
// 即该模型在 Models map 中的数字键；服务端该字段为数值类型，必须发数字而非字符串）
// 的 config/set 部分更新 patch。
func (ob *OnboardingState) ModelPatch() json.RawMessage {
	vals := map[string]any{}
	for i, f := range onboardFormFields {
		v := strings.TrimSpace(ob.FormValues[i])
		if f.Number {
			if n, err := strconv.Atoi(v); err == nil {
				vals[f.Key] = n
			}
		} else {
			vals[f.Key] = v
		}
	}
	patch := map[string]any{
		"Model": map[string]any{
			"Models":        map[string]any{"0": vals},
			"DefaultModelID": 0,
		},
	}
	b, _ := json.Marshal(patch)
	return b
}

// HasConfiguredModels 报告 config/get 返回的完整服务端配置中是否至少配置了一个模型
// （Model.Models 非空）。解析失败或模型集合缺失/为空时返回 false——用于新手引导的
// 触发判断：服务端尚无任何模型时才进入引导。
func HasConfiguredModels(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var cfg struct {
		Model *struct {
			Models map[string]json.RawMessage `json:"Models"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return false
	}
	return cfg.Model != nil && len(cfg.Model.Models) > 0
}
