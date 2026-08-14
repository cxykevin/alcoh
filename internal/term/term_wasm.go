//go:build js

package term

import (
	"io"
	"syscall/js"

	"github.com/cxykevin/alcoh/internal/input"
)

// wasmTerm 是 GOOS=js GOARCH=wasm 的 Terminal 实现（浏览器 + xterm.js）。
// 输入经全局 JS 回调 onInput/onResize 推入；输出经全局 term.write 逐帧写出。
type wasmTerm struct {
	evCh   chan Event
	parser *input.Parser
	// 全局 js.Func 引用，防止被 GC（程序退出时才释放）。
	onInput  js.Func
	onResize js.Func

	ch    chan byte // JS 回调 → parser 的字节缓冲
	w, h  int
	ready chan struct{}
}

// chanReader 实现 io.Reader：从 JS 回调填充的 channel 读取字节。
type chanReader struct {
	ch chan byte
}

func (r *chanReader) Read(p []byte) (int, error) {
	select {
	case b, ok := <-r.ch:
		if !ok {
			return 0, io.EOF
		}
		p[0] = b
		return 1, nil
	}
}

// Open 注册 WASM 终端桥。必须在 main() 内调用（尚未注册 JS 全局函数时调用会 panic）。
func Open() (Terminal, error) {
	t := &wasmTerm{
		evCh:  make(chan Event, 64),
		ch:    make(chan byte, 512),
		w:     80,
		h:     24,
		ready: make(chan struct{}),
	}
	t.parser = input.NewParser(&chanReader{ch: t.ch})

	t.onInput = js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].String()
		for i := 0; i < len(data); i++ {
			select {
			case t.ch <- data[i]:
			default: // 缓冲满则丢弃，避免阻塞 JS 事件循环
				return nil
			}
		}
		return nil
	})
	t.onResize = js.FuncOf(func(this js.Value, args []js.Value) any {
		cols, rows := 80, 24
		if len(args) >= 2 {
			cols = args[0].Int()
			rows = args[1].Int()
		}
		t.w, t.h = cols, rows
		select {
		case t.evCh <- Event{Kind: EventResize, W: cols, H: rows}:
		default:
		}
		return nil
	})
	js.Global().Set("alcohOnInput", t.onInput)
	js.Global().Set("alcohOnResize", t.onResize)

	// 初始尺寸：若 term 对象已就绪则读取
	go t.initialSize()
	return t, nil
}

func (t *wasmTerm) initialSize() {
	term := js.Global().Get("term")
	if !term.IsUndefined() && !term.IsNull() {
		if c := term.Get("cols"); !c.IsUndefined() {
			t.w = c.Int()
		}
		if r := term.Get("rows"); !r.IsUndefined() {
			t.h = r.Int()
		}
		select {
		case t.evCh <- Event{Kind: EventResize, W: t.w, H: t.h}:
		default:
		}
	}
}

func (t *wasmTerm) Size() (int, int) { return t.w, t.h }

func (t *wasmTerm) EnterRaw() error {
	// 浏览器本来就是 raw 语义（xterm.js 逐键回调），无需模式切换；
	// 通过 mouseEnableSeq 请求 xterm.js 上报滚轮 / 按键 SGR 序列。
	t.Write([]byte("\x1b[?1049h\x1b[2J\x1b[H" + hideCursorSeq + mouseEnableSeq + bracketedPasteEnableSeq))
	go t.readLoop()
	return nil
}

func (t *wasmTerm) ExitRaw() error {
	t.Write([]byte(bracketedPasteDisableSeq + mouseDisableSeq + "\x1b[0m\x1b[?25h\x1b[?1049l"))
	return nil
}

func (t *wasmTerm) Events() <-chan Event { return t.evCh }

func (t *wasmTerm) Write(p []byte) error {
	term := js.Global().Get("term")
	if term.IsUndefined() || term.IsNull() {
		return io.ErrClosedPipe
	}
	term.Call("write", string(p))
	return nil
}

func (t *wasmTerm) CopyToClipboard(text string) error {
	clip := js.Global().Get("navigator").Get("clipboard")
	if clip.IsUndefined() || clip.IsNull() {
		return io.ErrClosedPipe
	}
	// writeText 返回 Promise；启动写入即可，无需 await。
	clip.Call("writeText", text)
	return nil
}

func (t *wasmTerm) readLoop() {
	for {
		ev, err := t.parser.Next()
		if err != nil {
			select {
			case t.evCh <- Event{Kind: EventQuit}:
			default:
			}
			return
		}
		t.evCh <- eventFromInput(ev)
	}
}
