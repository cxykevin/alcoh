package term

import "testing"

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
