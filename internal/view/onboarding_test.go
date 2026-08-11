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

// TestOnboardingContentDraw 验证剩余步骤绘制：推理强度列表（选中高亮）与
// 操作教学都在屏幕上正确出现，且任意步骤都不 panic。
func TestOnboardingContentDraw(t *testing.T) {
	const w, h = 80, 20

	steps := []struct {
		name string
		ob   *model.OnboardingState
		want string
	}{
		{"effort", &model.OnboardingState{Step: model.OnboardStepEffort, EffortSel: 2}, "high"},
		{"teaching", &model.OnboardingState{Step: model.OnboardStepTeaching}, "基本操作"},
	}
	for _, s := range steps {
		rows := drawOnboarding(s.ob, w, h)
		joined := strings.Join(rows, "\n")
		if !strings.Contains(joined, s.want) {
			t.Errorf("%s: missing %q in:\n%s", s.name, s.want, joined)
		}
	}

	// 标题在所有步骤可见。
	if rows := drawOnboarding(&model.OnboardingState{Step: model.OnboardStepEffort}, w, h); !strings.Contains(rows[0], "首次设置") {
		t.Errorf("title row = %q", rows[0])
	}
}

// TestOnboardingEffortSelection 验证选中行以 "> " 标记。
func TestOnboardingEffortSelection(t *testing.T) {
	const w, h = 80, 20
	ob := &model.OnboardingState{Step: model.OnboardStepEffort, EffortSel: 1} // medium
	rows := drawOnboarding(ob, w, h)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "> medium") {
		t.Errorf("selected effort row should be marked, got:\n%s", joined)
	}
}
