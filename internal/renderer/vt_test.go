package renderer

import "testing"

func TestScreenFeedAndCursor(t *testing.T) {
	s := NewScreen(8, 2)
	s.Feed([]byte("hi"))
	if got := s.Buffer().Get(0, 0).R; got != 'h' {
		t.Fatal(got)
	}
	x, y := s.Cursor()
	if x != 2 || y != 0 {
		t.Fatalf("cursor=%d,%d", x, y)
	}
	s.Feed([]byte("\x1b[2;3HX"))
	if got := s.Buffer().Get(2, 1).R; got != 'X' {
		t.Fatalf("cell=%q", got)
	}
}
func TestScreenChunkedUTF8AndCSI(t *testing.T) {
	s := NewScreen(8, 2)
	if n := s.Feed([]byte("\x1b[")); n != 2 {
		t.Fatal(n)
	}
	s.Feed([]byte("2;2H中"))
	if s.Buffer().Get(1, 1).R != '中' {
		t.Fatalf("wide rune missing")
	}
}
func TestScreenSGRAndErase(t *testing.T) {
	s := NewScreen(8, 1)
	s.Feed([]byte("\x1b[31;1mA\x1b[0mB"))
	a, b := s.Buffer().Get(0, 0), s.Buffer().Get(1, 0)
	if a.Style.Fg != RGB(128, 0, 0) || !a.Style.Bold || !b.Style.Fg.IsDefault() {
		t.Fatalf("styles=%+v %+v", a.Style, b.Style)
	}
	s.Feed([]byte("\x1b[1;2H"))
	s.Feed([]byte("\x1b[2K"))
	for x := 0; x < 8; x++ {
		if s.Buffer().Get(x, 0).R != ' ' {
			t.Fatal("erase failed")
		}
	}
}
func TestScreenAlternateAndTitle(t *testing.T) {
	s := NewScreen(4, 1)
	s.Feed([]byte("main\x1b[?1049halt\x1b[?1049l"))
	if s.Buffer().Get(0, 0).R != 'm' {
		t.Fatal("main not restored")
	}
	s.Feed([]byte("\x1b]2;hello\a"))
	if s.Title() != "hello" {
		t.Fatal(s.Title())
	}
}
func TestScreenScroll(t *testing.T) {
	s := NewScreen(3, 2)
	s.Feed([]byte("a\r\nb\r\nc"))
	if s.Buffer().Get(0, 0).R != 'b' || s.Buffer().Get(0, 1).R != 'c' {
		t.Fatalf("scroll failed")
	}
}
