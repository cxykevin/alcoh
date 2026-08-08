package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/term"
)

func TestResizeForcesWholeScreenRedraw(t *testing.T) {
	var out bytes.Buffer
	backend := demo.New(true)
	defer backend.Close()
	a := New(term.Dump(&out, 16, 5), backend)
	a.resetBuffers(16, 5)
	a.render()
	out.Reset()

	a.resize(10, 4)
	got := out.String()
	if !strings.Contains(got, "\x1b[1;1H") || !strings.Contains(got, "\x1b[4;1H") {
		t.Fatalf("resize should redraw first and final rows, got %q", got)
	}
}
