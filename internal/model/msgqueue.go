package model

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
)

// 消息队列项的 kind（alkaid0 msgqueue，见 extension.md §2.8）。
const (
	// MsgQueueKindPrompt 是普通待发送消息；出队时才写入主消息库并进入正文。
	MsgQueueKindPrompt = "prompt"
	// MsgQueueKindCommand 是参与 loop 的斜杠命令；出队时执行。
	MsgQueueKindCommand = "command"
)

// SupportsMsgQueue 报告服务端是否支持会话消息队列：必须同时声明 alkaid0 v0.6
// 与 alk.cxykevin.top/msgqueue。队列实现与 v0.6 同期（未发版），只声明
// msgqueue 的服务端不按队列客户端对待。
func (m *AppModel) SupportsMsgQueue() bool {
	return m.AgentCaps.Has(acp.Alkaid0CapabilityV06) && m.AgentCaps.Has(acp.Alkaid0CapabilityMsgQueue)
}

// QueueAvailable 报告 /queue 面板是否可用：需要服务端能力与活动会话。
func (m *AppModel) QueueAvailable() bool {
	return m.Active != nil && m.SupportsMsgQueue()
}

// QueueItems 返回当前会话的排队项快照（FIFO 顺序）；无活动会话时为空。
func (m *AppModel) QueueItems() []acp.MsgQueueItem {
	if m.Active == nil {
		return nil
	}
	return m.Active.MsgQueue
}

// QueueItem 按 ID 查找排队项。
func (m *AppModel) QueueItem(id string) (acp.MsgQueueItem, bool) {
	for _, item := range m.QueueItems() {
		if item.ID == id {
			return item, true
		}
	}
	return acp.MsgQueueItem{}, false
}

// OpenQueueModal 打开消息队列面板。队列为空时同样打开（显示空状态），由调用方
// 先做能力校验并给出提示。返回是否成功打开。
func (m *AppModel) OpenQueueModal() bool {
	if !m.QueueAvailable() {
		return false
	}
	m.CloseSlash()
	m.clampQueueSelect()
	m.SetModal(ModalMsgQueue)
	return true
}

// CancelQueueModal 关闭消息队列面板且不做任何修改。
func (m *AppModel) CancelQueueModal() {
	if m.Modal == ModalMsgQueue {
		m.SetModal(NoModal)
	}
}

// QueueMove 上下移动队列面板的选中项（带环绕）。
func (m *AppModel) QueueMove(delta int) {
	n := len(m.QueueItems())
	if n == 0 {
		m.QueueSelect = 0
		return
	}
	m.QueueSelect = (m.QueueSelect + delta + n) % n
}

// QueueSelected 返回队列面板当前选中的排队项。
func (m *AppModel) QueueSelected() (acp.MsgQueueItem, bool) {
	if m.Modal != ModalMsgQueue && !m.QueueFocused {
		return acp.MsgQueueItem{}, false
	}
	items := m.QueueItems()
	if len(items) == 0 {
		return acp.MsgQueueItem{}, false
	}
	idx := m.QueueSelect
	if idx < 0 || idx >= len(items) {
		idx = len(items) - 1
	}
	return items[idx], true
}

// clampQueueSelect 把选中索引收敛到当前队列范围内：队列变短（出队/取消/清空）
// 时选中项顺移到最后一条，而不是停在越界位置。
func (m *AppModel) clampQueueSelect() {
	n := len(m.QueueItems())
	if n == 0 {
		m.QueueSelect = 0
		m.QueueFocused = false
		return
	}
	if m.QueueSelect < 0 {
		m.QueueSelect = 0
	}
	if m.QueueSelect >= n {
		m.QueueSelect = n - 1
	}
}

// FocusQueue 让输入框上方的排队框获得焦点，默认选中最后一条（FIFO 队列底部，
// 最近的排队项）：继续按 ↑ 才向更早的排队项移动，到顶后再按 ↑ 交还给输入历史。
func (m *AppModel) FocusQueue() bool {
	if !m.HasActive() || len(m.QueueItems()) == 0 {
		return false
	}
	m.QueueFocused = true
	m.QueueSelect = len(m.QueueItems()) - 1
	return true
}

// BlurQueue 取消排队框焦点，按键回到输入框。
func (m *AppModel) BlurQueue() { m.QueueFocused = false }

// QueueFocusMove 在排队框内移动选中项；返回 false 表示已到边界（没有移动）。
// 调用方据此决定 ↑ 到顶后是否转交输入历史。
func (m *AppModel) QueueFocusMove(delta int) bool {
	if !m.QueueFocused {
		return false
	}
	n := len(m.QueueItems())
	if n == 0 {
		m.QueueFocused = false
		return false
	}
	next := m.QueueSelect + delta
	if next < 0 || next >= n {
		return false
	}
	m.QueueSelect = next
	return true
}

// RemoveQueueItem 从本地快照移除一条排队项并收敛选中索引（删除/取消后立即
// 从输入框上方消失；服务端事件随后到达时内容一致）。
func (m *AppModel) RemoveQueueItem(id string) bool {
	if !m.HasActive() || id == "" {
		return false
	}
	items := m.Active.MsgQueue
	for i, item := range items {
		if item.ID != id {
			continue
		}
		m.Active.MsgQueue = append(items[:i], items[i+1:]...)
		m.clampQueueSelect()
		return true
	}
	return false
}

// QueueItemInputText 返回把排队项放回输入框时应显示的文本：prompt 用原文，
// command 还原成可重新发送的完整命令。
func QueueItemInputText(item acp.MsgQueueItem) string {
	if item.Kind == MsgQueueKindCommand {
		if text := strings.TrimSpace(item.Command + " " + item.Args); text != "" {
			return text
		}
	}
	return item.Text
}
