package view

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestMessageListToggles 验证可点击切换展开/折叠的正文行映射（Toggles）：
// 思考与工具块仅标题行可点击；思考后面已有内容时完全折叠成标题行，
// 点击标题展开到完整高度。
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

	// 思考后面已有工具调用 → 完全折叠成 1 行标题；工具调用默认折叠，
	// 也只有 1 行标题（「默认展开工具」关闭）。
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

// TestThoughtBlockPreview 验证只有位于正文结尾的思考保留最后 5 行预览；
// 思考后面已有内容时完全折叠成标题行；点击展开后显示完整高度。
func TestThoughtBlockPreview(t *testing.T) {
	ml := &MessageList{Theme: renderer.DefaultTheme()}
	text := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8"
	msg := &model.Message{MessageID: "t1", Kind: model.MsgThought, Text: text, Done: true}

	// 结尾处（tail=true）：标题 + 最后 5 行预览。
	tail := ml.thoughtBlock(msg, 60, true)
	if len(tail.lines) != 1+thoughtPreviewLines {
		t.Fatalf("tail thought rows = %d, want %d", len(tail.lines), 1+thoughtPreviewLines)
	}
	if got := tail.lines[0][0].Text; got != "▸ thinking  ✓ 8 lines" {
		t.Errorf("tail header = %q", got)
	}
	preview := []string{"  l4", "  l5", "  l6", "  l7", "  l8"}
	for i, want := range preview {
		if got := tail.lines[i+1][0].Text; got != want {
			t.Errorf("preview row %d = %q, want %q", i, got, want)
		}
	}

	// 后面已有内容（tail=false）：完全折叠，只剩标题行。
	head := ml.thoughtBlock(msg, 60, false)
	if len(head.lines) != 1 {
		t.Fatalf("non-tail thought rows = %d, want 1 (fully collapsed)", len(head.lines))
	}
	if got := head.lines[0][0].Text; got != "▸ thinking  ✓ 8 lines" {
		t.Errorf("collapsed header = %q", got)
	}

	// 点击展开：标题变成 ▾，显示完整高度（8 行），与是否在结尾无关。
	msg.Expanded = true
	expanded := ml.thoughtBlock(msg, 60, false)
	if len(expanded.lines) != 1+8 {
		t.Fatalf("expanded thought rows = %d, want %d", len(expanded.lines), 1+8)
	}
	if got := expanded.lines[0][0].Text; got != "▾ thinking" {
		t.Errorf("expanded header = %q", got)
	}
	if got := expanded.lines[1][0].Text; got != "  l1" {
		t.Errorf("expanded first content row = %q, want %q", got, "  l1")
	}

	// 流式中（默认 Expanded=true）且位于结尾：只显示尾部预览，避免撑高视图。
	streaming := &model.Message{MessageID: "t2", Kind: model.MsgThought, Text: text, Expanded: true}
	if rows := len(ml.thoughtBlock(streaming, 60, true).lines); rows != 1+thoughtPreviewLines {
		t.Errorf("streaming thought rows = %d, want %d", rows, 1+thoughtPreviewLines)
	}
}

// TestThoughtFoldsCompletelyWhenFollowedByContent 验证正文渲染层：最后一个思考
// 显示 5 行预览，后面一旦出现消息/工具等内容就完全折叠成 1 行。
func TestThoughtFoldsCompletelyWhenFollowedByContent(t *testing.T) {
	theme := renderer.DefaultTheme()
	s := model.NewSession("s1", "test")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "t1",
		IsThought: true, Text: "l1\nl2\nl3\nl4\nl5\nl6\nl7"})

	ml := &MessageList{Theme: theme}
	draw := func() {
		ml.Draw(renderer.NewCanvas(renderer.NewBuffer(60, 20)), renderer.NewRect(0, 0, 60, 20), s)
	}
	// 思考是最后一项：标题 + 5 行预览 = 6 行。
	draw()
	if ml.Rows != 1+thoughtPreviewLines {
		t.Fatalf("tail thought rows = %d, want %d", ml.Rows, 1+thoughtPreviewLines)
	}

	// 后面来一条用户消息：思考完全折叠为 1 行，加用户消息 1 行。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "u1", IsUser: true, Text: "hi"})
	draw()
	if ml.Rows != 2 {
		t.Fatalf("rows after following message = %d, want 2 (fully folded thought + user)", ml.Rows)
	}
}

