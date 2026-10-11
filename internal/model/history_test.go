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

	// 锚点基准是加载开始时上一帧渲染回写的正文行数。
	s.Rows = 60
	m.SetHistoryLoading(true)
	if !s.HistoryLoading {
		t.Fatal("HistoryLoading should be set while loading")
	}
	if m.AtScrollTop() {
		t.Fatal("must not trigger again while a page is loading")
	}

	s.Scroll = 20
	m.SetHistoryLoading(false)
	if s.HistoryLoading {
		t.Fatal("HistoryLoading should be cleared when the page arrives")
	}
	// 下一帧渲染时前面已经插入了 20 行：按锚点补偿一次，视口停在原处。
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

// TestHistoryAnchorWaitsForReplayedPage 验证锚点从"开始加载"一直保留到这一页
// 真正渲染出来：加载期间渲染的空帧不动偏移也不丢锚点，新内容到达那一帧一次补足；
// 加载结束仍没有新内容（已到最早一条或请求失败）则丢弃锚点，之后正常的内容变化
// 不会被误当成"前面插入的行"把视口推走。
func TestHistoryAnchorWaitsForReplayedPage(t *testing.T) {
	s := NewSession("s1", "test")
	s.Rows = 100
	s.Scroll = 40
	s.HistoryLoading = true
	s.BeginHistoryAnchor()

	// 加载中渲染：这一页还没到，行数没变 → 偏移不动，锚点仍保留。
	s.AdjustScrollForHistoryAnchor(100)
	if s.Scroll != 40 {
		t.Errorf("scroll = %d, want 40 while the page is still pending", s.Scroll)
	}

	// 更早的一页渲染出来（前面多了 30 行）→ 一次性补偿 30 行。
	s.AdjustScrollForHistoryAnchor(130)
	if s.Scroll != 70 {
		t.Errorf("scroll = %d, want 70 after 30 prepended lines", s.Scroll)
	}
	// 补偿已消费锚点：后续渲染不会再动偏移。
	s.AdjustScrollForHistoryAnchor(160)
	if s.Scroll != 70 {
		t.Errorf("scroll = %d, want 70 (anchor consumed)", s.Scroll)
	}

	// 加载结束但这一页没带来新行（已到最早一条或请求失败）：丢弃锚点，
	// 之后新消息带来的行数变化不再补偿。
	s.Rows = 160
	s.HistoryLoading = true
	s.BeginHistoryAnchor()
	s.AdjustScrollForHistoryAnchor(160) // 加载中的空帧
	s.HistoryLoading = false
	s.AdjustScrollForHistoryAnchor(160) // 加载结束、行数没变 → 丢弃锚点
	s.AdjustScrollForHistoryAnchor(190) // 之后的内容变化不再补偿
	if s.Scroll != 70 {
		t.Errorf("scroll = %d, want 70 after an empty page dropped its anchor", s.Scroll)
	}
}
