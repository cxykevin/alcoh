package app

import (
	"os"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/term"
)

// parserTerm 是走真实字节解析的测试终端：写入的字节经 input.Parser 解析为
// 终端事件，用于复现"终端把连续 Esc 合并进同一读批次"这类字节级场景。
type parserTerm struct {
	rd   *os.File
	wr   *os.File
	evCh chan term.Event
	w, h int
}

func newParserTerm(t *testing.T) *parserTerm {
	t.Helper()
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	p := &parserTerm{rd: rd, wr: wr, evCh: make(chan term.Event, 64), w: 100, h: 30}
	go func() {
		parser := input.NewParser(rd)
		for {
			ev, err := parser.Next()
			if err != nil {
				return
			}
			if ev.Kind == input.EventTypeMouse {
				p.evCh <- term.Event{Kind: term.EventMouse, Mouse: ev.Mouse}
				continue
			}
			p.evCh <- term.Event{Kind: term.EventKey, Key: ev.Key}
		}
	}()
	t.Cleanup(func() {
		_ = rd.Close()
		_ = wr.Close()
	})
	return p
}

func (p *parserTerm) send(s string)                { _, _ = p.wr.WriteString(s) }
func (p *parserTerm) EnterRaw() error              { return nil }
func (p *parserTerm) ExitRaw() error               { return nil }
func (p *parserTerm) Size() (int, int)             { return p.w, p.h }
func (p *parserTerm) Events() <-chan term.Event    { return p.evCh }
func (p *parserTerm) Write([]byte) error           { return nil }
func (p *parserTerm) CopyToClipboard(string) error { return nil }

// waitActiveState 轮询等待会话到达给定状态。
func waitActiveState(t *testing.T, a *App, want acp.SessionState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap := a.snapshot(); snap.HasActive && snap.ActiveState == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session never reached %v, state=%v", want, a.snapshot().ActiveState)
}

// TestEscCancelCoalescedBytes 回归测试：终端把两次 Esc 按键合并进同一读批次
// （快速连按，或应用读输入前 stdin 已有积压）时，第一次 Esc 仍必须打断运行中
// 的会话——此前整批字节被解析成一次 Alt+Esc，Esc 打断因此完全失效。
func TestEscCancelCoalescedBytes(t *testing.T) {
	setConfigDir(t)
	pt := newParserTerm(t)
	b := &trackBackend{Backend: demo.New(true)}
	a := New(pt, b)
	done := runApp(t, a)

	// 等待主页预创建会话建立后输入 prompt 并回车。
	time.Sleep(200 * time.Millisecond)
	pt.send("hi")
	time.Sleep(30 * time.Millisecond)
	pt.send("\r")
	waitActiveState(t, a, acp.StateRunning)

	// 两次 Esc 的字节落在同一读批次里。
	pt.send("\x1b\x1b")
	waitActiveState(t, a, acp.StateIdle)
	if b.cancelCount() == 0 {
		t.Fatal("session/cancel was never sent for coalesced Esc bytes")
	}

	// 退出。第二次 Esc 的 esc 窗口（input.escapeWindow）结束后再发送按键，
	// 否则紧跟其后的字节会按终端语义并入该 Esc 前缀。
	time.Sleep(150 * time.Millisecond)
	pt.send("\x11")
	time.Sleep(100 * time.Millisecond)
	pt.send("y")
	waitRun(t, done)
}
