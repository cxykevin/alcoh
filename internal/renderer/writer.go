package renderer

import (
	"bytes"
	"strconv"
)

// 常用 ANSI 序列常量。
const (
	// SGRReset 复位所有样式。
	SGRReset = "\x1b[0m"
	// EnterAltScreen 进入 alternate screen 并清屏、隐藏光标。
	EnterAltScreen = "\x1b[?1049h\x1b[2J\x1b[H"
	// ExitAltScreen 退出 alternate screen（恢复主屏、显示光标、复位样式）。
	ExitAltScreen = "\x1b[0m\x1b[?25h\x1b[?1049l"
	// HideCursor 隐藏光标。
	HideCursor = "\x1b[?25l"
	// ShowCursor 显示光标。
	ShowCursor = "\x1b[?25h"
	// ClearScreen 清屏并回原位。
	ClearScreen = "\x1b[2J\x1b[H"
)

// AnsiWriter 负责把 diff 指令编码为 ANSI 序列。
// 它维护虚拟游标位置与最近样式，做增量 SGR 与游标移动优化。
type AnsiWriter struct {
	buf         *bytes.Buffer
	curRow      int // 当前游标行（0-based）
	curCol      int // 当前游标列（0-based）
	lastStyle   Style
	hasLast     bool
	mode        ColorMode
	initialized bool
}

// NewAnsiWriter 创建 AnsiWriter。
func NewAnsiWriter(mode ColorMode) *AnsiWriter {
	return &AnsiWriter{buf: &bytes.Buffer{}, mode: mode, curRow: -1, curCol: -1}
}

// Bytes 返回累积的 ANSI 字节。
func (aw *AnsiWriter) Bytes() []byte { return aw.buf.Bytes() }

// Len 返回已累积字节数。
func (aw *AnsiWriter) Len() int { return aw.buf.Len() }

// Reset 清空累积内容并重置游标状态。
func (aw *AnsiWriter) Reset() {
	aw.buf.Reset()
	aw.curRow, aw.curCol = -1, -1
	aw.hasLast = false
	aw.initialized = false
}

// raw 直接写入字节。
func (aw *AnsiWriter) raw(s string) { aw.buf.WriteString(s) }

// MoveTo 移动到 (row, col)，0-based。
func (aw *AnsiWriter) MoveTo(row, col int) {
	if aw.curRow == row && aw.curCol == col {
		return
	}
	// 行内小距离用相对移动
	if aw.curRow == row && aw.curCol >= 0 && col-aw.curCol > 0 && col-aw.curCol <= 8 {
		aw.raw("\x1b[" + strconv.Itoa(col-aw.curCol) + "C")
		aw.curCol = col
		return
	}
	aw.raw("\x1b[" + strconv.Itoa(row+1) + ";" + strconv.Itoa(col+1) + "H")
	aw.curRow, aw.curCol = row, col
}

// ApplyStyle 应用样式：先发 SGRReset 清掉所有残留属性/颜色，
// 再设置目标的完整 SGR。这样每个变化 run 的终端状态都与目标样式完全一致，
// 背景色/前景色/属性不会跨 run 污染（双缓冲 diff 正确性的关键）。
func (aw *AnsiWriter) ApplyStyle(s Style) {
	if aw.hasLast && aw.lastStyle == s {
		return
	}
	aw.raw(SGRReset)
	if sgr := sgrForStyle(s, aw.mode); sgr != "" {
		aw.raw("\x1b[" + sgr + "m")
	}
	aw.lastStyle = s
	aw.hasLast = true
}

// WriteRune 在当前位置写入一个 rune 并前进列宽。
// width 为 1 或 2；宽度 0 的续列不写入。
func (aw *AnsiWriter) WriteRune(r rune, width int, st Style) {
	if width == 0 {
		return
	}
	aw.ApplyStyle(st)
	aw.raw(string(r))
	if width == 2 {
		// 宽字符占 2 列，后续 diff 的列号要 +2
		aw.curCol += 2
	} else {
		aw.curCol++
	}
}

// WriteSpace 写一个空格（用于清除残影）。
func (aw *AnsiWriter) WriteSpace(st Style) {
	aw.WriteRune(' ', 1, st)
}
