package model

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/term"
)

// MessageKind 是消息的类型。
type MessageKind int

const (
	MsgUser MessageKind = iota
	MsgAssistant
	MsgThought
)

// TimelineKind 描述正文中一个稳定、可更新的活动项。
type TimelineKind int

const (
	TimelineUserMessage TimelineKind = iota
	TimelineAssistantMessage
	TimelineThought
	TimelineToolCall
	TimelinePlan
	TimelineTerminal
	TimelineSystemNotice
)

// TimelineItem 是正文唯一顺序来源。后续 patch 只更新 payload，不改变首次出现位置。
type TimelineItem struct {
	Key      string
	Kind     TimelineKind
	Message  *Message
	ToolCall *ToolCall
	Plan     *acp.Plan
	Terminal *TerminalState
	Notice   string
}

// Message 是一条消息的 UI 状态。
type Message struct {
	MessageID string
	Kind      MessageKind
	Text      string
	Expanded  bool
	Done      bool
}

func (m *Message) Lines() []string {
	if m.Text == "" {
		return nil
	}
	return strings.Split(m.Text, "\n")
}

func (m *Message) Collapsed() bool { return m.Kind == MsgThought && m.Done && !m.Expanded }

// ToolCall 是工具调用的 UI 状态。
type ToolCall struct {
	ID        string
	Title     string
	Kind      acp.ToolCallKind
	Status    acp.ToolCallStatus
	Content   []acp.ToolCallContent
	Locations []acp.ToolCallLocation
	RawInput  string
	RawOutput string
	Expanded  bool
}

func (tc *ToolCall) StatusSymbol() string {
	switch tc.Status {
	case acp.ToolCompleted:
		return "✓"
	case acp.ToolFailed:
		return "✗"
	case acp.ToolCancelled:
		return "−"
	case acp.ToolInProgress:
		return "•"
	default:
		return " "
	}
}

func (tc *ToolCall) Running() bool {
	return tc.Status == acp.ToolPending || tc.Status == acp.ToolInProgress
}

// TerminalState 保存 agent 广播的终端内容。输出有界，避免流式日志无限占用内存。
type TerminalState struct {
	ID         string
	SessionID  string
	Kind       string
	Title      string
	Command    string
	Status     string
	Reason     string
	AgentID    string
	ToolID     string
	CreatedAt  string
	Transcript string
	Truncated  bool
	Expanded   bool
	// Restored 标记该终端来自服务端的持久化副本（服务端重启后内存中已无它），
	// 此时只有内容可信，命令/类型等元数据为空。
	Restored bool
	// Screen 是预览用的 VT 屏幕（尺寸由视图按预览框调整）；ScreenRows 是内容
	// 换行后的总行数，ScreenViewH 是构建时的可见视口高度。
	Screen      *term.VTScreen
	ScreenRows  int
	ScreenViewH int
	// Scroll 是预览向上回看的行数（0 = 停在底部）；FollowBottom 为 true 时
	// 新内容到达保持粘滞（始终显示最新输出）。
	Scroll       int
	FollowBottom bool
}

// NewTerminalState 创建终端状态：预览默认粘滞在底部。
func NewTerminalState(id string) *TerminalState {
	return &TerminalState{ID: id, Expanded: true, FollowBottom: true, Screen: term.NewVTScreen(80, 24)}
}

// Finished 报告终端是否已结束：结束的终端从活动列表移入历史段，
// 其内容仍可通过 terminal/history 取回。
func (t *TerminalState) Finished() bool {
	return t != nil && acp.TerminalStatusFinished(t.Status)
}

const maxTerminalTranscriptBytes = 32 << 10

func (t *TerminalState) Append(text string) {
	if text == "" {
		return
	}
	t.Transcript += text
	if len(t.Transcript) > maxTerminalTranscriptBytes {
		t.Transcript = t.Transcript[len(t.Transcript)-maxTerminalTranscriptBytes:]
		t.Truncated = true
	}
}

