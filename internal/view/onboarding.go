package view

import (
	"fmt"
	"strings"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// OnboardingContent 绘制全屏新手引导。根据 OnboardingState.Step 渲染不同步骤，
// 均带顶部标题、内容区与底部操作提示。纯展示：交互由 app 层 onboardingKey 处理。
type OnboardingContent struct {
	Theme renderer.Theme
	Ob    *model.OnboardingState
}

var _ widget.Widget = (*OnboardingContent)(nil)

// Draw 绘制引导。draw 写入一行文本并推进 y；返回 false 表示已到屏幕底部。
func (oc *OnboardingContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	if oc.Ob == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	stTitle := oc.Theme.Style(oc.Theme.Primary).WithBold(true)
	stText := oc.Theme.Style(oc.Theme.Text)
	stMuted := oc.Theme.Style(oc.Theme.TextMuted)
	stSel := oc.Theme.Style(oc.Theme.Primary)
	stErr := oc.Theme.Style(oc.Theme.Error)
	stOK := oc.Theme.Style(oc.Theme.Success)

	y := r.Y
	draw := func(ln string, st renderer.Style) bool {
		if y >= r.Y+r.H {
			return false
		}
		if ln != "" {
			c.PutText(r.X, y, renderer.Truncate(ln, r.W), st)
		}
		y++
		return true
	}
	if !draw("alcoh 首次设置", stTitle) {
		return
	}
	if !draw("", stText) {
		return
	}

	switch oc.Ob.Step {
	case model.OnboardStepWelcome:
		oc.drawWelcome(draw, stText, stMuted)
	case model.OnboardStepProvider:
		oc.drawProvider(draw, stText, stMuted, stSel)
	case model.OnboardStepForm:
		oc.drawForm(draw, stText, stMuted, stSel, stErr)
	case model.OnboardStepResult:
		oc.drawResult(draw, stText, stMuted, stSel, stOK)
	case model.OnboardStepEffort:
		oc.drawEffort(draw, stText, stMuted, stSel)
	case model.OnboardStepTeaching:
		oc.drawTeaching(draw, stText, stMuted, stSel)
	}
}

func (oc *OnboardingContent) drawWelcome(draw func(string, renderer.Style) bool, stText, stMuted renderer.Style) {
	lines := []string{
		"服务端还没有配置任何模型。接下来几步将帮你完成首次配置：",
		"",
		"  1. 选择模型服务商，填写模型信息（密钥、模型名、Token 上限等）",
		"  2. 可选打开 /server 配置编辑器做更详细的设置",
		"  3. 选择你第一个会话的推理强度",
		"",
	}
	for _, ln := range lines {
		if !draw(ln, stText) {
			return
		}
	}
	draw("Enter 开始    Esc 跳过（直接进入主页）", stMuted)
}

func (oc *OnboardingContent) drawProvider(draw func(string, renderer.Style) bool, stText, stMuted, stSel renderer.Style) {
	if !draw("选择模型服务商（自动预填提供方 URL，之后仍可修改）：", stText) {
		return
	}
	if !draw("", stText) {
		return
	}
	for i, p := range model.OnboardProviders() {
		marker := "  "
		st := stText
		if i == oc.Ob.ProviderSel {
			marker = "> "
			st = stSel
		}
		label := p.Name
		if p.URL != "" {
			label += "   " + p.URL
		}
		if !draw(marker+label, st) {
			return
		}
	}
	if !draw("", stText) {
		return
	}
	draw("↑↓ 选择    Enter 确认    Esc 返回欢迎页", stMuted)
}

func (oc *OnboardingContent) drawForm(draw func(string, renderer.Style) bool, stText, stMuted, stSel, stErr renderer.Style) {
	if !draw("填写模型信息（全部必填；提供方 URL 已预填，可修改）：", stText) {
		return
	}
	if !draw("", stText) {
		return
	}
	fields := model.OnboardFields()
	for i, f := range fields {
		val := ""
		if i < len(oc.Ob.FormValues) {
			val = oc.Ob.FormValues[i]
		}
		if f.Secret && val != "" {
			val = strings.Repeat("*", len(val)) // 密钥掩码显示，写回仍为明文
		}
		display := val
		if display == "" {
			display = "（必填）"
		}
		marker := "  "
		st := stText
		if i == oc.Ob.FormFocus {
			marker = "> "
			st = stSel
		}
		if !draw(fmt.Sprintf("%s%s：%s", marker, f.Label, display), st) {
			return
		}
	}
	if !draw("", stText) {
		return
	}
	if oc.Ob.FormError != "" {
		if !draw("错误: "+oc.Ob.FormError, stErr) {
			return
		}
		draw("", stText)
	}
	if oc.Ob.FormSubmitting {
		draw("正在保存模型配置…", stMuted)
		return
	}
	draw("↑↓ / Tab 切换字段    输入字符    退格删除    Enter 提交    Esc 返回", stMuted)
}

func (oc *OnboardingContent) drawResult(draw func(string, renderer.Style) bool, stText, stMuted, stSel, stOK renderer.Style) {
	if !draw("模型已添加并设为默认模型（Model.Models 0）。", stOK) {
		return
	}
	if !draw("", stText) {
		return
	}
	buttons := []string{
		"打开 /server 详细配置（定位到 Config/Model/Models）",
		"下一步",
	}
	for i, b := range buttons {
		marker := "  "
		st := stText
		if i == oc.Ob.ResultSel {
			marker = "> "
			st = stSel
		}
		if !draw(marker+"[ Enter ] "+b, st) {
			return
		}
	}
	if !draw("", stText) {
		return
	}
	draw("↑↓ 选择按钮    Enter 触发    Esc 跳过（进入主页）", stMuted)
}

func (oc *OnboardingContent) drawEffort(draw func(string, renderer.Style) bool, stText, stMuted, stSel renderer.Style) {
	if !draw("选择你第一个会话的推理强度（之后可用 /effort 随时调整）：", stText) {
		return
	}
	if !draw("", stText) {
		return
	}
	for i, e := range model.OnboardEffortCandidates {
		marker := "  "
		st := stText
		if i == oc.Ob.EffortSel {
			marker = "> "
			st = stSel
		}
		if !draw(marker+e, st) {
			return
		}
	}
	if !draw("", stText) {
		return
	}
	draw("↑↓ 选择    Enter 确认    Esc 返回上一步", stMuted)
}

func (oc *OnboardingContent) drawTeaching(draw func(string, renderer.Style) bool, stText, stMuted, stSel renderer.Style) {
	if !draw("基本操作", stSel) {
		return
	}
	if !draw("", stText) {
		return
	}
	lines := []string{
		"Enter          主页空输入恢复会话 / 会话内提交输入",
		"/              命令面板（/effort /model /server /settings）",
		"↑↓ ←→          移动与历史",
		"Shift+Enter    输入框内换行",
		"d              主页删除选中会话",
		"Ctrl+q         退出",
		"?              帮助",
	}
	for _, ln := range lines {
		if !draw(ln, stText) {
			return
		}
	}
	if !draw("", stText) {
		return
	}
	draw("Enter / Esc 完成，进入主页", stMuted)
}
