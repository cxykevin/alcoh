package view

import (
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// ThresholdContent 绘制 /threshold 弹窗：显示目标模型与当前压缩阈值，输入新值。
type ThresholdContent struct {
	Theme renderer.Theme
	Ts    *model.ThresholdState
}

var _ widget.Widget = (*ThresholdContent)(nil)

// Draw 绘制弹窗内容。
func (tc *ThresholdContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	ts := tc.Theme
	d := &connDrawer{c: c, r: r, y: r.Y, sts: connStyles{
		title: ts.Style(ts.Primary).WithBold(true),
		text:  ts.Style(ts.Text),
		muted: ts.Style(ts.TextMuted),
		sel:   ts.Style(ts.Primary),
		err:   ts.Style(ts.Error),
		ok:    ts.Style(ts.Success),
	}}
	if tc.Ts == nil {
		d.draw(i18n.T("正在获取当前压缩阈值…"), d.sts.muted)
		return
	}
	if tc.Ts.Loading {
		d.draw(i18n.T("正在获取当前压缩阈值…"), d.sts.muted)
		return
	}
	if tc.Ts.ModelName != "" {
		d.draw(i18n.T("模型: %s", tc.Ts.ModelName), d.sts.text)
	} else {
		d.draw(i18n.T("模型: —"), d.sts.text)
	}
	if !d.draw("", d.sts.text) {
		return
	}
	d.draw(i18n.T("压缩阈值（Token 数）:"), d.sts.text)
	input := tc.Ts.Input
	if input == "" {
		input = i18n.T("（必填）")
	}
	d.draw("> "+input, d.sts.sel)
	if !d.draw("", d.sts.text) {
		return
	}
	if tc.Ts.Error != "" {
		if !d.draw(i18n.T("错误: %s", tc.Ts.Error), d.sts.err) {
			return
		}
		d.draw("", d.sts.text)
	}
	d.draw(i18n.T("输入字符    退格删除    Enter 保存    Esc 取消"), d.sts.muted)
}