// SessionState 是单个会话的 UI 状态。
type SessionState struct {
	ID         string
	Title      string
	State      acp.SessionState
	StopReason *acp.StopReason

	Messages        []*Message
	receiveMessages []*Message
	msgIndex        map[string]*Message

	ToolCalls map[string]*ToolCall
	ToolOrder []string // 兼容旧逻辑；正文顺序改由 Timeline 决定。

	Plan         *acp.Plan
	PlanExpanded bool
	Usage        acp.Usage
	ModelName    string
	WorkingDir   string
	UpdatedAt    string
	Commands     []acp.AvailableCommand
	AgentConfig  []acp.ConfigOption

	Timeline      []*TimelineItem
	timelineIndex map[string]*TimelineItem
	// terminals/terminalOrder 是活动终端；terminalHistory/terminalHistoryOrder
	// 是已结束终端（shells 面板下半段，内容保留）。
	terminals            map[string]*TerminalState
	terminalOrder        []string
	terminalHistory      map[string]*TerminalState
	terminalHistoryOrder []string

	ProtocolUpdates []json.RawMessage
	Scroll          int
	// FollowBottom 为 true 时消息区锁定底部：新内容到达自动跟随，
	// Scroll 由渲染层同步为当前最大滚动偏移。用户一旦手动滚动即解除。
	FollowBottom bool

	Alkaid0MessageOrdering bool
	// Alkaid0ToolCalls 为 true 时按 alkaid0 私有协议（v0.4）渲染工具调用：
	// 标题由工具名与关键参数拼出（如 Edit(path)、Run shell*(cmd)），正文只
	// 展开标题未消费的其余参数。
	Alkaid0ToolCalls bool
}

func NewSession(id, title string) *SessionState {
	return &SessionState{
		ID:              id,
		Title:           title,
		State:           acp.StateIdle,
		msgIndex:        map[string]*Message{},
		ToolCalls:       map[string]*ToolCall{},
		timelineIndex:   map[string]*TimelineItem{},
		terminals:       map[string]*TerminalState{},
		terminalHistory: map[string]*TerminalState{},
	}
}

func (s *SessionState) appendTimeline(key string, kind TimelineKind) *TimelineItem {
	if item, ok := s.timelineIndex[key]; ok {
		return item
	}
	item := &TimelineItem{Key: key, Kind: kind}
	s.timelineIndex[key] = item
	s.Timeline = append(s.Timeline, item)
	return item
}

func messageTimelineKind(kind MessageKind) TimelineKind {
	switch kind {
	case MsgUser:
		return TimelineUserMessage
	case MsgThought:
		return TimelineThought
	default:
		return TimelineAssistantMessage
	}
}

func (s *SessionState) AppendChunk(ev *acp.MessageChunkEvent) {
	msg := s.message(ev.MessageID, ev.IsThought, ev.IsUser)
	msg.Text += ev.Text
	msg.Done = false
	if !ev.IsThought && !ev.IsUser {
		// 正文 chunk 开始流式 → 此前的思考流已经结束，立即折叠，不等整个 turn 的 idle。
		s.finishLatestThought()
	}
}

func (s *SessionState) ApplyMessage(ev *acp.MessageUpdateEvent) {
	msg := s.message(ev.Message.MessageID, ev.IsThought, ev.IsUser)
	var sb strings.Builder
	for i, blk := range ev.Message.Content {
		if i > 0 && sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		if blk.Text != nil {
			sb.WriteString(*blk.Text)
			continue
		}
		sb.WriteString(nonTextBlockPlaceholder(blk))
	}
	if ev.Message.ContentSet {
		msg.Text = sb.String()
		msg.Done = true
	}
	if msg.Kind == MsgThought && ev.Message.ContentSet {
		msg.Expanded = false
	}
	if !ev.IsThought && !ev.IsUser && ev.Message.ContentSet {
		// 正文完整块到达 → 此前的思考流已经结束（可能只有 chunk 无完整块），立即折叠。
		s.finishLatestThought()
	}
}

