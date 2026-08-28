package model

import (
	"encoding/json"
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
	Screen     *term.VTScreen
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

	Messages []*Message
	msgIndex map[string]*Message

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
	terminals     map[string]*TerminalState
	terminalOrder []string

	ProtocolUpdates []json.RawMessage
	Scroll          int
	// FollowBottom 为 true 时消息区锁定底部：新内容到达自动跟随，
	// Scroll 由渲染层同步为当前最大滚动偏移。用户一旦手动滚动即解除。
	FollowBottom bool
}

func NewSession(id, title string) *SessionState {
	return &SessionState{
		ID:            id,
		Title:         title,
		State:         acp.StateIdle,
		msgIndex:      map[string]*Message{},
		ToolCalls:     map[string]*ToolCall{},
		timelineIndex: map[string]*TimelineItem{},
		terminals:     map[string]*TerminalState{},
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
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := s.Messages[i]
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
	s.Messages = append(s.Messages, m)
	s.msgIndex[key] = m
	item := s.appendTimeline(messageTimelineKey(id, kind), messageTimelineKind(kind))
	item.Message = m
	return m
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
func (s *SessionState) ApplyTerminalInfo(info acp.TerminalInfo) {
	id := info.TerminalID
	if id == "" {
		id = "session"
		info.TerminalID = id
	}
	terminal, ok := s.terminals[id]
	if !ok {
		terminal = &TerminalState{ID: id, Expanded: true, Screen: term.NewVTScreen(80, 24)}
		s.terminals[id] = terminal
		s.terminalOrder = append(s.terminalOrder, id)
		item := s.appendTimeline("terminal:"+id, TimelineTerminal)
		item.Terminal = terminal
	}
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
	if info.Content != "" {
		terminal.Transcript = info.Content
		terminal.Truncated = false
		if terminal.Screen != nil {
			terminal.Screen.Reset()
			terminal.Screen.Feed(info.Content)
		}
	}
}

// ReplaceTerminals applies the v0.5 full snapshot.
func (s *SessionState) ReplaceTerminals(infos []acp.TerminalInfo) {
	seen := make(map[string]bool, len(infos))
	for _, info := range infos {
		if info.TerminalID != "" {
			seen[info.TerminalID] = true
			old := s.terminals[info.TerminalID]
			s.ApplyTerminalInfo(info)
			if old != nil { /* snapshot content replaces prior transcript */
			}
		}
	}
	for id := range s.terminals {
		if !seen[id] {
			s.RemoveTerminal(id)
		}
	}
}

func (s *SessionState) RemoveTerminal(id string) {
	delete(s.terminals, id)
	for i, v := range s.terminalOrder {
		if v == id {
			s.terminalOrder = append(s.terminalOrder[:i], s.terminalOrder[i+1:]...)
			break
		}
	}
	delete(s.timelineIndex, "terminal:"+id)
	for i, item := range s.Timeline {
		if item.Key == "terminal:"+id {
			s.Timeline = append(s.Timeline[:i], s.Timeline[i+1:]...)
			break
		}
	}
}

// Terminals returns shells in stable creation order.
func (s *SessionState) Terminals() []*TerminalState {
	out := make([]*TerminalState, 0, len(s.terminalOrder))
	for _, id := range s.terminalOrder {
		if t := s.terminals[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

func (s *SessionState) Terminal(id string) *TerminalState { return s.terminals[id] }

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
