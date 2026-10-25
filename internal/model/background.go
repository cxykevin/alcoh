package model

// 会话后台运行模式（/background）的客户端覆盖指令状态。
//
// /background 由客户端拦截执行：直接以服务端命令形式发送（服务端不推送模式
// 变化，客户端再经 alk.cxykevin.top/session/get_background 同步，见
// app/background.go），始终不进入消息队列。这里只维护面板可用性与本地状态。

// BackgroundAvailable 报告 /background 是否可用：需要服务端声明 alkaid0
// 扩展能力（后台模式是 alkaid0 的会话命令）且有活动会话。
func (m *AppModel) BackgroundAvailable() bool {
	return m.Active != nil && m.SupportsAlkaid0()
}

// Background 报告当前会话的后台运行模式；无活动会话时为 false。
func (m *AppModel) Background() bool {
	return m.Active != nil && m.Active.Background
}

// SetBackground 写入当前会话的后台运行模式（无活动会话时忽略）。
func (m *AppModel) SetBackground(on bool) {
	if m.Active != nil {
		m.Active.Background = on
	}
}
