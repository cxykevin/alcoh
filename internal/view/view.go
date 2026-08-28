package view

import (
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
	"github.com/cxykevin/alcoh/logo"
	"github.com/cxykevin/alcoh/product"
)

// AppView 是顶层视图：根据 model 状态绘制首页/会话视图及模态层。
type AppView struct {
	Theme     renderer.Theme
	SpinFrame int

	// Body/BodyRect/BodyScroll 记录最近一帧会话正文的块目录与消息区屏幕位置，
	// 供鼠标选择的原始 markdown 复制与选区高亮使用（渲染时填充）。
	Body       []BodyBlock
	BodyRect   renderer.Rect
	BodyScroll int
	// BodyToggles 记录最近一帧可点击切换展开/折叠的正文行（contentY → 目标，
	// 思考/工具标题行）。鼠标左键命中时展开/折叠对应单项。
	BodyToggles map[int]ToggleRef
}

// NewAppView 创建视图。
func NewAppView(theme renderer.Theme) *AppView {
	return &AppView{Theme: theme}
}

// Draw 绘制整个屏幕。
func (v *AppView) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	contentRect := r
	if m.Modal != model.NoModal {
		modalH := v.modalHeight(m)
		if modalH > contentRect.H {
			modalH = contentRect.H
		}
		contentRect.H -= modalH
	}
	switch m.View {
	case model.ViewHome:
		v.drawHome(c, contentRect, m)
	case model.ViewSession:
		v.drawSession(c, contentRect, m)
	}

	// 底部提示先绘制；弹窗接管底部区域。错误用红色带 "error: " 前缀，
	// 信息提示（如复制成功）用亮蓝色、不带前缀。
	if m.Error != "" && m.Modal == model.NoModal && contentRect.W > 2 && contentRect.H >= 2 {
		text := m.Error
		style := v.Theme.Style(v.Theme.Error)
		if m.ErrorInfo {
			style = v.Theme.Style(v.Theme.Info)
		} else {
			text = "error: " + m.Error
		}
		c.PutText(contentRect.X+1, contentRect.Y+contentRect.H-2, renderer.Truncate(text, contentRect.W-2), style)
	}
	if m.Modal != model.NoModal {
		v.drawModalAtInput(c, r, m)
	}
}

func (v *AppView) modalHeight(m *model.AppModel) int {
	h := 8
	switch m.Modal {
	case model.ModalPermission:
		if p := m.Permission; p != nil {
			h += len(p.Options)
			if permissionSubjectText(p.Subject) != "" {
				h++
			}
		}
	case model.ModalElicitation:
		if e := m.Elicitation; e != nil {
			if e.Request.Mode == acp.ElicitationModeForm {
				h = 6 + len(e.FieldOrder)*2
			} else {
				h = 8
			}
		}
	case model.ModalHelp, model.ModalSettings:
		h = 10
	case model.ModalEffort:
		h = 8
	case model.ModalModel:
		// 顶部"当前"行 + 空行 + 选项列表 + 空行 + 操作提示，再加标题横线 1 行。
		h = 3 + len(m.ModelOptions()) + 2
		if h < 6 {
			h = 6
		}
	case model.ModalExitConfirm:
		h = 5
	case model.ModalServer, model.ModalPlugins:
		h = 9
	case model.ModalOnboarding, model.ModalConnect:
		// 新手引导 / 连接向导占满整屏：底层内容无需缩小。
		h = 0
	}
	return h
}

