package app

import (
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// TestMouseClickToggleThought 验证鼠标左键点击思考标题行可展开/折叠该单项：
// 会话 idle 后思考折叠，滚动到顶部点击首条思考标题 → 该思考展开。
// 工具调用标题行的点击由 view.TestMessageListToggles 覆盖映射，此处验证端到端路径。
func TestMouseClickToggleThought(t *testing.T) {
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitIdle(t, a)

	// Tab 切到消息区 → Home 滚动到顶部，使首条思考标题行可见。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyHome))
	// 等待渲染帧更新：拿到首条思考标题行（contentY）且已滚到顶部。
	row := -1
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		if s.BodyScroll != 0 || s.ThoughtRow < 0 {
			return false
		}
		row = s.ThoughtRow
		return true
	})

	// 点击该标题行（终端坐标 1-based）。
	ft.sendMouse(input.MouseEvent{Button: input.MouseLeft, Action: input.MousePress, X: 1, Y: row + 1})
	time.Sleep(150 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	s := a.model.Active
	if s == nil {
		t.Fatal("active session should exist")
	}
	// 会话中第一个思考（user-thought-0）应已被点击展开。
	firstThought := -1
	for i, m := range s.Messages {
		if m.Kind == model.MsgThought {
			firstThought = i
			break
		}
	}
	if firstThought < 0 {
		t.Fatal("no thought message found in demo session")
	}
	if !s.Messages[firstThought].Expanded {
		t.Errorf("clicked thought %s should be expanded, Expanded=%v",
			s.Messages[firstThought].MessageID, s.Messages[firstThought].Expanded)
	}
}
