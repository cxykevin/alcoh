package view

import (
	"strconv"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// drawPermission 绘制权限请求弹窗。
func (v *AppView) drawPermission(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	p := m.Permission
	if p == nil {
		return
	}
	t := v.Theme
	desc := ""
	if p.Description != nil {
		desc = *p.Description
	}
	content := &PermissionContent{
		Theme:       t,
		Subject:     permissionSubjectText(p.Subject),
		Description: desc,
		Options:     p.Options,
		Selected:    m.PermSelected,
	}
	height := len(p.Options) + 8
	if permissionSubjectText(p.Subject) != "" {
		height++
	}
	if height > r.H-2 {
		height = r.H - 2
	}
	modal := &widget.Modal{
		Width:   60,
		Height:  height,
		Title:   p.Title,
		Style:   t.Style(t.BorderActive),
		Content: content,
	}
	modal.Draw(c, r)
}

// PermissionContent 是权限弹窗的内容区。
type PermissionContent struct {
	Theme       renderer.Theme
	Subject     string
	Description string
	Options     []acp.PermissionOption
	Selected    int
}

// Draw 绘制权限弹窗内容。
func (pc *PermissionContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	t := pc.Theme
	y := r.Y
	if pc.Subject != "" && y < r.Y+r.H {
		c.PutText(r.X, y, renderer.Truncate("对象: "+pc.Subject, r.W), t.Style(t.Primary).WithBold(true))
		y++
	}
	if pc.Description != "" && y < r.Y+r.H {
		for _, line := range wrapText(pc.Description, r.W) {
			if y >= r.Y+r.H {
				break
			}
			c.PutText(r.X, y, line, t.Style(t.TextMuted))
			y++
		}
	}
	y++
	for i, opt := range pc.Options {
		marker := "  "
		st := t.Style(t.Text)
		if i == pc.Selected {
			marker = "❯ "
			st = t.Style(t.Primary).WithBold(true)
		}
		kindTxt := "(" + string(opt.Kind) + ")"
		name := renderer.Truncate(opt.Name, maxInt(0, r.W-renderer.StringWidth(marker)-renderer.StringWidth(kindTxt)-2))
		c.PutText(r.X, y, marker+name, st)
		kindX := r.X + renderer.StringWidth(marker+name) + 1
		if kindX < r.X+r.W {
			c.PutText(kindX, y, kindTxt, t.Style(t.TextMuted))
		}
		y++
		if y >= r.Y+r.H {
			break
		}
	}
	if y < r.Y+r.H {
		y++
	}
	if y < r.Y+r.H {
		c.PutText(r.X, y, "↑↓ 选择    Enter 确认    a/r 快捷    Esc 取消", t.Style(t.TextMuted))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// drawHelp 绘制帮助弹窗。
func (v *AppView) drawHelp(c *renderer.Canvas, r renderer.Rect) {
	t := v.Theme
	lines := []string{
		"alcoh 快捷键",
		"",
		"Enter          提交输入        Shift+Enter / 行尾 \\ + Enter 换行",
		"/              命令面板        Ctrl+,         打开设置",
		"/effort        推理强度滑条    /clear         清除会话(on 不取消)",
		"↑↓             移动 / 历史     ←→            移动光标",
		"Ctrl+A/E       行首 / 行尾     Ctrl+K/U      删至行尾/行首",
		"Ctrl+W         删前一词        Ctrl+Y        粘贴",
		"Ctrl+/         撤销            Ctrl+Q        退出确认",
		"Ctrl+C         清空输入 / 取消任务 / 连按两次退出",
		"PageUp/Down    滚动消息        Home/End      顶部/底部",
		"鼠标滚轮       滚动消息        Shift+滚轮    半页滚动",
		"鼠标拖拽       框选文本        选中后 Ctrl+C 复制",
		"Tab            切换焦点        Enter         展开/折叠",
		"?              帮助            Esc           关闭",
		"Enter          恢复会话(空输入) d            删除选中会话（首页）",
		"",
		"权限弹窗: ↑↓ 选择选项, a=allow, r=reject, Enter 确认, Esc 取消",
		"       多个权限按到达顺序排队，逐条弹出；Esc 视为取消并处理下一条。",
		"",
		"ACP 状态: 状态栏展示当前 model/agent 元信息与 stop reason；",
		"       未知 session update 会作为一行系统提示写入正文（原始 JSON 保留在协议诊断中）。",
	}
	// 计算高度
	contentH := len(lines)
	modal := &widget.Modal{
		Width:  72,
		Height: contentH + 2,
		Title:  "帮助",
		Style:  t.Style(t.Border),
		Content: &TextLines{
			Theme: t,
			Lines: lines,
			Bold:  []int{0},
		},
	}
	modal.Draw(c, r)
}

// drawConfirm 绘制退出确认弹窗。
func (v *AppView) drawConfirm(c *renderer.Canvas, r renderer.Rect) {
	t := v.Theme
	modal := &widget.Modal{
		Width:  40,
		Height: 5,
		Title:  "退出",
		Style:  t.Style(t.Error),
		Content: &TextLines{
			Theme: t,
			Lines: []string{"确定退出 alcoh 吗？", "", "  y 退出    n / Esc 取消"},
		},
	}
	modal.Draw(c, r)
}

// drawSettings 绘制本地客户端设置。ACP 更新保持只读，不会猜测写回 RPC。
func (v *AppView) drawSettings(c *renderer.Canvas, r renderer.Rect, m *model.AppModel) {
	t := v.Theme
	modal := &widget.Modal{
		Width:  68,
		Height: 10,
		Title:  "设置（本地）",
		Style:  t.Style(t.BorderActive),
		Content: &SettingsContent{
			Theme:    t,
			Values:   m.Settings,
			Selected: m.SettingsSelected,
			ACPCount: protocolUpdateCount(m),
		},
	}
	modal.Draw(c, r)
}

// SettingsContent 是可键盘操作的本地设置列表。
type SettingsContent struct {
	Theme    renderer.Theme
	Values   config.Values
	Selected int
	ACPCount int
}

func (sc *SettingsContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	rows := []struct{ label, value string }{
		{"色彩模式", sc.Values.ColorMode},
		{"展开思考内容", onOff(sc.Values.ThinkingExpanded)},
		{"默认展开工具", onOff(sc.Values.ToolsExpanded)},
	}
	for i, row := range rows {
		if r.Y+i >= r.Y+r.H {
			break
		}
		marker := "  "
		style := sc.Theme.Style(sc.Theme.Text)
		if i == sc.Selected {
			marker = "❯ "
			style = sc.Theme.Style(sc.Theme.Primary).WithBold(true)
		}
		c.PutText(r.X, r.Y+i, marker+row.label, style)
		c.PutText(r.X+30, r.Y+i, renderer.Truncate(row.value, r.W-30), sc.Theme.Style(sc.Theme.TextMuted))
	}
	y := r.Y + len(rows) + 1
	if y < r.Y+r.H {
		c.PutText(r.X, y, "←→ 切换色彩模式    Enter 切换开关    Esc 关闭", sc.Theme.Style(sc.Theme.TextMuted))
	}
	y++
	if y < r.Y+r.H {
		c.PutText(r.X, y, "ACP 配置更新: "+strconv.Itoa(sc.ACPCount)+" 条（只读；未声明写回 RPC）", sc.Theme.Style(sc.Theme.TextMuted))
	}
}

func onOff(enabled bool) string {
	if enabled {
		return "开启"
	}
	return "关闭"
}

// EffortContent 是推理强度水平滑条内容区。
type EffortContent struct {
	Theme    renderer.Theme
	Levels   []string
	Current  string // 服务端当前值（未在候选内时仍展示在"当前"行）
	Selected int
}

// effortColor 返回推理强度等级对应的提示色。
// unset/未知值使用灰色；low→绿、medium→黄、high→橙、xhigh→红、max→蓝。
// 滑条选中项与输入框上横线右上角的 effort 提示共用该映射。
func effortColor(t renderer.Theme, level string) renderer.Color {
	switch level {
	case "low":
		return t.Success
	case "medium":
		return t.Warning
	case "high":
		return renderer.RGB(0xF5, 0x9C, 0x3D) // 橙色（主题未提供专门槽位）
	case "xhigh":
		return t.Error
	case "max":
		return t.Info
	default:
		return t.TextMuted
	}
}

// Draw 绘制水平滑条：候选值横向排布，选中值以 [值] 高亮作为滑块。
func (ec *EffortContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	t := ec.Theme
	y := r.Y
	if y < r.Y+r.H {
		c.PutText(r.X, y, "当前: "+renderer.Truncate(ec.Current, r.W), t.Style(t.TextMuted))
		y++
	}
	y++
	// 水平滑条：拼出 "值 值 [值] 值" 一行，选中项两侧留空隙便于辨识滑块。
	var sb strings.Builder
	for i, level := range ec.Levels {
		if i == ec.Selected {
			sb.WriteString("[")
			sb.WriteString(level)
			sb.WriteString("]")
		} else {
			sb.WriteString(level)
		}
		if i < len(ec.Levels)-1 {
			sb.WriteString("  ")
		}
	}
	if y < r.Y+r.H {
		line := renderer.Truncate(sb.String(), r.W)
		// 选中滑块在整行中保持主色加粗。
		// 选中滑块按等级着色（unset 灰色 … max 蓝色），其余保持正文色。
		level := ""
		if ec.Selected >= 0 && ec.Selected < len(ec.Levels) {
			level = ec.Levels[ec.Selected]
		}
		if pos := strings.Index(line, "["); pos >= 0 {
			if end := strings.Index(line[pos+1:], "]"); end >= 0 {
				sel := line[pos : pos+end+2]
				c.PutText(r.X, y, line[:pos], t.Style(t.Text))
				c.PutText(r.X+pos, y, sel, t.Style(effortColor(t, level)).WithBold(true))
				rest := line[pos+end+2:]
				c.PutText(r.X+pos+len(sel), y, rest, t.Style(t.Text))
			} else {
				c.PutText(r.X, y, line, t.Style(t.Text))
			}
		} else {
			c.PutText(r.X, y, line, t.Style(t.Text))
		}
		y++
	}
	y++
	if y < r.Y+r.H {
		c.PutText(r.X, y, "←→ 移动    Enter 确认    Esc 取消", t.Style(t.TextMuted))
	}
}

// effortContent 构造推理强度弹窗内容。值始终使用客户端硬编码候选，
// 服务端仅决定命令是否可用（公布 thought_level 配置）。
func effortContent(t renderer.Theme, m *model.AppModel) *EffortContent {
	return &EffortContent{
		Theme:    t,
		Levels:   append([]string(nil), model.EffortLevels()...),
		Current:  m.CurrentEffort(),
		Selected: m.EffortSelect,
	}
}

// ModelContent 是模型选择垂直列表内容区。
type ModelContent struct {
	Theme    renderer.Theme
	Current  string // 服务端当前值（可能不在候选内）
	Options  []acp.ConfigOptionValue
	Selected int
}

// Draw 绘制垂直模型列表：顶部"当前"行，下方候选列表（❯ 标记选中项），
// 底部操作提示。选项过多时以选中项为中心滚动窗口。
func (mc *ModelContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	t := mc.Theme
	y := r.Y
	if y < r.Y+r.H {
		c.PutText(r.X, y, "当前: "+renderer.Truncate(mc.Current, r.W), t.Style(t.TextMuted))
		y++
	}
	y++

	// 可用列表高度：当前行之后为选项区域，底部至少保留一行操作提示。
	available := r.H - (y - r.Y) - 1 // 保留底部提示行
	if available < 1 {
		available = 1
	}
	if available > len(mc.Options) {
		available = len(mc.Options)
	}
	// 选中项保持可见：先尝试居中，越界时收拢到边界。
	start := mc.Selected - available/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(mc.Options) - available; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < start+available; i++ {
		if y >= r.Y+r.H {
			break
		}
		opt := mc.Options[i]
		marker := "  "
		st := t.Style(t.Text)
		if i == mc.Selected {
			marker = "❯ "
			st = t.Style(t.Primary).WithBold(true)
		}
		name := opt.Name
		if name == "" {
			name = opt.Value
		}
		line := marker + name
		if opt.Description != "" {
			line += "  — " + opt.Description
		}
		c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
		y++
	}

	y = r.Y + r.H - 1
	if y >= r.Y {
		c.PutText(r.X, y, "↑↓ 选择    Enter 确认    Esc 取消", t.Style(t.TextMuted))
	}
}

// modelContent 构造模型选择弹窗内容。候选值与当前值均来自服务端公布的
// model 配置项（category="model"）；服务端决定 /model 命令是否可用。
func modelContent(t renderer.Theme, m *model.AppModel) *ModelContent {
	var options []acp.ConfigOptionValue
	if opt := m.ActiveConfigOption("model"); opt != nil {
		options = opt.Options
	}
	return &ModelContent{
		Theme:    t,
		Current:  m.CurrentModel(),
		Options:  options,
		Selected: m.ModelSelect,
	}
}

func protocolUpdateCount(m *model.AppModel) int {
	if m.Active == nil {
		return 0
	}
	return len(m.Active.ProtocolUpdates)
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// permissionSubjectText 把结构化 PermissionSubject 渲染为权限弹窗的单行描述。
// 优先展示 tool call 的标题与工具 ID；无法展开时退回 subject type。
func permissionSubjectText(subject *acp.PermissionSubject) string {
	if subject == nil {
		return ""
	}
	if subject.ToolCall != nil {
		title := strings.TrimSpace(subject.ToolCall.Title)
		if title == "" {
			title = subject.ToolCall.ToolCallID
		}
		text := ""
		for _, block := range subject.ToolCall.Content {
			if block.Text != nil && *block.Text != "" {
				text = strings.TrimSpace(*block.Text)
				break
			}
		}
		if text != "" {
			return title + ": " + text
		}
		return title
	}
	return subject.Type
}

func wrapText(text string, width int) []string {
	if width < 1 {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		for renderer.StringWidth(paragraph) > width {
			cut := 0
			used := 0
			for i, r := range paragraph {
				w := renderer.RuneWidth(r)
				if used+w > width {
					break
				}
				used += w
				cut = i + len(string(r))
			}
			if cut == 0 {
				break
			}
			lines = append(lines, paragraph[:cut])
			paragraph = paragraph[cut:]
		}
		lines = append(lines, paragraph)
	}
	return lines
}

// TextLines 是简单的多行文本内容（供模态使用）。
type TextLines struct {
	Theme renderer.Theme
	Lines []string
	Bold  []int
}

// Draw 绘制文本行。
func (tl *TextLines) Draw(c *renderer.Canvas, r renderer.Rect) {
	y := r.Y
	for i, ln := range tl.Lines {
		if y >= r.Y+r.H {
			break
		}
		st := tl.Theme.Style(tl.Theme.Text)
		if contains(tl.Bold, i) {
			st = tl.Theme.Style(tl.Theme.Primary).WithBold(true)
		}
		if ln == "" {
			y++
			continue
		}
		c.PutText(r.X, y, renderer.Truncate(ln, r.W), st)
		y++
	}
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
