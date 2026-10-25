package view

import (
	"slices"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/i18n"
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
		c.PutText(r.X, y, renderer.Truncate(i18n.T("对象: %s", pc.Subject), r.W), t.Style(t.Primary).WithBold(true))
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
		c.PutText(r.X, y, i18n.T("↑↓ 选择    Enter 确认    a/r 快捷    Esc 取消"), t.Style(t.TextMuted))
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
		i18n.T("alcoh 快捷键"),
		"",
		i18n.T("Enter          提交输入        Shift+Enter / 行尾 \\ + Enter 换行"),
		i18n.T("/              命令面板        Ctrl+,         打开设置"),
		i18n.T("/connect       连接模型服务商（模板/填 key/拉取模型）"),
		i18n.T("/effort        推理强度滑条    /clear         清除会话(on 不取消)"),
		i18n.T("↑↓             移动 / 历史     ←→            移动光标"),
		i18n.T("Ctrl+A/E       行首 / 行尾     Ctrl+K/U      删至行尾/行首"),
		i18n.T("Ctrl+W         删前一词        Ctrl+Y        粘贴"),
		i18n.T("Ctrl+/         撤销            Ctrl+Q        退出确认"),
		i18n.T("Ctrl+C         清空输入 / 取消任务 / 连按两次退出"),
		i18n.T("Esc            打断响应        空闲连按两次  打开撤回"),
		i18n.T("Ctrl+O         展开/折叠全部思考/工具"),
		i18n.T("PageUp/Down    滚动消息        Home/End      顶部/底部"),
		i18n.T("鼠标滚轮       滚动消息        Shift+滚轮    半页滚动"),
		i18n.T("鼠标拖拽       框选文本        选中后 Ctrl+C 复制"),
		i18n.T("鼠标点击       思考/工具标题    展开/折叠单项"),
		i18n.T("Tab            切换焦点        Enter         展开/折叠"),
		i18n.T("?              帮助            Esc           关闭"),
		i18n.T("Enter          恢复会话(空输入) d            删除选中会话（首页）"),
		"",
		i18n.T("权限弹窗: ↑↓ 选择选项, a=allow, r=reject, Enter 确认, Esc 取消"),
		i18n.T("       多个权限按到达顺序排队，逐条弹出；Esc 视为取消并处理下一条。"),
		"",
		i18n.T("ACP 状态: 状态栏展示当前 model/agent 元信息与 stop reason；"),
		i18n.T("       未知 session update 与 calling_info 参数块是协议细节，默认不在正文显示"),
		i18n.T("       （原始 JSON 仍保留在协议诊断中）；设置里打开“显示协议细节”可查看。"),
	}
	// 计算高度
	contentH := len(lines)
	modal := &widget.Modal{
		Width:  72,
		Height: contentH + 2,
		Title:  i18n.T("帮助"),
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
		Title:  i18n.T("退出"),
		Style:  t.Style(t.Error),
		Content: &TextLines{
			Theme: t,
			Lines: []string{i18n.T("确定退出 alcoh 吗？"), "", i18n.T("  y 退出    n / Esc 取消")},
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
		Title:  i18n.T("设置（本地）"),
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
		{i18n.T("色彩模式"), sc.Values.ColorMode},
		{i18n.T("展开思考内容"), onOff(sc.Values.ThinkingExpanded)},
		{i18n.T("默认展开工具"), onOff(sc.Values.ToolsExpanded)},
		{i18n.T("语言"), sc.Values.Language},
		{i18n.T("显示协议细节"), onOff(sc.Values.ShowProtocolDetails)},
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
		c.PutText(r.X, y, i18n.T("←→ 切换色彩模式 / 语言    Enter 切换开关    Esc 关闭"), sc.Theme.Style(sc.Theme.TextMuted))
	}
	y++
	if y < r.Y+r.H {
		c.PutText(r.X, y, i18n.T("ACP 配置更新: %d 条（只读；未声明写回 RPC）", sc.ACPCount), sc.Theme.Style(sc.Theme.TextMuted))
	}
}

func onOff(enabled bool) string {
	if enabled {
		return i18n.T("开启")
	}
	return i18n.T("关闭")
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
		c.PutText(r.X, y, i18n.T("当前: %s", renderer.Truncate(ec.Current, r.W)), t.Style(t.TextMuted))
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
		c.PutText(r.X, y, i18n.T("←→ 移动    Enter 确认    Esc 取消"), t.Style(t.TextMuted))
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
		c.PutText(r.X, y, i18n.T("当前: %s", renderer.Truncate(mc.Current, r.W)), t.Style(t.TextMuted))
		y++
	}
	y++

	// 可用列表高度：当前行之后为选项区域，底部至少保留一行操作提示。
	available := max(
		// 保留底部提示行
		r.H-(y-r.Y)-1, 1)
	if available > len(mc.Options) {
		available = len(mc.Options)
	}
	// 选中项保持可见：先尝试居中，越界时收拢到边界。
	start := max(mc.Selected-available/2, 0)
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
		c.PutText(r.X, y, i18n.T("↑↓ 选择    Enter 确认    Esc 取消"), t.Style(t.TextMuted))
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

