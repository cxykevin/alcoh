package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// effortRowText 提取缓冲区第 y 行的文本（跳过宽字符续列）。
func effortRowText(b *renderer.Buffer, y, w int) string {
	var txt []rune
	for x := 0; x < w; x++ {
		cell := b.Get(x, y)
		if cell.Width == 0 {
			continue
		}
		txt = append(txt, cell.R)
	}
	return strings.TrimRight(string(txt), " ")
}

func TestEffortContentDraw(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.ActivateSession("s1", "")
	// 服务端公布 thought_level，当前值 medium。
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "medium"}},
	})
	if !m.SupportsEffort() {
		t.Fatal("SupportsEffort should be true with thought_level config")
	}
	m.OpenEffortModal() // EffortSelect 初始化为 medium（索引 2）

	const w = 40
	b := renderer.NewBuffer(w, 6)
	canv := renderer.NewCanvas(b)
	ec := effortContent(theme, m)
	ec.Draw(canv, renderer.NewRect(0, 0, w, 6))

	// 第一行：当前值。
	if got := effortRowText(b, 0, w); !strings.Contains(got, "当前: medium") {
		t.Errorf("current line = %q, want containing 当前: medium", got)
	}
	// 滑条行（y=2）：全部候选值与选中高亮 [medium]。
	slider := effortRowText(b, 2, w)
	for _, level := range model.EffortLevels() {
		if !strings.Contains(slider, level) {
			t.Errorf("slider %q missing value %q", slider, level)
		}
	}
	if !strings.Contains(slider, "[medium]") {
		t.Errorf("slider %q should highlight [medium]", slider)
	}
	if strings.Contains(slider, "[high]") || strings.Contains(slider, "[unset]") {
		t.Errorf("slider %q should only highlight selected value", slider)
	}
	// 操作提示行（y=4）。
	hint := effortRowText(b, 4, w)
	if !strings.Contains(hint, "Enter 确认") || !strings.Contains(hint, "Esc 取消") {
		t.Errorf("hint line = %q, want Enter 确认 / Esc 取消", hint)
	}
}

// findRune 在 buffer 第 y 行 [x0, w) 范围内查找首个 want 字符。
func findRune(b *renderer.Buffer, x0, y, w int, want rune) int {
	for x := x0; x < w; x++ {
		if c := b.Get(x, y); c.R == want {
			return x
		}
	}
	return -1
}

// TestEffortSliderColor 验证滑条选中项按等级着色：
// high→橙色、unset→灰色（TextMuted），且均加粗。
func TestEffortSliderColor(t *testing.T) {
	theme := renderer.DefaultTheme()
	orange := renderer.RGB(0xF5, 0x9C, 0x3D)

	// high（索引 3）→ 橙色。
	m := &model.AppModel{}
	m.ActivateSession("s1", "")
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: "high"}},
	})
	m.OpenEffortModal()
	if m.EffortSelect != 3 {
		t.Fatalf("EffortSelect = %d, want 3 (high)", m.EffortSelect)
	}
	const w = 40
	b := renderer.NewBuffer(w, 6)
	ec := effortContent(theme, m)
	ec.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, 6))
	pos := findRune(b, 0, 2, w, '[')
	if pos < 0 {
		t.Fatal("slider missing '[' marker")
	}
	sel := b.Get(pos+1, 2)
	if sel.Style.Fg != orange || !sel.Style.Bold {
		t.Errorf("selected 'high' cell = Fg %v Bold %v, want %v bold", sel.Style.Fg, sel.Style.Bold, orange)
	}

	// unset（索引 0）→ 灰色（TextMuted）。
	ec.Selected = 0
	b = renderer.NewBuffer(w, 6)
	ec.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, 6))
	pos = findRune(b, 0, 2, w, '[')
	if pos < 0 {
		t.Fatal("slider missing '[' marker for unset")
	}
	sel = b.Get(pos+1, 2)
	if sel.Style.Fg != theme.TextMuted || !sel.Style.Bold {
		t.Errorf("selected 'unset' cell = Fg %v Bold %v, want %v bold", sel.Style.Fg, sel.Style.Bold, theme.TextMuted)
	}
}

// TestEffortTopRightIndicator 验证输入框上方横线右上角显示当前 effort
// （仅 level、无提示文字），且 unset 不显示。
func TestEffortTopRightIndicator(t *testing.T) {
	theme := renderer.DefaultTheme()
	orange := renderer.RGB(0xF5, 0x9C, 0x3D)
	const w, h = 60, 20

	draw := func(current string) *renderer.Buffer {
		m := &model.AppModel{}
		m.Input = widget.NewInputBuffer()
		m.ActivateSession("s1", "")
		m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
			SessionID: "s1",
			Options:   []acp.ConfigOption{{ConfigID: "thought_level", Type: "select", CurrentValue: current}},
		})
		v := NewAppView(theme)
		b := renderer.NewBuffer(w, h)
		v.drawSession(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)
		return b
	}

	// 无 plan/slash、单行输入时，输入框上方横线位于 y = h-5。
	const sepY = h - 5

	// high → 右上角 (w-5..w-2) 显示 "high"，橙色加粗；最右列仍为横线。
	b := draw("high")
	first := b.Get(w-5, sepY)
	if first.R != 'h' || first.Style.Fg != orange || !first.Style.Bold {
		t.Errorf("top-right 'high' = R %q Fg %v Bold %v, want 'h' %v bold", first.R, first.Style.Fg, first.Style.Bold, orange)
	}
	for i := 1; i < 4; i++ {
		if c := b.Get(w-5+i, sepY); c.R != rune("high"[i]) {
			t.Errorf("top-right char %d = %q, want %q", i, c.R, "high"[i])
		}
	}
	if c := b.Get(w-1, sepY); c.R != '─' {
		t.Errorf("rightmost column = %q, want horizontal line ─", c.R)
	}

	// unset → 不显示，横线完整贯穿。
	b = draw("unset")
	for x := 0; x < w; x++ {
		if c := b.Get(x, sepY); c.R != '─' {
			t.Errorf("unset row x=%d = %q, want horizontal line ─", x, c.R)
		}
	}
}
