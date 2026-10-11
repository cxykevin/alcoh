package view

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// newRewindTurnModel 构造一个带 n 轮对话的会话：每轮是用户消息 + AI 回复，
// 消息 ID 依次为 msg_1..msg_2n（msg_1 是会话第一条，不能作为 rewind 目标）。
func newRewindTurnModel(t *testing.T, turns int) *model.AppModel {
	t.Helper()
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.6":{}}`)})
	m.ActivateSession("s1", "")
	names := []string{"第一条", "第二条", "第三条", "第四条"}
	replies := []string{"回复一", "回复二", "回复三", "回复四"}
	for i := range turns {
		m.ApplyEvent(&acp.MessageChunkEvent{
			SessionID: "s1",
			MessageID: "msg_" + strconv.Itoa(2*i+1),
			IsUser:    true,
			Text:      names[i],
		})
		m.ApplyEvent(&acp.MessageChunkEvent{
			SessionID: "s1",
			MessageID: "msg_" + strconv.Itoa(2*i+2),
			Text:      replies[i],
		})
	}
	return m
}

// TestRewindContentDrawEachTurn 验证选择器里每条对话占三行：第一行是用户内容
// （❯ 标记选中项）、第二行是灰色 AI 回复、第三行是空行。
func TestRewindContentDrawEachTurn(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := newRewindTurnModel(t, 2)
	if !m.OpenRewindModal() {
		t.Fatal("OpenRewindModal = false, want true")
	}

	const w, h = 48, 12
	b := renderer.NewBuffer(w, h)
	rewindContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	// 布局：y=0 提示行、y=1 空行，随后每条对话占 3 行。可选目标只有 msg_3。
	if got := effortRowText(b, 0, w); !strings.Contains(got, "选择要撤回的消息") {
		t.Errorf("hint row = %q, want 提示行", got)
	}
	head := effortRowText(b, 2, w)
	if !strings.HasPrefix(head, "❯ ") || !strings.Contains(head, "1. 第二条") {
		t.Errorf("head row = %q, want ❯ 1. 第二条", head)
	}
	reply := effortRowText(b, 3, w)
	if !strings.Contains(reply, "回复二") {
		t.Errorf("reply row = %q, want containing 回复二", reply)
	}
	if got := effortRowText(b, 4, w); got != "" {
		t.Errorf("separator row = %q, want empty", got)
	}
}

// TestRewindContentDrawKeepsTurnSpacing 验证多条对话之间以空行分隔（下一条对话的
// 内容行落在上一条的第三个行之后），且序号自下而上：底部最近一条是 1 号。
func TestRewindContentDrawKeepsTurnSpacing(t *testing.T) {
	theme := renderer.DefaultTheme()
	m := newRewindTurnModel(t, 3)
	if !m.OpenRewindModal() {
		t.Fatal("OpenRewindModal = false, want true")
	}

	const w, h = 48, 16
	b := renderer.NewBuffer(w, h)
	rewindContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	// 目标为 msg_3、msg_5：内容行在 y=2 与 y=5，回复行在 y=3 与 y=6。
	// 序号自下而上：底部（最近）的 msg_5 是 1 号，上方的 msg_3 是 2 号。
	first := effortRowText(b, 2, w)
	if !strings.Contains(first, "2. 第二条") {
		t.Errorf("row 2 = %q, want 2. 第二条", first)
	}
	if got := effortRowText(b, 4, w); got != "" {
		t.Errorf("row 4 = %q, want 分隔空行", got)
	}
	second := effortRowText(b, 5, w)
	if !strings.Contains(second, "1. 第三条") {
		t.Errorf("row 5 = %q, want 1. 第三条", second)
	}
	if got := effortRowText(b, 6, w); !strings.Contains(got, "回复三") {
		t.Errorf("row 6 = %q, want containing 回复三", got)
	}
}
