package model

import (
	"encoding/json"
)

// OnboardingStep 是新手引导的步骤。引导与 /connect 向导同义：服务端无模型
// 时启动直接进入 /connect（ConnectState.FromOnboarding），完成模型配置后
// 才进入本状态机的剩余步骤（选推理强度 → 操作教学）。
type OnboardingStep int

const (
	// OnboardStepEffort 选择用户第一个会话的推理强度（thought_level）。
	OnboardStepEffort OnboardingStep = iota
	// OnboardStepTeaching 基本操作教学。
	OnboardStepTeaching
	// OnboardStepDone 引导完成（过渡用，不渲染）。
	OnboardStepDone
)

// OnboardEffortCandidates 是引导里可选的首会话推理强度（不含 unset）。
var OnboardEffortCandidates = []string{"low", "medium", "high", "xhigh", "max"}

// OnboardingState 保存引导剩余步骤的状态（模型配置由 /connect 向导完成）。
type OnboardingState struct {
	Step      OnboardingStep
	EffortSel int // effort 候选选中索引
}

// OpenOnboardingEffort 在 /connect 完成模型配置后进入引导的推理强度步骤。
func (m *AppModel) OpenOnboardingEffort() {
	m.Onboarding = &OnboardingState{Step: OnboardStepEffort}
	m.CloseSlash()
	m.ClearError()
	m.SetModal(ModalOnboarding)
}

// CloseOnboarding 关闭新手引导（返回主页）。不在此处触发主页预创建会话——
// 由 app 层在引导结束时调用 goHome 创建。
func (m *AppModel) CloseOnboarding() {
	m.Onboarding = nil
	m.SetModal(NoModal)
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
