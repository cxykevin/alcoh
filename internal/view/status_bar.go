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
		pct := min(int(float64(s.Usage.Used)/float64(s.Usage.Size)*100), 100)
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
	var spans []Span
	if s := m.ActiveSession(); s != nil {
		name := s.Title
		if name == "" {
			name = s.ID
		}
		spans = append(spans, Span{Text: name, Style: st})
		switch s.State {
		case acp.StateRunning:
			spans = append(spans, Span{Text: "  ", Style: st})
			spans = append(spans, runningState(SpinFrame(sb.SpinFrame), sb.SpinFrame, t)...)
		case acp.StateRequiresAction:
			spans = append(spans, Span{Text: "  ? action", Style: st})
		case acp.StateIdle:
			idle := "● idle"
			if s.StopReason != nil && *s.StopReason != "" && *s.StopReason != acp.StopEndTurn {
				idle += " (" + string(*s.StopReason) + ")"
			}
			spans = append(spans, Span{Text: "  " + idle, Style: st})
		default:
			spans = append(spans, Span{Text: "  ● idle", Style: st})
		}
		if n := m.PendingPermissionCount(); n > 0 {
			spans = append(spans, Span{Text: "  +" + itoa(n) + " perm", Style: st})
		}
		if s.WorkingDir != "" {
			spans = append(spans, Span{Text: "  " + s.WorkingDir, Style: st})
		}
	} else {
		spans = append(spans, Span{Text: "·", Style: st})
	}
	// 插件状态文本（插件经 status 请求设置，见 internal/plugin）。
	for _, line := range m.PluginStatusLines() {
		spans = append(spans, Span{Text: "  [" + line + "]", Style: st})
	}
	// 依序绘制 spans，超出 maxLeft 时按原 Truncate 语义截断。
	maxLeft := max(r.W-rightW-4, 1)
	x := r.X + 1
	for _, sp := range spans {
		rem := r.X + 1 + maxLeft - x
		if rem <= 0 {
			break
		}
		if renderer.StringWidth(sp.Text) > rem {
			c.PutText(x, r.Y, renderer.Truncate(sp.Text, rem), sp.Style)
			break
		}
		c.PutText(x, r.Y, sp.Text, sp.Style)
		x += renderer.StringWidth(sp.Text)
	}
	c.PutText(r.X+r.W-rightW-2, r.Y, right, st)
}

// runningWords 是状态栏在会话运行期间循环展示的动态动词（替代固定 "running"）。
var runningWords = []string{
	"coding", "vibing", "diving", "building", "exploring", "shipping",
}

// runningState 构造运行状态的状态栏 span：spinner + 随时间循环的动词。
// 动词使用主题蓝作为基色，并叠加一个从左向右扫过的高光动画：
// 高光前缘最亮（纯白加粗），向尾部渐隐回主题蓝，扫到词尾后从头再来。
func runningState(spin string, frame int, t renderer.Theme) []Span {
	base := t.Style(t.ToolRunning)
	// 动词约每 2 秒（20 tick × 100ms）切换一次。
	word := runningWords[(frame/20)%len(runningWords)]
	runes := []rune(word)
	n := len(runes)
	// 高光带宽度（字符数）；周期设为 n+band-1，使光带从左扫到右后连续复位。
	const band = 3
	cycle := n + band - 1
	head := frame % cycle
	highlight := renderer.RGB(0xFF, 0xFF, 0xFF)

	spans := make([]Span, 0, n+1)
	spans = append(spans, Span{Text: spin + " ", Style: base})
	for i, r := range runes {
		st := base
		if d := head - i; d >= 0 && d < band {
			// d=0 是高光前缘（纯白），d 越大越接近主题蓝，形成渐隐拖尾。
			w := float64(d) / float64(band-1)
			st = base.WithFg(base.Fg.Blend(highlight, w)).WithBold(true)
		}
		spans = append(spans, Span{Text: string(r), Style: st})
	}
	return spans
}