// finishLatestThought 把最近一个未完成的思考标记为完成并折叠。
// 真实 wire 中 thought 与正文共享 messageId，chunk 流没有显式"思考结束"信号；
// 正文内容开始到达即视为思考流结束，立即折叠而不是等整个 turn 的 idle。
func (s *SessionState) finishLatestThought() {
	for _, m := range slices.Backward(s.Messages) {

		if m.Kind == MsgThought && !m.Done {
			m.Done = true
			m.Expanded = false
			return
		}
	}
}

// nonTextBlockPlaceholder 为非文本 ContentBlock 生成一行安全描述，避免整段内容被静默丢失。
func nonTextBlockPlaceholder(blk acp.ContentBlock) string {
	kind := blk.Type
	if kind == "" {
		kind = "unknown"
	}
	label := "[" + kind
	if blk.Name != nil && *blk.Name != "" {
		label += " " + *blk.Name
	}
	if blk.Title != nil && *blk.Title != "" {
		label += " · " + *blk.Title
	}
	if blk.MimeType != nil && *blk.MimeType != "" {
		label += " · " + *blk.MimeType
	}
	if blk.URI != nil && *blk.URI != "" {
		label += " · " + *blk.URI
	}
	return label + "]"
}

// message 按 (messageId, kind) 索引消息。
// 同一轮回复中 thought 与正文可共享 messageId（如 alkaid0 流式期间
// agent_thought_chunk / agent_message_chunk 使用同一 MsgID），因此索引必须带
// kind 前缀，否则正文 chunk 会追加进思维链、完整块正文会覆盖 thought。
func (s *SessionState) message(id string, thought, user bool) *Message {
	key := messageKey(id, thought, user)
	if m, ok := s.msgIndex[key]; ok {
		return m
	}
	kind := MsgAssistant
	if thought {
		kind = MsgThought
	} else if user {
		kind = MsgUser
	}
	m := &Message{MessageID: id, Kind: kind, Expanded: thought}
	s.receiveMessages = append(s.receiveMessages, m)
	s.Messages = append(s.Messages, m)
	s.msgIndex[key] = m
	item := s.appendTimeline(messageTimelineKey(id, kind), messageTimelineKind(kind))
	item.Message = m
	if s.Alkaid0MessageOrdering {
		s.sortMessagesByID()
	}
	return m
}

// SetAlkaid0V04 标记服务端声明 alkaid0 私有协议 v0.4：启用消息按 ID 排序与
// 工具调用的私有渲染（正文只展开标题未消费的参数）。
func (s *SessionState) SetAlkaid0V04(enabled bool) {
	s.Alkaid0MessageOrdering = enabled
	s.Alkaid0ToolCalls = enabled
	s.sortMessagesByID()
}

func (s *SessionState) SetAlkaid0MessageOrdering(enabled bool) {
	s.Alkaid0MessageOrdering = enabled
	s.sortMessagesByID()
}

func (s *SessionState) sortMessagesByID() {
	s.Messages = append(s.Messages[:0], s.receiveMessages...)
	if s.Alkaid0MessageOrdering {
		sort.SliceStable(s.Messages, func(i, j int) bool {
			return compareAlkaid0MessageID(s.Messages[i].MessageID, s.Messages[j].MessageID) < 0
		})
	}
	ordered := make([]*TimelineItem, 0, len(s.receiveMessages))
	for _, message := range s.receiveMessages {
		if item := s.timelineIndex[messageTimelineKey(message.MessageID, message.Kind)]; item != nil {
			ordered = append(ordered, item)
		}
	}
	if s.Alkaid0MessageOrdering {
		sort.SliceStable(ordered, func(i, j int) bool {
			return compareAlkaid0MessageID(ordered[i].Message.MessageID, ordered[j].Message.MessageID) < 0
		})
	}
	pos := 0
	for i, item := range s.Timeline {
		if item.Message != nil {
			s.Timeline[i] = ordered[pos]
			pos++
		}
	}
}

