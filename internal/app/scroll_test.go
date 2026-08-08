package app

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// waitSessionEntered 等待会话进入。经 App.snapshot 读取，避免与事件循环并发读写。
func waitSessionEntered(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.snapshot().HasActive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("session never entered")
}

// waitIdle 等待会话进入 idle（含权限弹窗关闭路径）。经 App.snapshot 读取。
func waitIdle(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := a.snapshot()
		if snap.HasActive && snap.ActiveState == acp.StateIdle && snap.Modal == model.NoModal {
			return
		}
		// 若权限弹窗打开则批准，让脚本继续。
		if snap.Modal == model.ModalPermission {
			ft := a.term.(*fakeTerm)
			ft.sendKey(input.RuneKey('a', input.ModNone))
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("session never idle")
}

func TestWheelAndPageScroll(t *testing.T) {
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	// 首页输入框输入 prompt 并按 Enter 创建会话
	for _, r := range "帮我实现一个 TCP 服务器" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitIdle(t, a)

	if !a.snapshot().HasActive {
		t.Fatal("no active session")
	}

	// 初始应贴底：FollowBottom 为 true。
	if !a.snapshot().FollowBottom {
		t.Error("session should start follow-bottom")
	}

	// 滚轮向上 1 步：解除贴底，从底部偏移向上滚动 3 行。
	ft.sendMouse(input.MouseEvent{Button: input.MouseWheelUp, Action: input.MousePress, X: 50, Y: 10})
	time.Sleep(100 * time.Millisecond)
	afterWheelUp := a.snapshot().ActiveScroll
	if a.snapshot().FollowBottom {
		t.Error("wheel up should detach from bottom")
	}
	t.Logf("after wheel up: scroll=%d", afterWheelUp)

	// 滚轮向下：回到底部并重新贴底。
	ft.sendMouse(input.MouseEvent{Button: input.MouseWheelDown, Action: input.MousePress, X: 50, Y: 10})
	time.Sleep(100 * time.Millisecond)
	if scroll := a.snapshot().ActiveScroll; scroll != afterWheelUp {
		t.Logf("after wheel down: scroll=%d", scroll)
	}

	// PageUp：向上翻半屏。
	ft.sendKey(input.SimpleKey(input.KeyPageUp))
	time.Sleep(100 * time.Millisecond)
	afterPageUp := a.snapshot().ActiveScroll
	t.Logf("after page up: scroll=%d", afterPageUp)
	if afterPageUp > afterWheelUp {
		t.Errorf("page up should scroll up: before=%d after=%d", afterWheelUp, afterPageUp)
	}

	// PageDown：回到底部。
	ft.sendKey(input.SimpleKey(input.KeyPageDown))
	time.Sleep(100 * time.Millisecond)
	afterPageDown := a.snapshot().ActiveScroll
	t.Logf("after page down: scroll=%d", afterPageDown)

	// End：明确贴底。
	ft.sendKey(input.SimpleKey(input.KeyEnd))
	time.Sleep(100 * time.Millisecond)
	if !a.snapshot().FollowBottom {
		t.Error("End should re-follow bottom")
	}

	// 退出
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}
