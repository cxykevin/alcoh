package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/logo"
)

// TestHomeDrawLogo 验证首页右侧面板水平+垂直居中绘制 ascii logo：
// # 用白色背景填充、/ 用亮蓝背景填充，其余字符保留背景。
// 右侧面板从 listW=32 开始；logo 28×5 在面板内 XY 居中。
func TestHomeDrawLogo(t *testing.T) {
	const w, h = 100, 20
	const listW = 32
	m := model.New()
	b := renderer.NewBuffer(w, h)
	v := NewAppView(renderer.DefaultTheme())
	v.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)

	logoW, logoH := asciiLogoSize(logo.Ansi)
	panelW := w - listW
	if logoW >= panelW || logoH >= h {
		t.Fatalf("logo %dx%d should fit panel %dx%d", logoW, logoH, panelW, h)
	}
	x0 := listW + (panelW-logoW)/2
	y0 := (h - logoH) / 2

	// logo 第 0 行 "       ##            ##"：x0+7、x0+8 为白色背景填充。
	if c := b.Get(x0+7, y0); c.Style.Bg != renderer.RGB(0xFF, 0xFF, 0xFF) {
		t.Errorf("logo # cell Bg = %v, want white bg fill", c.Style.Bg)
	}
	// logo 第 3 行 "##  ## ## ##     //  ##   ##"：x0+17、x0+18 为亮蓝背景填充。
	if c := b.Get(x0+17, y0+3); c.Style.Bg != renderer.RGB(0x00, 0x99, 0xFF) {
		t.Errorf("logo / cell Bg = %v, want bright blue bg fill", c.Style.Bg)
	}
	// 空白保留默认背景：logo 第 0 行起始列。
	if c := b.Get(x0, y0); !c.Style.Bg.IsDefault() {
		t.Errorf("logo leading space cell Bg = %v, want default", c.Style.Bg)
	}
	// 会话列表最左侧不画 logo（logo 位于右侧面板）。
	if c := b.Get(5, y0); !c.Style.Bg.IsDefault() {
		t.Errorf("session list cell Bg = %v, want default", c.Style.Bg)
	}
	// logo 下方空一行显示欢迎语 "Welcome to alcoh!"（水平居中）。
	wy := y0 + logoH + 1
	welcome := "Welcome to alcoh!"
	ver := verSuffix()
	lineW := renderer.StringWidth(welcome) + renderer.StringWidth(ver)
	startX := listW + (panelW-lineW)/2
	cell := b.Get(startX, wy)
	if cell.Style.Fg != renderer.RGB(0x4F, 0x9C, 0xF9) {
		t.Errorf("welcome line Fg = %v, want primary", cell.Style.Fg)
	}
	// 欢迎语之后接灰色版本号后缀（" v0.0.0"）。
	verX := startX + renderer.StringWidth(welcome)
	vc := b.Get(verX, wy)
	if vc.Style.Fg != renderer.RGB(0x8B, 0x94, 0x9E) {
		t.Errorf("version suffix Fg = %v, want text-muted gray", vc.Style.Fg)
	}
	if got, want := string(rune(vc.R)), ver[:1]; got != want {
		t.Errorf("version suffix first rune = %q, want %q", got, want)
	}
	// 欢迎语与 logo 之间留空行：logo 最后一行下一行为空。
	if !b.Get(x0, wy-1).Style.Bg.IsDefault() || b.Get(x0, wy-1).R != ' ' {
		t.Errorf("blank row between logo and welcome: got %q", b.Get(x0, wy-1).R)
	}
}

// TestHomeSlashPanelCoversBackground 验证首页命令面板是浮层：打开后面板
// 区域填充默认背景空格，遮住下方内容（如 logo）的残留像素。
func TestHomeSlashPanelCoversBackground(t *testing.T) {
	const w, h = 100, 20
	m := model.New()
	for _, r := range []rune{'/'} {
		m.Input.InsertRune(r)
	}
	m.UpdateSlashState()
	if !m.SlashOpen {
		t.Fatal("slash should open after typing /")
	}

	b := renderer.NewBuffer(w, h)
	v := NewAppView(renderer.DefaultTheme())
	v.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)

	// 面板在输入框上方，高度 slashH（主页固定 8 行，右侧面板 1/3 封顶）。
	rightX := 32
	slashH := 8
	if slashH > h/3 {
		slashH = h / 3
	}
	// 面板区域内应为默认背景空格：既不是主题 PanelBg，也不是下方 logo 的白底。
	y := h - slashH - 3 // 面板中某一行（输入框上方）
	foundDefault := false
	for x := rightX; x < w; x++ {
		c := b.Get(x, y)
		if c.R == ' ' && c.Style.Bg.IsDefault() {
			foundDefault = true
			break
		}
	}
	if !foundDefault {
		t.Errorf("slash panel should fill default-background spaces to cover content below")
	}
}

