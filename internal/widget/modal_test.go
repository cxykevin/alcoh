package widget

import (
	"testing"

	"github.com/cxykevin/alcoh/internal/renderer"
)

type modalTestContent struct{}

func (modalTestContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	c.PutText(r.X, r.Y, "content", renderer.DefaultStyle())
}

func TestModalDrawBottomAnchorsToAvailableBottom(t *testing.T) {
	canvas := renderer.NewCanvas(renderer.NewBuffer(24, 20))
	modal := &Modal{
		Width:   6,
		Height:  4,
		Title:   "x",
		Style:   renderer.DefaultStyle(),
		Content: modalTestContent{},
	}
	r := renderer.NewRect(3, 5, 20, 10)
	modal.DrawBottom(canvas, r, 2)

	// bottom = r.Y+r.H-2 = 13; the 4-row box therefore starts at y=9.
	if got := canvas.B.Get(10, 9).R; got != '┌' {
		t.Fatalf("top-left border = %q, want ┌", got)
	}
	if got := canvas.B.Get(10, 12).R; got != '└' {
		t.Fatalf("bottom-left border = %q, want └", got)
	}
	if got := canvas.B.Get(10, 13).R; got != ' ' {
		t.Fatalf("bottom margin was overwritten at y=13: %q", got)
	}
	if got := canvas.B.Get(3, 9).R; got != ' ' {
		t.Fatalf("modal should be horizontally centered inside r: %q", got)
	}
}

func TestModalDrawSheetIsFullWidthAndTopBorderOnly(t *testing.T) {
	canvas := renderer.NewCanvas(renderer.NewBuffer(12, 8))
	modal := &Modal{Height: 3, Title: "帮助", Style: renderer.DefaultStyle(), Content: modalTestContent{}}
	modal.DrawSheet(canvas, renderer.NewRect(2, 1, 8, 6))

	// 面板占据 r 的最下方 3 行，顶部横线横跨整个宽度。
	for _, x := range []int{2, 9} {
		if got := canvas.B.Get(x, 4).R; got != '─' {
			t.Fatalf("top border at x=%d = %q, want ─", x, got)
		}
	}
	if got := canvas.B.Get(2, 5).R; got == '│' || got == '└' || got == '┘' {
		t.Fatalf("left side border should be absent, got %q", got)
	}
	if got := canvas.B.Get(5, 6).R; got == '─' || got == '└' || got == '┘' {
		t.Fatalf("bottom border should be absent, got %q", got)
	}
}

func TestModalDrawBottomClipsHeightToRect(t *testing.T) {
	canvas := renderer.NewCanvas(renderer.NewBuffer(8, 6))
	modal := &Modal{Width: 20, Height: 20, Style: renderer.DefaultStyle()}
	modal.DrawBottom(canvas, renderer.NewRect(0, 0, 8, 6), 2)

	// The requested box is wider/taller than the available area; drawing must
	// stay inside the canvas and still leave the requested margin when possible.
	if got := canvas.B.Get(0, 5).R; got != ' ' {
		t.Fatalf("bottom margin was overwritten after clipping: %q", got)
	}
}

func TestModalDrawBottomRejectsFullHeightMargin(t *testing.T) {
	canvas := renderer.NewCanvas(renderer.NewBuffer(8, 4))
	modal := &Modal{Width: 8, Height: 2, Style: renderer.DefaultStyle()}
	modal.DrawBottom(canvas, renderer.NewRect(0, 0, 8, 4), 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			if got := canvas.B.Get(x, y).R; got != ' ' {
				t.Fatalf("full bottom margin was overwritten at (%d,%d): %q", x, y, got)
			}
		}
	}
}
