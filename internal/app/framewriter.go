package app

import (
	"sync/atomic"
	"time"
)

// frameFlushTimeout 是退出时等待在途帧写完的最长时间。
const frameFlushTimeout = 1200 * time.Millisecond

// frameWriter 把整帧 ANSI 字节交给独立 goroutine 写出，事件循环只在写入器空闲时
// 提交新帧，写入期间跳过的中间帧由下一帧重新绘制（保留最新状态即可）。
//
// 为什么需要异步写：Windows 控制台的 WriteFile 没有超时——控制台句柄不可轮询，
// SetWriteDeadline 直接返回 ErrNoDeadline。当终端/ConPTY 停止消费输出（拖动或
// 缩放窗口、最小化、RDP 断开、Windows Terminal 忙）时写会长时间阻塞。若在主循环
// 里同步写，键盘、鼠标、Ctrl+C 事件也一并停摆：整个界面卡死，只能关窗口。
// 改为异步后主循环始终响应，终端恢复消费时立即补上最新画面。
type frameWriter struct {
	ch   chan []byte
	busy atomic.Bool
	done chan struct{}
	w    func([]byte) error
}

// newFrameWriter 创建写入器并启动写出 goroutine。
func newFrameWriter(w func([]byte) error) *frameWriter {
	fw := &frameWriter{
		ch:   make(chan []byte, 1),
		done: make(chan struct{}),
		w:    w,
	}
	go fw.loop()
	return fw
}

func (fw *frameWriter) loop() {
	for {
		select {
		case b := <-fw.ch:
			_ = fw.w(b) // 写出错误与同步路径一致：仅忽略（不中断会话）
			fw.busy.Store(false)
		case <-fw.done:
			return
		}
	}
}

// Ready 报告写入器空闲（没有在途帧），此时可以重新绘制下一帧。
func (fw *frameWriter) Ready() bool { return !fw.busy.Load() }

// TryFrame 在空闲时提交一帧。返回 false 表示上一帧还在写，调用方必须保留
// 自己的帧缓冲（不要交换 front/back），等 Ready 后再重画。
// 生产者只有事件循环一个，因此 busy 的判断/设置无需额外加锁。
func (fw *frameWriter) TryFrame(b []byte) bool {
	if fw.busy.Load() {
		return false
	}
	fw.busy.Store(true)
	select {
	case fw.ch <- b:
		return true
	default:
		fw.busy.Store(false)
		return false
	}
}

// Close 等待在途帧写完（最多 timeout）后停止写入器。返回是否已经写完；
// 超时（终端彻底停摆）时不阻塞退出，交给进程结束收尾。
func (fw *frameWriter) Close(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for fw.busy.Load() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	close(fw.done)
	return !fw.busy.Load()
}
