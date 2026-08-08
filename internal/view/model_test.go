package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

func TestModelContentDraw(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.ActivateSession("s1", "")
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options: []acp.ConfigOption{
			{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "demo-go-2",
				Options: []acp.ConfigOptionValue{
					{Value: "demo-go-1", Name: "Demo Go 1", Description: "最快"},
					{Value: "demo-go-2", Name: "Demo Go 2", Description: "最强"},
					{Value: "demo-go-3", Name: "Demo Go 3"},
				}},
		},
	})
	if !m.SupportsModel() {
		t.Fatal("SupportsModel should be true with category=model config")
	}
	m.OpenModelModal() // ModelSelect 初始化为 demo-go-2（索引 1）
	if m.ModelSelect != 1 {
		t.Fatalf("ModelSelect = %d, want 1", m.ModelSelect)
	}

	const w, h = 40, 8
	b := renderer.NewBuffer(w, h)
	canv := renderer.NewCanvas(b)
	mc := modelContent(theme, m)
	mc.Draw(canv, renderer.NewRect(0, 0, w, h))

	// 第一行：当前值。
	row0 := effortRowText(b, 0, w)
	if !strings.Contains(row0, "当前: demo-go-2") {
		t.Errorf("current line = %q, want containing 当前: demo-go-2", row0)
	}
	// 选项列表：全部候选都渲染，且选中项带 ❯。
	// 布局：y=0 "当前"，y=1 空行，选项从 y=2 开始。
	rows := map[int]string{2: "Demo Go 1", 3: "Demo Go 2", 4: "Demo Go 3"}
	for y, name := range rows {
		row := effortRowText(b, y, w)
		if !strings.Contains(row, name) {
			t.Errorf("row %d = %q, want containing %q", y, row, name)
		}
	}
	sel := effortRowText(b, 3, w)
	if !strings.HasPrefix(sel, "❯ ") {
		t.Errorf("selected row = %q, want ❯ prefix", sel)
	}
	if strings.HasPrefix(effortRowText(b, 2, w), "❯ ") || strings.HasPrefix(effortRowText(b, 4, w), "❯ ") {
		t.Errorf("non-selected rows should not have ❯ marker")
	}
	// 操作提示行。
	hint := effortRowText(b, h-1, w)
	if !strings.Contains(hint, "Enter 确认") || !strings.Contains(hint, "Esc 取消") {
		t.Errorf("hint line = %q, want Enter 确认 / Esc 取消", hint)
	}
}

func TestModelContentScroll(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := &model.AppModel{}
	m.ActivateSession("s1", "")
	var options []acp.ConfigOptionValue
	for i := 0; i < 10; i++ {
		options = append(options, acp.ConfigOptionValue{Value: "m/" + string(rune('a'+i)), Name: "Model " + string(rune('A'+i))})
	}
	m.ApplyEvent(&acp.ConfigOptionUpdateEvent{
		SessionID: "s1",
		Options: []acp.ConfigOption{
			{ConfigID: "model", Category: "model", Type: "select", CurrentValue: "m/e", Options: options},
		},
	})
	m.OpenModelModal()
	if m.ModelSelect != 4 { // m/e → 索引 4
		t.Fatalf("ModelSelect = %d, want 4", m.ModelSelect)
	}
	// 小高度下选中项必须保持可见（以选中项为中心滚动窗口）。
	const w, h = 40, 7
	b := renderer.NewBuffer(w, h)
	mc := modelContent(theme, m)
	mc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	// 内容区域高度 h-1=6："当前"行 + 空行 + 选项区域 4 行（保留底部提示）。
	// 选中项（Model E）应出现在可视窗口内。选项从 y=2 开始。
	found := false
	for y := 2; y < h-1; y++ {
		row := effortRowText(b, y, w)
		if strings.HasPrefix(row, "❯ Model E") {
			found = true
		}
	}
	if !found {
		t.Errorf("selected Model E not visible in scrolled window")
	}
	// 顶部候选项（Model A）应被滚出窗口。
	for y := 2; y < h-1; y++ {
		if strings.Contains(effortRowText(b, y, w), "Model A") {
			t.Errorf("Model A should be scrolled out of the window")
		}
	}
}