func compareAlkaid0MessageID(a, b string) int {
	ra, oka := alkaid0MessageIDNumber(a)
	rb, okb := alkaid0MessageIDNumber(b)
	if oka && okb && ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	if oka != okb {
		if oka {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func alkaid0MessageIDNumber(id string) (uint64, bool) {
	if !strings.HasPrefix(id, "msg_") {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(id, "msg_"), 10, 64)
	return n, err == nil
}

// messageKey 生成消息索引键：thought 与 user/assistant 分开，避免共享 messageId 冲突。
func messageKey(id string, thought, user bool) string {
	if thought {
		return "t:" + id
	}
	return "m:" + id
}

// messageTimelineKey 生成时间线键，与 messageKey 同构，保证 thought/正文各自独立行。
func messageTimelineKey(id string, kind MessageKind) string {
	if kind == MsgThought {
		return "thought:" + id
	}
	return "message:" + id
}

// ToggleMessage 在消息列表中展开/折叠指定消息。仅通过 id 查找：先试 thought，
// 再试 user/assistant，避免共享 messageId 时命中错误 kind。
func (s *SessionState) ToggleMessage(id string) {
	for _, key := range []string{messageKey(id, true, false), messageKey(id, false, false)} {
		if m, ok := s.msgIndex[key]; ok {
			m.Expanded = !m.Expanded
			return
		}
	}
}

func (s *SessionState) CollapseThoughts() {
	for _, m := range s.Messages {
		if m.Kind == MsgThought && m.Done {
			m.Expanded = false
		}
	}
}

// ExpandAll 展开会话中全部思维链（思考消息）与工具调用（Ctrl+O）。
func (s *SessionState) ExpandAll() {
	for _, m := range s.Messages {
		if m.Kind == MsgThought {
			m.Expanded = true
		}
	}
	for _, tc := range s.ToolCalls {
		tc.Expanded = true
	}
}

// CollapseAll 折叠会话中全部思维链与工具调用（再次按 Ctrl+O 收回）。
func (s *SessionState) CollapseAll() {
	for _, m := range s.Messages {
		if m.Kind == MsgThought {
			m.Expanded = false
		}
	}
	for _, tc := range s.ToolCalls {
		tc.Expanded = false
	}
}

// AllExpanded 报告会话中全部思维链与工具调用是否均已展开。
func (s *SessionState) AllExpanded() bool {
	for _, m := range s.Messages {
		if m.Kind == MsgThought && !m.Expanded {
			return false
		}
	}
	for _, tc := range s.ToolCalls {
		if !tc.Expanded {
			return false
		}
	}
	return true
}

// HasCollapsible 报告会话是否存在可展开/折叠的思维链或工具调用。
func (s *SessionState) HasCollapsible() bool {
	for _, m := range s.Messages {
		if m.Kind == MsgThought {
			return true
		}
	}
	return len(s.ToolCalls) > 0
}

// MarkStreamingDone 在会话转入 idle 时补齐完成标记。
// 部分 agent（如 alkaid0）流式期间只发 *message_chunk / *thought_chunk，不补发完整块；
// 若没有它，消息会永远停留在“流式中”状态。
func (s *SessionState) MarkStreamingDone() {
	for _, m := range s.Messages {
		if !m.Done {
			m.Done = true
		}
	}
}

func (s *SessionState) ApplyToolCall(ev *acp.ToolCallUpdateEvent) {
	tc, ok := s.ToolCalls[ev.ToolCallID]
	if !ok {
		tc = &ToolCall{ID: ev.ToolCallID, Status: acp.ToolPending, Expanded: true}
		s.ToolCalls[ev.ToolCallID] = tc
		s.ToolOrder = append(s.ToolOrder, ev.ToolCallID)
		item := s.appendTimeline("tool:"+ev.ToolCallID, TimelineToolCall)
		item.ToolCall = tc
	}
	if ev.Status != nil {
		tc.Status = *ev.Status
	}
	if ev.Title != nil {
		tc.Title = *ev.Title
	}
	if ev.Kind != nil {
		tc.Kind = *ev.Kind
	}
	if ev.ContentAppend {
		tc.Content = append(tc.Content, ev.Content...)
	} else if ev.ContentSet {
		tc.Content = ev.Content
	}
	if ev.Locations != nil {
		tc.Locations = append([]acp.ToolCallLocation(nil), ev.Locations...)
	}
	if len(ev.RawInput) > 0 {
		tc.RawInput = string(ev.RawInput)
	}
	if len(ev.RawOutput) > 0 {
		tc.RawOutput = string(ev.RawOutput)
	}
}

func (s *SessionState) ToggleToolCall(id string) {
	if tc, ok := s.ToolCalls[id]; ok {
		tc.Expanded = !tc.Expanded
	}
}

func (s *SessionState) ApplyPlan(ev *acp.PlanUpdateEvent) {
	s.Plan = &ev.Plan
	item := s.appendTimeline("plan:"+ev.Plan.PlanID, TimelinePlan)
	item.Plan = s.Plan
}

// ApplyTerminal adds or updates a terminal using flat compatibility fields.
func (s *SessionState) ApplyTerminal(id, title, command, status, output string) {
	s.ApplyTerminalInfo(acp.TerminalInfo{TerminalID: id, Title: title, Command: command, Status: status, Content: output})
}

// ApplyTerminalInfo upserts metadata without erasing omitted fields.
// 已结束（status 为 stop/finished/killed 等）的终端不进活动列表，直接归入历史段。
func (s *SessionState) ApplyTerminalInfo(info acp.TerminalInfo) {
	if acp.TerminalStatusFinished(info.Status) {
		s.applyTerminalHistoryInfo(info)
		return
	}
	id := info.TerminalID
	if id == "" {
		id = "session"
		info.TerminalID = id
	}
	terminal, ok := s.terminals[id]
	if !ok {
		terminal = NewTerminalState(id)
		s.putTerminalActive(terminal)
	}
	s.mergeTerminalInfo(terminal, info)
}

// mergeTerminalInfo 把非空字段合并进终端状态；快照里的 content 整体替换既有内容。
func (s *SessionState) mergeTerminalInfo(terminal *TerminalState, info acp.TerminalInfo) {
	if info.SessionID != "" {
		terminal.SessionID = info.SessionID
	}
	if info.Kind != "" {
		terminal.Kind = info.Kind
	}
	if info.Title != "" {
		terminal.Title = info.Title
	}
	if info.Command != "" {
		terminal.Command = info.Command
	}
	if info.Status != "" {
		terminal.Status = info.Status
	}
	if info.Reason != "" {
		terminal.Reason = info.Reason
	}
	if info.AgentID != "" {
		terminal.AgentID = info.AgentID
	}
	if info.ToolID != "" {
		terminal.ToolID = info.ToolID
	}
	if info.CreatedAt != "" {
		terminal.CreatedAt = info.CreatedAt
	}
	if info.Restored {
		terminal.Restored = true
	}
	if info.Content != "" {
		terminal.Transcript = info.Content
		terminal.Truncated = false
		if terminal.Screen != nil {
			terminal.Screen.Reset()
			terminal.Screen.Feed(info.Content)
		}
	}
}

// ApplyTerminalHistory 合并 terminal/history 的查询结果（已结束终端，含
// 服务端重启后仅剩持久化副本的条目）。同 ID 已存在于活动列表时搬运过去，
// 保留本地已收集到的完整内容。
func (s *SessionState) ApplyTerminalHistory(infos []acp.TerminalInfo) {
	for _, info := range infos {
		if info.TerminalID != "" {
			s.applyTerminalHistoryInfo(info)
		}
	}
}

func (s *SessionState) applyTerminalHistoryInfo(info acp.TerminalInfo) {
	id := info.TerminalID
	if id == "" {
		return
	}
	terminal, ok := s.terminalHistory[id]
	if !ok {
		if active, activeOK := s.terminals[id]; activeOK {
			s.ArchiveTerminal(id)
			terminal = active
		} else {
			terminal = NewTerminalState(id)
			s.putTerminalHistory(terminal)
		}
	}
	s.mergeTerminalInfo(terminal, info)
	if terminal.Status == "" {
		terminal.Status = "finished"
	}
}

// LinkToolCallTerminal 用 run 工具调用的信息补全终端条目。
// 历史回放（session/resume）里的 run 工具调用带 terminal_id，工具入参则放在
// content 的 alk.cxykevin.top/calling_info 块里（alkaid0 不发送标准 rawInput，
// 见 extension.md §4.1）：据此复原列表首行（reason）与命令，并据工具调用状态
// 决定它进哪一段——已结束的进历史段，仍在执行的留在活动段（随后由
// terminal/list 与 history 校正）。terminal/history 的持久化副本只有内容，
// 没有这层复原就只能显示 @temp/run/<n>。
func (s *SessionState) LinkToolCallTerminal(id string, contents []acp.ToolCallContent, rawInput json.RawMessage, status *acp.ToolCallStatus) {
	if id == "" {
		return
	}
	var args struct {
		Type    string `json:"type"`
		Reason  string `json:"reason"`
		Command string `json:"command"`
	}
	for _, content := range contents {
		if content.Type != acp.ToolCallingInfoType || len(content.Args) == 0 {
			continue
		}
		_ = json.Unmarshal(content.Args, &args)
		break
	}
	// 兼容仍然发送 rawInput 的实现。
	if args.Command == "" && len(rawInput) > 0 {
		_ = json.Unmarshal(rawInput, &args)
	}
	terminal := s.terminals[id]
	if terminal == nil {
		terminal = s.terminalHistory[id]
	}
	if terminal == nil {
		terminal = NewTerminalState(id)
		if status != nil && toolCallStatusFinished(*status) {
			s.putTerminalHistory(terminal)
		} else {
			s.putTerminalActive(terminal)
		}
	}
	// 只补空字段：同一个终端会被多次工具调用引用（启动 shell，随后 wait / kill），
	// 先到的"启动"调用给出真实 reason / command，后到的不能覆盖。
	// wait / kill 的 command 是 run id，也不是终端自身的命令，直接跳过。
	if args.Type == "" || args.Type == "shell" || args.Type == "sleep" || args.Type == "python" {
		if terminal.Reason == "" && args.Reason != "" {
			terminal.Reason = args.Reason
		}
		if terminal.Command == "" && args.Command != "" {
			terminal.Command = args.Command
		}
		if terminal.Kind == "" && args.Type != "" {
			terminal.Kind = args.Type
		}
	}
}

// toolCallStatusFinished 报告工具调用是否已结束（结束的 run 工具调用对应已结束终端）。
func toolCallStatusFinished(status acp.ToolCallStatus) bool {
	switch status {
	case acp.ToolCompleted, acp.ToolFailed, acp.ToolCancelled:
		return true
	default:
		return false
	}
}

// putTerminalActive 把终端放入活动段（已存在则原位更新），并从历史段移除同 ID 条目：
// 一个终端只会出现在一段里。
func (s *SessionState) putTerminalActive(terminal *TerminalState) {
	if terminal == nil || terminal.ID == "" {
		return
	}
	s.removeTerminalHistoryEntry(terminal.ID)
	if _, ok := s.terminals[terminal.ID]; !ok {
		// 新终端排在最前：面板里"新的 shell 在上"。
		s.terminalOrder = append([]string{terminal.ID}, s.terminalOrder...)
	}
	s.terminals[terminal.ID] = terminal
	item := s.appendTimeline("terminal:"+terminal.ID, TimelineTerminal)
	item.Terminal = terminal
}

// removeTerminalHistoryEntry 从历史段移除指定终端（不含时间线）。
func (s *SessionState) removeTerminalHistoryEntry(id string) {
	delete(s.terminalHistory, id)
	for i, v := range s.terminalHistoryOrder {
		if v == id {
			s.terminalHistoryOrder = append(s.terminalHistoryOrder[:i], s.terminalHistoryOrder[i+1:]...)
			break
		}
	}
}

// putTerminalHistory 把终端放入历史段（已存在则原位更新）。
// 历史段按"最后结束的排最前"排列：面板里历史紧接在活动 shell 下方，
// 最近结束的终端最靠近上方（与服务端 terminal/history 的升序相反）。
func (s *SessionState) putTerminalHistory(terminal *TerminalState) {
	if terminal == nil || terminal.ID == "" {
		return
	}
	// 同一终端只出现在一段里：进历史段时从活动段移除（时间线由 ArchiveTerminal 处理）。
	delete(s.terminals, terminal.ID)
	for i, v := range s.terminalOrder {
		if v == terminal.ID {
			s.terminalOrder = append(s.terminalOrder[:i], s.terminalOrder[i+1:]...)
			break
		}
	}
	if _, ok := s.terminalHistory[terminal.ID]; !ok {
		s.terminalHistoryOrder = append([]string{terminal.ID}, s.terminalHistoryOrder...)
	}
	s.terminalHistory[terminal.ID] = terminal
}

// ArchiveTerminal 把活动终端移入历史段：内容保留，只从活动列表与时间线移除。
func (s *SessionState) ArchiveTerminal(id string) {
	terminal, ok := s.terminals[id]
	if !ok {
		return
	}
	delete(s.terminals, id)
	for i, v := range s.terminalOrder {
		if v == id {
			s.terminalOrder = append(s.terminalOrder[:i], s.terminalOrder[i+1:]...)
			break
		}
	}
	s.removeTerminalTimeline(id)
	if !acp.TerminalStatusFinished(terminal.Status) {
		terminal.Status = "finished"
	}
	s.putTerminalHistory(terminal)
}

// MergeTerminals 并入一组终端快照（增量推送里的 terminals 字段）：只做
// upsert，不因快照缺失而移除其它终端——结束仍由显式的 stop/shell_stop 驱动。
func (s *SessionState) MergeTerminals(infos []acp.TerminalInfo) {
	for _, info := range infos {
		if info.TerminalID == "" {
			continue
		}
		if acp.TerminalStatusFinished(info.Status) {
			s.applyTerminalHistoryInfo(info)
			continue
		}
		s.ApplyTerminalInfo(info)
	}
}

// ReplaceTerminals applies the v0.5 full snapshot.
// 快照只描述活动终端：不在快照里的活动终端视为已结束并移入历史段（保留内容）。
// 例外是历史查询的推送——它的条目全部已结束，此时只更新历史段，不动活动列表。
func (s *SessionState) ReplaceTerminals(infos []acp.TerminalInfo) {
	historyOnly := len(infos) > 0
	for _, info := range infos {
		if info.TerminalID != "" && !acp.TerminalStatusFinished(info.Status) {
			historyOnly = false
			break
		}
	}
	if historyOnly {
		s.ApplyTerminalHistory(infos)
		return
	}
	seen := make(map[string]bool, len(infos))
	for _, info := range infos {
		if info.TerminalID == "" {
			continue
		}
		if acp.TerminalStatusFinished(info.Status) {
			s.applyTerminalHistoryInfo(info)
			continue
		}
		seen[info.TerminalID] = true
		s.ApplyTerminalInfo(info)
	}
	for _, id := range append([]string(nil), s.terminalOrder...) {
		if !seen[id] {
			s.ArchiveTerminal(id)
		}
	}
}

// RemoveTerminal 彻底丢弃终端（活动与历史两段）。
func (s *SessionState) RemoveTerminal(id string) {
	delete(s.terminals, id)
	for i, v := range s.terminalOrder {
		if v == id {
			s.terminalOrder = append(s.terminalOrder[:i], s.terminalOrder[i+1:]...)
			break
		}
	}
	delete(s.terminalHistory, id)
	for i, v := range s.terminalHistoryOrder {
		if v == id {
			s.terminalHistoryOrder = append(s.terminalHistoryOrder[:i], s.terminalHistoryOrder[i+1:]...)
			break
		}
	}
	s.removeTerminalTimeline(id)
}

func (s *SessionState) removeTerminalTimeline(id string) {
	delete(s.timelineIndex, "terminal:"+id)
	for i, item := range s.Timeline {
		if item.Key == "terminal:"+id {
			s.Timeline = append(s.Timeline[:i], s.Timeline[i+1:]...)
			break
		}
	}
}

// Terminals returns active shells, most recently created first.
func (s *SessionState) Terminals() []*TerminalState {
	out := make([]*TerminalState, 0, len(s.terminalOrder))
	for _, id := range s.terminalOrder {
		if t := s.terminals[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

// TerminalHistory returns finished shells, most recently finished first.
func (s *SessionState) TerminalHistory() []*TerminalState {
	out := make([]*TerminalState, 0, len(s.terminalHistoryOrder))
	for _, id := range s.terminalHistoryOrder {
		if t := s.terminalHistory[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

func (s *SessionState) Terminal(id string) *TerminalState { return s.terminals[id] }

// TerminalHistoryByID 返回历史段（已结束）中的终端。
func (s *SessionState) TerminalHistoryByID(id string) *TerminalState { return s.terminalHistory[id] }

func (s *SessionState) Running() bool {
	return s.State == acp.StateRunning || s.State == acp.StateRequiresAction
}

// AppendSystemNotice 在时间线末尾追加一条只读系统提示。相同 key 会复用已存在的项，
// 以便未知 session update 汇总为一行诊断而不重复。
func (s *SessionState) AppendSystemNotice(key, notice string) {
	if key == "" || notice == "" {
		return
	}
	item := s.appendTimeline("notice:"+key, TimelineSystemNotice)
	item.Notice = notice
}

// ConfigOption 返回指定 configId 的 agent 配置项；不存在时返回 nil。
func (s *SessionState) ConfigOption(configID string) *acp.ConfigOption {
	for i := range s.AgentConfig {
		if s.AgentConfig[i].ConfigID == configID {
			return &s.AgentConfig[i]
		}
	}
	return nil
}

// ModelConfigOption 返回 agent 公布的模型选择配置项。
// ACP v2 中模型选择器的语义标识是 category="model"；兼容按 configId="model" 匹配。
func (s *SessionState) ModelConfigOption() *acp.ConfigOption {
	for i := range s.AgentConfig {
		if s.AgentConfig[i].Category == "model" {
			return &s.AgentConfig[i]
		}
	}
	for i := range s.AgentConfig {
		if s.AgentConfig[i].ConfigID == "model" {
			return &s.AgentConfig[i]
		}
	}
	return nil
}

// ModelLabel 返回状态栏展示的模型名称。优先 session-info 广播的 model 字段；
// 否则回退到 agent 公布的 model config（category="model"）当前值的显示名。
func (s *SessionState) ModelLabel() string {
	if s.ModelName != "" {
		return s.ModelName
	}
	opt := s.ModelConfigOption()
	if opt == nil {
		return ""
	}
	for _, o := range opt.Options {
		if o.Value == opt.CurrentValue {
			return o.Name
		}
	}
	return opt.CurrentValue
}

// applyAgentConfig 用最新 patch 更新 agent 配置项列表；对具备 configId 的项按 configId
// upsert，无 configId 时按 name 匹配，否则整批替换。任何情况都保留原顺序。
func (s *SessionState) applyAgentConfig(options []acp.ConfigOption) {
	if len(options) == 0 {
		return
	}
	haveIDOrName := false
	for _, opt := range options {
		if opt.ConfigID != "" || opt.Name != "" {
			haveIDOrName = true
			break
		}
	}
	if !haveIDOrName {
		s.AgentConfig = append([]acp.ConfigOption(nil), options...)
		return
	}
	next := append([]acp.ConfigOption(nil), s.AgentConfig...)
	for _, incoming := range options {
		found := -1
		for i, existing := range next {
			if incoming.ConfigID != "" && existing.ConfigID == incoming.ConfigID {
				found = i
				break
			}
			if incoming.ConfigID == "" && incoming.Name != "" && existing.Name == incoming.Name {
				found = i
				break
			}
		}
		if found >= 0 {
			next[found] = incoming
		} else {
			next = append(next, incoming)
		}
	}
	s.AgentConfig = next
}
