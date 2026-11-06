package model

// /goal 会话目标看板的纯状态测试：能力门控、编辑态输入与提交校验、
// 服务端快照应用（空串表示清除）。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

func goalCaps(t *testing.T, m *AppModel, supported bool) {
	t.Helper()
	raw := map[string]json.RawMessage{}
	if supported {
		raw[acp.Alkaid0CapabilityV06] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: data})
}

// TestGoalAvailability 验证 /goal 需要服务端 goal 能力与活动会话同时具备。
func TestGoalAvailability(t *testing.T) {
	m := New()
	if m.GoalAvailable() || m.OpenGoalModal() {
		t.Fatal("goal panel should be unavailable on the home view")
	}
	m.ActivateSession("s1", "会话")
	if m.GoalAvailable() || m.OpenGoalModal() {
		t.Fatal("goal panel should require the server capability")
	}
	goalCaps(t, m, true)
	if !m.GoalAvailable() || !m.OpenGoalModal() {
		t.Fatal("goal panel should open with capability and active session")
	}
	if m.Modal != ModalGoal {
		t.Fatalf("modal = %v, want ModalGoal", m.Modal)
	}
	if !containsString(m.SlashCommands(), "/goal") {
		t.Fatalf("/goal missing from slash commands: %v", m.SlashCommands())
	}
}

// TestGoalEditCommit 验证编辑态以当前 goal 初始化，提交去掉首尾空白，
// 空白与超长被本地拦截且保持编辑态。
func TestGoalEditCommit(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "会话")
	goalCaps(t, m, true)
	m.Active.Goal = "旧目标"
	if !m.OpenGoalModal() || !m.BeginGoalEdit() {
		t.Fatal("failed to enter goal edit mode")
	}
	if got := m.GoalDraft(); got != "旧目标" {
		t.Fatalf("draft = %q, want current goal", got)
	}

	m.GoalInput.Clear()
	m.GoalInput.InsertText("  新目标  ")
	text, err := m.CommitGoalEdit()
	if err != nil || text != "新目标" {
		t.Fatalf("CommitGoalEdit = %q, %v", text, err)
	}
	if m.GoalEditing || m.GoalInput != nil {
		t.Fatal("commit should leave edit mode and drop the buffer")
	}

	// 空白：返回 ErrGoalEmpty，仍留在编辑态。
	m.BeginGoalEdit()
	m.GoalInput.Clear()
	m.GoalInput.InsertText("   ")
	if _, err := m.CommitGoalEdit(); err != ErrGoalEmpty {
		t.Fatalf("empty commit err = %v, want ErrGoalEmpty", err)
	}
	if !m.GoalEditing {
		t.Fatal("failed commit should keep edit mode")
	}

	// 超长：1025 个字符被拒绝，仍留在编辑态。
	m.GoalInput.Clear()
	m.GoalInput.InsertText(strings.Repeat("目", MaxGoalRunes+1))
	if _, err := m.CommitGoalEdit(); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long commit err = %v, want too-long error", err)
	}
	if !m.GoalEditing {
		t.Fatal("failed commit should keep edit mode")
	}
}

// TestGoalCancelAndSnapshot 验证关闭看板丢弃编辑态，goal 快照整体替换
// （空串清除），且 goal 不进入正文时间线。
func TestGoalCancelAndSnapshot(t *testing.T) {
	m := New()
	m.ActivateSession("s1", "会话")
	goalCaps(t, m, true)
	m.OpenGoalModal()
	m.BeginGoalEdit()
	m.CancelGoalModal()
	if m.Modal != NoModal || m.GoalEditing || m.GoalInput != nil {
		t.Fatalf("cancel left modal=%v editing=%v input=%v", m.Modal, m.GoalEditing, m.GoalInput != nil)
	}

	before := len(m.Active.Messages)
	m.ApplyEvent(&acp.GoalUpdateEvent{SessionID: "s1", Goal: "服务端目标"})
	if got := m.GoalText(); got != "服务端目标" {
		t.Fatalf("goal = %q", got)
	}
	m.ApplyEvent(&acp.GoalUpdateEvent{SessionID: "s1", Goal: ""})
	if got := m.GoalText(); got != "" {
		t.Fatalf("goal should be cleared, got %q", got)
	}
	if len(m.Active.Messages) != before {
		t.Fatal("goal updates must not add conversation messages")
	}
}
