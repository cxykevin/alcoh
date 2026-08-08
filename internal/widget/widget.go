// Package widget 提供 TUI 布局原语与交互组件。
// 采用命令式绘制 + 裁剪模型，无 reconcile。
package widget

import (
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// Widget 是可绘制的组件。
type Widget interface {
	// Draw 把自身绘制到 canvas 的 r 区域内。
	Draw(c *renderer.Canvas, r renderer.Rect)
}

// Focusable 是可接收按键的组件。返回 true 表示已处理。
type Focusable interface {
	OnKey(ke input.KeyEvent) bool
}

// Mouseable 预留：鼠标交互（本轮暂不实现）。
type Mouseable interface{}
