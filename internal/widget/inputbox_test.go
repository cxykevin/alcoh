package widget

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/renderer"
)

func TestInputBufferBasic(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('h')
	b.InsertRune('i')
	b.InsertRune('中')
	if b.Text() != "hi中" {
		t.Errorf("Text() = %q, want hi中", b.Text())
	}
	// CX 是 rune 索引（"hi中" = 3 个 rune）
	if b.CX != 3 {
		t.Errorf("CX = %d, want 3", b.CX)
	}
	b.MoveLeft() // 跨过宽字符 '中'
	if b.CX != 2 {
		t.Errorf("MoveLeft after wide char: CX = %d, want 2", b.CX)
	}
	b.MoveLeft()
	if b.CX != 1 {
		t.Errorf("MoveLeft: CX = %d, want 1", b.CX)
	}
	b.MoveRight()
	b.MoveRight()
	if b.CX != 3 {
		t.Errorf("MoveRight x2: CX = %d, want 3", b.CX)
	}
}

func TestInputBufferBackspaceWide(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('中')
	b.Backspace() // 删除整个宽字符（一个 rune）
	if b.Text() != "" {
		t.Errorf("Text() = %q, want empty", b.Text())
	}
	if b.CX != 0 {
		t.Errorf("CX = %d, want 0", b.CX)
	}
}

func TestInputBufferMultiline(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('a')
	b.InsertRune('b')
	b.InsertNewline()
	b.InsertRune('c')
	if b.Text() != "ab\nc" {
		t.Errorf("Text() = %q, want %q", b.Text(), "ab\nc")
	}
	// 上移（保持列：行0有2字符，光标在列1）
	b.MoveUp()
	if b.CY != 0 || b.CX != 1 {
		t.Errorf("MoveUp: CY=%d CX=%d, want 0,1", b.CY, b.CX)
	}
	// 下移回来
	b.MoveDown()
	if b.CY != 1 || b.CX != 1 {
		t.Errorf("MoveDown: CY=%d CX=%d, want 1,1", b.CY, b.CX)
	}
}

func TestInputBufferUndoRedo(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('a')
	b.InsertRune('b')
	b.InsertRune('c')
	b.Undo()
	if b.Text() != "ab" {
		t.Errorf("after undo: %q, want ab", b.Text())
	}
	b.Undo()
	if b.Text() != "a" {
		t.Errorf("after 2nd undo: %q, want a", b.Text())
	}
	b.Redo()
	if b.Text() != "ab" {
		t.Errorf("after redo: %q, want ab", b.Text())
	}
}

func TestInputBufferKill(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('a')
	b.InsertRune('b')
	b.InsertRune('c')
	b.InsertRune('d')
	// 光标在 4，移到 1
	b.CX = 1
	b.KillToEnd()
	if b.Text() != "a" {
		t.Errorf("KillToEnd: %q, want a", b.Text())
	}
	if b.killText != "bcd" {
		t.Errorf("killText = %q, want bcd", b.killText)
	}
	b.Yank()
	if b.Text() != "abcd" {
		t.Errorf("after yank: %q, want abcd", b.Text())
	}
}

func TestInputBufferHistory(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('x')
	got := b.Submit()
	if got != "x" {
		t.Errorf("Submit() = %q, want x", got)
	}
	if b.Text() != "" {
		t.Errorf("after submit Text() = %q, want empty", b.Text())
	}
	b.InsertRune('y')
	// Up 进入历史
	b.HistoryUp()
	if b.Text() != "x" {
		t.Errorf("HistoryUp: %q, want x", b.Text())
	}
	// Down 回到草稿
	b.HistoryDown()
	if b.Text() != "y" {
		t.Errorf("HistoryDown: %q, want y", b.Text())
	}
}

func TestInputBoxCursorUsesSuppliedStyle(t *testing.T) {
	buf := NewInputBuffer()
	buf.InsertRune('x')
	buf.CX = 0

	base := renderer.DefaultStyle()
	cursor := renderer.DefaultStyle().WithFg(renderer.RGB(1, 2, 3)).WithBg(renderer.RGB(4, 5, 6))
	ib := &InputBox{Buf: buf, Prompt: "> ", Style: base, Cursor: cursor, Focused: true}
	canvas := renderer.NewCanvas(renderer.NewBuffer(8, 1))
	ib.Draw(canvas, renderer.NewRect(0, 0, 8, 1))

	got := canvas.B.Get(2, 0)
	if got.R != 'x' {
		t.Fatalf("cursor cell rune = %q, want x", got.R)
	}
	if got.Style != cursor {
		t.Errorf("cursor style = %#v, want %#v", got.Style, cursor)
	}
}

