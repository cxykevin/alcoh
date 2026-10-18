package renderer

import "testing"

// TestCanvasInputCursorSharedWithSub 子 canvas 里绘制的输入框光标必须能被
// 根 canvas 读到（弹窗内容经 Sub 绘制时帧尾仍能定位硬件光标）。
func TestCanvasInputCursorSharedWithSub(t *testing.T) {
	b := NewBuffer(10, 4)
	c := NewCanvas(b)
	if _, _, ok := c.InputCursor(); ok {
		t.Fatal("初始不应有输入光标")
	}

	sub := c.Sub(NewRect(0, 0, 5, 2))
	sub.SetInputCursor(3, 1)
	x, y, ok := c.InputCursor()
	if !ok || x != 3 || y != 1 {
		t.Fatalf("根 canvas 读到 (%d,%d,%v)，期望 (3,1,true)", x, y, ok)
	}

	c.ClearInputCursor()
	if _, _, ok := c.InputCursor(); ok {
		t.Fatal("ClearInputCursor 后不应再报告输入光标")
	}
}
