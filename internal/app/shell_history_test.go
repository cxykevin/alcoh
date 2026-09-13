package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// historyBackend 在 demo backend 之上声明 alkaid0 v0.5/v0.6 能力，并用自有事件
// 通道广播 terminal/history 查询结果（与真实客户端 clientSession.TerminalHistory
// 的行为一致），用于验证历史 shell 的拉取、应用与面板展示接线。
type historyBackend struct {
	*demo.Backend
	events chan acp.Event

	mu        sync.Mutex
	history   []acp.TerminalInfo
	active    []acp.TerminalInfo
	calls     int
	listCalls int
}

func newHistoryBackend() *historyBackend {
	return &historyBackend{Backend: demo.New(true), events: make(chan acp.Event, 64)}
}

func (b *historyBackend) Events() <-chan acp.Event { return b.events }

func (b *historyBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"session\":{\"delete\":{}},\"alk.cxykevin.top/alkaid0/v0.4\":{},\"alk.cxykevin.top/alkaid0/v0.5\":{},\"alk.cxykevin.top/alkaid0/v0.6\":{}}")}
}

func (b *historyBackend) setHistory(infos []acp.TerminalInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.history = infos
}

func (b *historyBackend) setActive(infos []acp.TerminalInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active = infos
}

func (b *historyBackend) historyCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func (b *historyBackend) listCallsCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.listCalls
}

func (b *historyBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &historySession{Session: s, b: b}, nil
}

// historySession 让会话句柄实现 acp.TerminalControl：terminal/history 返回预置
// 结果，并按真实客户端的方式把结果作为 TerminalHistoryEvent 广播给 UI。
type historySession struct {
	acp.Session
	b *historyBackend
}

// ListTerminals 返回预置的活动终端（前台 run 只有查列表才能看到），
// 并按真实客户端的方式广播 TerminalListEvent。
func (s *historySession) ListTerminals(context.Context) ([]acp.TerminalInfo, error) {
	s.b.mu.Lock()
	s.b.listCalls++
	infos := append([]acp.TerminalInfo(nil), s.b.active...)
	s.b.mu.Unlock()
	select {
	case s.b.events <- &acp.TerminalListEvent{SessionID: s.ID(), Terminals: infos}:
	default:
	}
	return infos, nil
}

func (s *historySession) TerminalStatus(context.Context, string) (acp.TerminalInfo, error) {
	return acp.TerminalInfo{}, nil
}

func (s *historySession) StopTerminal(context.Context, string) error { return nil }

func (s *historySession) TerminalHistory(context.Context, string) ([]acp.TerminalInfo, error) {
	s.b.mu.Lock()
	s.b.calls++
	infos := append([]acp.TerminalInfo(nil), s.b.history...)
	s.b.mu.Unlock()
	select {
	case s.b.events <- &acp.TerminalHistoryEvent{SessionID: s.ID(), Terminals: infos}:
	default:
	}
	return infos, nil
}

// TestShellHistoryFetchedOnSessionEntry 验证进入会话会拉取已结束 shell 的内容，
// 面板在没有活跃 shell 时也能打开，并先列历史、再等活跃。
func TestShellHistoryFetchedOnSessionEntry(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newHistoryBackend()
	b.setHistory([]acp.TerminalInfo{
		{TerminalID: "@temp/run/1", Status: "finished", Command: "go build ./...", Content: "build ok\n"},
		{TerminalID: "@temp/run/2", Status: "killed", Command: "npm run dev", Content: "server up\n"},
	})
	// 正在运行的前台终端：只出现在 terminal/list 结果里。
	b.setActive([]acp.TerminalInfo{
		{TerminalID: "@temp/run/3", Status: "running", Command: "npm run dev", Kind: "shell"},
	})
	a := New(ft, b)
	done := runApp(t, a)

	// 主页输入 prompt 回车：复用预创建会话进入会话视图。
	time.Sleep(200 * time.Millisecond)
	for _, r := range "hi" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap := a.snapshot(); len(snap.HistoryIDs) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := a.snapshot()
	if len(snap.HistoryIDs) != 2 || len(snap.HistoryTexts) != 2 {
		t.Fatalf("history = %v %v (rpc calls=%d)", snap.HistoryIDs, snap.HistoryTexts, b.historyCalls())
	}
	// 历史段最新在前：服务端按 createdAt 升序返回，客户端倒序展示。
	if snap.HistoryIDs[0] != "@temp/run/2" || snap.HistoryTexts[0] != "server up\n" {
		t.Fatalf("history[0] = %s %q (want newest first)", snap.HistoryIDs[0], snap.HistoryTexts[0])
	}
	if snap.HistoryIDs[1] != "@temp/run/1" {
		t.Fatalf("history[1] = %s", snap.HistoryIDs[1])
	}
	if b.historyCalls() == 0 {
		t.Fatal("terminal/history was never requested")
	}
	if b.listCallsCount() == 0 {
		t.Fatal("terminal/list was never requested")
	}
	// 列表顺序：活动终端在前（来自 terminal/list），历史在后。
	if len(snap.ShellIDs) != 3 || snap.ShellIDs[0] != "@temp/run/3" {
		t.Fatalf("panel list = %v (running terminal must be listed first)", snap.ShellIDs)
	}
	ft.sendKey(input.SimpleKey(input.KeyDown))
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.snapshot().ShellPanel {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !a.snapshot().ShellPanel {
		t.Fatal("shell panel must open when only history shells exist")
	}

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}
