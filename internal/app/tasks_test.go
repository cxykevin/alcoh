package app

// /tasks 任务列表面板的交互测试：命令打开面板（列表为空时也显示空状态）、
// Esc 关闭。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// tasksBackend 包装 demo backend，声明 alkaid0 扩展能力（/tasks 的门控）。
type tasksBackend struct{ *demo.Backend }

func (b *tasksBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
}

// TestTasksPanelOpenAndClose 验证 /tasks 打开任务列表面板，Esc 返回会话。
func TestTasksPanelOpenAndClose(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, &tasksBackend{Backend: demo.New(true)})
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "/tasks")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalTasks
	})

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.NoModal
	})

	quitApp(ft)
	waitRun(t, done)
}
