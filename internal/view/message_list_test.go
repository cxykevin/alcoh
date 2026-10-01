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
			Content: []acp.ContentBlock{{Type: "text", Text: new("想 1")}}},
	})
	status := acp.ToolCompleted
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{
		SessionID: "s1", ToolCallID: "c1", Status: &status, Title: new("read_file"),
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

// TestMessageBlockSrcMaps 验证正文行的"每格源下标"：渲染前缀（"  " / "  ❯ "）
// 在原文里没有对应字符（-1），其余列指向该行原文的 rune；中文占两格、两格同指
// 一个 rune；长行 wrap 的续行继续用同一逻辑行的原文下标。
func TestMessageBlockSrcMaps(t *testing.T) {
	theme := renderer.DefaultTheme()
	ml := &MessageList{Theme: theme}

	// 用户消息：首行前缀 "  ❯ "（4 格），中文每字占两格。
	blk := ml.messageBlock(&model.Message{Kind: model.MsgUser, Text: "你好ab"}, 40)
	if len(blk.srcLines) != len(blk.lines) {
		t.Fatalf("srcLines %d != lines %d", len(blk.srcLines), len(blk.lines))
	}
	if want := []int{-1, -1, -1, -1, 0, 0, 1, 1, 2, 3}; !equalInts(blk.srcLines[0].Map, want) {
		t.Fatalf("user map = %v, want %v", blk.srcLines[0].Map, want)
	}

	// 用户消息长行 wrap：续行前缀 "  "（2 格），下标从续行首字接着算。
	const width = 12 // 正文宽 width-4 = 8 列
	long := "1234567890abcdef"
	wrapped := renderer.Wrap(long, width-4)
	if len(wrapped) < 2 {
		t.Fatalf("expected wrapped rows, got %v", wrapped)
	}
	blk = ml.messageBlock(&model.Message{Kind: model.MsgUser, Text: long}, width)
	if len(blk.srcLines) != len(wrapped) {
		t.Fatalf("got %d rows, want %d", len(blk.srcLines), len(wrapped))
	}
	cont := blk.srcLines[1].Map
	if len(cont) < 3 || cont[0] != -1 || cont[1] != -1 || cont[2] != len(wrapped[0]) {
		t.Fatalf("continuation map = %v, want first char at source %d", cont, len(wrapped[0]))
	}
	if blk.srcLines[1].First {
		t.Error("continuation row should not be marked First")
	}

	// 助手消息：前缀 "  " 两格；粗体标记符不渲染，因而不占格也不出现在下标里。
	blk = ml.messageBlock(&model.Message{Kind: model.MsgAssistant, Text: "**加粗**"}, 40)
	if want := []int{-1, -1, 2, 2, 3, 3}; !equalInts(blk.srcLines[0].Map, want) {
		t.Fatalf("bold map = %v, want %v", blk.srcLines[0].Map, want)
	}
}

//go:fix inline
func strPtr(s string) *string { return new(s) }
