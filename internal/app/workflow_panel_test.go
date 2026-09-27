package app

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// workflowRunID 是测试里 workflow 终端的 run id（与 terminal id 同值）。
const workflowRunID = "@temp/run/7"

// workflowBackend 在 demo backend 之上声明 alkaid0 v0.5 能力，并用自有事件通道
// 广播 workflow/status 结果（与真实客户端 clientSession.WorkflowStatus 的行为
// 一致），用于验证面板里 workflow 预览的拉取与按键接线。
type workflowBackend struct {
	*demo.Backend
	events chan acp.Event

	mu          sync.Mutex
	session     string
	status      acp.WorkflowStatusResult
	statusCalls int
}

func newWorkflowBackend() *workflowBackend {
	return &workflowBackend{Backend: demo.New(true), events: make(chan acp.Event, 128)}
}

func (b *workflowBackend) Events() <-chan acp.Event { return b.events }

func (b *workflowBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(
		`{"session":{"delete":{}},"alk.cxykevin.top/alkaid0/v0.4":{},"alk.cxykevin.top/alkaid0/v0.5":{}}`)}
}

func (b *workflowBackend) setStatus(result acp.WorkflowStatusResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = result
}

func (b *workflowBackend) statusCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.statusCalls
}

// sessionID 返回最近创建的会话 ID：workflow 事件按会话归属，推送时要用同一个 ID。
func (b *workflowBackend) sessionID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.session
}

func (b *workflowBackend) push(ev acp.Event) {
	select {
	case b.events <- ev:
	default:
	}
}

func (b *workflowBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.session = s.ID()
	b.mu.Unlock()
	return &workflowSession{Session: s, b: b}, nil
}

// workflowSession 让会话句柄实现 acp.TerminalControl 与 acp.WorkflowControl：
// workflow/status 返回预置快照，并按真实客户端的方式把结果广播给 UI。
type workflowSession struct {
	acp.Session
	b *workflowBackend
}

func (s *workflowSession) ListTerminals(context.Context) ([]acp.TerminalInfo, error) {
	return nil, nil
}

func (s *workflowSession) TerminalStatus(context.Context, string) (acp.TerminalInfo, error) {
	return acp.TerminalInfo{}, nil
}

func (s *workflowSession) StopTerminal(context.Context, string) error { return nil }

func (s *workflowSession) TerminalHistory(context.Context, string) ([]acp.TerminalInfo, error) {
	return nil, nil
}

func (s *workflowSession) WorkflowStatus(_ context.Context, runID string) (acp.WorkflowStatusResult, error) {
	s.b.mu.Lock()
	s.b.statusCalls++
	result := s.b.status
	s.b.mu.Unlock()
	result.RunID = runID
	result.TerminalID = runID
	s.b.push(&acp.WorkflowStatusEvent{SessionID: s.ID(), Result: result})
	return result, nil
}