func TestInputBoxCursorHonorsRectOrigin(t *testing.T) {
	buf := NewInputBuffer()
	buf.InsertRune('x')
	buf.CX = 0

	base := renderer.DefaultStyle()
	cursor := base.WithBg(renderer.RGB(4, 5, 6))
	ib := &InputBox{Buf: buf, Prompt: "> ", Style: base, Cursor: cursor, Focused: true}
	canvas := renderer.NewCanvas(renderer.NewBuffer(20, 1))
	ib.Draw(canvas, renderer.NewRect(10, 0, 8, 1))

	if got := canvas.B.Get(12, 0); got.R != 'x' || got.Style != cursor {
		t.Fatalf("cursor cell at translated position = %#v, want rune x with cursor style", got)
	}
	if got := canvas.B.Get(2, 0); got.Style == cursor {
		t.Fatal("cursor must not be drawn at origin-relative position")
	}
}

func TestInputBoxCursorWrapsWithoutPromptOffset(t *testing.T) {
	buf := NewInputBuffer()
	for _, r := range "abcdX" {
		buf.InsertRune(r)
	}
	base := renderer.DefaultStyle()
	cursor := base.WithBg(renderer.RGB(4, 5, 6))
	ib := &InputBox{Buf: buf, Prompt: "> ", Style: base, Cursor: cursor, Focused: true}
	canvas := renderer.NewCanvas(renderer.NewBuffer(8, 3))

	for _, cx := range []int{4, 5} {
		buf.CX = cx
		canvas.B.Clear()
		ib.Draw(canvas, renderer.NewRect(0, 0, 6, 3))
		wantX := 0
		if cx == 5 {
			wantX = 1
		}
		got := canvas.B.Get(wantX, 1)
		if got.Style != cursor {
			t.Fatalf("CX=%d cursor at (%d,1) style=%#v, want %#v", cx, wantX, got.Style, cursor)
		}
	}
}

func TestInputBufferPaste(t *testing.T) {
	b := NewInputBuffer()
	b.InsertRune('x')
	b.MoveLeft()
	b.InsertText("a\r\nb\r中")
	if got := b.Text(); got != "a\nb\n中x" {
		t.Fatalf("paste = %q, want %q", got, "a\nb\n中x")
	}
	if b.CY != 2 || b.CX != 1 {
		t.Fatalf("cursor = (%d,%d), want (2,1)", b.CY, b.CX)
	}
	b.Undo()
	if got := b.Text(); got != "x" {
		t.Fatalf("undo paste = %q, want x", got)
	}
}

func TestInputBufferTrailingContinuation(t *testing.T) {
	b := NewInputBuffer()
	for _, r := range "中文\\" {
		b.InsertRune(r)
	}
	if !b.ConsumeTrailingContinuation() {
		t.Fatal("trailing continuation should be consumed")
	}
	if b.Text() != "中文\n" || b.CY != 1 || b.CX != 0 {
		t.Fatalf("after continuation: text=%q CY=%d CX=%d", b.Text(), b.CY, b.CX)
	}
	b.InsertRune('x')
	b.CX = 0
	if b.ConsumeTrailingContinuation() {
		t.Fatal("non-tail continuation must not be consumed")
	}
}

func TestInputBufferVisualHeightUsesCJKWidth(t *testing.T) {
	b := NewInputBuffer()
	for _, r := range "中文ab" {
		b.InsertRune(r)
	}
	// 宽度 6，首行扣除 prompt 的 2 列，只能容纳“中文”，余下 ab 自动换行。
	if got := b.VisualHeight(6, 2); got != 2 {
		t.Errorf("VisualHeight = %d, want 2", got)
	}
}

func TestInputBoxOnKey(t *testing.T) {
	ib := &InputBox{Buf: NewInputBuffer()}
	ib.OnKey(input.RuneKey('h', input.ModNone))
	ib.OnKey(input.RuneKey('i', input.ModNone))
	if ib.Buf.Text() != "hi" {
		t.Errorf("OnKey insert: %q, want hi", ib.Buf.Text())
	}
	// Ctrl+U 删至行首
	ib.OnKey(input.RuneKey('u', input.ModCtrl))
	if ib.Buf.Text() != "" {
		t.Errorf("Ctrl+U: %q, want empty", ib.Buf.Text())
	}
	// Enter 不消费（返回 false，由上层提交）
	handled := ib.OnKey(input.SimpleKey(input.KeyEnter))
	if handled {
		t.Errorf("Enter should not be consumed by InputBox")
	}
}
