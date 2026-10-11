package model

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// TestHistoryAnchorCompensatesInsertedLines 验证加载更早历史后的视口锚定：
// 回放的内容接在正文前面，渲染前把新增行数加到滚动偏移上，视口停在原处；
// 补偿只做一次，没有记录锚点时不动偏移。
func TestHistoryAnchorCompensatesInsertedLines(t *testing.T) {
	s := NewSession("s1", "test")
	s.Rows = 100
	s.Scroll = 40

	// 行数没变（这一页没带来新行）：偏移不动。
	s.BeginHistoryAnchor()
	s.AdjustScrollForHistoryAnchor(100)
	if s.Scroll != 40 {
		t.Errorf("scroll = %d, want 40 when no lines were prepended", s.Scroll)
	}

	// 前面插入 30 行：偏移补 30，视口停在同一位置。
	s.BeginHistoryAnchor()
	s.AdjustScrollForHistoryAnchor(130)
	if s.Scroll != 70 {
		t.Errorf("scroll = %d, want 70 after 30 prepended lines", s.Scroll)
	}
	// 补偿只生效一次：同一份内容再渲染不会重复加偏移。
	s.AdjustScrollForHistoryAnchor(160)
	if s.Scroll != 70 {
		t.Errorf("scroll = %d, want 70 (adjust applies once)", s.Scroll)
	}

	// 未记录锚点时不补偿。
	s.Scroll = 5
	s.AdjustScrollForHistoryAnchor(200)
	if s.Scroll != 5 {
		t.Errorf("scroll = %d, want 5 without an anchor", s.Scroll)
	}
}

// TestAtScrollTopGatesEarlierHistoryLoading 验证"继续取更早一页"的判定：
// 需要活跃会话、正文确实超出一屏（MaxScroll > 0）、视口停在顶部、且不在加载中；
// 加载开始置位并阻止重复触发，加载结束清位并记录锚点。
func TestAtScrollTopGatesEarlierHistoryLoading(t *testing.T) {
	m := New()
	if m.AtScrollTop() {
		t.Fatal("no active session must not trigger")
	}
	m.View = ViewSession
	m.Active = NewSession("s1", "test")
	s := m.Active
	if m.AtScrollTop() {
		t.Fatal("short body (MaxScroll = 0) must not trigger")
	}
	s.MaxScroll = 10
	s.Scroll = 3
	if m.AtScrollTop() {
		t.Fatal("viewport scrolled into the body must not trigger")
	}
	s.Scroll = 0
	if !m.AtScrollTop() {
		t.Fatal("viewport at top of an overflowing body should trigger")
	}

	m.SetHistoryLoading(true)
	if !s.HistoryLoading {
		t.Fatal("HistoryLoading should be set while loading")
	}
	if m.AtScrollTop() {
		t.Fatal("must not trigger again while a page is loading")
	}

	s.Rows = 60
	s.Scroll = 20
	m.SetHistoryLoading(false)
	if s.HistoryLoading {
		t.Fatal("HistoryLoading should be cleared when the page arrives")
	}
	// 结束后记录锚点：下一帧渲染按新增行数补偿（这里模拟前面插入 20 行）。
	s.AdjustScrollForHistoryAnchor(80)
	if s.Scroll != 40 {
		t.Errorf("scroll = %d, want 40 after 20 prepended lines", s.Scroll)
	}
	// 补偿后视口已不在顶部，用户继续上滚到顶才会触发下一页。
	if m.AtScrollTop() {
		t.Fatal("compensated viewport is no longer at the top")
	}
	s.Scroll = 0
	s.MaxScroll = 40
	if !m.AtScrollTop() {
		t.Fatal("scrolling back to the top should allow the next page")
	}
}

// TestEarlierHistoryPageSlotsBeforeKnownMessages 验证回放的一页旧消息按 messageId
// 归位到已知消息之前（旧的 dbID 更小），不是追加到正文末尾——这是视口锚定
// “新增内容都在正文前面”的前提。
func TestEarlierHistoryPageSlotsBeforeKnownMessages(t *testing.T) {
	s := NewSession("s1", "test")
	s.SetAlkaid0MessageOrdering(true)
	// 首屏：活跃分支最后两条。
	for _, id := range []string{"msg_5", "msg_6"} {
		s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: id, Text: id})
	}
	// 更早的一页回放到达（顺序不定）。
	for _, id := range []string{"msg_3", "msg_1", "msg_2"} {
		s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: id, Text: id})
	}
	assertMessageOrder(t, s, []string{"msg_1", "msg_2", "msg_3", "msg_5", "msg_6"})
}
