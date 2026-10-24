package acp

// 会话定时任务（alk.cxykevin.top/cron）的协议测试：能力门控、RPC 参数形状与
// cron/list 响应解码。

import (
	"context"
	"encoding/json"
	"testing"
)

func cronTestCaps(t *testing.T, b *ClientBackend, cron bool) {
	t.Helper()
	raw := map[string]json.RawMessage{}
	if cron {
		raw[Alkaid0CapabilityCron] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: data}
	b.state = backendReady
	b.mu.Unlock()
}

// TestCronRequiresCapability 验证 cron RPC 只在服务端声明 cron 能力时可用，
// 缺少能力时不发任何请求。
func TestCronRequiresCapability(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.ListCrons(context.Background()); err == nil {
		t.Fatal("expected error without cron capability")
	}
	if err := sess.DeleteCron(context.Background(), 3); err == nil {
		t.Fatal("expected error without cron capability")
	}
	if len(ft.requests) != 1 {
		t.Fatalf("no cron RPC should be sent before the capability gate, requests = %v", ft.requests)
	}
}

// TestCronListDecodesJobs 验证 list 带 sessionId、解析任务字段，并把 nil jobs
// 归一为空数组。
func TestCronListDecodesJobs(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodCronList, func(params, result any) error {
		p := params.(CronListParams)
		if p.SessionID != "s1" {
			t.Fatalf("params = %#v", p)
		}
		r := result.(*CronListResult)
		r.Jobs = []CronJob{{
			ID:       3,
			Cron:     "0 9 * * *",
			Prompt:   "Summarize yesterday's commits",
			RunOnce:  false,
			Workdir:  "/tmp/project",
			NextRuns: []string{"2026-10-25 09:00:00 GMT+8"},
		}}
		return nil
	})
	b := probeClient(t, ft)
	cronTestCaps(t, b, true)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}

	jobs, err := sess.ListCrons(context.Background())
	if err != nil {
		t.Fatalf("ListCrons: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
	job := jobs[0]
	if job.ID != 3 || job.Cron != "0 9 * * *" || job.Prompt != "Summarize yesterday's commits" ||
		job.RunOnce || job.Workdir != "/tmp/project" || len(job.NextRuns) != 1 {
		t.Fatalf("job = %#v", job)
	}

	// nil jobs 归一为空数组，方便 UI 直接使用。
	ft.handlers[MethodCronList] = func(params, result any) error {
		result.(*CronListResult).Jobs = nil
		return nil
	}
	jobs, err = sess.ListCrons(context.Background())
	if err != nil {
		t.Fatalf("ListCrons: %v", err)
	}
	if jobs == nil {
		t.Fatal("nil jobs should be normalized to an empty slice")
	}
}

// TestCronDeleteParams 验证 delete 带 sessionId 与 id。
func TestCronDeleteParams(t *testing.T) {
	ft := &fakeTransport{}
	var got []CronDeleteParams
	ft.set(MethodCronDelete, func(params, result any) error {
		got = append(got, params.(CronDeleteParams))
		result.(*CronDeleteResult).ID = params.(CronDeleteParams).ID
		return nil
	})
	b := probeClient(t, ft)
	cronTestCaps(t, b, true)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}

	if err := sess.DeleteCron(context.Background(), 7); err != nil {
		t.Fatalf("DeleteCron: %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "s1" || got[0].ID != 7 {
		t.Fatalf("delete params = %#v", got)
	}
}
