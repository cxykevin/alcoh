package app

import (
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// blockingTerm 模拟终端停止消费输出：Windows 控制台的 WriteFile 没有超时，
// 窗口拖动/最小化、RDP 断开、Windows Terminal 忙时写会长时间阻塞。
type blockingTerm struct {
	*fakeTerm
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (b *blockingTerm) Write(p []byte) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

// TestRunSurvivesBlockedTerminalWrite 验证写出被卡住时事件循环仍然处理按键并能
// 正常退出：帧写出改为异步 + 写入期间跳过中间帧后，终端停摆不再拖死整个 UI。
func TestRunSurvivesBlockedTerminalWrite(t *testing.T) {
	ft := newFakeTerm()
	bt := &blockingTerm{fakeTerm: ft, release: make(chan struct{}), entered: make(chan struct{})}
	a := New(bt, demo.New(true))
	done := runApp(t, a)
	defer close(bt.release)

	select {
	case <-bt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal write was never invoked")
	}

	// 写出阻塞期间按键仍必须生效：两次 Ctrl+C 落在同一个 2 秒窗口内即退出。
	ft.sendKey(input.RuneKey('c', input.ModCtrl))
	time.Sleep(60 * time.Millisecond)
	ft.sendKey(input.RuneKey('c', input.ModCtrl))
	waitRun(t, done)
}
