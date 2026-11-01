package app

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// TestHomeCtrlDStashSwapsOnEmptyInput 主页 Ctrl+D 在空输入时召回暂存，
// 不能被“空输入按 d 删除会话”的快捷键抢走。
func TestHomeCtrlDStashSwapsOnEmptyInput(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	a.model.Input.InsertText("主页草稿")
	a.dispatchKey(input.RuneKey('d', input.ModCtrl))
	if got := a.model.Input.Text(); got != "" {
		t.Fatalf("after Ctrl+D Text() = %q, want empty", got)
	}
	if a.model.Input.Stash != "主页草稿" {
		t.Fatalf("Stash = %q, want 主页草稿", a.model.Input.Stash)
	}
	// 回主页会重建输入框，但暂存区（内存草稿）应保留。
	a.model.GoHome()
	if a.model.Input.Stash != "主页草稿" {
		t.Fatalf("after GoHome Stash = %q, want 主页草稿", a.model.Input.Stash)
	}
	// 输入为空时再按一次 Ctrl+D 应把暂存召回，而不是触发删除会话。
	a.dispatchKey(input.RuneKey('d', input.ModCtrl))
	if got := a.model.Input.Text(); got != "主页草稿" {
		t.Fatalf("second Ctrl+D Text() = %q, want 主页草稿", got)
	}
	if a.model.Input.Stash != "" {
		t.Fatalf("second Ctrl+D Stash = %q, want empty", a.model.Input.Stash)
	}
}

// TestSlashCommandSubmitRestoresStash 本地斜杠命令也算一次提交，执行后
// 暂存区内容同样回填输入框。
func TestSlashCommandSubmitRestoresStash(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	a.model.Input.InsertText("暂存草稿")
	a.dispatchKey(input.RuneKey('d', input.ModCtrl))
	a.model.Input.InsertText("/alcoh_help")
	if !a.tryLocalSlashCommand() {
		t.Fatal("/alcoh_help should be handled locally")
	}
	if got := a.model.Input.Text(); got != "暂存草稿" {
		t.Fatalf("after slash command Text() = %q, want 暂存草稿", got)
	}
	if a.model.Input.Stash != "" {
		t.Fatalf("after slash command Stash = %q, want empty", a.model.Input.Stash)
	}
}

// TestSessionSubmitRestoresStash 会话视图回车提交后暂存内容回填输入框，
// 暂存区清空。
func TestSessionSubmitRestoresStash(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	a.model.ActivateSession("s1", "会话")
	a.model.Input.InsertText("暂存草稿")
	a.dispatchKey(input.RuneKey('d', input.ModCtrl))
	a.model.Input.InsertText("要提交的命令")
	if got := a.model.SubmitInput(); got != "要提交的命令" {
		t.Fatalf("SubmitInput() = %q, want 要提交的命令", got)
	}
	if got := a.model.Input.Text(); got != "暂存草稿" {
		t.Fatalf("after submit Text() = %q, want 暂存草稿", got)
	}
	if a.model.Input.Stash != "" {
		t.Fatalf("after submit Stash = %q, want empty", a.model.Input.Stash)
	}
}
