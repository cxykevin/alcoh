package view

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestMessageListToggles 验证可点击切换展开/折叠的正文行映射（Toggles）：
// 思考与工具块仅标题行可点击；展开后内容行不可点击。
func TestMessageListToggles(t *testing.T) {
	theme := renderer.DefaultTheme()
	s := model.NewSession("s1", "test")
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1", IsThought: true,
		Message: acp.Message{MessageID: "t1", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: strPtr("想 1")}}},
	})
	status := acp.ToolCompleted
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{
		SessionID: "s1", ToolCallID: "c1", Status: &status, Title: strPtr("read_file"),
		RawOutput: []byte("out1"),
	})

	ml := &MessageList{Theme: theme}
	const w, h = 60, 20
	ml.Draw(renderer.NewCanvas(renderer.NewBuffer(w, h)), renderer.NewRect(0, 0, w, h), s)

	// 折叠的思考 1 行（标题），工具 2 行（标题 + out）。
	want := map[int]ToggleRef{
		0: {Kind: ToggleThought, ID: "t1"},
		1: {Kind: ToggleTool, ID: "c1"},
	}
	for row, ref := range want {
		if got, ok := ml.Toggles[row]; !ok || got != ref {
			t.Errorf("Toggles[%d] = %+v, ok=%v; want %+v", row, got, ok, ref)
		}
	}
	if _, ok := ml.Toggles[2]; ok {
		t.Error("tool content row should not be clickable")
	}

	// 模拟点击思考标题 → 展开：标题行仍可点击，思考内容行不可点击。
	s.ToggleMessage("t1")
	ml.Draw(renderer.NewCanvas(renderer.NewBuffer(w, h)), renderer.NewRect(0, 0, w, h), s)
	if got, ok := ml.Toggles[0]; !ok || got != (ToggleRef{Kind: ToggleThought, ID: "t1"}) {
		t.Errorf("expanded thought header row should stay clickable, got %+v ok=%v", got, ok)
	}
	if _, ok := ml.Toggles[1]; ok {
		t.Error("thought content row should not be clickable")
	}
	// 思考展开后工具标题行下移，仍可点击。
	if got, ok := ml.Toggles[2]; !ok || got != (ToggleRef{Kind: ToggleTool, ID: "c1"}) {
		t.Errorf("tool header should move down and stay clickable, got %+v ok=%v", got, ok)
	}
}

func strPtr(s string) *string { return &s }
