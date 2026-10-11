package view

import (
	"strconv"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestHistoryLoadingNoticeAndAnchoredRows 验证加载更早一页时的正文渲染接线：
// 顶部多一行提示，渲染层回写 MaxScroll（滚动到顶的判定依赖它），而 Rows 基准
// 保持不含提示行——提示行在加载开始/结束时出现与消失，若参与锚定基准会让
// 新增行数的补偿偏差一行。
func TestHistoryLoadingNoticeAndAnchoredRows(t *testing.T) {
	const w, h = 60, 10
	s := model.NewSession("s1", "test")
	// 造一屏装不下的正文。
	for i := 0; i < 30; i++ {
		s.AppendChunk(&acp.MessageChunkEvent{
			SessionID: "s1", MessageID: "msg_" + strconv.Itoa(i+1), Text: "第 " + strconv.Itoa(i+1) + " 行",
		})
	}
	ml := &MessageList{Theme: renderer.DefaultTheme()}
	draw := func() {
		ml.Draw(renderer.NewCanvas(renderer.NewBuffer(w, h)), renderer.NewRect(0, 0, w, h), s)
	}

	draw()
	if s.MaxScroll <= 0 {
		t.Fatalf("MaxScroll = %d, want > 0 for an overflowing body", s.MaxScroll)
	}
	rows := s.Rows
	if s.MaxScroll != rows-h {
		t.Fatalf("MaxScroll = %d, want rows-viewH = %d", s.MaxScroll, rows-h)
	}

	// 开始加载：正文顶部多一行提示，视口锚定基准（Rows）保持不变。
	s.HistoryLoading = true
	draw()
	if s.Rows != rows {
		t.Errorf("Rows = %d, want %d (the notice row must not move the anchor)", s.Rows, rows)
	}
	if s.MaxScroll != rows+1-h {
		t.Errorf("MaxScroll = %d, want %d (notice row is scrollable)", s.MaxScroll, rows+1-h)
	}
	blocks := ml.buildBlocks(s, w-1)
	if len(blocks) == 0 || len(blocks[0].lines) == 0 {
		t.Fatal("expected a notice block at the top of the body")
	}
	if got := blocks[0].lines[0][0].Text; got != i18n.T("正在加载更早的历史…") {
		t.Errorf("top block = %q, want the earlier-history notice", got)
	}

	// 加载结束（提示行消失）：基准回到正文总行数。
	s.HistoryLoading = false
	draw()
	if s.Rows != rows {
		t.Errorf("Rows = %d, want %d after the notice disappears", s.Rows, rows)
	}
	if s.MaxScroll != rows-h {
		t.Errorf("MaxScroll = %d, want %d after the notice disappears", s.MaxScroll, rows-h)
	}
	if blocks := ml.buildBlocks(s, w-1); len(blocks) == 0 || len(blocks[0].lines) == 0 {
		t.Fatal("expected the body to start with a message block")
	} else if got := blocks[0].lines[0][0].Text; got == i18n.T("正在加载更早的历史…") {
		t.Errorf("notice row should be gone after loading, got %q", got)
	}
}
