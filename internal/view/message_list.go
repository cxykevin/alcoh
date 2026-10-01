package view

import (
	"encoding/binary"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func SpinFrame(n int) string { return spinnerFrames[n%len(spinnerFrames)] }

// ToggleKind 是正文块顶行可点击目标的类型。
type ToggleKind int

const (
	// ToggleNone 表示该行不可点击。
	ToggleNone ToggleKind = iota
	// ToggleThought 表示思考消息：点击标题行展开/折叠。
	ToggleThought
	// ToggleTool 表示工具调用：点击标题行展开/折叠。
	ToggleTool
)

// ToggleRef 描述正文某行被鼠标点击时应切换展开/折叠的目标。
type ToggleRef struct {
	Kind ToggleKind
	ID   string
}

type block struct {
	lines    [][]Span
	raw      string     // 块级原始内容（工具/终端/思考等，复制用，无渲染前缀）
	srcLines []SrcLine  // 行级插针：每渲染行对应的原始逻辑行文本（消息块）
	toggle   *ToggleRef // 非 nil 时顶行可点击切换展开/折叠（思考/工具）
}

// SrcLine 是行级插针的一行：Text 为该渲染行对应的原始 markdown 逻辑行，
// First 表示该渲染行是否为该原始逻辑行的首个渲染行（长行 wrap 拆分时，
// 后续渲染行 First=false，复制时拼回同一行，避免整行重复输出）。
// Map 按渲染行的单元格列（含渲染前缀）给出该列对应的 Text rune 下标；
// -1 表示该列不是来自原文（渲染前缀、列表符号、宽字符续列等）。部分框选
// 复制时据此只取选中的字符，未渲染的标记不会被带出来。
type SrcLine struct {
	Text  string
	First bool
	Map   []int
}

// BodyBlock 描述正文中一个可复制条目：Raw 为块级原始文本，Src 为行级插针，
// Start/End 为该条目在消息区渲染行序列（contentY，0-based，不随滚动变化）中的闭区间。
type BodyBlock struct {
	Raw        string
	Src        []SrcLine
	Start, End int
}

type MessageList struct {
	Theme     renderer.Theme
	SpinFrame int
	Scroll    int         // 最近一次 Draw 实际使用的滚动偏移
	Body      []BodyBlock // 最近一次 Draw 构建的正文块目录
	// Toggles 记录可点击切换展开/折叠的正文行（contentY → 目标，仅标题行），
	// 由 Draw 每次重建，供鼠标点击命中测试使用。
	Toggles map[int]ToggleRef
	// Rows 是最近一次 Draw 的内容总行数（contentY 上界，与滚动无关），
	// 供拖拽自动滚动的边界判断使用。
	Rows int

	// cache 按 Timeline 条目 Key 缓存上一帧的渲染结果：条目内容没变时直接复用，
	// 避免每帧对整段会话重做 markdown 解析 / 语法高亮 / 换行。长会话流式输出时
	// 帧耗时本来与会话长度成正比（几百条消息可达数百毫秒），一旦超过动画帧间隔，
	// 事件循环被绘制占满，按键长时间得不到处理，表现就是"卡死"。
	cache      map[string]blockCacheEntry
	cacheWidth int
	cacheTheme renderer.Theme
	cacheReady bool
}

// blockCacheEntry 是单个 Timeline 条目的渲染缓存：sig 变化即重绘该条目。
type blockCacheEntry struct {
	sig uint64
	blk *block
}

// ResetBodyCache 丢弃正文渲染缓存（切换会话或主题变化时调用）。
func (ml *MessageList) ResetBodyCache() {
	ml.cache = nil
	ml.cacheReady = false
}

// prepareBodyCache 在换行宽度或配色变化时整表失效并重建缓存表。
func (ml *MessageList) prepareBodyCache(width int) {
	if !ml.cacheReady || ml.cacheWidth != width || ml.cacheTheme != ml.Theme {
		ml.cache = make(map[string]blockCacheEntry, 64)
		ml.cacheWidth = width
		ml.cacheTheme = ml.Theme
		ml.cacheReady = true
	}
}

// cached 命中缓存时返回上次的块，否则调用 build 重建并写回缓存。
func (ml *MessageList) cached(key string, sig uint64, build func() *block) *block {
	if ml.cache != nil && key != "" {
		if e, ok := ml.cache[key]; ok && e.sig == sig {
			return e.blk
		}
	}
	blk := build()
	if ml.cache != nil && key != "" {
		ml.cache[key] = blockCacheEntry{sig: sig, blk: blk}
	}
	return blk
}

func (ml *MessageList) Draw(c *renderer.Canvas, r renderer.Rect, s *model.SessionState) {
	width := r.W - 1
	if width <= 0 {
		return
	}
	ml.prepareBodyCache(width)
	blocks := ml.buildBlocks(s, width)
	total := 0
	for _, blk := range blocks {
		total += len(blk.lines)
	}
	ml.Rows = total
	viewH := r.H
	maxScroll := max(total-viewH, 0)
	scroll := s.Scroll
	if s.FollowBottom {
		scroll = maxScroll
	} else if scroll >= maxScroll {
		// 手动滚动到（或超过）底部即视为重新贴底，后续新消息自动跟随。
		scroll = maxScroll
		s.FollowBottom = true
	}
	if scroll < 0 {
		scroll = 0
	}
	// 回写同步：渲染后 Scroll 即当前实际偏移。贴底状态下把它固定在
	// 当前最大滚动偏移，这样 ScrollUp 解除贴底时能直接从底部继续滚动。
	s.Scroll = scroll
	ml.Scroll = scroll
	ml.Toggles = map[int]ToggleRef{}
	ml.Body = ml.bodyBlocks(blocks)
	contentY := 0
	for _, blk := range blocks {
		if contentY >= scroll+viewH {
			break
		}
		for _, line := range blk.lines {
			if contentY >= scroll+viewH {
				break
			}
			if contentY >= scroll {
				ml.drawLine(c, r.X, r.Y+contentY-scroll, width, line)
			}
			contentY++
		}
	}
	(&widget.Scrollbar{Total: total, View: viewH, Top: scroll, Track: ml.Theme.Style(ml.Theme.BorderSubtle), Thumb: ml.Theme.Style(ml.Theme.Border)}).Draw(c, renderer.NewRect(r.X+r.W-1, r.Y, 1, viewH))
}

func (ml *MessageList) drawLine(c *renderer.Canvas, x, y, maxW int, line []Span) {
	for _, sp := range line {
		c.PutText(x, y, sp.Text, sp.Style)
		x += renderer.StringWidth(sp.Text)
		if x > maxW {
			break
		}
	}
}

// buildBlocks 仅按 Timeline 的首次出现顺序构建正文。工具、计划和终端不再被
// 统一追加在所有消息之后。
func (ml *MessageList) buildBlocks(s *model.SessionState, width int) []*block {
	blocks := make([]*block, 0, len(s.Timeline))
	for _, item := range s.Timeline {
		switch item.Kind {
		case model.TimelineUserMessage, model.TimelineAssistantMessage:
			if item.Message != nil {
				m := item.Message
				blocks = append(blocks, ml.cached(item.Key, ml.messageSig(m, width), func() *block {
					return ml.messageBlock(m, width)
				}))
			}
		case model.TimelineThought:
			if item.Message != nil {
				m := item.Message
				blocks = append(blocks, ml.cached(item.Key, ml.thoughtSig(m, width), func() *block {
					return ml.thoughtBlock(m, width)
				}))
			}
		case model.TimelineToolCall:
			if item.ToolCall != nil {
				tc := item.ToolCall
				private := s.Alkaid0ToolCalls
				blocks = append(blocks, ml.cached(item.Key, ml.toolSig(tc, width, private), func() *block {
					return ml.toolBlock(tc, width, private)
				}))
			}
		case model.TimelinePlan:
			// 计划只由固定在输入框上方的 PlanPanel 绘制，不进入正文上下文。
			continue
		case model.TimelineTerminal:
			if item.Terminal != nil {
				ts := item.Terminal
				blocks = append(blocks, ml.cached(item.Key, ml.terminalSig(ts, width), func() *block {
					return ml.terminalBlock(ts, width)
				}))
			}
		case model.TimelineSystemNotice:
			if item.Notice != "" {
				notice := item.Notice
				blocks = append(blocks, ml.cached(item.Key, ml.noticeSig(notice, width), func() *block {
					return &block{lines: [][]Span{{{Text: notice, Style: ml.Theme.Style(ml.Theme.TextMuted)}}}, raw: notice}
				}))
			}
		}
	}
	return blocks
}

// 渲染签名：把影响某个条目渲染结果的字段喂给 FNV-1a，任何变化都会换一个值。
// 字符串按字节遍历、不复制（大段工具输出也不产生额外分配），只在宽度/配色
// 变化时才整表失效。签名只需保证"不变→同值"，哈希碰撞只影响缓存命中。
const (
	sigOffset64 = 14695981039346656037
	sigPrime64  = 1099511628211
)

type sigBuilder struct{ sum uint64 }

func newSig() *sigBuilder { return &sigBuilder{sum: sigOffset64} }

func (s *sigBuilder) bytes(bs []byte) *sigBuilder {
	for _, b := range bs {
		s.sum ^= uint64(b)
		s.sum *= sigPrime64
	}
	return s
}

func (s *sigBuilder) num(v int) *sigBuilder {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	return s.bytes(b[:])
}

func (s *sigBuilder) bool(v bool) *sigBuilder {
	if v {
		return s.num(1)
	}
	return s.num(0)
}

// str 先写长度再写内容，避免相邻字段拼接歧义。
func (s *sigBuilder) str(v string) *sigBuilder {
	s.num(len(v))
	for i := 0; i < len(v); i++ {
		s.sum ^= uint64(v[i])
		s.sum *= sigPrime64
	}
	return s
}

func (s *sigBuilder) ptrStr(v *string) *sigBuilder {
	if v == nil {
		return s.num(0)
	}
	s.num(1)
	return s.str(*v)
}

func (s *sigBuilder) sum64() uint64 { return s.sum }

// messageSig 覆盖 messageBlock 的输入：正文、用户/助手/思考类型、折叠态与宽度。
func (ml *MessageList) messageSig(m *model.Message, width int) uint64 {
	s := newSig()
	s.num(int(m.Kind)).str(m.Text).bool(m.Expanded).bool(m.Done).num(width)
	return s.sum64()
}

// thoughtSig 覆盖 thoughtBlock 的输入；未完成的思考标题带 spinner，随帧号变化。
func (ml *MessageList) thoughtSig(m *model.Message, width int) uint64 {
	s := newSig()
	s.num(int(m.Kind)).str(m.Text).bool(m.Expanded).bool(m.Done).num(width).num(len(m.Lines()))
	if !m.Done {
		s.num(ml.SpinFrame)
	}
	return s.sum64()
}

// toolSig 覆盖 toolBlock 的输入（含展开后的 raw 输入输出、内容块、位置与
// alkaid0 私有渲染开关）。
func (ml *MessageList) toolSig(tc *model.ToolCall, width int, alkaid0 bool) uint64 {
	s := newSig()
	s.str(tc.ID).str(tc.Title).str(string(tc.Kind)).str(string(tc.Status)).bool(tc.Expanded).bool(alkaid0)
	s.str(tc.RawInput).str(tc.RawOutput).num(width)
	for _, location := range tc.Locations {
		s.str(location.Path)
		if location.Line != nil {
			s.num(int(*location.Line))
		} else {
			s.num(-1)
		}
	}
	for _, ct := range tc.Content {
		s.str(ct.Type).str(string(ct.Args)).str(ct.Name)
		s.ptrStr(ct.Text)
		if ct.Content != nil {
			s.str(ct.Content.Type).ptrStr(ct.Content.Text).ptrStr(ct.Content.Name).
				ptrStr(ct.Content.MimeType).ptrStr(ct.Content.URI).ptrStr(ct.Content.Title).
				ptrStr(ct.Content.Data)
		}
	}
	if tc.Running() {
		s.num(ml.SpinFrame)
	}
	return s.sum64()
}

// terminalSig 覆盖 terminalBlock 的输入（标题/状态/展开态/内容）。
func (ml *MessageList) terminalSig(t *model.TerminalState, width int) uint64 {
	s := newSig()
	s.str(t.ID).str(t.Title).str(t.Status).str(t.Command).bool(t.Expanded).
		bool(t.Truncated).str(t.Transcript).num(width)
	return s.sum64()
}

// noticeSig 覆盖系统提示块：正文 + 宽度（配色由整表失效覆盖）。
func (ml *MessageList) noticeSig(notice string, width int) uint64 {
	s := newSig()
	s.str(notice).num(width)
	return s.sum64()
}

// bodyBlocks 把 blocks 展平为按渲染行（contentY）索引的正文块目录。
// 消息块带行级插针 Src；其余块保留块级 Raw。空内容跳过，但行号仍累计。
// 同时为思考/工具块记录顶行（标题行）的 ToggleRef，供鼠标点击切换。
func (ml *MessageList) bodyBlocks(blocks []*block) []BodyBlock {
	var out []BodyBlock
	row := 0
	for _, blk := range blocks {
		n := len(blk.lines)
		switch {
		case len(blk.srcLines) == n && n > 0:
			out = append(out, BodyBlock{Src: blk.srcLines, Start: row, End: row + n - 1})
		case blk.raw != "":
			out = append(out, BodyBlock{Raw: blk.raw, Start: row, End: row + n - 1})
		}
		if blk.toggle != nil {
			ml.Toggles[row] = *blk.toggle
		}
		row += n
	}
	return out
}

func (ml *MessageList) messageBlock(msg *model.Message, width int) *block {
	t := ml.Theme
	blk := &block{}
	if msg.Kind == model.MsgUser {
		srcLines := strings.Split(msg.Text, "\n")
		row := 0
		for li, ln := range srcLines {
			// 用户消息按原文换行渲染，渲染 rune 与原文逐字对应。
			idents := seqIdents(0, len([]rune(ln)))
			off := 0
			wrapped := renderer.Wrap(ln, width-4)
			for j, wl := range wrapped {
				prefix := "  "
				if row == 0 {
					prefix = "  ❯ "
				}
				n := min(len([]rune(wl)), len(idents)-off)
				style := t.Style(t.Primary).WithBold(true)
				blk.lines = append(blk.lines, []Span{{Text: prefix + wl, Style: style}})
				blk.srcLines = append(blk.srcLines, SrcLine{
					Text:  strings.TrimRight(srcLines[li], " \t"),
					First: j == 0,
					Map:   cellMap(renderer.StringWidth(prefix), []Span{{Text: wl, Style: style}}, idents[off:off+n]),
				})
				off += n
				row++
			}
		}
	} else {
		for _, sl := range Markdown(msg.Text, t) {
			for j, wl := range wrapLine(sl, width-4) {
				blk.lines = append(blk.lines, prependSpan(wl.Spans, "  ", t.Style(t.Text)))
				// 插针：该渲染行对应的原始 markdown 逻辑行；长行 wrap 的首行
				// 标记 First，后续续行 First=false（复制时拼回同一行）。
				blk.srcLines = append(blk.srcLines, SrcLine{
					Text:  sl.Src,
					First: j == 0,
					Map:   cellMap(renderer.StringWidth("  "), wl.Spans, wl.Map),
				})
			}
		}
	}
	if len(blk.lines) == 0 {
		blk.lines = [][]Span{{}}
		blk.srcLines = append(blk.srcLines, SrcLine{})
	}
	return blk
}

func (ml *MessageList) thoughtBlock(msg *model.Message, width int) *block {
	t := ml.Theme
	// 思考内容复制原文（折叠时仅显示标题，复制仍取全部原始行）。
	blk := &block{raw: strings.Join(msg.Lines(), "\n"), toggle: &ToggleRef{Kind: ToggleThought, ID: msg.MessageID}}
	if msg.Collapsed() {
		n := len(msg.Lines())
		blk.lines = [][]Span{{{Text: "▸ thinking  ✓ " + itoa(n) + " lines", Style: t.Style(t.TextMuted).WithDim(true)}}}
		return blk
	}
	title := "▾ thinking"
	if !msg.Done {
		title = SpinFrame(ml.SpinFrame) + " thinking…"
	}
	blk.lines = append(blk.lines, []Span{{Text: title, Style: t.Style(t.TextMuted).WithItalic(true)}})
	// 先换行再截断：限制的是渲染行数，而非原始逻辑行数（长行 wrap 后可能占多行）。
	var wrapped []string
	for _, ln := range msg.Lines() {
		wrapped = append(wrapped, renderer.Wrap(ln, width-4)...)
	}
	if len(wrapped) > 5 {
		wrapped = wrapped[len(wrapped)-5:]
	}
	for _, wl := range wrapped {
		blk.lines = append(blk.lines, []Span{{Text: "  " + wl, Style: t.Style(t.TextMuted)}})
	}
	return blk
}

func (ml *MessageList) toolBlock(tc *model.ToolCall, width int, alkaid0 bool) *block {
	t := ml.Theme
	// alkaid0 v0.4：标题由工具名与关键参数拼出，正文只展开标题未消费的参数。
	var call *alkaid0Call
	if alkaid0 {
		call = alkaid0ToolCall(tc)
	}
	// 工具内容不是 markdown，复制其原始文本（标题/输入输出/内容块/位置）。
	blk := &block{raw: ml.toolRaw(tc, call), toggle: &ToggleRef{Kind: ToggleTool, ID: tc.ID}}
	title := tc.Title
	if title == "" {
		title = string(tc.Kind)
	}
	if call != nil {
		title = call.Title
	}
	st := t.Style(t.ToolPending)
	switch tc.Status {
	case acp.ToolCompleted:
		st = t.Style(t.ToolDone).WithDim(true)
	case acp.ToolFailed:
		st = t.Style(t.ToolFailed)
	case acp.ToolInProgress:
		st = t.Style(t.ToolRunning)
	}
	spin := ""
	if tc.Running() {
		spin = SpinFrame(ml.SpinFrame) + " "
	}
	blk.lines = append(blk.lines, []Span{{Text: "▌ " + truncateRune(title, width-6) + "  " + spin + tc.StatusSymbol(), Style: st}})
	if !tc.Expanded {
		return blk
	}
	if tc.RawInput != "" {
		blk.lines = append(blk.lines, []Span{{Text: "  in: " + truncateRune(tc.RawInput, width-8), Style: t.Style(t.MDCode)}})
	}
	if tc.RawOutput != "" {
		for ln := range strings.SplitSeq(tc.RawOutput, "\n") {
			for _, wl := range renderer.Wrap(ln, width-8) {
				blk.lines = append(blk.lines, []Span{{Text: "  out: " + wl, Style: t.Style(t.MDCode)}})
			}
		}
	}
	for _, location := range tc.Locations {
		place := location.Path
		if location.Line != nil {
			place += ":" + itoa(int(*location.Line))
		}
		blk.lines = append(blk.lines, []Span{{Text: "  at: " + truncateRune(place, width-8), Style: t.Style(t.Info)}})
	}
	if call != nil {
		for _, ln := range alkaid0BodyArgs(call) {
			for _, wl := range renderer.Wrap(ln, width-8) {
				blk.lines = append(blk.lines, []Span{{Text: "  " + wl, Style: t.Style(t.MDCode)}})
			}
		}
	}
	// 私有渲染下服务端的首个文本块是完整参数的预览：标题已消费的参数不再重复展示。
	skipArgsText := call != nil
	for _, ct := range tc.Content {
		switch ct.Type {
		case "content":
			if ct.Content == nil {
				continue
			}
			if ct.Content.Text != nil {
				if skipArgsText {
					skipArgsText = false
					continue
				}
				for _, ln := range renderer.Wrap(*ct.Content.Text, width-8) {
					blk.lines = append(blk.lines, []Span{{Text: "  " + ln, Style: t.Style(t.MDCode)}})
				}
				continue
			}
			placeholder := nonTextContentPlaceholder(ct.Content)
			blk.lines = append(blk.lines, []Span{{Text: "  " + truncateRune(placeholder, width-8), Style: t.Style(t.TextMuted)}})
		case "diff":
			text := ""
			if ct.Text != nil {
				text = *ct.Text
			}
			for ln := range strings.SplitSeq(text, "\n") {
				st := t.Style(t.TextMuted)
				switch {
				case strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
					st = t.Style(t.Info).WithBold(true)
				case strings.HasPrefix(ln, "@@"):
					st = t.Style(t.Secondary)
				case strings.HasPrefix(ln, "+"):
					st = t.Style(t.ToolDone)
				case strings.HasPrefix(ln, "-"):
					st = t.Style(t.ToolFailed)
				}
				for _, wl := range renderer.Wrap(ln, width-8) {
					blk.lines = append(blk.lines, []Span{{Text: "  " + wl, Style: st}})
				}
			}
		case "terminal":
			text := ""
			if ct.Text != nil {
				text = *ct.Text
			}
			for ln := range strings.SplitSeq(text, "\n") {
				for _, wl := range renderer.Wrap(ln, width-8) {
					blk.lines = append(blk.lines, []Span{{Text: "  " + wl, Style: t.Style(t.MDCode)}})
				}
			}
		default:
			label := ct.Type
			if label == "" {
				label = "unknown"
			}
			if ct.Text != nil && *ct.Text != "" {
				for _, ln := range renderer.Wrap(*ct.Text, width-8) {
					blk.lines = append(blk.lines, []Span{{Text: "  [" + label + "] " + ln, Style: t.Style(t.TextMuted)}})
				}
			} else {
				blk.lines = append(blk.lines, []Span{{Text: "  [" + label + "]", Style: t.Style(t.TextMuted).WithDim(true)}})
			}
		}
	}
	return blk
}

// toolRaw 拼接工具调用的原始文本（复制用），不含渲染前缀与样式。
func (ml *MessageList) toolRaw(tc *model.ToolCall, call *alkaid0Call) string {
	var sb strings.Builder
	title := tc.Title
	if title == "" {
		title = string(tc.Kind)
	}
	if call != nil {
		title = call.Title
	}
	sb.WriteString(title)
	if call != nil {
		for _, ln := range alkaid0BodyArgs(call) {
			sb.WriteString("\n" + ln)
		}
	}
	if tc.RawInput != "" {
		sb.WriteString("\n" + i18n.T("输入: ") + tc.RawInput)
	}
	if tc.RawOutput != "" {
		sb.WriteString("\n" + tc.RawOutput)
	}
	for _, location := range tc.Locations {
		place := location.Path
		if location.Line != nil {
			place += ":" + itoa(int(*location.Line))
		}
		sb.WriteString("\n" + i18n.T("位置: ") + place)
	}
	// 私有渲染下服务端的首个文本块是完整参数的预览：正文已用其余参数替代。
	skipArgsText := call != nil
	for _, ct := range tc.Content {
		switch ct.Type {
		case "content":
			if ct.Content != nil && ct.Content.Text != nil {
				if skipArgsText {
					skipArgsText = false
					continue
				}
				sb.WriteString("\n" + *ct.Content.Text)
			}
		case "diff", "terminal":
			if ct.Text != nil {
				sb.WriteString("\n" + *ct.Text)
			}
		default:
			if ct.Text != nil && *ct.Text != "" {
				sb.WriteString("\n" + *ct.Text)
			}
		}
	}
	return sb.String()
}

func nonTextContentPlaceholder(blk *acp.ContentBlock) string {
	kind := blk.Type
	if kind == "" {
		kind = "content"
	}
	label := "[" + kind
	if blk.Name != nil && *blk.Name != "" {
		label += " " + *blk.Name
	}
	if blk.MimeType != nil && *blk.MimeType != "" {
		label += " · " + *blk.MimeType
	}
	if blk.URI != nil && *blk.URI != "" {
		label += " · " + *blk.URI
	}
	return label + "]"
}

func (ml *MessageList) terminalBlock(terminal *model.TerminalState, width int) *block {
	t := ml.Theme
	blk := &block{raw: terminal.Transcript}
	title := terminal.Title
	if title == "" {
		title = "terminal " + terminal.ID
	}
	if terminal.Status != "" {
		title += "  " + terminal.Status
	}
	blk.lines = [][]Span{{{Text: "▌ " + truncateRune(title, width-4), Style: t.Style(t.Info)}}}
	if !terminal.Expanded {
		return blk
	}
	if terminal.Truncated {
		blk.lines = append(blk.lines, []Span{{Text: "  … earlier terminal output truncated", Style: t.Style(t.TextMuted).WithDim(true)}})
	}
	for ln := range strings.SplitSeq(terminal.Transcript, "\n") {
		for _, wl := range renderer.Wrap(ln, width-6) {
			blk.lines = append(blk.lines, []Span{{Text: "  " + wl, Style: t.Style(t.MDCode)}})
		}
	}
	return blk
}

func (ml *MessageList) planBlock(plan *acp.Plan, expanded bool, width int) *block {
	t := ml.Theme
	blk := &block{}
	if !expanded {
		blk.lines = [][]Span{{{Text: "▸ plan  (" + itoa(len(plan.Entries)) + " items)", Style: t.Style(t.Secondary).WithDim(true)}}}
		return blk
	}
	blk.lines = append(blk.lines, []Span{{Text: "▾ plan", Style: t.Style(t.Secondary).WithBold(true)}})
	for _, e := range plan.Entries {
		sym, st := "○", t.Style(t.TextMuted)
		switch e.Status {
		case acp.PlanInProgress:
			sym, st = "●", t.Style(t.ToolRunning)
		case acp.PlanCompleted:
			sym, st = "✓", t.Style(t.ToolDone)
		case acp.PlanCancelled:
			sym, st = "✗", t.Style(t.ToolFailed)
		}
		for _, ln := range renderer.Wrap(e.Content, width-6) {
			blk.lines = append(blk.lines, []Span{{Text: "  " + sym + " " + ln, Style: st}})
		}
	}
	return blk
}

func prependSpan(spans []Span, text string, style renderer.Style) []Span {
	return append([]Span{{Text: text, Style: style}}, spans...)
}

// wrapLine 按 maxW 列宽把一行带源下标的渲染行拆成多行：样式与源下标随 rune
// 一起换行，返回行的 Spans/Map 都按该行重排（Src 由调用方维护）。
func wrapLine(line StyledLine, maxW int) []StyledLine {
	if maxW <= 1 {
		maxW = 1
	}
	var result []StyledLine
	var cur []Span
	var curMap []int
	w := 0
	appendRune := func(r rune, st renderer.Style, id int) {
		if len(cur) > 0 && cur[len(cur)-1].Style == st {
			cur[len(cur)-1].Text += string(r)
		} else {
			cur = append(cur, Span{Text: string(r), Style: st})
		}
		curMap = append(curMap, id)
	}
	flush := func() {
		result = append(result, StyledLine{Spans: cur, Map: curMap})
		cur, curMap, w = nil, nil, 0
	}
	idx := 0 // 当前 rune 在 line.Spans 拼接序列中的序号
	for _, sp := range line.Spans {
		for _, r := range sp.Text {
			id := identAt(line.Map, idx)
			idx++
			if r == '\n' {
				flush()
				continue
			}
			rw := renderer.RuneWidth(r)
			if rw == 0 {
				continue
			}
			if r == '\t' {
				// 制表符展开成最多 4 个空格：都指向同一个 tab，复制时只输出一次。
				for i := 0; i < 4 && w < maxW; i++ {
					appendRune(' ', sp.Style, id)
					w++
				}
				continue
			}
			if w+rw > maxW {
				flush()
			}
			appendRune(r, sp.Style, id)
			w += rw
		}
	}
	if len(cur) > 0 || w > 0 {
		flush()
	}
	if len(result) == 0 {
		result = []StyledLine{{Spans: []Span{}}}
	}
	return result
}

// identAt 返回第 i 个渲染 rune 的源下标；未映射或不带映射时返回 -1。
func identAt(idents []int, i int) int {
	if i < 0 || i >= len(idents) {
		return -1
	}
	return idents[i]
}

// cellMap 把"每个 rune 的源下标"展开成"每个单元格（列）的源下标"：宽字符的两
// 格都指向同一个 rune，零宽 rune 不占格；prefixW 是渲染前缀的列数（不来自原文，
// 映射为 -1），使列号与屏幕上该渲染行的列号一一对应。
func cellMap(prefixW int, spans []Span, idents []int) []int {
	out := negIdents(prefixW)
	idx := 0
	for _, sp := range spans {
		for _, r := range sp.Text {
			id := identAt(idents, idx)
			idx++
			switch width := renderer.RuneWidth(r); width {
			case 0:
				// 零宽 rune 不占列，列位与它无关。
			case 1:
				out = append(out, id)
			default:
				for range width {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i, neg := len(b), n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func truncateRune(s string, maxW int) string { return renderer.Truncate(s, maxW) }
