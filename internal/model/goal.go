package model

// 会话 goal（alk.cxykevin.top/goal）的纯状态逻辑：/goal 看板的打开/关闭、
// 编辑态输入缓冲与本地快照维护（见 ../alkaid0/docs/acp/extension.md §2.9）。
// goal 由服务端推送快照，客户端不假设它会跨任务保留。

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cxykevin/alcoh/internal/widget"
)

// MaxGoalRunes 是服务端对 goal 的长度上限（按 Unicode 码点计）。
const MaxGoalRunes = 1024

// ErrGoalEmpty 表示编辑结果去掉首尾空白后为空；清空目标应使用 /goal clear。
var ErrGoalEmpty = errors.New("goal is empty")

// GoalAvailable 报告 /goal 面板是否可用：需要服务端 goal 能力与活动会话。
func (m *AppModel) GoalAvailable() bool {
	return m.Active != nil && m.SupportsGoal()
}

// GoalText 返回当前会话的 goal 快照；无活动会话时为空。
func (m *AppModel) GoalText() string {
	if m.Active == nil {
		return ""
	}
	return m.Active.Goal
}

// OpenGoalModal 打开 goal 看板（只读）。由调用方先做能力校验并给出提示。
func (m *AppModel) OpenGoalModal() bool {
	if !m.GoalAvailable() {
		return false
	}
	m.CloseSlash()
	m.GoalEditing = false
	m.GoalInput = nil
	m.SetModal(ModalGoal)
	return true
}

// CancelGoalModal 关闭 goal 看板并丢弃编辑态。
func (m *AppModel) CancelGoalModal() {
	if m.Modal == ModalGoal {
		m.GoalEditing = false
		m.GoalInput = nil
		m.SetModal(NoModal)
	}
}

// BeginGoalEdit 进入编辑态：输入缓冲以当前 goal 为初始内容，光标停在末尾。
func (m *AppModel) BeginGoalEdit() bool {
	if m.Modal != ModalGoal || !m.GoalAvailable() {
		return false
	}
	m.GoalInput = widget.NewInputBuffer()
	m.GoalInput.InsertText(m.GoalText())
	m.GoalEditing = true
	return true
}

// CancelGoalEdit 退出编辑态但保留看板，丢弃草稿。
func (m *AppModel) CancelGoalEdit() {
	if m.Modal != ModalGoal {
		return
	}
	m.GoalEditing = false
	m.GoalInput = nil
}

// GoalDraft 返回编辑态输入框的当前文本；不在编辑态时为空。
func (m *AppModel) GoalDraft() string {
	if !m.GoalEditing || m.GoalInput == nil {
		return ""
	}
	return m.GoalInput.Text()
}

// CommitGoalEdit 校验并提交编辑态草稿，返回需要发给服务端的 goal 文本。
// 首尾空白会被去掉；空文本返回 ErrGoalEmpty（清空请用 /goal clear），
// 超过 MaxGoalRunes 个字符返回带实际数量的错误。成功时退出编辑态。
func (m *AppModel) CommitGoalEdit() (string, error) {
	if m.Modal != ModalGoal || !m.GoalEditing || m.GoalInput == nil {
		return "", ErrGoalEmpty
	}
	text := strings.TrimSpace(m.GoalInput.Text())
	if text == "" {
		return "", ErrGoalEmpty
	}
	if n := utf8.RuneCountInString(text); n > MaxGoalRunes {
		return "", fmt.Errorf("goal is too long: %d runes (max %d)", n, MaxGoalRunes)
	}
	m.GoalEditing = false
	m.GoalInput = nil
	return text, nil
}

// ApplyGoal 用服务端快照替换当前会话的 goal；空字符串表示目标已清除。
func (m *AppModel) ApplyGoal(goal string) {
	if m.Active == nil {
		return
	}
	m.Active.Goal = goal
}
