package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestShellPreviewFillsBoxWrapsAndUsesWhite 验证预览区：
//   - VT 屏幕随预览框尺寸重建，长行按框宽自动换行（不截断、不加省略号）；
//   - 内容铺满整个预览框宽度；
//   - 终端文字统一白色。
func TestShellPreviewFillsBoxWrapsAndUsesWhite(t *testing.T) {
	useEnglish(t)
	const w, h = 120, 24
	m := &model.AppModel{}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(
		"{\"alk.cxykevin.top/alkaid0/v0.5\":{}}")})
	m.ActivateSession("s1", "会话")
	m.ShellPanel = true
	// 单行 200 字符：不换行时会只剩一行并带省略号。
	long := strings.Repeat("abcdefghij", 20)
	m.Active.ApplyTerminalInfo(acp.TerminalInfo{
		TerminalID: "@temp/run/1", Reason: "build", Command: "make", Status: "running", Content: long,
	})
	m.ShellSelected = 0

	b := renderer.NewBuffer(w, h)
	canv := renderer.NewCanvas(b)
	p := &ShellPanel{Theme: renderer.DefaultTheme()}
	p.Draw(canv, renderer.NewRect(0, 0, w, h), m)

	// 预览框内部：列表占 3/5 宽，框体再各让 1 列边框，最右一列是滚动条。
	left := w * 3 / 5
	innerX := left + 2
	innerW := w - 1 - innerX - 1
	rows := 0
	white := renderer.RGB(0xFF, 0xFF, 0xFF)
	// y=0 是预览框上边框，y=1 是标题 + 状态行（带色），其余为终端内容。
	for y := 2; y < h-2; y++ {
		filled := 0
		whiteCells := 0
		for x := innerX; x < innerX+innerW; x++ {
			cell := b.Get(x, y)
			if cell.Width == 0 || cell.R == ' ' {
				continue
			}
			filled++
			if cell.Style.Fg == white {
				whiteCells++
			}
		}
		if filled > 0 {
			rows++
			if whiteCells == 0 {
				t.Fatalf("row %d has content but is not white (filled=%d)", y, filled)
			}
		}
	}
	if rows < 2 {
		t.Fatalf("long single line must wrap into multiple rows, got %d content rows", rows)
	}
	// 预览区不应出现截断省略号。
	for _, line := range bufferText(b) {
		runes := []rune(line)
		if len(runes) <= left {
			continue
		}
		if strings.Contains(string(runes[left:]), "…") {
			t.Fatalf("preview must wrap instead of truncating: %q", line)
		}
	}
	if m.Active.Terminal("@temp/run/1").Screen == nil {
		t.Fatal("screen must be kept in sync")
	}
	if sw := m.Active.Terminal("@temp/run/1").Screen.Width; sw != innerW {
		t.Fatalf("screen width = %d, want %d (preview inner width)", sw, innerW)
	}
	// 滚动条画在预览框最右内列。
	barX := innerX + innerW
	track := 0
	for y := 2; y < h-2; y++ {
		if cell := b.Get(barX, y); cell.R == '·' || cell.R == '█' || cell.R == '│' {
			track++
		}
	}
	if track == 0 {
		t.Fatalf("scrollbar column %d is empty", barX)
	}
}

// TestAppViewBodyRows 验证渲染时写入正文内容总行数与块目录（拖拽自动滚动的
// 边界判断依赖它），回首页后清零。
func TestAppViewBodyRows(t *testing.T) {
	useEnglish(t)
	const w, h = 120, 24
	// 用 model.New()：会话视图会画输入框，需要有初始化的 Input。
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1",
		Message: acp.Message{MessageID: "m1", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("你好，世界")}}},
	})

	v := NewAppView(renderer.DefaultTheme())
	canv := renderer.NewCanvas(renderer.NewBuffer(w, h))
	v.Draw(canv, renderer.NewRect(0, 0, w, h), m)
	if len(v.Body) == 0 || v.BodyRows == 0 {
		t.Fatalf("body blocks = %d, rows = %d", len(v.Body), v.BodyRows)
	}

	// 回首页：正文数据清零，避免残留上一会话的行数。
	m.GoHome()
	v.Draw(canv, renderer.NewRect(0, 0, w, h), m)
	if v.BodyRows != 0 || len(v.Body) != 0 {
		t.Fatalf("home: rows = %d, blocks = %d", v.BodyRows, len(v.Body))
	}
}