// drawModalAtInput 将弹窗作为底部全宽面板绘制，替换输入框和状态栏。
func (v *AppView) drawModalAtInput(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	h := v.modalHeight(m)
	title := ""
	style := v.Theme.Style(v.Theme.BorderActive)
	var content widget.Widget
	switch m.Modal {
	case model.ModalPermission:
		p := m.Permission
		if p != nil {
			title = permissionTitle(p)
		}
		content = permissionContent(v.Theme, m)
	case model.ModalElicitation:
		e := m.Elicitation
		if e != nil {
			title = i18n.T("请求信息")
			if e.Request.Message != "" {
				title = e.Request.Message
			}
		}
		content = elicitationContent(v.Theme, m)
	case model.ModalHelp:
		h = 10
		title = i18n.T("帮助")
		style = v.Theme.Style(v.Theme.Border)
		content = helpContent(v.Theme)
	case model.ModalExitConfirm:
		h = 5
		title = i18n.T("退出")
		style = v.Theme.Style(v.Theme.Error)
		content = &TextLines{Theme: v.Theme, Lines: []string{i18n.T("确定退出 alcoh 吗？"), "", i18n.T("  y 退出    n / Esc 取消")}}
	case model.ModalSettings:
		h = 10
		title = i18n.T("设置（本地）")
		content = &SettingsContent{Theme: v.Theme, Values: m.Settings, Selected: m.SettingsSelected, ACPCount: protocolUpdateCount(m)}
	case model.ModalEffort:
		h = 8
		title = i18n.T("推理强度 (thought_level)")
		content = effortContent(v.Theme, m)
	case model.ModalModel:
		h = 3 + len(m.ModelOptions()) + 2
		if h < 6 {
			h = 6
		}
		title = i18n.T("模型选择 (model)")
		content = modelContent(v.Theme, m)
	case model.ModalServer:
		// 配置编辑器占满整屏（DrawSheet 全宽面板）。
		h = r.H
		title = i18n.T("服务端配置 (alk.cxykevin.top/config)")
		content = &ConfigTree{Theme: v.Theme, Tree: m.ServerCfg}
	case model.ModalPlugins:
		// 本地配置编辑器占满整屏（复用 /server 的 ConfigTree 面板）。
		h = r.H
		title = i18n.T("本地配置 (config.json)")
		content = &ConfigTree{Theme: v.Theme, Tree: m.PluginsCfg}
	case model.ModalOnboarding:
		// 新手引导占满整屏（DrawSheet 全宽面板）。
		h = r.H
		title = i18n.T("首次设置")
		content = &OnboardingContent{Theme: v.Theme, Ob: m.Onboarding}
	case model.ModalConnect:
		// /connect 向导占满整屏（DrawSheet 全宽面板）。
		h = r.H
		title = i18n.T("连接模型服务商")
		content = &ConnectContent{Theme: v.Theme, Cs: m.Connect}
	default:
		return
	}
	if h > r.H {
		h = r.H
	}
	if h < 1 {
		return
	}
	(&widget.Modal{Height: h, Title: title, Style: style, Content: content}).DrawSheet(c, r)
}

