package renderer

import (
	"strings"
	"testing"
)

// TestAnsiWriterHomeCursor 验证帧尾归位：输出以 HomeCursorSeq 结束，
// 且虚拟游标同步回左上角（后续相对移动才能从 (0,0) 起算）。
func TestAnsiWriterHomeCursor(t *testing.T) {
	aw := NewAnsiWriter(ColorModeTrueColor)
	aw.MoveTo(5, 7)
	aw.WriteRune('x', 1, DefaultStyle())
	aw.HomeCursor()
	if got := string(aw.Bytes()); !strings.HasSuffix(got, HomeCursorSeq) {
		t.Fatalf("帧尾没有归位序列: %q", got)
	}

	// 归位后虚拟游标应回到 (0,0)：同列 3 列的小距离移动会走 CUF。
	aw.MoveTo(0, 3)
	if got := string(aw.Bytes()); !strings.HasSuffix(got, "\x1b[3C") {
		t.Fatalf("归位后游标未同步到左上角: %q", got)
	}
}
