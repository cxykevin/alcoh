package model

// /cron 面板的纯状态测试：能力门控、选中移动、快照替换/删除与命令面板接线。

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

func cronCaps(cron bool) acp.AgentCapabilities {
	raw := map[string]json.RawMessage{}
	if cron {
		raw[acp.Alkaid0CapabilityCron] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	return acp.AgentCapabilities{Raw: data}
}

// TestCronAvailability 验证 /cron 需要服务端 cron 能力与活动会话同时具备，
// 且命令只在两者都满足时进入命令面板。
func TestCronAvailability(t *testing.T) {
	m := New()
	if m.SupportsCron() || m.CronAvailable() {
		t.Fatal("cron must be unavailable without the capability")
	}
	m.AgentCaps = cronCaps(true)
	if !m.SupportsCron() {
		t.Fatal("SupportsCron should follow the capability marker")
	}
	if m.CronAvailable() {
		t.Fatal("cron must be unavailable without an active session")
	}
	if slices.Contains(m.SlashCommands(), "/cron") {
		t.Fatal("/cron should not appear without an active session")
	}
	m.ActivateSession("s1", "会话")
	if !m.CronAvailable() {
		t.Fatal("cron should be available with capability and active session")
	}
	if !slices.Contains(m.SlashCommands(), "/cron") {
		t.Fatal("/cron should appear in the command palette")
	}
}

// TestCronModalSelectionAndRemove 验证面板打开/选中移动/快照替换/删除：
// 选中索引带环绕，任务变少时收敛到范围内，删除后选中项顺移。
func TestCronModalSelectionAndRemove(t *testing.T) {
	m := New()
	m.AgentCaps = cronCaps(true)
	m.ActivateSession("s1", "会话")
	if !m.OpenCronModal() {
		t.Fatal("OpenCronModal should succeed")
	}
	if m.Modal != ModalCron || m.CronSelect != 0 {
		t.Fatalf("modal = %v select = %d, want ModalCron/0", m.Modal, m.CronSelect)
	}
	// 空列表：移动不 panic，选中项为空。
	m.CronMove(1)
	if _, ok := m.CronSelected(); ok {
		t.Fatal("empty list should not report a selected job")
	}

	m.ApplyCrons([]acp.CronJob{{ID: 1}, {ID: 2}, {ID: 3}})
	if len(m.CronJobs()) != 3 || m.CronSelect != 0 {
		t.Fatalf("jobs = %v select = %d", m.CronJobs(), m.CronSelect)
	}
	m.CronMove(-1)
	if m.CronSelect != 2 {
		t.Errorf("CronMove(-1) from first = %d, want 2 (wrap)", m.CronSelect)
	}
	m.CronMove(1)
	if m.CronSelect != 0 {
		t.Errorf("CronMove(1) from last = %d, want 0 (wrap)", m.CronSelect)
	}

	// 越界索引回退到最后一条而不是空结果。
	m.CronSelect = 9
	if job, ok := m.CronSelected(); !ok || job.ID != 3 {
		t.Errorf("CronSelected out of range = %v/%v, want job 3", job.ID, ok)
	}

	// 删除选中项：移除后索引收敛；删除全部后选中回到 0。
	m.CronSelect = 0
	if !m.RemoveCron(1) {
		t.Fatal("RemoveCron(1) should succeed")
	}
	if len(m.CronJobs()) != 2 || m.CronSelect != 0 {
		t.Fatalf("after remove: jobs = %v select = %d", m.CronJobs(), m.CronSelect)
	}
	if !m.RemoveCron(2) || !m.RemoveCron(3) {
		t.Fatal("RemoveCron should remove remaining jobs")
	}
	if len(m.CronJobs()) != 0 || m.CronSelect != 0 {
		t.Fatalf("after removing all: jobs = %v select = %d", m.CronJobs(), m.CronSelect)
	}
	if m.RemoveCron(1) {
		t.Fatal("RemoveCron should report a missing job")
	}

	m.CancelCronModal()
	if m.Modal != NoModal {
		t.Fatal("CancelCronModal should close the modal")
	}
}

// TestApplyCronsCopiesSnapshot 验证 cron/list 的结果被拷贝，调用方复用底层
// 数组不会污染面板内容。
func TestApplyCronsCopiesSnapshot(t *testing.T) {
	m := New()
	m.AgentCaps = cronCaps(true)
	m.ActivateSession("s1", "会话")
	jobs := []acp.CronJob{{ID: 1, Prompt: "one"}}
	m.ApplyCrons(jobs)
	jobs[0].Prompt = "mutated"
	if got := m.CronJobs()[0].Prompt; got != "one" {
		t.Fatalf("snapshot should be copied, got %q", got)
	}
}
