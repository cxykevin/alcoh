package app

// Esc 快捷键测试：Esc 打断运行；会话严格 idle 且输入框为空时连按两次打开
// 撤回选择器。实现见 keymap.go 的 sessionKey 与 escRewindWindow / App.escRewindAt。

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

func escKey(a *App) { a.sessionKey(input.SimpleKey(input.KeyEsc)) }

// TestEscCancelsWithNonEmptyInput 验证输入框有内容时 Esc 仍然打断运行，输入
// 原样保留：Esc 不承担清空输入的职责，避免与停止会话冲突（清空走 Ctrl+C）。
func TestEscCancelsWithNonEmptyInput(t *testing.T) {
	a, s := newRewindApp(t)
	a.model.Active.State = acp.StateRunning
	a.model.Input.InsertText("草稿")

	escKey(a)
	waitCancels(t, s, 1)
	if got := a.model.Input.Text(); got != "草稿" {
		t.Fatalf("输入框 = %q, want %q（Esc 不清空输入）", got, "草稿")
	}
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

// TestEscTwiceRequiresIdle 验证只有严格 idle 才接受连按：其它状态（如 other）
// 连按两次既不打开选择器，也不留下连按计数。
func TestEscTwiceRequiresIdle(t *testing.T) {
	a, _ := newRewindApp(t)
	a.model.Active.State = acp.StateOther

	escKey(a)
	escKey(a)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("非 idle 状态不应打开撤回选择器")
	}
	if !a.escRewindAt.IsZero() {
		t.Fatal("非 idle 状态不应留下连按计数")
	}
}

// TestEscTwiceWhileRunningCancelsOnly 验证运行中连按两次 Esc 只是两次打断运行，
// 不会打开撤回选择器。
func TestEscTwiceWhileRunningCancelsOnly(t *testing.T) {
	a, s := newRewindApp(t)
	a.model.Active.State = acp.StateRunning

	escKey(a)
	escKey(a)
	waitCancels(t, s, 2)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("运行中连按 Esc 不应打开撤回选择器")
	}
}

// TestEscTwiceRequiresEmptyInput 验证空闲但输入框非空时连按两次 Esc 不打开
// 撤回选择器（也不清空输入）。
func TestEscTwiceRequiresEmptyInput(t *testing.T) {
	a, _ := newRewindApp(t)
	a.model.Input.InsertText("草稿")

	escKey(a)
	escKey(a)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("输入框非空时不应打开撤回选择器")
	}
	if !a.escRewindAt.IsZero() {
		t.Fatal("输入框非空时不应留下连按计数")
	}
	if got := a.model.Input.Text(); got != "草稿" {
		t.Fatalf("输入框 = %q, want %q", got, "草稿")
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

	// 这次 Esc 因输入框非空不参与连按。
	escKey(a)
	if a.model.Modal == model.ModalRewind {
		t.Fatal("输入框非空时不应触发撤回选择器")
	}
	if got := a.model.Input.Text(); got != "a" {
		t.Fatalf("输入框 = %q, want %q", got, "a")
	}
}
