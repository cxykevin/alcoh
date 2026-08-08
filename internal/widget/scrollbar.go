package widget

import (
	"github.com/cxykevin/alcoh/internal/renderer"
)

// Scrollbar 绘制右侧视觉滚动条。
type Scrollbar struct {
	Total int // 内容总行数
	View  int // 视口行数
	Top   int // 视口顶部内容行号
	Track renderer.Style
	Thumb renderer.Style
}

func (s *Scrollbar) Draw(c *renderer.Canvas, r renderer.Rect) {
	barW := 1
	if r.W < barW {
		return
	}
	// 内容不超一屏时显示占位（细条）
	track := renderer.CellRune('│', s.Track)
	if s.Total <= s.View {
		// 无滚动需要：细点线
		for y := r.Y; y < r.Y+r.H; y++ {
			c.Put(r.X, y, renderer.CellRune('·', s.Track))
		}
		return
	}
	// 计算 thumb 位置
	thumbH := s.View * s.View / s.Total
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > s.View {
		thumbH = s.View
	}
	scrollable := s.Total - s.View
	ratio := float64(s.Top) / float64(scrollable)
	thumbTop := int(ratio * float64(s.View-thumbH))

	thumb := renderer.CellRune('█', s.Thumb)
	for y := r.Y; y < r.Y+r.H; y++ {
		off := y - r.Y
		if off >= thumbTop && off < thumbTop+thumbH {
			c.Put(r.X, y, thumb)
		} else {
			c.Put(r.X, y, track)
		}
	}
}