// TestWorkflowPanelFetchesStatusAndDrivesPanes 验证 shells 面板的 workflow 预览
// 接线：打开面板会查询完整快照（workflow/status）并重建图 / 节点 / 日志，
// Tab 在图上与节点日志之间切焦点，↑↓ 与 hjkl 切换选中节点、k/j 回看日志，
// r 重新拉取。
func TestWorkflowPanelFetchesStatusAndDrivesPanes(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newWorkflowBackend()
	b.setStatus(workflowStatusResult(t))
	a := New(ft, b)
	done := runApp(t, a)

	// 主页输入 prompt 回车：复用预创建会话进入会话视图。
	time.Sleep(200 * time.Millisecond)
	for _, r := range "hi" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.HasActive })

	// 推送 workflow 快照通知：workflow 终端随事件出现在面板列表里。
	b.push(&acp.WorkflowEvent{
		SessionID: b.sessionID(), Kind: acp.WorkflowKindSnapshot,
		RunID: workflowRunID, TerminalID: workflowRunID,
		Workflow: &acp.WorkflowInfo{WorkflowID: "w1", Status: "running", CurrentNode: "collect"},
		Graph: &acp.WorkflowGraph{
			Nodes: map[string]acp.WorkflowGraphNode{"collect": {Name: "采集"}, "merge": {Name: "汇总"}},
			Edges: map[string][]string{"collect": {"merge"}},
			Start: []string{"collect"},
		},
	})
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return len(s.ShellIDs) == 1 && s.ShellIDs[0] == workflowRunID
	})

	// ↓ 打开面板：打开即拉取一次完整快照，日志与图随之重建。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ShellPanel })
	waitSnapshot(t, a, func(s modelSnapshot) bool { return len(s.WorkflowLogs) == workflowTestLogs })
	if b.statusCallCount() == 0 {
		t.Fatal("workflow/status was never requested when opening the panel")
	}
	snap := a.snapshot()
	if len(snap.WorkflowNodes) != 2 || snap.WorkflowNodes[0] != "collect" || snap.WorkflowNodes[1] != "merge" {
		t.Fatalf("workflow nodes = %v", snap.WorkflowNodes)
	}
	if snap.WorkflowSelected != "collect" {
		t.Fatalf("selected node = %q, want the current node", snap.WorkflowSelected)
	}
	if snap.WorkflowLogs[0] != "log-1" || snap.WorkflowLogs[workflowTestLogs-1] != "log-"+strconv.Itoa(workflowTestLogs) {
		t.Fatalf("node logs = %v", snap.WorkflowLogs)
	}
	if snap.ShellWorkflowLog {
		t.Fatal("focus must start on the graph pane")
	}

	// Tab：焦点转到节点日志分栏，k/j 回看 / 回到最新。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ShellWorkflowLog })
	ft.sendKey(input.RuneKey('k', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.WorkflowLogScroll == 3 })
	ft.sendKey(input.RuneKey('j', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.WorkflowLogScroll == 0 })

	// Tab 回图分栏：←→ 与 hl（与 ←→ 同义）切换选中节点。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.ShellWorkflowLog })
	ft.sendKey(input.SimpleKey(input.KeyRight))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.WorkflowSelected == "merge" })
	ft.sendKey(input.RuneKey('h', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.WorkflowSelected == "collect" })
	ft.sendKey(input.RuneKey('l', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.WorkflowSelected == "merge" })
	ft.sendKey(input.SimpleKey(input.KeyLeft))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.WorkflowSelected == "collect" })

	// r：重新拉取完整快照（刷新面板）。
	before := b.statusCallCount()
	ft.sendKey(input.RuneKey('r', input.ModNone))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && b.statusCallCount() == before {
		time.Sleep(20 * time.Millisecond)
	}
	if b.statusCallCount() == before {
		t.Fatal("pressing r must re-query workflow/status")
	}

	// Esc 关闭面板、Ctrl+q 退出确认。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.ShellPanel })
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// workflowTestLogs 是预置快照里节点 collect 的日志行数（足够长，保证日志分栏
// 有可回看的行，k 不会被渲染层收敛回 0）。
const workflowTestLogs = 40

// workflowStatusResult 构造 workflow/status 的响应：图 采集 → 汇总，节点 collect
// 有 workflowTestLogs 行日志与一条终值结果。
func workflowStatusResult(t *testing.T) acp.WorkflowStatusResult {
	t.Helper()
	logs := make([]acp.WorkflowLogEntry, 0, workflowTestLogs+1)
	for i := 1; i <= workflowTestLogs; i++ {
		logs = append(logs, acp.WorkflowLogEntry{
			Sequence: i, Type: "node_log", NodeID: "collect",
			Payload: json.RawMessage(`{"type":"node_log","nodeId":"collect","message":"log-` + strconv.Itoa(i) + `"}`),
		})
	}
	logs = append(logs, acp.WorkflowLogEntry{
		Sequence: workflowTestLogs + 1, Type: "node_result", NodeID: "collect",
		Payload: json.RawMessage(`{"type":"node_result","nodeId":"collect","result":"42"}`),
	})
	return acp.WorkflowStatusResult{
		Status:   "running",
		Workflow: acp.WorkflowInfo{WorkflowID: "w1", Status: "running", CurrentNode: "collect"},
		Graph: &acp.WorkflowGraph{
			Nodes: map[string]acp.WorkflowGraphNode{"collect": {Name: "采集"}, "merge": {Name: "汇总"}},
			Edges: map[string][]string{"collect": {"merge"}},
			Start: []string{"collect"},
		},
		Logs: logs,
	}
}