// RewindContent 是 /rewind 对话选择器的内容区：列出可撤回的用户消息。
type RewindContent struct {
	Theme    renderer.Theme
	Targets  []model.RewindTarget
	Selected int
}

// rewindItemRows 是选择器里每条对话占用的行数：用户内容行、AI 回复行，以及
// 一条分隔空行。
const rewindItemRows = 3

// Draw 绘制撤回目标列表：顶部语义提示，下方每条对话占三行——第一行是用户内容
// （❯ 标记选中项），第二行是灰色 AI 回复，第三行留空；底部是操作提示。
// 序号自下而上：列表底部（最近一条）是 1 号，往上递增。目标过多时以选中项
// 为中心滚动窗口。
func (rc *RewindContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	t := rc.Theme
	y := r.Y
	if y < r.Y+r.H {
		c.PutText(r.X, y, renderer.Truncate(i18n.T("选择要撤回的消息：该消息及其之后的内容会从对话中移除"), r.W), t.Style(t.TextMuted))
		y++
	}
	y++

	// 可用条目数：提示行之后为列表区域，底部至少保留一行操作提示。
	available := max((r.H-(y-r.Y)-1)/rewindItemRows, 1)
	if available > len(rc.Targets) {
		available = len(rc.Targets)
	}
	start := max(rc.Selected-available/2, 0)
	if maxStart := len(rc.Targets) - available; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < start+available; i++ {
		if y >= r.Y+r.H {
			break
		}
		target := rc.Targets[i]
		marker := "  "
		st := t.Style(t.Text)
		if i == rc.Selected {
			marker = "❯ "
			st = t.Style(t.Primary).WithBold(true)
		}
		// 序号自下而上：最下面（最近一条）是 1 号，越往上数字越大。
		line := marker + i18n.T("%d. %s", len(rc.Targets)-i, rewindLine(target.Text))
		c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
		y++
		drawRewindReply(c, r, y, t, marker, target)
		y += rewindItemRows - 1
	}

	y = r.Y + r.H - 1
	if y >= r.Y {
		c.PutText(r.X, y, i18n.T("↑↓ 选择    Enter 确认    Esc 取消"), t.Style(t.TextMuted))
	}
}

// drawRewindReply 绘制一条对话的第二行：灰色的 AI 回复（对齐到第一行的内容列）。
// 该轮没有正文回复时这一行留空。
func drawRewindReply(c *renderer.Canvas, r renderer.Rect, y int, t renderer.Theme, marker string, target model.RewindTarget) {
	if y >= r.Y+r.H {
		return
	}
	indent := renderer.StringWidth(marker)
	if reply := renderer.Truncate(rewindLine(target.Reply), r.W-indent); reply != "" {
		c.PutText(r.X+indent, y, reply, t.Style(t.TextMuted))
	}
}

// rewindLine 把消息原文压成一行（取第一个非空行，去制表符与首尾空白），
// 供选择器列表展示。
func rewindLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\t", " "))
		if line != "" {
			return line
		}
	}
	return ""
}

func rewindContent(t renderer.Theme, m *model.AppModel) *RewindContent {
	return &RewindContent{
		Theme:    t,
		Targets:  m.RewindTargets(),
		Selected: m.RewindSelect,
	}
}

// QueueContent 是 /queue 会话消息队列面板的内容区：列出服务端队列的全量
// 快照（FIFO）。队列项尚未进入正文，出队后才会以 user_message 显示。
type QueueContent struct {
	Theme    renderer.Theme
	Items    []acp.MsgQueueItem
	Selected int
}

// queueItemRows 是队列面板里每一项占用的行数：每项一行，过长文本截断。
const queueItemRows = 1