// TestHomeDrawLogoNarrow 验证面板过窄/过矮时隐藏 logo。
func TestHomeDrawLogoNarrow(t *testing.T) {
	m := model.New()
	b := renderer.NewBuffer(30, 12)
	v := NewAppView(renderer.DefaultTheme())
	v.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, 30, 12), m)
	if c := b.Get(15, 6); !c.Style.Bg.IsDefault() {
		t.Errorf("narrow home should hide logo, got bg fill at center")
	}
}

// TestHomeHints 验证首页快捷键提示：左侧会话列表底部有 "d 删除会话"，
// 输入框上方有 "← 恢复会话"。
func TestHomeHints(t *testing.T) {
	const w, h = 100, 24
	m := model.New()
	m.Sessions = []*acp.SessionInfo{
		{SessionID: "s0", Title: "t0"},
		{SessionID: "s1", Title: "t1"},
	}
	m.SetAgentInfo(acp.AgentInfo{}, acp.AgentCapabilities{Raw: json.RawMessage(`{"session":{"delete":{}}}`)})

	b := renderer.NewBuffer(w, h)
	v := NewAppView(renderer.DefaultTheme())
	v.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)

	// 左侧列表底部一行（列表 inner 底行）应显示 "d 删除会话"。
	listBottom := h - 2 // inner = r.Inset(1,1)，底行 = 1 + (h-2) - 1 = h-2
	leftRow := effortRowText(b, listBottom, 32)
	if !strings.Contains(leftRow, "d 删除会话") {
		t.Errorf("sidebar hint row = %q, want containing \"d 删除会话\"", leftRow)
	}

	// 输入框上方提示行 "← 恢复会话"：在首页右面板，位于输入框上方。
	// 输入框 y 与 listW=32 同 drawHome 计算，inputH=1，slashH=0：
	// y = right.H - 1 - 2 - 0 = h-3，sepY = y，提示在 sepY-2 = h-5。
	hintRow := effortRowText(b, h-5, w-32)
	if !strings.Contains(hintRow, "← 恢复会话") {
		t.Errorf("input hint row = %q, want containing \"← 恢复会话\"", hintRow)
	}
}

// TestDrawBottomPromptStyle 验证底部提示的渲染：错误用红色带 "error: " 前缀，
// 信息提示（如复制成功）用亮蓝色且不带前缀。
func TestDrawBottomPromptStyle(t *testing.T) {
	theme := renderer.DefaultTheme()
	cases := []struct {
		name   string
		info   bool
		msg    string
		want   string
		wantFG renderer.Color
	}{
		{"error", false, "boom", "error: boom", theme.Error},
		{"info", true, "已复制 3 个字符", "已复制 3 个字符", theme.Info},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := model.New()
			if c.info {
				m.ShowInfo(c.msg)
			} else {
				m.ShowError(c.msg)
			}

			const w, h = 40, 6
			b := renderer.NewBuffer(w, h)
			v := NewAppView(theme)
			v.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)

			y := h - 2
			var txt []rune
			for x := 0; x < w; x++ {
				cell := b.Get(x, y)
				if cell.Width == 0 {
					continue
				}
				txt = append(txt, cell.R)
			}
			got := strings.TrimSpace(string(txt))
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("bottom row = %q, want prefix %q", got, c.want)
			}
			cell := b.Get(1, y)
			if cell.Width == 0 {
				t.Fatal("no cell at start of prompt text")
			}
			if cell.Style.Fg != c.wantFG {
				t.Errorf("prompt fg = %v, want %v", cell.Style.Fg, c.wantFG)
			}
		})
	}
}
