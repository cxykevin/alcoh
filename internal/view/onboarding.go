package view

import (
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// OnboardingContent 绘制新手引导剩余步骤（模型配置由 /connect 向导完成）：
// 选第一个会话的推理强度 → 基本操作教学。纯展示：交互由 app 层 onboardingKey
// 处理。
type OnboardingContent struct {
	Theme renderer.Theme
	Ob    *model.OnboardingState
}

var _ widget.Widget = (*OnboardingContent)(nil)

// Draw 绘制引导。
func (oc *OnboardingContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	if oc.Ob == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	t := oc.Theme
	d := &connDrawer{c: c, r: r, y: r.Y, sts: connStyles{
		title: t.Style(t.Primary).WithBold(true),
		text:  t.Style(t.Text),
		muted: t.Style(t.TextMuted),
		sel:   t.Style(t.Primary),
		err:   t.Style(t.Error),
		ok:    t.Style(t.Success),
	}}
	if !d.draw(i18n.T("首次设置"), d.sts.title) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	switch oc.Ob.Step {
	case model.OnboardStepEffort:
		oc.drawEffort(d)
	case model.OnboardStepTeaching:
		oc.drawTeaching(d)
	}
}

func (oc *OnboardingContent) drawEffort(d *connDrawer) {
	if !d.draw(i18n.T("选择你第一个会话的推理强度（之后可用 /effort 随时调整）："), d.sts.text) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	for i, e := range model.OnboardEffortCandidates {
		marker := "  "
		st := d.sts.text
		if i == oc.Ob.EffortSel {
			marker = "> "
			st = d.sts.sel
		}
		if !d.draw(marker+e, st) {
			return
		}
	}
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("↑↓ 选择    Enter 确认    Esc 跳过"), d.sts.muted)
}

func (oc *OnboardingContent) drawTeaching(d *connDrawer) {
	if !d.draw(i18n.T("基本操作"), d.sts.sel) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	lines := []string{
		i18n.T("Enter          主页空输入恢复会话 / 会话内提交输入"),
		i18n.T("/              命令面板（/effort /model /server /settings）"),
		i18n.T("↑↓ ←→          移动与历史"),
		i18n.T("Shift+Enter    输入框内换行"),
		i18n.T("d              主页删除选中会话"),
		i18n.T("Ctrl+q         退出"),
		i18n.T("?              帮助"),
	}
	for _, ln := range lines {
		if !d.draw(ln, d.sts.text) {
			return
		}
	}
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("Enter / Esc 完成，进入主页"), d.sts.muted)
}
