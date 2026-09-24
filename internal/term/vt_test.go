package term

import (
	"testing"
	"time"
)

func TestVTScreenIncrementalCSI(t *testing.T) {
	s := NewVTScreen(8, 3)
	s.Feed("hello")
	s.Feed("\x1b[2;1Hworld")
	if got := s.Lines()[1]; got != "world   " {
		t.Fatalf("line=%q", got)
	}
	s.Feed("\x1b[2K")
	if got := s.Lines()[1]; got != "        " {
		t.Fatalf("clear=%q", got)
	}
}
func TestVTScreenSplitEscapeAndScroll(t *testing.T) {
	s := NewVTScreen(4, 2)
	s.Feed("a\nb\n")
	s.Feed("c")
	if got := s.Lines()[0]; got != "b   " {
		t.Fatalf("scroll line=%q", got)
	}
	if got := s.Lines()[1]; got != "c   " {
		t.Fatalf("last line=%q", got)
	}
}

// TestVTScreenZeroWidthAtLineEnd 验证零宽字符落在行尾时不再越界 panic。
// 旧实现按显示宽度判断换行，零宽字符（rw == 0）在行尾（X == Width）不会被
// 换行拦截，直接写 Cells[Y][Width] 触发 index out of range：服务端终端输出里
// 只要有一行末尾带 ZWJ/组合记号/变体选择符，整个 TUI 就会在 panic 展开中挂死。
func TestVTScreenZeroWidthAtLineEnd(t *testing.T) {
	s := NewVTScreen(4, 2)
	s.Feed("abcd\u200de") // \u200d = ZWJ，列宽 0
	rows := s.Lines()
	if rows[0] != "abcd" {
		t.Fatalf("row0=%q, want %q", rows[0], "abcd")
	}
	if rows[1] != "e   " {
		t.Fatalf("row1=%q, want %q", rows[1], "e   ")
	}
}

// TestVTScreenTabTerminatesOnNarrowScreen 验证 1 列宽屏幕上的 \t 不会空转：
// 换行把列号归零后，无上限的 tab 展开会一直写不出进度（UI 卡死、CPU 占满）。
func TestVTScreenTabTerminatesOnNarrowScreen(t *testing.T) {
	s := NewVTScreen(1, 3)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Feed("a\tb")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("VTScreen.Feed did not terminate on a 1-column screen")
	}
}
