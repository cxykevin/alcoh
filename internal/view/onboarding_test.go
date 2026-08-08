package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// drawOnboarding 渲染新手引导并返回各行文本。
func drawOnboarding(ob *model.OnboardingState, w, h int) []string {
	b := renderer.NewBuffer(w, h)
	canv := renderer.NewCanvas(b)
	oc := &OnboardingContent{Theme: renderer.DefaultTheme(), Ob: ob}
	oc.Draw(canv, renderer.NewRect(0, 0, w, h))
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = effortRowText(b, y, w)
	}
	return rows
}

// TestOnboardingContentDraw 验证各步骤绘制：标题、内容要点、密钥掩码与操作提示
// 都在屏幕上正确出现，且任意步骤都不 panic。
func TestOnboardingContentDraw(t *testing.T) {
	const w, h = 80, 20

	steps := []struct {
		name string
		ob   *model.OnboardingState
		want string
	}{
		{"welcome", &model.OnboardingState{Step: model.OnboardStepWelcome}, "服务端还没有配置任何模型"},
		{"provider", &model.OnboardingState{Step: model.OnboardStepProvider}, "选择模型服务商"},
		{"form", &model.OnboardingState{
			Step:       model.OnboardStepForm,
			FormValues: []string{"https://api.deepseek.com", "sk-secret", "", "", "", ""},
		}, "模型名称"},
		{"result", &model.OnboardingState{Step: model.OnboardStepResult}, "打开 /server 详细配置"},
		{"effort", &model.OnboardingState{Step: model.OnboardStepEffort}, "推理强度"},
		{"teaching", &model.OnboardingState{Step: model.OnboardStepTeaching}, "基本操作"},
	}
	for _, s := range steps {
		rows := drawOnboarding(s.ob, w, h)
		if !strings.Contains(strings.Join(rows, "\n"), s.want) {
			t.Errorf("%s: missing %q in:\n%s", s.name, s.want, strings.Join(rows, "\n"))
		}
	}

	// 标题在所有步骤可见。
	if rows := drawOnboarding(&model.OnboardingState{Step: model.OnboardStepWelcome}, w, h); !strings.Contains(rows[0], "alcoh 首次设置") {
		t.Errorf("title row = %q", rows[0])
	}
}

// TestOnboardingFormSecretMasked 验证密钥字段在表单中掩码显示（不泄露明文）。
func TestOnboardingFormSecretMasked(t *testing.T) {
	const w, h = 80, 20
	ob := &model.OnboardingState{
		Step:       model.OnboardStepForm,
		FormValues: []string{"url", "super-secret-key", "m", "id", "8192", "128000"},
		FormFocus:  1, // ProviderKey
	}
	rows := drawOnboarding(ob, w, h)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "super-secret-key") {
		t.Error("form should mask the secret key")
	}
	if !strings.Contains(joined, "**") {
		t.Error("masked key should render asterisks")
	}
}

// TestOnboardingFormError 验证校验错误信息显示在表单底部。
func TestOnboardingFormError(t *testing.T) {
	const w, h = 80, 20
	ob := &model.OnboardingState{
		Step:       model.OnboardStepForm,
		FormValues: make([]string, 6),
		FormError:  "字段「模型名称」是必需的",
	}
	rows := drawOnboarding(ob, w, h)
	if !strings.Contains(strings.Join(rows, "\n"), "错误: 字段「模型名称」是必需的") {
		t.Error("form error should be visible")
	}
}
