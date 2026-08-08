package view

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func SpinFrame(n int) string { return spinnerFrames[n%len(spinnerFrames)] }

type block struct {
	lines    [][]Span
	raw      string    // 块级原始内容（工具/终端/思考等，复制用，无渲染前缀）
	srcLines []SrcLine // 行级插针：每渲染行对应的原始逻辑行文本（消息块）
}

// SrcLine 是行级插针的一行：Text 为该渲染行对应的原始 markdown 逻辑行，
// First 表示该渲染行是否为该原始逻辑行的首个渲染行（长行 wrap 拆分时，
// 后续渲染行 First=false，复制时去重避免整行重复输出）。
type SrcLine struct {
	Text  string
	First bool
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
}

func (ml *MessageList) Draw(c *renderer.Canvas, r renderer.Rect, s *model.SessionState) {
	width := r.W - 1
	if width <= 0 {
		return
	}
	blocks := ml.buildBlocks(s, width)
	total := 0
	for _, blk := range blocks {
		total += len(blk.lines)
	}
	viewH := r.H
	maxScroll := total - viewH
	if maxScroll < 0 {
		maxScroll = 0
	}
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
				blocks = append(blocks, ml.messageBlock(item.Message, width))
			}
		case model.TimelineThought:
			if item.Message != nil {
				blocks = append(blocks, ml.thoughtBlock(item.Message, width))
			}
		case model.TimelineToolCall:
			if item.ToolCall != nil {
				blocks = append(blocks, ml.toolBlock(item.ToolCall, width))
			}
		case model.TimelinePlan:
			// 计划只由固定在输入框上方的 PlanPanel 绘制，不进入正文上下文。
			continue
		case model.TimelineTerminal:
			if item.Terminal != nil {
				blocks = append(blocks, ml.terminalBlock(item.Terminal, width))
			}
		case model.TimelineSystemNotice:
			if item.Notice != "" {
				blocks = append(blocks, &block{lines: [][]Span{{{Text: item.Notice, Style: ml.Theme.Style(ml.Theme.TextMuted)}}}, raw: item.Notice})
			}
		}
	}
	return blocks
}

// bodyBlocks 把 blocks 展平为按渲染行（contentY）索引的正文块目录。
// 消息块带行级插针 Src；其余块保留块级 Raw。空内容跳过，但行号仍累计。
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
			wrapped := renderer.Wrap(ln, width-4)
			for j, wl := range wrapped {
				prefix := "  "
				if row == 0 {
					prefix = "  ❯ "
				}
				blk.lines = append(blk.lines, []Span{{Text: prefix + wl, Style: t.Style(t.Primary).WithBold(true)}})
				blk.srcLines = append(blk.srcLines, SrcLine{Text: strings.TrimRight(srcLines[li], " \t"), First: j == 0})
				row++
			}
		}
	} else {
		for _, sl := range Markdown(msg.Text, t) {
			wrapped := wrapSpans(sl.Spans, width-4)
			for j, line := range wrapped {
				blk.lines = append(blk.lines, prependSpan(line, "  ", t.Style(t.Text)))
				// 插针：该渲染行对应的原始 markdown 逻辑行；长行 wrap 的首行
				// 标记 First，后续续行 First=false（复制时整行只输出一次）。
				blk.srcLines = append(blk.srcLines, SrcLine{Text: sl.Src, First: j == 0})
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
	blk := &block{raw: strings.Join(msg.Lines(), "\n")}
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

func (ml *MessageList) toolBlock(tc *model.ToolCall, width int) *block {
	t := ml.Theme
	// 工具内容不是 markdown，复制其原始文本（标题/输入输出/内容块/位置）。
	blk := &block{raw: ml.toolRaw(tc)}
	title := tc.Title
	if title == "" {
		title = string(tc.Kind)
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
		for _, ln := range strings.Split(tc.RawOutput, "\n") {
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
	for _, ct := range tc.Content {
		switch ct.Type {
		case "content":
			if ct.Content == nil {
				continue
			}
			if ct.Content.Text != nil {
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
			for _, ln := range strings.Split(text, "\n") {
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
			for _, ln := range strings.Split(text, "\n") {
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
func (ml *MessageList) toolRaw(tc *model.ToolCall) string {
	var sb strings.Builder
	title := tc.Title
	if title == "" {
		title = string(tc.Kind)
	}
	sb.WriteString(title)
	if tc.RawInput != "" {
		sb.WriteString("\n输入: " + tc.RawInput)
	}
	if tc.RawOutput != "" {
		sb.WriteString("\n" + tc.RawOutput)
	}
	for _, location := range tc.Locations {
		place := location.Path
		if location.Line != nil {
			place += ":" + itoa(int(*location.Line))
		}
		sb.WriteString("\n位置: " + place)
	}
	for _, ct := range tc.Content {
		switch ct.Type {
		case "content":
			if ct.Content != nil && ct.Content.Text != nil {
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
	for _, ln := range strings.Split(terminal.Transcript, "\n") {
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

func wrapSpans(spans []Span, maxW int) [][]Span {
	if maxW <= 1 {
		maxW = 1
	}
	var result [][]Span
	var cur []Span
	w := 0
	appendRune := func(r rune, st renderer.Style) {
		if len(cur) > 0 && cur[len(cur)-1].Style == st {
			cur[len(cur)-1].Text += string(r)
		} else {
			cur = append(cur, Span{Text: string(r), Style: st})
		}
	}
	for _, sp := range spans {
		for _, r := range sp.Text {
			if r == '\n' {
				result = append(result, cur)
				cur, w = nil, 0
				continue
			}
			rw := renderer.RuneWidth(r)
			if rw == 0 {
				continue
			}
			if r == '\t' {
				for i := 0; i < 4 && w < maxW; i++ {
					appendRune(' ', sp.Style)
					w++
				}
				continue
			}
			if w+rw > maxW {
				result = append(result, cur)
				cur, w = nil, 0
			}
			appendRune(r, sp.Style)
			w += rw
		}
	}
	if len(cur) > 0 || w > 0 {
		result = append(result, cur)
	}
	if len(result) == 0 {
		result = [][]Span{{}}
	}
	return result
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
