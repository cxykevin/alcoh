package model

import (
	"encoding/json"
	"testing"
)

// TestHasConfiguredModels 验证新手引导触发判断：config 的 Model.Models 非空才算
// 已配置模型；缺失/为空/畸形 JSON 均视为无模型。
func TestHasConfiguredModels(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "empty", raw: `{}`, want: false},
		{name: "no model key", raw: `{"Agent":{}}`, want: false},
		{name: "model without models", raw: `{"Model":{"DefaultModelID":"0"}}`, want: false},
		{name: "empty models", raw: `{"Model":{"Models":{}}}`, want: false},
		{name: "one model", raw: `{"Model":{"Models":{"0":{"ModelName":"m"}}}}`, want: true},
		{name: "malformed", raw: `{`, want: false},
		{name: "nil", raw: ``, want: false},
	}
	for _, c := range cases {
		if got := HasConfiguredModels(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("%s: HasConfiguredModels = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestOnboardingEffortOpenClose 验证 /connect 完成后进入引导剩余步骤的
// Modal 与状态：从 effort 步骤开始，关闭后回到无模态。
func TestOnboardingEffortOpenClose(t *testing.T) {
	m := New()
	m.OpenOnboardingEffort()
	if m.Modal != ModalOnboarding {
		t.Fatalf("Modal = %v, want ModalOnboarding", m.Modal)
	}
	if m.Onboarding == nil {
		t.Fatal("Onboarding should be set")
	}
	if m.Onboarding.Step != OnboardStepEffort {
		t.Fatalf("Step = %v, want effort", m.Onboarding.Step)
	}
	m.CloseOnboarding()
	if m.Onboarding != nil {
		t.Error("Onboarding should be nil after close")
	}
	if m.Modal != NoModal {
		t.Errorf("Modal = %v, want NoModal after close", m.Modal)
	}
}

// TestOnboardingEffortCandidates 验证 effort 候选非空且不含 unset。
func TestOnboardingEffortCandidates(t *testing.T) {
	if len(OnboardEffortCandidates) < 4 {
		t.Errorf("effort candidates = %d, want >= 4", len(OnboardEffortCandidates))
	}
	for _, c := range OnboardEffortCandidates {
		if c == "unset" {
			t.Error("effort candidates should not include unset")
		}
	}
}
