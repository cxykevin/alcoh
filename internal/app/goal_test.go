package app

// /goal 会话目标看板的交互测试：/goal 打开看板、e 进入编辑并提交
// （走 goal/set），以及带参 /goal clear 清除目标。

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

// goalBackend 包装 demo backend，声明 goal 能力并记录 goal/set 请求。
type goalBackend struct {
	*demo.Backend
	mu   sync.Mutex
	goal string
	sets []string
}

func (b *goalBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.6":{}}`)}
}

func (b *goalBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &goalSession{Session: s, b: b}, nil
}

func (b *goalBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &goalSession{Session: s, b: b}, nil
}

func (b *goalBackend) setLog() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.sets...)
}

// goalSession 实现 acp.GoalControl；目标状态保存在 backend 上。
type goalSession struct {
	acp.Session
	b *goalBackend
}

func (s *goalSession) GetGoal(context.Context) (string, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	return s.b.goal, nil
}

func (s *goalSession) SetGoal(_ context.Context, goal string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.goal = goal
	s.b.sets = append(s.b.sets, goal)
	return nil
}

// TestGoalPanelEditAndClear 验证看板编辑提交与 /goal clear 都落到 goal/set。
func TestGoalPanelEditAndClear(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &goalBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "/goal")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalGoal
	})

	ft.sendKey(input.RuneKey('e', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalGoal && s.GoalEditing
	})
	typeText(ft, "面板目标")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalGoal && !s.GoalEditing
	})
	waitCondition(t, "goal/set after edit", func() bool {
		sets := b.setLog()
		return len(sets) == 1 && sets[0] == "面板目标"
	})

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.NoModal
	})
	typeText(ft, "/goal clear")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "goal/set clear", func() bool {
		sets := b.setLog()
		return len(sets) == 2 && sets[1] == ""
	})

	quitApp(ft)
	waitRun(t, done)
}
