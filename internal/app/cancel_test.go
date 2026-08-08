package app

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// TestEscCancel 验证：AI 响应进行中按 Esc 会取消（会话回到 idle）。
// 会话空闲时按 Esc 不产生副作用。
func TestEscCancel(t *testing.T) {
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	// 首页输入框输入 prompt 并按 Enter 创建会话并发送 prompt。
	for _, r := range "帮我实现一个 TCP 服务器" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	// 等待会话建立并进入 running。经 App.snapshot 读取，避免与事件循环并发读写。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := a.snapshot()
		if snap.HasActive && snap.ActiveRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !a.snapshot().ActiveRunning {
		t.Fatal("session never entered running state")
	}

	// 按 Esc 打断。
	ft.sendKey(input.SimpleKey(input.KeyEsc))

	// 等待会话回到 idle。
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := a.snapshot()
		if snap.HasActive && snap.ActiveState == acp.StateIdle {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if snap := a.snapshot(); !snap.HasActive || snap.ActiveState != acp.StateIdle {
		t.Fatalf("session not cancelled after Esc, state=%v", snap.ActiveState)
	}

	// 空闲时再按一次 Esc：不应触发任何动作（会话保持 idle、不退出）。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	time.Sleep(150 * time.Millisecond)
	if a.snapshot().Quitting {
		t.Fatal("Esc on idle session should not quit")
	}
	if snap := a.snapshot(); !snap.HasActive || snap.ActiveState != acp.StateIdle {
		t.Fatalf("idle session state changed after Esc: %v", snap.ActiveState)
	}

	// 退出。
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}
