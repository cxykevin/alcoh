package app

// Esc 快捷键测试：输入框非空时清空输入；输入框为空时连按两次打开撤回
// 选择器。实现见 keymap.go 的 sessionKey 与 escRewindWindow / App.escRewindAt。

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

func escKey(a *App) { a.sessionKey(input.SimpleKey(input.KeyEsc)) }

// TestEscClearsInputWithoutCanceling 验证输入框有内容时 Esc 只清空输入，
// 即使会话运行中也不打断；打断需要输入框为空时再按 Esc。
func TestEscClearsInputWithoutCanceling(t *testing.T) {
	a, s := newRewindApp(t)
	a.model.Active.State = acp.StateRunning
	a.model.Input.InsertText("草稿")

	escKey(a)
	if got := a.model.Input.Text(); got != "" {
		t.Fatalf("输入框 = %q, want 清空", got)
	}
	if s.cancelCount() != 0 {
		t.Fatalf("cancels = %d, want 0（清空输入不打断运行）", s.cancelCount())
	}
	if !a.escRewindAt.IsZero() {
		t.Fatal("清空输入后不应留下连按计数")
	}

	// 输入框已空：再按 Esc 才是打断运行。
	escKey(a)
	waitCancels(t, s, 1)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("打断运行不应打开撤回选择器")
	}
}

// TestEscTwiceOpensRewind 验证空闲时输入框为空连按两次 Esc 打开撤回选择器。
func TestEscTwiceOpensRewind(t *testing.T) {
	a, _ := newRewindApp(t)

	escKey(a)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("第一次 Esc 不应打开选择器")
	}
	if a.escRewindAt.IsZero() {
		t.Fatal("第一次 Esc 应记录连按起点")
	}

	escKey(a)
	if a.model.Modal != model.ModalRewind {
		t.Fatalf("modal = %v, want ModalRewind", a.model.Modal)
	}
	if !a.escRewindAt.IsZero() {
		t.Fatal("打开选择器后应清空连按计数")
	}
}

// TestEscTwiceOutsideWindow 验证两次 Esc 超出判定窗口时不打开，第二次只作为
// 新的连按起点。
func TestEscTwiceOutsideWindow(t *testing.T) {
	a, _ := newRewindApp(t)
	escKey(a)
	a.escRewindAt = time.Now().Add(-escRewindWindow - time.Millisecond)

	escKey(a)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("超出窗口的两次 Esc 不应打开选择器")
	}
	if a.escRewindAt.IsZero() {
		t.Fatal("超出窗口的第二次 Esc 应重新开始计数")
	}
}

// TestEscTwiceInterruptedByOtherKey 验证两次 Esc 之间插入其它按键时不构成连按。
func TestEscTwiceInterruptedByOtherKey(t *testing.T) {
	a, _ := newRewindApp(t)
	escKey(a)

	a.sessionKey(input.RuneKey('a', 0))
	if !a.escRewindAt.IsZero() {
		t.Fatal("其它按键应打断连按计数")
	}

	// 这次 Esc 只清空刚输入的 a，不算连按第二次。
	escKey(a)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("清空输入的 Esc 不应触发撤回选择器")
	}
	if got := a.model.Input.Text(); got != "" {
		t.Fatalf("输入框 = %q, want 清空", got)
	}
}
