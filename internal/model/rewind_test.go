package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

// rewindCapabilities 返回声明 alkaid0 v0.6（rewind 协议）能力的服务端能力声明。
func rewindCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.6":{}}`)}
}

// newRewindModel 构造一个已激活、带用户消息与命令消息的模型：DB 消息
// msg_1/msg_2/msg_3/msg_5（msg_1 是会话第一条）与合成 ID 的命令消息 cmd_7_1。
func newRewindModel() *AppModel {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, rewindCapabilities())
	m.ActivateSession("s1", "test")
	for _, ev := range []struct {
		id   string
		user bool
		text string
	}{
		{"msg_1", true, "第一条"},
		{"msg_2", false, "回复一"},
		{"msg_3", true, "第二条"},
		{"cmd_7_1", true, "命令文本"},
		{"msg_5", true, "第三条"},
	} {
		m.ApplyEvent(&acp.MessageChunkEvent{SessionID: "s1", MessageID: ev.id, IsUser: ev.user, Text: ev.text})
	}
	return m
}

func messageIDs(s *SessionState) []string {
	out := make([]string, 0, len(s.Messages))
	for _, m := range s.Messages {
		out = append(out, m.MessageID)
	}
	return out
}

func assertIDs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

// TestRewindTargetsSkipFirstAndSyntheticIDs 验证 rewind 目标只收 DB 用户消息：
// 会话第一条消息没有"前一条"（服务端会拒绝），斜杠命令的合成消息不入库、
// 无法定位，两者都不出现在选择器里。
func TestRewindTargetsSkipFirstAndSyntheticIDs(t *testing.T) {
	m := newRewindModel()
	targets := m.RewindTargets()
	if len(targets) != 2 {
		t.Fatalf("targets = %#v, want 2 entries", targets)
	}
	if targets[0].MessageID != "msg_3" || targets[0].Text != "第二条" {
		t.Fatalf("targets[0] = %#v", targets[0])
	}
	if targets[1].MessageID != "msg_5" || targets[1].Text != "第三条" {
		t.Fatalf("targets[1] = %#v", targets[1])
	}
	// msg_5 之后没有 AI 正文回复。
	if targets[1].Reply != "" {
		t.Fatalf("targets[1].Reply = %q, want empty", targets[1].Reply)
	}
}

// TestRewindTargetCarriesReply 验证选择器每条对话带上该轮 AI 回复：回复取用户
// 消息之后、下一条用户消息之前的第一条 assistant 正文，思考（与正文共享
// messageId）不算回复；没有正文的下一条目标回复为空。
func TestRewindTargetCarriesReply(t *testing.T) {
	m := New()
	m.SetAgentInfo(acp.AgentInfo{}, rewindCapabilities())
	m.ActivateSession("s1", "test")
	for _, ev := range []struct {
		id      string
		user    bool
		thought bool
		text    string
	}{
		{"msg_1", true, false, "第一条"},
		{"msg_2", false, false, "回复一"},
		{"msg_3", true, false, "第二条"},
		{"msg_4", false, true, "想一下"},
		{"msg_4", false, false, "回复二\n第二行"},
		{"msg_5", true, false, "第三条"},
	} {
		m.ApplyEvent(&acp.MessageChunkEvent{SessionID: "s1", MessageID: ev.id, IsUser: ev.user, IsThought: ev.thought, Text: ev.text})
	}
	targets := m.RewindTargets()
	if len(targets) != 2 {
		t.Fatalf("targets = %#v, want 2 entries", targets)
	}
	if targets[0].MessageID != "msg_3" || targets[0].Reply != "回复二\n第二行" {
		t.Fatalf("targets[0] = %#v, want msg_3 with reply 回复二", targets[0])
	}
	// 下一条用户消息（msg_5）后面没有 assistant 正文：范围在下一条用户消息处截断。
	if targets[1].MessageID != "msg_5" || targets[1].Reply != "" {
		t.Fatalf("targets[1] = %#v, want msg_5 without reply", targets[1])
	}
}

