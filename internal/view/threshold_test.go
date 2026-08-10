package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// drawThreshold 渲染 /threshold 弹窗并返回各行文本。
func drawThreshold(ts *model.ThresholdState, w, h int) []string {
	b := renderer.NewBuffer(w, h)
	canv := renderer.NewCanvas(b)
	tc := &ThresholdContent{Theme: renderer.DefaultTheme(), Ts: ts}
	tc.Draw(canv, renderer.NewRect(0, 0, w, h))
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = effortRowText(b, y, w)
	}
	return rows
}

func TestThresholdContentDraw(t *testing.T) {
	const w, h = 80, 10
	cases := []struct {
		name string
		ts   *model.ThresholdState
		want []string
	}{
		{"loading", &model.ThresholdState{Loading: true}, []string{"正在获取当前压缩阈值"}},
		{"loaded", &model.ThresholdState{ModelName: "deepseek-chat", Input: "32768"}, []string{"deepseek-chat", "压缩阈值", "32768", "Enter 保存"}},
		{"error", &model.ThresholdState{ModelName: "m", Input: "abc", Error: "压缩阈值需为正整数"}, []string{"错误: 压缩阈值需为正整数"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := drawThreshold(c.ts, w, h)
			joined := strings.Join(rows, "\n")
			for _, want := range c.want {
				if !strings.Contains(joined, want) {
					t.Errorf("missing %q in render:\n%s", want, joined)
				}
			}
		})
	}
}
