package model

// 会话定时任务（alk.cxykevin.top/cron）的纯状态逻辑：/cron 面板的打开/关闭、
// 选中移动与本地快照维护（见 ../alkaid0/docs/acp/extension.md §3.11）。
// 任务由 AI 的 cron 工具创建，客户端只负责查看与删除。

import "github.com/cxykevin/alcoh/internal/acp"

// CronAvailable 报告 /cron 面板是否可用：需要服务端 cron 能力与活动会话。
func (m *AppModel) CronAvailable() bool {
	return m.Active != nil && m.SupportsCron()
}

// CronJobs 返回当前会话的定时任务快照（服务端按 id 升序）；无活动会话时为空。
func (m *AppModel) CronJobs() []acp.CronJob {
	if m.Active == nil {
		return nil
	}
	return m.Active.Crons
}

// OpenCronModal 打开定时任务面板。任务列表为空时同样打开（显示空状态），
// 由调用方先做能力校验并给出提示。返回是否成功打开。
func (m *AppModel) OpenCronModal() bool {
	if !m.CronAvailable() {
		return false
	}
	m.CloseSlash()
	m.CronSelect = 0
	m.clampCronSelect()
	m.SetModal(ModalCron)
	return true
}

// CancelCronModal 关闭定时任务面板且不做任何修改。
func (m *AppModel) CancelCronModal() {
	if m.Modal == ModalCron {
		m.SetModal(NoModal)
	}
}

// CronMove 上下移动定时任务面板的选中项（带环绕）。
func (m *AppModel) CronMove(delta int) {
	n := len(m.CronJobs())
	if n == 0 {
		m.CronSelect = 0
		return
	}
	m.CronSelect = (m.CronSelect + delta + n) % n
}

// CronSelected 返回面板当前选中的定时任务。
func (m *AppModel) CronSelected() (acp.CronJob, bool) {
	if m.Modal != ModalCron {
		return acp.CronJob{}, false
	}
	jobs := m.CronJobs()
	if len(jobs) == 0 {
		return acp.CronJob{}, false
	}
	idx := m.CronSelect
	if idx < 0 || idx >= len(jobs) {
		idx = len(jobs) - 1
	}
	return jobs[idx], true
}

// ApplyCrons 用 cron/list 的全量结果替换本地快照，并收敛选中索引。
func (m *AppModel) ApplyCrons(jobs []acp.CronJob) {
	if m.Active == nil {
		return
	}
	m.Active.Crons = append([]acp.CronJob(nil), jobs...)
	m.clampCronSelect()
}

// RemoveCron 从本地快照移除一条定时任务并收敛选中索引（删除成功后立即从面板
// 消失；随后刷新列表以服务端为准）。返回是否找到该任务。
func (m *AppModel) RemoveCron(id uint32) bool {
	if m.Active == nil {
		return false
	}
	jobs := m.Active.Crons
	for i, job := range jobs {
		if job.ID != id {
			continue
		}
		m.Active.Crons = append(jobs[:i], jobs[i+1:]...)
		m.clampCronSelect()
		return true
	}
	return false
}

// clampCronSelect 把选中索引收敛到当前任务范围内：任务变少时选中项顺移到
// 最后一条，而不是停在越界位置。
func (m *AppModel) clampCronSelect() {
	n := len(m.CronJobs())
	if n == 0 {
		m.CronSelect = 0
		return
	}
	if m.CronSelect < 0 {
		m.CronSelect = 0
	}
	if m.CronSelect >= n {
		m.CronSelect = n - 1
	}
}