// TestTruncateFromDropsTargetAndTail 验证撤回目标消息及其之后的内容（含命令
// 消息）都从正文与时间线移除，且已裁掉的目标不再命中。
func TestTruncateFromDropsTargetAndTail(t *testing.T) {
	m := newRewindModel()
	s := m.Active
	if !s.TruncateFrom("msg_3") {
		t.Fatal("TruncateFrom(msg_3) = false, want true")
	}
	assertIDs(t, messageIDs(s), []string{"msg_1", "msg_2"})
	for _, item := range s.Timeline {
		if item.Message != nil && item.Message.MessageID != "msg_1" && item.Message.MessageID != "msg_2" {
			t.Fatalf("timeline keeps %s after truncation", item.Message.MessageID)
		}
	}
	if s.TruncateFrom("msg_5") {
		t.Fatal("TruncateFrom 对已裁掉的消息应返回 false")
	}
	if s.TruncateFrom("msg_99") {
		t.Fatal("TruncateFrom 对不存在的 ID 应返回 false")
	}
	// 目标消息本身也被裁掉（服务端把分支头移到它的前一条）。
	for _, id := range messageIDs(s) {
		if id == "msg_3" {
			t.Fatal("目标消息应随分支撤回一起移除")
		}
	}
}

// TestRewindModalSelectionAndOutcome 验证选择器的打开/移动/确认与结果消费：
// 成功结果裁剪正文、把被撤回消息原文交给 app 回填，且只消费一次。
func TestRewindModalSelectionAndOutcome(t *testing.T) {
	m := newRewindModel()
	if !m.SupportsRewind() {
		t.Fatal("SupportsRewind = false, want true")
	}
	if !m.OpenRewindModal() {
		t.Fatal("OpenRewindModal = false, want true")
	}
	if m.Modal != ModalRewind {
		t.Fatalf("modal = %v, want ModalRewind", m.Modal)
	}
	// 默认选中最近一条目标（msg_5）。
	if m.RewindSelect != 1 {
		t.Fatalf("RewindSelect = %d, want 1", m.RewindSelect)
	}
	m.RewindMove(-1)
	m.RewindMove(-1) // 环绕回最后一条
	if m.RewindSelect != 1 {
		t.Fatalf("RewindSelect = %d, want 1 after wrapping", m.RewindSelect)
	}
	m.RewindMove(1)
	if m.RewindSelect != 0 {
		t.Fatalf("RewindSelect = %d, want 0", m.RewindSelect)
	}
	target, ok := m.RewindSelected()
	if !ok || target.MessageID != "msg_3" || target.Text != "第二条" {
		t.Fatalf("RewindSelected = %#v, %v", target, ok)
	}
	if m.Modal != NoModal {
		t.Fatalf("modal = %v, want NoModal after confirming", m.Modal)
	}

	// 服务端广播成功结果：正文裁到目标之前，原文交给 app。
	m.ApplyEvent(&acp.RewindEvent{SessionID: "s1", MessageID: "msg_3", Success: true})
	out := m.TakeRewindOutcome()
	if out == nil || !out.Success || out.MessageID != "msg_3" || out.Text != "第二条" {
		t.Fatalf("outcome = %#v", out)
	}
	assertIDs(t, messageIDs(m.Active), []string{"msg_1", "msg_2"})
	if m.TakeRewindOutcome() != nil {
		t.Fatal("结果应只消费一次")
	}
}

// TestRewindOutcomeFailureKeepsMessages 验证失败的 rewind 只记录原因，不动正文。
func TestRewindOutcomeFailureKeepsMessages(t *testing.T) {
	m := newRewindModel()
	m.ApplyEvent(&acp.RewindEvent{SessionID: "s1", MessageID: "msg_3", Success: false, Error: "目标已被撤回"})
	out := m.TakeRewindOutcome()
	if out == nil || out.Success || out.Error != "目标已被撤回" || out.MessageID != "msg_3" {
		t.Fatalf("outcome = %#v", out)
	}
	assertIDs(t, messageIDs(m.Active), []string{"msg_1", "msg_2", "msg_3", "cmd_7_1", "msg_5"})
}

// TestSupportsRewindRequiresCapabilityAndTargets 验证 /rewind 的可用性判定：
// 需要 alkaid0 v0.6 能力，且没有可选目标时不打开选择器。
func TestSupportsRewindRequiresCapabilityAndTargets(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "test")
	if m.SupportsRewind() {
		t.Fatal("未声明 v0.6 能力时 SupportsRewind 应为 false")
	}
	m.SetAgentInfo(acp.AgentInfo{}, rewindCapabilities())
	if !m.SupportsRewind() {
		t.Fatal("声明 v0.6 能力且有活动会话时 SupportsRewind 应为 true")
	}
	if m.OpenRewindModal() {
		t.Fatal("没有可选目标时不应打开选择器")
	}

	m.GoHome()
	if m.SupportsRewind() {
		t.Fatal("主页（无活动会话）时 SupportsRewind 应为 false")
	}
}
