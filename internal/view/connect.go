package view

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// ConnectContent 绘制 /connect 向导（全屏）。纯展示：交互由 app 层 connectKey
// 处理。步骤：服务商模板 → base_url/key 表单 → 模型列表选择 → 完成。
type ConnectContent struct {
	Theme renderer.Theme
	Cs    *model.ConnectState
}

var _ widget.Widget = (*ConnectContent)(nil)

// connDrawer 是向导绘制辅助：跟踪当前行 y，自动截断到屏幕底部。
type connDrawer struct {
	c   *renderer.Canvas
	r   renderer.Rect
	y   int
	sts connStyles
}

type connStyles struct {
	title renderer.Style
	text  renderer.Style
	muted renderer.Style
	sel   renderer.Style
	err   renderer.Style
	ok    renderer.Style
}

// draw 写入一行文本并推进 y；返回 false 表示已到屏幕底部。
func (d *connDrawer) draw(ln string, st renderer.Style) bool {
	if d.y >= d.r.Y+d.r.H {
		return false
	}
	if ln != "" {
		d.c.PutText(d.r.X, d.y, renderer.Truncate(ln, d.r.W), st)
	}
	d.y++
	return true
}

// Draw 绘制向导。
func (cc *ConnectContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	if cc.Cs == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	t := cc.Theme
	d := &connDrawer{c: c, r: r, y: r.Y, sts: connStyles{
		title: t.Style(t.Primary).WithBold(true),
		text:  t.Style(t.Text),
		muted: t.Style(t.TextMuted),
		sel:   t.Style(t.Primary),
		err:   t.Style(t.Error),
		ok:    t.Style(t.Success),
	}}
	if !d.draw(i18n.T("连接模型服务商"), d.sts.title) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	switch cc.Cs.Step {
	case model.ConnectStepProvider:
		cc.drawProvider(d)
	case model.ConnectStepForm:
		cc.drawForm(d)
	case model.ConnectStepSelect:
		cc.drawSelect(d)
	case model.ConnectStepDone:
		cc.drawDone(d)
	}
}

func (cc *ConnectContent) drawProvider(d *connDrawer) {
	if !d.draw(i18n.T("选择一个服务商模板（自动预填 base_url，之后仍可修改）："), d.sts.text) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	for i, p := range model.ConnectTemplates() {
		marker := "  "
		st := d.sts.text
		if i == cc.Cs.ProviderSel {
			marker = "> "
			st = d.sts.sel
		}
		label := i18n.T(p.Name)
		if p.BaseURL != "" {
			label += "   " + p.BaseURL
		}
		if !d.draw(marker+label, st) {
			return
		}
	}
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("↑↓ 选择    Enter 确认    Esc 取消"), d.sts.muted)
}

func (cc *ConnectContent) drawForm(d *connDrawer) {
	if !d.draw(i18n.T("填写连接信息（填完 key 后回车，自动拉取模型列表）："), d.sts.text) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	fields := []struct {
		label, value string
	}{
		{i18n.T("base_url"), cc.Cs.BaseURL},
		{i18n.T("API key"), maskKey(cc.Cs.Key)},
	}
	for i, f := range fields {
		marker := "  "
		st := d.sts.text
		if i == cc.Cs.FormFocus {
			marker = "> "
			st = d.sts.sel
		}
		display := f.value
		if display == "" {
			display = i18n.T("（必填）")
		}
		if !d.draw(marker+f.label+"："+display, st) {
			return
		}
	}
	if !d.draw("", d.sts.text) {
		return
	}
	if cc.Cs.FormError != "" {
		if !d.draw(i18n.T("错误: %s", cc.Cs.FormError), d.sts.err) {
			return
		}
		d.draw("", d.sts.text)
	}
	if cc.Cs.Fetching {
		d.draw(i18n.T("正在获取模型列表…"), d.sts.muted)
		return
	}
	d.draw(i18n.T("↑↓ / Tab 切换字段    输入字符    退格删除    Enter 拉取模型    Esc 返回"), d.sts.muted)
}

func (cc *ConnectContent) drawSelect(d *connDrawer) {
	if !d.draw(i18n.T("选择要添加的模型（将写入服务端配置并设为默认）："), d.sts.text) {
		return
	}
	if !d.draw("", d.sts.text) {
		return
	}
	models := cc.Cs.Models
	if len(models) == 0 {
		d.draw(i18n.T("服务商未返回任何模型"), d.sts.muted)
		return
	}
	// 模型列表按选中项滚动，底部保留一行操作提示。
	maxRows := d.r.Y + d.r.H - d.y - 1
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > len(models) {
		maxRows = len(models)
	}
	start := cc.Cs.ModelSel - maxRows/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(models) - maxRows; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	for i := start; i < len(models) && i < start+maxRows; i++ {
		marker := "  "
		st := d.sts.text
		if i == cc.Cs.ModelSel {
			marker = "❯ "
			st = d.sts.sel
		}
		line := marker + models[i].ID
		if models[i].Name != "" && models[i].Name != models[i].ID {
			line += "  — " + models[i].Name
		}
		if models[i].TokenLimit > 0 {
			line += "  (" + itoa(models[i].TokenLimit) + " ctx)"
		}
		if !d.draw(line, st) {
			return
		}
	}
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("↑↓ 选择    Enter 确认    Esc 返回修改"), d.sts.muted)
}

func (cc *ConnectContent) drawDone(d *connDrawer) {
	if cc.Cs.Result != "" {
		if !d.draw(i18n.T("✓ %s", cc.Cs.Result), d.sts.ok) {
			return
		}
	}
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("已连接。可用 /model 切换、/server 修改详细配置。"), d.sts.text)
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("Enter / Esc 关闭"), d.sts.text)
}

// maskKey 掩码显示 API key（保留首 4 位便于辨认）。
func maskKey(v string) string {
	if len(v) <= 4 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + strings.Repeat("*", len(v)-4)
}
