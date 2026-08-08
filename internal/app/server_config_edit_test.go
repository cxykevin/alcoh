package app

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// TestServerConfigEditorAddThenEditInt 回归测试：新增模型并等写回与整配置重载
// 完成（Saving 解除）后，编辑数字字段 CompressSize。两个 set 串行完成，
// 每次写回都触发全量重载，最终服务端值与界面显示均为编辑后的新值——
// 验证"改动即保存、等待服务端确认后全量重载"的链路不丢 int 编辑。
func TestServerConfigEditorAddThenEditInt(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开编辑器
	waitAtomic(t, &b.gets, 1, "config/get calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// 导航到 Models 集合页 (新增) 行并新增模型。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // Model
	ft.sendKey(input.SimpleKey(input.KeyDown))  // Models
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 集合页
	ft.sendKey(input.SimpleKey(input.KeyDown))  // 键 2
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (新增)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 新增（Current=新模型子页）

	// 新增后进入 Saving 阻塞：先等写回发出（Saving 已置位），再等写回完成、
	// 重载结束、重定向到新模型页，之后才允许继续编辑。
	waitAtomic(t, &b.sets, 1, "config/set calls after add")
	waitNotSaving(t, a)
	if got := a.snapshot().ServerCurKey; got != "3" {
		t.Fatalf("current after add = %q, want model 3", got)
	}

	// 编辑新模型第一行 CompressSize（int 字段）。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 编辑 CompressSize
	time.Sleep(30 * time.Millisecond)
	ft.sendKey(input.RuneKey('u', input.ModCtrl)) // 清空预填 128000
	for _, r := range "99999" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 提交（Saving 阻塞开始）

	// 第二个 set（CompressSize 编辑）串行完成，其后全量重载读到新值。
	waitAtomic(t, &b.sets, 2, "config/set calls")
	// 重载完成（Saving 解除）且仍在模型 3 子页。
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.ServerCfg && !s.ServerSaving && s.ServerCurKey == "3"
	})

	// 锁内读取最终值断言（编辑写回 + 全量重载后以服务端为准，显示编辑后的值）。
	a.modelMu.RLock()
	var compNum float64
	compOK := false
	if ed := a.model.ServerCfg; ed != nil {
		if cur := ed.Current(); cur != nil && cur.Key == "3" {
			if n := nodeChild(cur, "CompressSize"); n != nil && n.Kind == model.ConfigNumber {
				compNum = n.Num
				compOK = true
			}
		}
	}
	a.modelMu.RUnlock()
	if !compOK || compNum != 99999 {
		t.Fatalf("CompressSize after edit = %v (ok=%v), want 99999", compNum, compOK)
	}

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// waitNotSaving 轮询直到配置编辑器不在保存中（写回与全量重载已完成）。
// 经锁内快照读取，避免与事件循环并发读写。
func waitNotSaving(t *testing.T, a *App) {
	t.Helper()
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg && !s.ServerSaving })
}

// ---- 小辅助（供本文件 UI 驱动测试用）----

// nodeChild 返回节点的直接子节点。
func nodeChild(n *model.ConfigNode, key string) *model.ConfigNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}
