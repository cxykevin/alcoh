package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/term"
)

// TestRenderPlacesCursorAtInput 回归"光标乱飘"：每帧结束后硬件光标必须
// 落在输入框的文本光标处（IME 候选框跟随输入位置），而不是左上角。
func TestRenderPlacesCursorAtInput(t *testing.T) {
	var out bytes.Buffer
	backend := demo.New(true)
	defer backend.Close()
	a := New(term.Dump(&out, 20, 6), backend)
	a.resetBuffers(20, 6)

	if !a.render() {
		t.Fatal("首帧 render 被跳过")
	}
	if !a.view.InputCursorVisible {
		t.Fatal("输入框可见时未记录光标位置")
	}
	if a.view.InputCursorX == 0 && a.view.InputCursorY == 0 {
		t.Fatalf("输入光标仍在左上角: (%d,%d)", a.view.InputCursorX, a.view.InputCursorY)
	}
	assertScreenCursor(t, out.Bytes(), a.view.InputCursorX, a.view.InputCursorY)

	// 输入内容并左移：下一帧的硬件光标要移动到新的插入点（中为宽字符）。
	a.model.Input.InsertText("ab中")
	a.model.Input.MoveLeft()
	out.Reset()
	if !a.render() {
		t.Fatal("第二帧 render 被跳过")
	}
	if !a.view.InputCursorVisible {
		t.Fatal("第二帧未记录输入光标")
	}
	assertScreenCursor(t, out.Bytes(), a.view.InputCursorX, a.view.InputCursorY)

	// 配置编辑器（/server）底部的值输入框：弹窗接替主输入框，硬件光标要
	// 落到编辑处，而不是左上角。
	a.model.Modal = model.ModalServer
	a.model.ServerCfg = model.NewConfigEditor(json.RawMessage(`{"Version":1}`))
	a.model.ServerCfg.BeginEdit()
	out.Reset()
	if !a.render() {
		t.Fatal("配置弹窗帧 render 被跳过")
	}
	if !a.view.InputCursorVisible {
		t.Fatal("配置编辑器未记录输入光标")
	}
	// 20x6 全屏弹窗：上下各留 1 行边框内边距，内容区为 1..4 行，
	// 输入框在内容区倒数第二行（3），底部提示在 4。
	if a.view.InputCursorY != 3 {
		t.Fatalf("配置编辑器光标行 = %d，期望 3", a.view.InputCursorY)
	}
	assertScreenCursor(t, out.Bytes(), a.view.InputCursorX, a.view.InputCursorY)
}

// assertScreenCursor 把帧输出重放进 VT 屏幕模型，校验终端最终光标位置。
func assertScreenCursor(t *testing.T, out []byte, wantX, wantY int) {
	t.Helper()
	scr := renderer.NewScreen(20, 6)
	scr.Feed(out)
	x, y := scr.Cursor()
	if x != wantX || y != wantY {
		t.Fatalf("硬件光标在 (%d,%d)，期望输入光标 (%d,%d)", x, y, wantX, wantY)
	}
}