// Draw 绘制排队消息列表：顶部提示，随后每项一行（❯ 标记选中项），底部是
// 操作提示。条目过多时以选中项为中心滚动窗口。
func (qc *QueueContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	t := qc.Theme
	y := r.Y
	if y < r.Y+r.H {
		hint := i18n.T("排队消息 %d 条：出队后才会发送", len(qc.Items))
		c.PutText(r.X, y, renderer.Truncate(hint, r.W), t.Style(t.TextMuted))
		y++
	}
	y++ // 空行

	available := max(r.H-(y-r.Y)-1, 1)
	start := max(qc.Selected-available/2, 0)
	if maxStart := len(qc.Items) - available; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	if len(qc.Items) == 0 {
		c.PutText(r.X, y, i18n.T("（队列为空）"), t.Style(t.TextMuted))
	} else {
		for i := start; i < start+available && i < len(qc.Items); i++ {
			if y >= r.Y+r.H-1 {
				break
			}
			item := qc.Items[i]
			marker := "  "
			st := t.Style(t.Text)
			if i == qc.Selected {
				marker = "❯ "
				st = t.Style(t.Primary).WithBold(true)
			}
			line := marker + itoa(i+1) + ". " + queueItemLine(item)
			c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
			y += queueItemRows
		}
	}

	y = r.Y + r.H - 1
	if y >= r.Y {
		c.PutText(r.X, y, i18n.T("↑↓ 选择    e 编辑    d 取消    c 清空    Esc 关闭"), t.Style(t.TextMuted))
	}
}

// queueItemLine 把排队项压成一行展示：prompt 直接显示文本，command 显示命令
// 与参数。多行文本只取第一个非空行。
func queueItemLine(item acp.MsgQueueItem) string {
	text := rewindLine(item.Text)
	if item.Kind == model.MsgQueueKindCommand {
		if text == "" && item.Command != "" {
			text = strings.TrimSpace(item.Command + " " + item.Args)
		}
		if text == "" {
			text = item.Command
		}
		return i18n.T("[命令] %s", text)
	}
	return i18n.T("[消息] %s", text)
}

// queueContent 构造 /queue 面板内容。
func queueContent(t renderer.Theme, m *model.AppModel) *QueueContent {
	return &QueueContent{
		Theme:    t,
		Items:    m.QueueItems(),
		Selected: m.QueueSelect,
	}
}

// CronContent 是 /cron 定时任务面板的内容区：列出服务端为当前会话保存的
// 定时任务（由 AI 的 cron 工具创建）。每条任务占 cronItemRows 行：表达式与
// 类型、提示词、下次触发时间。
type CronContent struct {
	Theme    renderer.Theme
	Jobs     []acp.CronJob
	Selected int
}

// cronItemRows 是每条定时任务占用的行数。
const cronItemRows = 3

// Draw 绘制定时任务列表：顶部提示，随后每条任务占三行（❯ 标记选中项），
// 底部是操作提示。任务过多时以选中项为中心滚动窗口。
func (cc *CronContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	t := cc.Theme
	y := r.Y
	if y < r.Y+r.H {
		hint := i18n.T("定时任务 %d 条：由 AI 通过 cron 工具创建", len(cc.Jobs))
		c.PutText(r.X, y, renderer.Truncate(hint, r.W), t.Style(t.TextMuted))
		y++
	}
	y++ // 空行

	available := max((r.H-(y-r.Y)-1)/cronItemRows, 1)
	start := max(cc.Selected-available/2, 0)
	if maxStart := len(cc.Jobs) - available; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	if len(cc.Jobs) == 0 {
		c.PutText(r.X, y, i18n.T("（暂无定时任务）"), t.Style(t.TextMuted))
	} else {
		for i := start; i < start+available && i < len(cc.Jobs); i++ {
			if y+cronItemRows > r.Y+r.H-1 {
				break
			}
			job := cc.Jobs[i]
			marker := "  "
			st := t.Style(t.Text)
			if i == cc.Selected {
				marker = "❯ "
				st = t.Style(t.Primary).WithBold(true)
			}
			kind := i18n.T("周期")
			if job.RunOnce {
				kind = i18n.T("一次")
			}
			line := marker + i18n.T("#%d  %s  [%s]", job.ID, job.Cron, kind)
			c.PutText(r.X, y, renderer.Truncate(line, r.W), st)
			y++

			prompt := rewindLine(job.Prompt)
			if prompt == "" {
				prompt = i18n.T("（无提示词）")
			}
			c.PutText(r.X+2, y, renderer.Truncate(prompt, max(r.W-2, 0)), t.Style(t.Text))
			y++

			next := i18n.T("（无法计算下次触发时间）")
			if len(job.NextRuns) > 0 && job.NextRuns[0] != "" {
				next = i18n.T("下次: %s", job.NextRuns[0])
			}
			c.PutText(r.X+2, y, renderer.Truncate(next, max(r.W-2, 0)), t.Style(t.TextMuted))
			y++
		}
	}

	y = r.Y + r.H - 1
	if y >= r.Y {
		c.PutText(r.X, y, i18n.T("↑↓ 选择    d 取消定时任务    r 刷新    Esc 关闭"), t.Style(t.TextMuted))
	}
}

