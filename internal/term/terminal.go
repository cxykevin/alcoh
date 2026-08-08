// Package term 提供跨平台终端抽象层。
// 所有平台差异（raw mode、尺寸查询、尺寸变更通知、信号）收敛于此包；
// 上层 app 只依赖本接口，不感知平台。
package term

import (
	"encoding/base64"
	"strings"

	"github.com/cxykevin/alcoh/internal/input"
)

// osc52Clipboard 构造 OSC 52 序列 `ESC]52;c;<base64>BEL`，把文本写入系统剪贴板。
// 被桌面端（unix/windows/wasip1）共用；浏览器端走 navigator.clipboard。
func osc52Clipboard(text string) []byte {
	var sb strings.Builder
	sb.WriteString("\x1b]52;c;")
	sb.WriteString(base64.StdEncoding.EncodeToString([]byte(text)))
	sb.WriteString("\x07")
	return []byte(sb.String())
}

// EventKind 是终端事件的类型。
type EventKind int

const (
	// EventKey 表示一个按键事件（Key 字段有效）。
	EventKey EventKind = iota
	// EventResize 表示终端尺寸变化（W,H 字段有效）。
	EventResize
	// EventQuit 表示外部终止信号（SIGINT/SIGTERM）。
	EventQuit
	// EventMouse 表示鼠标事件（Mouse 字段有效）。
	EventMouse
)

// Event 是终端事件（按键 / 鼠标 / 尺寸变化 / 退出信号）。
type Event struct {
	Kind  EventKind
	Key   input.KeyEvent
	Mouse input.MouseEvent
	W, H  int
}

// 常用 ANSI 序列（平台无关）。
const (
	hideCursorSeq = "\x1b[?25l"
	// mouseEnableSeq 打开 xterm 事件跟踪 + SGR 扩展格式。
	// ?1000: 按键按下/释放；?1002: 按下期间的拖拽移动（选择必需，滚轮也一并可靠上报）；
	// ?1006: SGR 参数格式（无 223 列上限）。
	mouseEnableSeq  = "\x1b[?1000h\x1b[?1002h\x1b[?1006h"
	mouseDisableSeq = "\x1b[?1006l\x1b[?1002l\x1b[?1000l"
)

// eventFromInput 把 parser 事件转换为 term 事件。
func eventFromInput(ev input.Event) Event {
	if ev.Kind == input.EventTypeMouse {
		return Event{Kind: EventMouse, Mouse: ev.Mouse}
	}
	return Event{Kind: EventKey, Key: ev.Key}
}

// Terminal 描述一个可供 TUI 使用的终端。
type Terminal interface {
	// EnterRaw 进入 raw 模式：关闭回显/行缓冲，开启 VT 处理，
	// 进入 alternate screen、隐藏光标，并启动事件采集。
	EnterRaw() error
	// ExitRaw 完整恢复终端：恢复 termios/Console 模式、退出 alternate screen。
	ExitRaw() error
	// Size 返回当前终端单元格宽高。
	Size() (w, h int)
	// Events 返回终端事件流。
	Events() <-chan Event
	// Write 输出一帧 ANSI 字节。
	Write(p []byte) error
	// CopyToClipboard 将文本写入系统剪贴板（桌面端用 OSC 52，浏览器用 navigator.clipboard）。
	CopyToClipboard(text string) error
}