// TestEmptyAssistantMessageTakesNoRow 验证正文为空的 agent 消息不占行：
// alkaid0 每一步工具调用都会带一条正文为空的 assistant 消息（该步只有思考或
// 工具参数），按空消息渲染会在折叠后的工具调用之间留下多余空行。
func TestEmptyAssistantMessageTakesNoRow(t *testing.T) {
	theme := renderer.DefaultTheme()
	s := model.NewSession("s1", "test")
	tool := func(id, title string) {
		status := acp.ToolCompleted
		s.ApplyToolCall(&acp.ToolCallUpdateEvent{
			SessionID: "s1", ToolCallID: id, Status: &status, Title: new(title),
		})
	}
	tool("c1", "read_file")
	// 空正文的 assistant 消息（真实 wire 里每个工具步都有一条）。
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1",
		Message: acp.Message{MessageID: "msg_1", ContentSet: true}})
	tool("c2", "write_file")

	ml := &MessageList{Theme: theme}
	ml.Draw(renderer.NewCanvas(renderer.NewBuffer(60, 20)), renderer.NewRect(0, 0, 60, 20), s)
	if ml.Rows != 2 {
		t.Fatalf("rows = %d, want 2 (two adjacent tool titles, no blank row)", ml.Rows)
	}
	if got, ok := ml.Toggles[1]; !ok || got != (ToggleRef{Kind: ToggleTool, ID: "c2"}) {
		t.Fatalf("second tool header should sit right below the first, got %+v ok=%v", got, ok)
	}

	// 思考后面只有一条空消息时仍算正文结尾：保留 5 行预览。
	s2 := model.NewSession("s2", "test")
	s2.AppendChunk(&acp.MessageChunkEvent{SessionID: "s2", MessageID: "t1",
		IsThought: true, Text: "l1\nl2\nl3\nl4\nl5\nl6\nl7"})
	s2.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s2",
		Message: acp.Message{MessageID: "msg_2", ContentSet: true}})
	ml2 := &MessageList{Theme: theme}
	ml2.Draw(renderer.NewCanvas(renderer.NewBuffer(60, 20)), renderer.NewRect(0, 0, 60, 20), s2)
	if ml2.Rows != 1+thoughtPreviewLines {
		t.Fatalf("tail thought rows = %d, want %d (empty message is not content)",
			ml2.Rows, 1+thoughtPreviewLines)
	}
}

// TestBodyDetailHidesThoughtsAndTools 验证 Ctrl+T 的三档渲染：
// 「隐藏」档位思考（含 thinking 标题）与工具调用整块不渲染，只剩消息；
// 「展开」档位两者都显示正文。
func TestBodyDetailHidesThoughtsAndTools(t *testing.T) {
	theme := renderer.DefaultTheme()
	s := model.NewSession("s1", "test")
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "t1", IsThought: true, Text: "l1\nl2"})
	s.ApplyMessage(&acp.MessageUpdateEvent{SessionID: "s1", IsThought: true,
		Message: acp.Message{MessageID: "t1", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("l1\nl2")}}}})
	status := acp.ToolCompleted
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{SessionID: "s1", ToolCallID: "c1",
		Status: &status, Title: new("read_file"), RawOutput: []byte("out1")})
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "m1", Text: "正文"})

	ml := &MessageList{Theme: theme}
	draw := func() {
		ml.Draw(renderer.NewCanvas(renderer.NewBuffer(60, 20)), renderer.NewRect(0, 0, 60, 20), s)
	}

	// 正常：思考（后面有内容 → 完全折叠）1 行 + 工具标题 1 行 + 正文 1 行。
	draw()
	if ml.Rows != 3 {
		t.Fatalf("normal rows = %d, want 3", ml.Rows)
	}

	// 展开：思考完整高度（1+2）、工具标题 + out、正文。
	s.Detail = model.DetailExpanded
	s.ExpandAll()
	draw()
	if ml.Rows <= 3 {
		t.Fatalf("expanded rows = %d, want more than normal", ml.Rows)
	}

	// 隐藏：思考与工具都不渲染，只剩正文一行，且没有可点击的标题行。
	s.Detail = model.DetailHidden
	draw()
	if ml.Rows != 1 {
		t.Fatalf("hidden rows = %d, want 1 (assistant text only)", ml.Rows)
	}
	if len(ml.Toggles) != 0 {
		t.Fatalf("hidden detail should have no toggle rows, got %v", ml.Toggles)
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
