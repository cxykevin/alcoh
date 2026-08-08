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

// TestOnboardingOpenClose 验证进入/退出引导的 Modal 与状态。
func TestOnboardingOpenClose(t *testing.T) {
	m := New()
	m.OpenOnboarding()
	if m.Modal != ModalOnboarding {
		t.Fatalf("Modal = %v, want ModalOnboarding", m.Modal)
	}
	if m.Onboarding == nil {
		t.Fatal("Onboarding should be set")
	}
	if m.Onboarding.Step != OnboardStepWelcome {
		t.Fatalf("Step = %v, want welcome", m.Onboarding.Step)
	}
	if len(m.Onboarding.FormValues) != 6 {
		t.Errorf("form fields = %d, want 6", len(m.Onboarding.FormValues))
	}
	m.CloseOnboarding()
	if m.Onboarding != nil {
		t.Error("Onboarding should be nil after close")
	}
	if m.Modal != NoModal {
		t.Errorf("Modal = %v, want NoModal after close", m.Modal)
	}
}

// TestOnboardingValidateForm 验证表单校验：全部必填、数字字段必须为正整数。
func TestOnboardingValidateForm(t *testing.T) {
	ob := &OnboardingState{FormValues: []string{"url", "", "", "", "", ""}}
	if err := ob.ValidateForm(); err == nil {
		t.Fatal("expected error with empty ProviderKey")
	}
	ob.FormValues = []string{"url", "key", "m", "id", "8192", "128000"}
	if err := ob.ValidateForm(); err != nil {
		t.Fatalf("valid form should pass: %v", err)
	}
	ob.FormValues[4] = "abc"
	if err := ob.ValidateForm(); err == nil {
		t.Fatal("expected error for non-numeric TokenLimit")
	}
	ob.FormValues[4] = "0"
	if err := ob.ValidateForm(); err == nil {
		t.Fatal("expected error for zero TokenLimit")
	}
	ob.FormValues = []string{"url", "key", "m", "id", "8192", "-1"}
	if err := ob.ValidateForm(); err == nil {
		t.Fatal("expected error for negative CompressSize")
	}
}

// TestOnboardingModelPatch 验证表单写回 patch：新模型写入 Model.Models.<0>、
// DefaultModelID 指向该 map 的数字键 0（数值类型，非 ModelID 值）、数字字段为数值。
func TestOnboardingModelPatch(t *testing.T) {
	ob := &OnboardingState{FormValues: []string{
		"https://api.deepseek.com", "sk-123", "deepseek-chat", "deepseek-chat", "8192", "128000",
	}}
	patch := ob.ModelPatch()
	var got map[string]any
	if err := json.Unmarshal(patch, &got); err != nil {
		t.Fatal(err)
	}
	modelObj, ok := got["Model"].(map[string]any)
	if !ok {
		t.Fatalf("patch missing Model: %s", patch)
	}
	if def := modelObj["DefaultModelID"]; def != float64(0) {
		t.Errorf("DefaultModelID = %v, want 0 (Models map key, numeric type)", def)
	}
	models, ok := modelObj["Models"].(map[string]any)
	if !ok {
		t.Fatalf("patch missing Models: %s", patch)
	}
	m0, ok := models["0"].(map[string]any)
	if !ok {
		t.Fatalf("patch missing Models.0: %s", patch)
	}
	if m0["ProviderURL"] != "https://api.deepseek.com" || m0["ProviderKey"] != "sk-123" {
		t.Errorf("provider fields = %v", m0)
	}
	if m0["ModelName"] != "deepseek-chat" || m0["ModelID"] != "deepseek-chat" {
		t.Errorf("model identity fields = %v", m0)
	}
	if n, ok := m0["TokenLimit"].(float64); !ok || n != 8192 {
		t.Errorf("TokenLimit = %v (%T), want number 8192", m0["TokenLimit"], m0["TokenLimit"])
	}
	if n, ok := m0["CompressSize"].(float64); !ok || n != 128000 {
		t.Errorf("CompressSize = %v, want number 128000", m0["CompressSize"])
	}
}

// TestOnboardingSetFormProvider 验证选服务商后预填表单第一个字段（提供方 URL）。
func TestOnboardingSetFormProvider(t *testing.T) {
	ob := &OnboardingState{FormValues: make([]string, 6)}
	ob.SetFormProvider("https://api.openai.com/v1")
	if ob.FormValues[0] != "https://api.openai.com/v1" {
		t.Errorf("FormValues[0] = %q, want pre-filled URL", ob.FormValues[0])
	}
}

// TestOnboardingProviders 验证预设服务商含三家 + 末尾"自定义"（无预设 URL），
// 且 effort 候选非空。
func TestOnboardingProviders(t *testing.T) {
	providers := OnboardProviders()
	if len(providers) < 4 {
		t.Errorf("providers = %d, want >= 4", len(providers))
	}
	last := providers[len(providers)-1]
	if last.Name != "自定义" || last.URL != "" {
		t.Errorf("last provider = %+v, want 自定义 with empty URL", last)
	}
	if len(OnboardEffortCandidates) < 4 {
		t.Errorf("effort candidates = %d, want >= 4", len(OnboardEffortCandidates))
	}
}