// drawModalAtInputLegacy 保留旧的底部边框弹窗绘制实现，供兼容调用。
func (v *AppView) drawModalAtInputLegacy(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	bottom := r.Y + r.H - 2 // 保留状态栏与最底部空行
	if bottom <= r.Y {
		bottom = r.Y + r.H
	}
	bottomMargin := r.Y + r.H - bottom
	availableH := bottom - r.Y
	switch m.Modal {
	case model.ModalPermission:
		p := m.Permission
		h := 8
		if p != nil {
			h += len(p.Options)
			if permissionSubjectText(p.Subject) != "" {
				h++
			}
		}
		if h > availableH {
			h = availableH
		}
		(&widget.Modal{Width: minModalWidth(r.W, 60), Height: h, Title: permissionTitle(p), Style: v.Theme.Style(v.Theme.BorderActive), Content: permissionContent(v.Theme, m)}).DrawBottom(c, r, bottomMargin)
	case model.ModalHelp:
		lines := 13
		if lines+2 > availableH {
			lines = availableH - 2
		}
		if lines < 0 {
			lines = 0
		}
		(&widget.Modal{Width: minModalWidth(r.W, 72), Height: lines + 2, Title: i18n.T("帮助"), Style: v.Theme.Style(v.Theme.Border), Content: helpContent(v.Theme)}).DrawBottom(c, r, bottomMargin)
	case model.ModalExitConfirm:
		(&widget.Modal{Width: minModalWidth(r.W, 40), Height: minValue(availableH, 5), Title: i18n.T("退出"), Style: v.Theme.Style(v.Theme.Error), Content: &TextLines{Theme: v.Theme, Lines: []string{i18n.T("确定退出 alcoh 吗？"), "", i18n.T("  y 退出    n / Esc 取消")}}}).DrawBottom(c, r, bottomMargin)
	case model.ModalSettings:
		(&widget.Modal{Width: minModalWidth(r.W, 68), Height: minValue(availableH, 10), Title: i18n.T("设置（本地）"), Style: v.Theme.Style(v.Theme.BorderActive), Content: &SettingsContent{Theme: v.Theme, Values: m.Settings, Selected: m.SettingsSelected, ACPCount: protocolUpdateCount(m)}}).DrawBottom(c, r, bottomMargin)
	case model.ModalEffort:
		(&widget.Modal{Width: minModalWidth(r.W, 68), Height: minValue(availableH, 8), Title: i18n.T("推理强度 (thought_level)"), Style: v.Theme.Style(v.Theme.BorderActive), Content: effortContent(v.Theme, m)}).DrawBottom(c, r, bottomMargin)
	case model.ModalServer:
		(&widget.Modal{Width: minModalWidth(r.W, 70), Height: minValue(availableH, availableH), Title: i18n.T("服务端配置"), Style: v.Theme.Style(v.Theme.BorderActive), Content: &ConfigTree{Theme: v.Theme, Tree: m.ServerCfg}}).DrawBottom(c, r, bottomMargin)
	case model.ModalPlugins:
		(&widget.Modal{Width: minModalWidth(r.W, 70), Height: minValue(availableH, availableH), Title: i18n.T("本地配置"), Style: v.Theme.Style(v.Theme.BorderActive), Content: &ConfigTree{Theme: v.Theme, Tree: m.PluginsCfg}}).DrawBottom(c, r, bottomMargin)
	}
}

func minModalWidth(screen, wanted int) int {
	if screen < wanted {
		return screen
	}
	return wanted
}

