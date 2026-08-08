package renderer

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiffEmpty(t *testing.T) {
	back := NewBuffer(20, 5)
	front := NewBuffer(20, 5)
	var buf bytes.Buffer
	n, err := Render(back, front, ColorModeTrueColor, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("diff of identical buffers should be empty, got %d bytes: %q", buf.Len(), buf.String())
	}
}

func TestDiffSingleCell(t *testing.T) {
	back := NewBuffer(5, 2)
	front := NewBuffer(5, 2)
	front.Set(2, 1, Cell{R: 'x', Style: DefaultStyle(), Width: 1})
	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	out := buf.String()
	// 应移动到 (2,1) 并写 x
	if !strings.Contains(out, "\x1b[2;3H") || !strings.Contains(out, "x") {
		t.Errorf("diff output missing move/write: %q", out)
	}
}

func TestDiffWideCharSkipsContinuation(t *testing.T) {
	back := NewBuffer(6, 1)
	front := NewBuffer(6, 1)
	front.PutText(0, 0, "中", DefaultStyle(), 6)
	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	out := buf.String()
	// 输出应包含"中"，且后面直接跟下一个定位序列（续列不单独写）
	if !strings.Contains(out, "中") {
		t.Fatalf("expected 中 in output: %q", out)
	}
	// 检查没有把续列当作第二个字符写出：定位到下一格而非 2 个格子内重复
	// 一个宽字符只出现一次
	if strings.Count(out, "中") != 1 {
		t.Errorf("wide char should be written once, got %q", out)
	}
}

func TestDiffNarrowOverWide(t *testing.T) {
	back := NewBuffer(6, 1)
	back.PutText(0, 0, "中", DefaultStyle(), 6) // 宽字符占 0,1
	front := NewBuffer(6, 1)
	front.Set(0, 0, Cell{R: 'a', Style: DefaultStyle(), Width: 1}) // 窄覆盖宽
	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	out := buf.String()
	// 写 'a' 后应清 x+1 残影（写空格）
	if !strings.Contains(out, "a") {
		t.Fatalf("expected 'a': %q", out)
	}
	// 统计空格：a 后应有一个清残影空格
	spaces := strings.Count(out, " ")
	if spaces < 1 {
		t.Errorf("expected continuation cleanup space after narrow-over-wide, got %q", out)
	}
}

func TestDiffCursorDeletionClearsWideContinuationStyle(t *testing.T) {
	back := NewBuffer(4, 1)
	front := NewBuffer(4, 1)
	cursor := DefaultStyle().WithFg(RGB(1, 2, 3)).WithBg(RGB(4, 5, 6))
	back.Set(0, 0, Cell{R: '中', Style: cursor, Width: 2})
	// 删除字符后，首列成为新的虚拟光标；续列必须恢复默认样式。
	front.Set(0, 0, Cell{R: ' ', Style: cursor, Width: 1})

	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	out := buf.String()
	if !strings.Contains(out, "\x1b[0m\x1b[39;49m ") {
		t.Errorf("wide-character continuation should be cleared with its default style, got %q", out)
	}
}

func TestDiffStyleChange(t *testing.T) {
	back := NewBuffer(10, 1)
	back.PutText(0, 0, "hello", DefaultStyle(), 10)
	front := NewBuffer(10, 1)
	st := DefaultStyle().WithFg(RGB(255, 0, 0)).WithBold(true)
	front.PutText(0, 0, "hello", st, 10)
	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	out := buf.String()
	// 新样式应输出：reset + 完整 SGR（bold + red fg + default bg 49）
	if !strings.Contains(out, "\x1b[0m\x1b[1;38;2;255;0;0;49m") {
		t.Errorf("expected reset+full SGR, got %q", out)
	}
}

func TestDiffBgChangeResetsFg(t *testing.T) {
	// 关键回归：同一行两个 run，第一个有前景色，第二个只有背景色（前景默认）。
	// 第二个 run 必须显式重置前景（39），否则旧前景污染背景 run 后的字符。
	back := NewBuffer(12, 1)
	front := NewBuffer(12, 1)
	red := DefaultStyle().WithFg(RGB(255, 0, 0))
	blue := DefaultStyle().WithBg(RGB(0, 0, 255))
	// front: 前 5 字符红色，后 5 字符仅蓝背景
	front.PutText(0, 0, "AAAAA", red, 12)
	front.PutText(5, 0, "BBBBB", blue, 12)
	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	out := buf.String()
	// 蓝背景 run 必须包含前景重置 39
	if !strings.Contains(out, "\x1b[0m\x1b[39;48;2;0;0;255m") {
		t.Errorf("bg-only run must reset fg (39), got %q", out)
	}
}

func TestDiffStable(t *testing.T) {
	// 第二次渲染相同帧应零增量
	back := NewBuffer(10, 3)
	front := NewBuffer(10, 3)
	front.PutText(1, 1, "中x", DefaultStyle(), 8)
	var buf bytes.Buffer
	Render(back, front, ColorModeTrueColor, &buf)
	first := buf.Len()
	Render(front, front, ColorModeTrueColor, &buf)
	if buf.Len() != first {
		t.Errorf("stable frames should produce no delta, got %q", buf.String()[first:])
	}
}

func TestBufferSetWide(t *testing.T) {
	b := NewBuffer(4, 1)
	b.PutText(0, 0, "中", DefaultStyle(), 4)
	c0 := b.Get(0, 0)
	c1 := b.Get(1, 0)
	if c0.Width != 2 {
		t.Errorf("expected width 2 at col0, got %d", c0.Width)
	}
	if c1.Width != 0 {
		t.Errorf("expected continuation at col1, got %d", c1.Width)
	}
}

func TestDetectColorMode(t *testing.T) {
	// 不改变环境变量，只验证函数存在且返回合法值
	m := DetectColorMode()
	if m < ColorModeMono || m > ColorModeTrueColor {
		t.Errorf("invalid color mode %d", m)
	}
}
