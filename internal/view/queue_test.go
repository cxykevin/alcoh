package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestQueueListDrawsAboveInput 验证输入框上方的排队列表：高度 = 标题分隔行 +
// 可见项，聚焦时用 ❯ 标记选中项；超过上限时只显示 5 项。
func TestQueueListDrawsAboveInput(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.MsgQueue = []acp.MsgQueueItem{
		{ID: "q1", Kind: model.MsgQueueKindPrompt, Text: "第一条"},
		{ID: "q2", Kind: model.MsgQueueKindPrompt, Text: "第二条"},
	}
	if got := queueListHeight(m); got != 3 {
		t.Fatalf("queueListHeight = %d, want 3 (1 header + 2 items)", got)
	}
	m.QueueFocused = true
	m.QueueSelect = 1

	const w = 48
	h := queueListHeight(m)
	b := renderer.NewBuffer(w, h)
	(&AppView{Theme: theme}).drawQueueList(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)

	if header := effortRowText(b, 0, w); !strings.Contains(header, "排队消息") {
		t.Errorf("header = %q, want 排队消息", header)
	}
	first := effortRowText(b, 1, w)
	if strings.HasPrefix(first, "❯") || !strings.Contains(first, "第一条") {
		t.Errorf("first row = %q, want unselected 第一条", first)
	}
	second := effortRowText(b, 2, w)
	if !strings.HasPrefix(second, "❯ ") || !strings.Contains(second, "第二条") {
		t.Errorf("second row = %q, want selected 第二条", second)
	}

	// 超过 5 项时高度封顶（标题 + 5 行）。
	for i := range 10 {
		m.Active.MsgQueue = append(m.Active.MsgQueue, acp.MsgQueueItem{ID: "x", Text: "x"})
		_ = i
	}
	if got := queueListHeight(m); got != 1+queueListMaxRows {
		t.Errorf("queueListHeight with many items = %d, want %d", got, 1+queueListMaxRows)
	}
}

// TestQueueContentFitsItems 回归 e2e 发现的排版问题：弹窗高度必须为内容区
// （提示 + 空行 + n 项 + 操作提示）加下上边框留足空间，否则最后一项会被
// 底部操作提示行挤掉、面板只剩"排队消息 N 条"却看不到条目。
func TestQueueContentFitsItems(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.MsgQueue = []acp.MsgQueueItem{
		{ID: "q1", Kind: model.MsgQueueKindPrompt, Text: "待清空 A"},
		{ID: "q2", Kind: model.MsgQueueKindPrompt, Text: "待清空 B"},
	}
	m.SetModal(model.ModalMsgQueue)

	modalH := queueModalHeight(m)
	if modalH != 7 { // n+5：2 项 + 提示/空行/操作提示 + 上下边框
		t.Fatalf("queueModalHeight = %d, want 7", modalH)
	}
	// 内容区高度 = 弹窗高度 - 上下边框。
	const w = 48
	contentH := modalH - 2
	b := renderer.NewBuffer(w, contentH)
	queueContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))

	if got := effortRowText(b, 0, w); !strings.Contains(got, "排队消息 2 条") {
		t.Errorf("hint row = %q", got)
	}
	first := effortRowText(b, 2, w)
	if !strings.HasPrefix(first, "❯ ") || !strings.Contains(first, "待清空 A") {
		t.Errorf("first item row = %q, want selected 待清空 A", first)
	}
	if second := effortRowText(b, 3, w); !strings.Contains(second, "待清空 B") {
		t.Errorf("second item row = %q, want 待清空 B", second)
	}
	if footer := effortRowText(b, contentH-1, w); !strings.Contains(footer, "清空") {
		t.Errorf("footer row = %q, want operation hint", footer)
	}
}
