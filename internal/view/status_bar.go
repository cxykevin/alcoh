package view

import (
	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// StatusBar 是会话视图底部的状态行。
type StatusBar struct {
	Theme     renderer.Theme
	SpinFrame int
}

// Draw 绘制状态行：左侧会话名 + 状态，右侧 model + ctx 用量。
// 不填充背景色（保持终端默认背景，符合用户要求）。
func (sb *StatusBar) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	t := sb.Theme
	st := t.Style(t.TextMuted)

	// 右侧固定信息
	right := "model —"
	if s := m.ActiveSession(); s != nil {
		// 优先 session-info 的 model 字段，否则回退到 agent 公布的
		// model config（category="model"）当前值显示名。
		if label := s.ModelLabel(); label != "" {
			right = "model " + label
		}
	}
	ctxTxt := ""
	if s := m.ActiveSession(); s != nil && s.Usage.Size > 0 {
		pct := int(float64(s.Usage.Used) / float64(s.Usage.Size) * 100)
		if pct > 100 {
			pct = 100
		}
		ctxTxt = "  ctx " + itoa(pct) + "% (" + itoa(s.Usage.Used) + "/" + itoa(s.Usage.Size) + ")"
	} else if s := m.ActiveSession(); s != nil && s.Usage.Used > 0 {
		ctxTxt = "  ctx " + itoa(s.Usage.Used)
	}
	if s := m.ActiveSession(); s != nil && s.Usage.Cost != nil && s.Usage.Cost.Amount != "" {
		ctxTxt += "  cost " + s.Usage.Cost.Amount
		if s.Usage.Cost.Currency != "" {
			ctxTxt += " " + s.Usage.Cost.Currency
		}
	}
	right += ctxTxt
	rightW := renderer.StringWidth(right)
	if rightW > r.W-20 {
		right = renderer.Truncate(right, r.W-20)
		rightW = renderer.StringWidth(right)
	}

	// 左侧：会话名 + 状态
	left := "·"
	if s := m.ActiveSession(); s != nil {
		name := s.Title
		if name == "" {
			name = s.ID
		}
		left = name
		stateTxt := "● idle"
		switch s.State {
		case acp.StateRunning:
			stateTxt = SpinFrame(sb.SpinFrame) + " running"
		case acp.StateRequiresAction:
			stateTxt = "? action"
		case acp.StateIdle:
			stateTxt = "● idle"
			if s.StopReason != nil && *s.StopReason != "" && *s.StopReason != acp.StopEndTurn {
				stateTxt += " (" + string(*s.StopReason) + ")"
			}
		}
		left += "  " + stateTxt
		if n := m.PendingPermissionCount(); n > 0 {
			left += "  +" + itoa(n) + " perm"
		}
		if s.WorkingDir != "" {
			left += "  " + s.WorkingDir
		}
	}
	// 插件状态文本（插件经 status 请求设置，见 internal/plugin）。
	for _, line := range m.PluginStatusLines() {
		left += "  [" + line + "]"
	}
	maxLeft := r.W - rightW - 4
	if maxLeft < 1 {
		maxLeft = 1
	}
	c.PutText(r.X+1, r.Y, renderer.Truncate(left, maxLeft), st)
	c.PutText(r.X+r.W-rightW-2, r.Y, right, st)
}