func minValue(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func permissionTitle(p *acp.PermissionRequest) string {
	if p == nil {
		return i18n.T("权限请求")
	}
	return p.Title
}

func permissionContent(t renderer.Theme, m *model.AppModel) *PermissionContent {
	if m.Permission == nil {
		return &PermissionContent{Theme: t}
	}
	return &PermissionContent{Theme: t, Subject: permissionSubjectText(m.Permission.Subject), Description: optionalText(m.Permission.Description), Options: m.Permission.Options, Selected: m.PermSelected}
}

func elicitationContent(t renderer.Theme, m *model.AppModel) widget.Widget {
	if m.Elicitation == nil {
		return &TextLines{Theme: t, Lines: []string{i18n.T("无请求")}}
	}

	e := m.Elicitation
	if e.Request.Mode == acp.ElicitationModeURL {
		lines := []string{
			"",
			i18n.T("URL: %s", e.Request.URL),
			"",
			i18n.T("Enter 接受 | d 拒绝 | Esc 取消"),
		}
		return &TextLines{Theme: t, Lines: lines}
	}

	// Form 模式
	lines := []string{""}
	if e.Request.Message != "" {
		lines = append(lines, e.Request.Message, "")
	}

	for i, field := range e.FieldOrder {
		// 字段名
		prefix := "  "
		if i == e.FieldIndex {
			prefix = "> "
		}

		fieldLabel := field
		// 检查是否必需
		if required, ok := e.Schema["required"].([]interface{}); ok {
			for _, r := range required {
				if str, ok := r.(string); ok && str == field {
					fieldLabel += " *"
					break
				}
			}
		}

		lines = append(lines, prefix+fieldLabel+":")

		// 字段值
		value := ""
		if m.ElicitationFormData != nil {
			value = m.ElicitationFormData[field]
		}
		if value == "" {
			value = i18n.T("(空)")
		}
		lines = append(lines, "    "+value)
	}

	lines = append(lines, "")
	if e.ErrorMessage != "" {
		lines = append(lines, i18n.T("错误: %s", e.ErrorMessage), "")
	}
	lines = append(lines, i18n.T("↑↓ 选择字段 | Tab 下一个 | 输入文本 | Ctrl+Enter 提交 | Esc 取消"))

	return &TextLines{Theme: t, Lines: lines}
}

func helpContent(t renderer.Theme) *TextLines {
	return &TextLines{Theme: t, Lines: []string{
		i18n.T("alcoh 快捷键"), "",
		i18n.T("Enter          提交输入        Shift+Enter / 行尾 \\ + Enter 换行"),
		i18n.T("/              命令面板        Ctrl+,         打开设置"),
		i18n.T("/connect       连接模型服务商（模板/填 key/拉取模型）"),
		i18n.T("/effort        推理强度滑条    /clear         清除会话(on 不取消)"),
		i18n.T("↑↓             移动 / 历史     ←→            移动光标"),
		i18n.T("Ctrl+A/E       行首 / 行尾     Ctrl+K/U      删至行尾/行首"),
		i18n.T("Ctrl+W         删前一词        Ctrl+Y        粘贴"),
		i18n.T("Ctrl+/         撤销            Ctrl+Q        退出"),
		i18n.T("PageUp/Down    滚动            Home/End      顶部/底部"),
		i18n.T("Tab            切换焦点        Enter         展开/折叠"),
		i18n.T("鼠标点击       思考/工具标题    展开/折叠单项"),
		i18n.T("?              帮助            Esc           关闭"),
		i18n.T("Enter          恢复会话(空输入) d            删除选中会话（首页）"), "",
		i18n.T("权限弹窗: ↑↓ 选择选项, a=allow, r=reject, Enter 确认, Esc 取消"),
	}}
}

// 布局（自上而下）：
//
//	消息区(flex) / 横线 / 输入框 / 横线 / 状态栏 / 底部空 1 行（终端默认背景）。
func (v *AppView) drawSession(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	s := m.ActiveSession()
	if s == nil {
		v.drawHome(c, r, m)
		return
	}
	pp := &PlanPanel{Theme: v.Theme, SpinFrame: v.SpinFrame}
	planH := pp.Height(s)
	if planH > r.H {
		planH = r.H
	}

	// 弹窗接管输入框和状态栏；主体只绘制消息与固定计划面板。
	if m.ShellPanel && m.Modal == model.NoModal {
		(&ShellPanel{Theme: v.Theme}).Draw(c, r, m)
		return
	}
	if m.Modal != model.NoModal {
		msgH := r.H - planH
		if msgH > 0 {
			ml := &MessageList{Theme: v.Theme, SpinFrame: v.SpinFrame}
			ml.Draw(c, renderer.NewRect(r.X, r.Y, r.W, msgH), s)
			v.Body = ml.Body
			v.BodyRect = renderer.NewRect(r.X, r.Y, r.W, msgH)
			v.BodyScroll = ml.Scroll
			v.BodyToggles = ml.Toggles
		}
		if planH > 0 {
			pp.Draw(c, renderer.NewRect(r.X, r.Y+msgH, r.W, planH), s)
		}
		return
	}

	spacerH := 1 // 底部留空一行
	statusH := 1
	sepH := 1  // 输入栏下方横线
	sep2H := 1 // 输入栏上方横线
	inputH := 1
	if m.Input != nil {
		inputH = m.Input.VisualHeight(r.W, renderer.StringWidth("> "))
		if inputH > 6 {
			inputH = 6
		}
		maxIn := r.H / 3
		if inputH > maxIn {
			inputH = maxIn
		}
		if inputH < 1 {
			inputH = 1
		}
	}
	slashH := 0
	if m.SlashOpen {
		slashH = 8
		if slashH > r.H/3 {
			slashH = r.H / 3
		}
	}

	msgH := r.H - spacerH - statusH - sepH - sep2H - inputH - planH - slashH
	if msgH < 1 {
		msgH = 1
	}

	y := r.Y
	msgRect := renderer.NewRect(r.X, y, r.W, msgH)
	y += msgH
	planRect := renderer.NewRect(r.X, y, r.W, planH)
	y += planH

	ml := &MessageList{Theme: v.Theme, SpinFrame: v.SpinFrame}
	ml.Draw(c, msgRect, s)
	v.Body = ml.Body
	v.BodyRect = msgRect
	v.BodyScroll = ml.Scroll
	v.BodyToggles = ml.Toggles

	if planH > 0 {
		pp := &PlanPanel{Theme: v.Theme, SpinFrame: v.SpinFrame}
		pp.Draw(c, planRect, s)
	}
	if slashH > 0 {
		(&SlashPanel{Theme: v.Theme}).Draw(c, renderer.NewRect(r.X, y, r.W, slashH), m)
		y += slashH
	}

	sepStyle := v.Theme.Style(v.Theme.BorderSubtle)
	sep := func(yy int) {
		for x := r.X; x < r.X+r.W; x++ {
			c.Put(x, yy, renderer.CellRune('─', sepStyle))
		}
	}

	// 输入框上方横线
	sep(y)
	// 横线右上角显示当前 thinking effort：只有 level，无提示文字；
	// unset 不显示。颜色与滑条选中一致（effortColor）。
	if m.SupportsEffort() {
		if level := m.CurrentEffort(); level != "" && level != "unset" {
			if w := len(level); w+2 < r.W {
				x := r.X + r.W - w - 1
				c.PutText(x, y, level, v.Theme.Style(effortColor(v.Theme, level)).WithBold(true))
			}
		}
	}
	y++
	inputRect := renderer.NewRect(r.X, y, r.W, inputH)
	y += inputH

	// 输入框
	ghost, _ := m.SlashCompletion()
	ib := &widget.InputBox{
		Buf:        m.Input,
		Prompt:     "> ",
		Style:      v.Theme.Style(v.Theme.Text),
		Cursor:     v.Theme.StyleOn(v.Theme.Background, v.Theme.Primary),
		Focused:    m.Modal == model.NoModal,
		GhostText:  ghost,
		GhostStyle: v.Theme.Style(v.Theme.TextMuted),
	}
	ib.Draw(c, inputRect)

	// 输入框下方横线；覆盖左侧显示活动 shell 数量。
	sep(y)
	if n := len(m.Shells()); n > 0 {
		badge := itoa(n) + " shell"
		if renderer.StringWidth(badge)+2 < r.W {
			badgeStyle := v.Theme.StyleOn(renderer.RGB(255, 255, 255), renderer.RGB(0, 190, 200)).WithBold(true)
			c.PutText(r.X+1, y, " "+badge+" ", badgeStyle)
		}
	}
	y++

	// 状态栏
	statusRect := renderer.NewRect(r.X, y, r.W, statusH)
	sb := &StatusBar{Theme: v.Theme, SpinFrame: v.SpinFrame}
	sb.Draw(c, statusRect, m)
	// y += statusH 之后为底部空行（不绘制 → 终端默认背景）
}

// drawHome 绘制首页：左侧会话列表 + 右侧欢迎与首条 prompt 输入框。
// 列表默认不聚焦；宽度不足时隐藏。按左键后全屏聚焦。
func (v *AppView) drawHome(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	// 首页无正文，清空鼠标选择所需的正文目录，避免残留上一会话区域。
	v.Body = nil
	v.BodyRect = renderer.Rect{}
	v.BodyScroll = 0
	v.BodyToggles = nil

	listW := 32
	if r.W > 0 {
		listW = minValue(listW, r.W)
	}

	// 列表隐藏条件：未聚焦且宽度不足 60 列时直接隐藏。
	listHidden := !m.HomeListFocused && r.W < 60

	if m.HomeListFocused {
		// 全屏聚焦模式：只绘制列表，隐藏右侧面板。
		(&SessionList{Theme: v.Theme}).Draw(c, renderer.NewRect(r.X, r.Y, r.W, r.H), m)
		return
	}

	if !listHidden {
		(&SessionList{Theme: v.Theme}).Draw(c, renderer.NewRect(r.X, r.Y, listW, r.H), m)
	}

	rightX := r.X
	rightW := r.W
	if !listHidden {
		rightX = r.X + listW
		rightW = r.W - listW
	}
	right := renderer.NewRect(rightX, r.Y, rightW, r.H)
	if right.W < 1 || right.H < 1 {
		return
	}
	// 右侧面板中央水平+垂直居中绘制 ascii logo（# 白色背景，/ 亮蓝背景）。
	v.drawLogoCentered(c, right, logo.Ansi)
	// logo 下方空一行显示欢迎语，随后以灰色附版本号。
	if tw, th := asciiLogoSize(logo.Ansi); tw < right.W && th+2 < right.H {
		wy := right.Y + (right.H-th)/2 + th + 1
		msg := "Welcome to alcoh!"
		wx := right.X + (right.W-renderer.StringWidth(msg)-renderer.StringWidth(verSuffix()))/2
		c.PutText(wx, wy, msg, v.Theme.Style(v.Theme.Primary).WithBold(true))
		c.PutText(wx+renderer.StringWidth(msg), wy, verSuffix(), v.Theme.Style(v.Theme.TextMuted))
	}

	if m.Modal != model.NoModal {
		return
	}

	inputH := m.Input.VisualHeight(right.W, renderer.StringWidth("> "))
	if inputH > 6 {
		inputH = 6
	}
	if max := right.H - 8; inputH > max {
		inputH = max
	}
	if inputH < 1 {
		inputH = 1
	}
	// 命令输入面板：输入 / 时在输入框上方弹出命令建议列表。
	slashH := 0
	if m.SlashOpen {
		slashH = 8
		if slashH > right.H/3 {
			slashH = right.H / 3
		}
	}
	y := right.Y + right.H - inputH - 2 - slashH
	if y < right.Y+6 {
		y = right.Y + 6
	}
	// 极矮终端：输入框不能越过终端底部，且需让出底部错误提示行（contentRect.H-2），
	// 避免输入框/提示行与底部提示重叠。
	if y+inputH > right.Y+right.H-2 {
		y = right.Y + right.H - 2 - inputH
	}
	if slashH > 0 {
		(&SlashPanel{Theme: v.Theme}).Draw(c, renderer.NewRect(right.X, y, right.W, slashH), m)
	}
	sepY := y + slashH
	sepStyle := v.Theme.Style(v.Theme.BorderSubtle)
	for x := right.X; x < right.X+right.W; x++ {
		c.Put(x, sepY-1, renderer.CellRune('─', sepStyle))
	}
	// 输入框上方提示：slash 面板打开时隐藏（该行被命令列表占用）。
	if !m.SlashOpen && sepY-2 >= right.Y {
		c.PutText(right.X, sepY-2, renderer.Truncate(i18n.T("← 恢复会话"), right.W), v.Theme.Style(v.Theme.TextMuted))
	}
	ghost, _ := m.SlashCompletion()
	ib := &widget.InputBox{Buf: m.Input, Prompt: "> ", Style: v.Theme.Style(v.Theme.Text), Cursor: v.Theme.StyleOn(v.Theme.Background, v.Theme.Primary), Focused: m.Modal == model.NoModal, GhostText: ghost, GhostStyle: v.Theme.Style(v.Theme.TextMuted)}
	ib.Draw(c, renderer.NewRect(right.X, y+slashH, right.W, inputH))
}

// drawLogoCentered 在区域 r 内水平+垂直居中绘制 ascii logo。
// # 用白色背景填充，/ 用亮蓝背景填充，其余字符跳过（保留背景）。
func (v *AppView) drawLogoCentered(c *renderer.Canvas, r renderer.Rect, s string) {
	logoW, logoH := asciiLogoSize(s)
	if logoW < 1 || logoH < 1 || logoW > r.W || logoH > r.H {
		return
	}
	x0 := r.X + (r.W-logoW)/2
	y0 := r.Y + (r.H-logoH)/2
	white := renderer.Style{Bg: renderer.RGB(0xFF, 0xFF, 0xFF)}
	brightBlue := renderer.Style{Bg: renderer.RGB(0x00, 0x99, 0xFF)}
	cx, cy := x0, y0
	for _, rn := range s {
		switch rn {
		case '\n':
			cy++
			cx = x0
		case '#':
			c.Put(cx, cy, renderer.CellRune(' ', white))
			cx++
		case '/':
			c.Put(cx, cy, renderer.CellRune(' ', brightBlue))
			cx++
		default:
			cx++
		}
	}
}

// verSuffix 返回欢迎语尾部追加的版本号文本（如 " v0.1.2"）。
// BuildNote 非空时附到版本号之后（" v0.1.2 (note)"），便于区分本地构建。
func verSuffix() string {
	s := " v" + product.Version
	if product.BuildNote != "" {
		s += " (" + product.BuildNote + ")"
	}
	return s
}

// asciiLogoSize 返回 ascii 文本的最大行宽（列）与行数。
func asciiLogoSize(s string) (w, h int) {
	if s == "" {
		return 0, 0
	}
	cur := 0
	h = 1
	for _, r := range s {
		switch r {
		case '\n':
			h++
			cur = 0
		default:
			cur++
			if cur > w {
				w = cur
			}
		}
	}
	return w, h
}

// SessionList 是首页左侧会话列表。
type SessionList struct {
	Theme renderer.Theme
}

// Draw 绘制会话列表。当列表未聚焦（!HomeListFocused）时不显示选中高亮。
// 每项占 3 行：标题、更新时间、空行。底部固定留一行快捷键提示。
func (sl *SessionList) Draw(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	t := sl.Theme
	inner := r.Inset(1, 1)
	// 标题
	c.PutText(inner.X, inner.Y, "sessions", t.Style(t.Primary).WithBold(true))
	y := inner.Y + 2
	if len(m.Sessions) == 0 {
		c.PutText(inner.X, y, i18n.T("( 无会话 )"), t.Style(t.TextMuted))
		sl.drawHint(c, inner, t)
		return
	}
	// 每项固定占 3 行（标题、时间、空行）。计算可见项数并让选中项保持可见：
	// 先尝试居中，越界时收拢到边界，实现选中项超出屏幕时列表自动滚动。
	const itemRows = 3
	headerRows := 2 // "sessions" 标题 + 间隔行
	hintRows := 1   // 底部 i18n.T("r 刷新  d 删除会话") 提示行
	visible := (inner.H - headerRows - hintRows - 1) / itemRows
	if visible < 1 {
		visible = 1
	}
	if visible > len(m.Sessions) {
		visible = len(m.Sessions)
	}
	selIdx := 0
	if m.HomeListFocused && m.HomeSelected >= 0 {
		selIdx = m.HomeSelected
	}
	start := selIdx - visible/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(m.Sessions) - visible; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}

	y = inner.Y + headerRows
	for i := start; i < start+visible; i++ {
		if y >= inner.Y+inner.H-hintRows-1 {
			break
		}
		s := m.Sessions[i]
		sel := m.HomeListFocused && i == m.HomeSelected
		prefix := "  "
		st := t.Style(t.TextMuted)
		if sel {
			prefix = "> "
			st = t.Style(t.Primary).WithBold(true)
		}
		title := s.Title
		if title == "" {
			title = s.SessionID
		}
		c.PutText(inner.X, y, prefix+renderer.Truncate(title, inner.W-3), st)
		y++
		// 时间行
		if s.UpdatedAt != "" {
			c.PutText(inner.X, y, prefix+formatSessionTime(s.UpdatedAt), t.Style(t.TextMuted))
		}
		y++
		// 空行
		y++
	}
	sl.drawHint(c, inner, t)
}

// drawHint 在列表底部绘制 i18n.T("r 刷新  d 删除会话") 快捷键提示。
func (sl *SessionList) drawHint(c *renderer.Canvas, inner renderer.Rect, t renderer.Theme) {
	hintY := inner.Y + inner.H - 1
	if hintY >= inner.Y {
		c.PutText(inner.X, hintY, i18n.T("r 刷新  d 删除会话"), t.Style(t.TextMuted))
	}
}

// formatSessionTime 把 RFC3339 时间戳格式化为可读的"月-日 时:分"；解析失败时原样返回。
func formatSessionTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return parsed.Format("01-02 15:04")
}
