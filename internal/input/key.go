// Package input 定义按键事件类型与转义序列解析器（纯逻辑，平台无关）。
package input

// Mod 是键盘修饰键位掩码。
type Mod int

const (
	ModNone Mod = 0
	// ModShift 表示 Shift 修饰。
	ModShift Mod = 1 << iota
	// ModAlt 表示 Alt（Meta）修饰。
	ModAlt
	// ModCtrl 表示 Ctrl 修饰。
	ModCtrl
)

// KeyType 是按键的类型（普通字符 vs 功能键）。
type KeyType int

const (
	// KeyRune 表示普通可打印字符（含 Ctrl+letter 映射的字符）。
	KeyRune KeyType = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyDelete
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyPaste
)

// KeyEvent 描述一次按键。
type KeyEvent struct {
	Type KeyType
	Rune rune // Type==KeyRune 时有效
	Mod  Mod
	Text string // Type==KeyPaste 时有效
}

// RuneKey 构造普通字符按键。
func RuneKey(r rune, mod Mod) KeyEvent {
	return KeyEvent{Type: KeyRune, Rune: r, Mod: mod}
}

// SimpleKey 构造无修饰功能键。
func SimpleKey(t KeyType) KeyEvent {
	return KeyEvent{Type: t}
}

// MouseButton 是鼠标按键或滚轮。
type MouseButton int

const (
	// MouseNone 是没有按键（motion 事件）。
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseAction 是鼠标事件动作。
type MouseAction int

const (
	// MousePress 按下（滚轮事件也用它）。
	MousePress MouseAction = iota
	// MouseRelease 释放。
	MouseRelease
	// MouseMove 移动/拖拽。
	MouseMove
)

// MouseEvent 描述一次鼠标事件。坐标从 1 开始计（终端惯例）。
type MouseEvent struct {
	Button MouseButton
	Action MouseAction
	Mod    Mod
	X, Y   int
}

// IsWheel 报告是否为滚轮事件。
func (m MouseEvent) IsWheel() bool {
	switch m.Button {
	case MouseWheelUp, MouseWheelDown, MouseWheelLeft, MouseWheelRight:
		return true
	}
	return false
}

// EventKind 区分按键与鼠标事件。
type EventKind int

const (
	// EventTypeKey 键盘事件。
	EventTypeKey EventKind = iota
	// EventTypeMouse 鼠标事件。
	EventTypeMouse
)

// Event 是解析器产出的输入事件。
type Event struct {
	Kind  EventKind
	Key   KeyEvent
	Mouse MouseEvent
}

// KeyEventOf 用按键构造 Event。
func KeyEventOf(k KeyEvent) Event { return Event{Kind: EventTypeKey, Key: k} }

// MouseEventOf 用鼠标构造 Event。
func MouseEventOf(m MouseEvent) Event { return Event{Kind: EventTypeMouse, Mouse: m} }

// IsCtrl 报告按键是否为 Ctrl 组合。
func (k KeyEvent) IsCtrl() bool { return k.Mod&ModCtrl != 0 }

// IsAlt 报告按键是否为 Alt 组合。
func (k KeyEvent) IsAlt() bool { return k.Mod&ModAlt != 0 }

// IsShift 报告按键是否为 Shift 组合。
func (k KeyEvent) IsShift() bool { return k.Mod&ModShift != 0 }
