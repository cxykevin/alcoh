package app

// /cron 定时任务面板的交互测试：打开面板拉取列表、d 删除选中任务后重新查询，
// 以及缺少 cron 能力时命令不可用。

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// cronBackend 包装 demo backend，声明 cron 能力并记录任务列表与删除请求。
type cronBackend struct {
	*demo.Backend
	mu      sync.Mutex
	jobs    []acp.CronJob
	lists   int
	deletes []uint32
}

func (b *cronBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/cron":{}}`)}
}

func (b *cronBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &cronSession{Session: s, b: b}, nil
}

func (b *cronBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &cronSession{Session: s, b: b}, nil
}

// cronSession 实现 acp.CronControl；任务状态保存在 backend 上。
type cronSession struct {
	acp.Session
	b *cronBackend
}

func (s *cronSession) ListCrons(ctx context.Context) ([]acp.CronJob, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.lists++
	return append([]acp.CronJob(nil), s.b.jobs...), nil
}

func (s *cronSession) DeleteCron(ctx context.Context, id uint32) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.deletes = append(s.b.deletes, id)
	for i, job := range s.b.jobs {
		if job.ID == id {
			s.b.jobs = append(s.b.jobs[:i], s.b.jobs[i+1:]...)
			break
		}
	}
	return nil
}

// TestCronPanelListAndDelete 验证 /cron 打开面板并显示服务端任务，d 取消选中
// 任务后重新拉取列表。
func TestCronPanelListAndDelete(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &cronBackend{Backend: demo.New(true)}
	b.jobs = []acp.CronJob{
		{ID: 1, Cron: "0 9 * * *", Prompt: "第一条定时任务", NextRuns: []string{"2026-10-25 09:00:00 GMT+8"}},
		{ID: 2, Cron: "*/30 * * * *", Prompt: "第二条定时任务", RunOnce: true, NextRuns: []string{"2026-10-24 12:30:00 GMT+8"}},
	}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "/cron")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalCron && len(s.CronJobs) == 2
	})
	if s := a.snapshot(); s.CronSel != 0 {
		t.Errorf("cron selection = %d, want first job", s.CronSel)
	}

	ft.sendKey(input.RuneKey('d', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalCron && len(s.CronJobs) == 1 && s.CronJobs[0].ID == 2
	})
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	time.Sleep(50 * time.Millisecond)

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	deleted := append([]uint32(nil), b.deletes...)
	lists := b.lists
	ids := make([]uint32, 0, len(b.jobs))
	for _, job := range b.jobs {
		ids = append(ids, job.ID)
	}
	b.mu.Unlock()
	if len(deleted) != 1 || deleted[0] != 1 {
		t.Errorf("deleted = %v, want [1]", deleted)
	}
	if lists != 2 {
		t.Errorf("list calls = %d, want 2 (open + refresh after delete)", lists)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Errorf("remaining jobs = %v, want [2]", ids)
	}
}

// TestCronUnavailableWithoutCapability 验证服务端未声明 cron 能力时 /cron
// 只提示不可用，不打开面板。
func TestCronUnavailableWithoutCapability(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	a.model.ActivateSession("s1", "会话")
	a.openCron()
	if a.model.Modal == model.ModalCron {
		t.Fatal("cron modal must not open without the capability")
	}
	if a.model.Error == "" {
		t.Fatal("expected an error explaining /cron is unavailable")
	}
}
