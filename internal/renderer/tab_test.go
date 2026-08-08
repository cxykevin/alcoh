package renderer

import "testing"

func TestPutTextExpandsTabToFourSpaces(t *testing.T) {
	b := NewBuffer(10, 1)
	b.PutText(0, 0, "a\tb", DefaultStyle(), 10)
	want := []rune{'a', ' ', ' ', ' ', ' ', 'b'}
	for x, r := range want {
		if got := b.Get(x, 0).R; got != r {
			t.Errorf("cell %d = %q, want %q", x, got, r)
		}
	}
}