// cronContent 构造 /cron 面板内容。
func cronContent(t renderer.Theme, m *model.AppModel) *CronContent {
	return &CronContent{
		Theme:    t,
		Jobs:     m.CronJobs(),
		Selected: m.CronSelect,
	}
}

// TasksContent 是 /tasks AI 任务列表的滚动内容区。任务来自 alkaid0 的 task
// 工具（服务端把 @task 解析成 plan entries 后经 plan_update 全量推送）。每条任务
// 占一行：完成绿色、进行中蓝色、未完成紫色（取消红色）；内容超过一屏时右侧
// 绘制滚动条。
type TasksContent struct {
	Theme   renderer.Theme
	Entries []acp.PlanEntry
	Scroll  int
	// Rows 非空时写回本次绘制可用的条目行数（供 PgUp/PgDn 翻页）。
	Rows *int
}

// Draw 绘制任务列表：顶部摘要、空行、任务行，底部一行操作提示。
func (tc *TasksContent) Draw(c *renderer.Canvas, r renderer.Rect) {
	if r.H <= 0 {
		return
	}
	t := tc.Theme
	done, inProgress := 0, 0
	for _, e := range tc.Entries {
		switch e.Status {
		case acp.PlanCompleted:
			done++
		case acp.PlanInProgress:
			inProgress++
		}
	}

	y := r.Y
	if len(tc.Entries) == 0 {
		c.PutText(r.X, y, renderer.Truncate(i18n.T("本会话还没有 AI 任务列表"), r.W), t.Style(t.TextMuted))
	} else {
		summary := i18n.T("任务 %d/%d 完成", done, len(tc.Entries))
		if inProgress > 0 {
			summary += i18n.T("   ·  %d 进行中", inProgress)
		}
		c.PutText(r.X, y, renderer.Truncate(summary, r.W), t.Style(t.TextMuted))
	}
	y++ // 摘要行
	y++ // 空行

	// 任务行窗口：底部留一行操作提示。
	footerY := r.Y + r.H - 1
	rows := max(footerY-y, 0)
	if tc.Rows != nil {
		*tc.Rows = rows
	}
	start := min(max(tc.Scroll, 0), max(len(tc.Entries)-rows, 0))

	if len(tc.Entries) == 0 {
		if y < footerY {
			c.PutText(r.X, y, renderer.Truncate(i18n.T("AI 通过 task 工具维护任务（@task），任务生成后自动推送到这里"), r.W), t.Style(t.TextMuted))
		}
	} else {
		showBar := len(tc.Entries) > rows && r.W > 1
		textW := r.W
		if showBar {
			textW--
		}
		for i := 0; i < rows && start+i < len(tc.Entries); i++ {
			e := tc.Entries[start+i]
			sym, st := tasksStatusStyle(t, e.Status)
			c.PutText(r.X, y+i, renderer.Truncate(" "+sym+" "+e.Content, textW), st)
		}
		if showBar {
			bar := widget.Scrollbar{
				Total: len(tc.Entries),
				View:  rows,
				Top:   start,
				Track: t.Style(t.BorderSubtle),
				Thumb: t.Style(t.Primary),
			}
			bar.Draw(c, renderer.NewRect(r.X+r.W-1, y, 1, rows))
		}
	}

	if footerY >= r.Y {
		c.PutText(r.X, footerY, renderer.Truncate(i18n.T("↑↓ 滚动    PgUp/PgDn 翻页    Home/End 首尾    Esc 关闭"), r.W), t.Style(t.TextMuted))
	}
}

// tasksStatusStyle 返回任务状态的符号与颜色：完成绿、进行中蓝、未完成紫、
// 取消红。
func tasksStatusStyle(t renderer.Theme, status acp.PlanEntryStatus) (string, renderer.Style) {
	switch status {
	case acp.PlanCompleted:
		return "✓", t.Style(t.ToolDone)
	case acp.PlanInProgress:
		return "●", t.Style(t.ToolRunning)
	case acp.PlanCancelled:
		return "✗", t.Style(t.ToolFailed)
	default:
		return "○", t.Style(t.Secondary)
	}
}

// tasksContent 构造 /tasks 面板内容。
func tasksContent(t renderer.Theme, m *model.AppModel) *TasksContent {
	return &TasksContent{
		Theme:   t,
		Entries: m.TaskEntries(),
		Scroll:  m.TasksScroll,
		Rows:    &m.TasksRows,
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
	for paragraph := range strings.SplitSeq(text, "\n") {
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
	return slices.Contains(xs, v)
}
