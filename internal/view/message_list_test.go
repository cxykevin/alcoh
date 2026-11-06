package view

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestMessageListToggles 验证可点击切换展开/折叠的正文行映射（Toggles）：
// 思考与工具块仅标题行可点击；折叠的思考保留 5 行预览，点击标题展开到完整高度。
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

	// 折叠的思考 2 行（标题 + 1 行预览），工具 2 行（标题 + out）。
	want := map[int]ToggleRef{
		0: {Kind: ToggleThought, ID: "t1"},
		2: {Kind: ToggleTool, ID: "c1"},
	}
	for row, ref := range want {
		if got, ok := ml.Toggles[row]; !ok || got != ref {
			t.Errorf("Toggles[%d] = %+v, ok=%v; want %+v", row, got, ok, ref)
		}
	}
	for _, row := range []int{1, 3} {
		if _, ok := ml.Toggles[row]; ok {
			t.Errorf("content row %d should not be clickable", row)
		}
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

// TestThoughtBlockPreview 验证思考折叠时只显示最后 5 个渲染行作为预览，
// 点击展开后显示完整高度（全部内容行）；流式中同样只保留尾部预览。
func TestThoughtBlockPreview(t *testing.T) {
	ml := &MessageList{Theme: renderer.DefaultTheme()}
	text := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8"
	msg := &model.Message{MessageID: "t1", Kind: model.MsgThought, Text: text, Done: true}

	collapsed := ml.thoughtBlock(msg, 60)
	if len(collapsed.lines) != 1+thoughtPreviewLines {
		t.Fatalf("collapsed thought rows = %d, want %d", len(collapsed.lines), 1+thoughtPreviewLines)
	}
	if got := collapsed.lines[0][0].Text; got != "▸ thinking  ✓ 8 lines" {
		t.Errorf("collapsed header = %q", got)
	}
	preview := []string{"  l4", "  l5", "  l6", "  l7", "  l8"}
	for i, want := range preview {
		if got := collapsed.lines[i+1][0].Text; got != want {
			t.Errorf("preview row %d = %q, want %q", i, got, want)
		}
	}

	// 点击展开：标题变成 ▾，内容显示完整高度（8 行）。
	msg.Expanded = true
	expanded := ml.thoughtBlock(msg, 60)
	if len(expanded.lines) != 1+8 {
		t.Fatalf("expanded thought rows = %d, want %d", len(expanded.lines), 1+8)
	}
	if got := expanded.lines[0][0].Text; got != "▾ thinking" {
		t.Errorf("expanded header = %q", got)
	}
	if got := expanded.lines[1][0].Text; got != "  l1" {
		t.Errorf("expanded first content row = %q, want %q", got, "  l1")
	}

	// 流式中（默认 Expanded=true）仍只显示尾部预览，避免长思考撑高视图。
	streaming := &model.Message{MessageID: "t2", Kind: model.MsgThought, Text: text, Expanded: true}
	if rows := len(ml.thoughtBlock(streaming, 60).lines); rows != 1+thoughtPreviewLines {
		t.Errorf("streaming thought rows = %d, want %d", rows, 1+thoughtPreviewLines)
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
