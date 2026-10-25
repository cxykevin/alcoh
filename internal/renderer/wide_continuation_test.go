package renderer

import (
	"bytes"
	"regexp"
	"testing"
	"time"
)

// 连续两个续列（前一个宽字符首列被覆盖后残留的孤儿续列）会让旧版 Diff
// 在外层扫描里反复 x--/x++，永远不返回，界面表现为整个卡死。
// Buffer.Set 现在会清掉孤儿续列，但 Diff 仍必须对任何 buffer 状态都能终止。
func TestDiffTerminatesOnConsecutiveContinuations(t *testing.T) {
	back := NewBuffer(12, 1)
	front := NewBuffer(12, 1)
	st := DefaultStyle()

	// 旧帧：宽字符在 1，2 是它的续列，3 是窄字符。
	back.Set(1, 0, Cell{R: '文', Style: st, Width: 2})
	back.Set(3, 0, Cell{R: 'x', Style: st, Width: 1})

	// 新帧：1 是同一个宽字符（2 为续列），3 是失去归属的孤儿续列。
	front.Set(1, 0, Cell{R: '文', Style: st, Width: 2})
	front.Cells[front.Index(3, 0)] = Cell{R: 0, Style: st, Width: 0}

	if f := front.Get(3, 0); f.Width != 0 {
		t.Fatalf("前置条件不成立：front[3] 应为孤儿续列，实际 %+v", f)
	}
	if !back.Get(3, 0).Equal(Cell{R: 'x', Style: st, Width: 1}) {
		t.Fatalf("前置条件不成立：back[3] 应为窄字符")
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		Render(back, front, ColorModeTrueColor, &buf)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Diff 在连续续列上未返回（死循环）")
	}

	// 孤儿续列在新帧里没有字符：旧帧的 x 必须被空格擦掉。
	plain := ansiPattern.ReplaceAllString(buf.String(), "")
	if plain != " " {
		t.Errorf("孤儿续列应输出一个空格清掉旧内容，实际可见输出 %q", plain)
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// 宽字符覆盖另一个宽字符的首列时，后者原来的续列必须一起清掉，
// 否则网格里会留下没有归属的续列（Diff 会把它当成半个字符跳过）。
func TestBufferSetWideOverWideHeadClearsOrphanContinuation(t *testing.T) {
	b := NewBuffer(8, 1)
	st := DefaultStyle()
	b.Set(2, 0, Cell{R: '中', Style: st, Width: 2}) // 占 2,3
	b.Set(1, 0, Cell{R: '文', Style: st, Width: 2}) // 覆盖 1,2；3 是旧续列

	if got := b.Get(1, 0); got.Width != 2 || got.R != '文' {
		t.Fatalf("1 应为宽字符首列，实际 %#v", got)
	}
	if got := b.Get(2, 0); got.Width != 0 {
		t.Fatalf("2 应为续列，实际 %#v", got)
	}
	if got := b.Get(3, 0); got.Width != 1 || got.R != ' ' {
		t.Fatalf("3 应被清成空格，实际 %#v", got)
	}
}
