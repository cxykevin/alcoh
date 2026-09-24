package app

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/demo"
)

// TestWithModelUnlocksOnPanic 验证持锁执行在 panic 展开时也会释放锁：旧实现是
// 手工 Lock/Unlock，模型层 panic 会跳过 Unlock，Run 的退出清理随即死锁在
// modelMu.Lock 上——进程永远卡在 panic 展开中，终端留在 raw + alternate screen。
func TestWithModelUnlocksOnPanic(t *testing.T) {
	a := &App{}
	func() {
		defer func() { _ = recover() }()
		a.withModel(func() { panic("boom") })
	}()
	if !a.modelMu.TryLock() {
		t.Fatal("modelMu still locked after panic")
	}
	a.modelMu.Unlock()
}

// hangCloseBackend 的后端关闭永久阻塞，模拟没有写超时的传输/插件管道。
type hangCloseBackend struct {
	*demo.Backend
	release chan struct{}
}

func (b *hangCloseBackend) Close() error {
	<-b.release
	return nil
}

// TestRunRestoresTerminalWhenShutdownBlocks 验证后端关闭卡死时退出仍然完成：
// 终端先被还原、Run 在 shutdownCleanupTimeout 后返回，而不是永远卡在退出清理里
// （旧行为：终端留在 raw + alternate screen，进程不退出，只能关窗口）。
func TestRunRestoresTerminalWhenShutdownBlocks(t *testing.T) {
	ft := newFakeTerm()
	be := &hangCloseBackend{Backend: demo.New(true), release: make(chan struct{})}
	a := New(ft, be)
	done := runApp(t, a)
	time.Sleep(200 * time.Millisecond)
	quitApp(ft)
	waitRun(t, done)
	if ft.raw {
		t.Error("terminal must be restored even when backend shutdown blocks")
	}
	close(be.release)
}
