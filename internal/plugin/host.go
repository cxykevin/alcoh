package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/input"
	pbv1 "github.com/cxykevin/alcoh/pb/plugin/v1"
	"google.golang.org/protobuf/proto"
)

// initializeTimeout 是 initialize 握手的最长等待时间。
const initializeTimeout = 3 * time.Second

// UIEventKind 是插件 → 宿主事件的类型（由 app 事件循环应用）。
type UIEventKind int

const (
	// EventNotify 展示底部提示（信息/成功/错误）。
	EventNotify UIEventKind = iota
	// EventStatus 更新状态栏插件文本。
	EventStatus
	// EventFailed 插件进程终止或被禁用（仅上报一次）。
	EventFailed
)

// UIEvent 是插件 → 宿主的 UI 事件。
type UIEvent struct {
	Kind   UIEventKind
	Plugin string // 来源插件名
	Text   string // notify 正文 / status 文本 / 失败原因
	IsErr  bool   // notify 为错误级别
}

// Plugin 是单个已握手（或失败）的插件实例。
type Plugin struct {
	cfg    pluginConfig
	tr     *transport
	info   *pbv1.InitializeResult
	fail   error // 非 nil 表示插件不可用（启动失败 / 中途退出）
	events chan acp.IncomingMessage
	done   chan struct{} // 插件入站请求转发 goroutine 退出信号
}

// Host 管理全部插件进程并分发 hooks。零值可用（不加载任何插件）。
type Host struct {
	mu      sync.Mutex
	plugins []*Plugin

	// byCommand 是斜杠命令名 → 插件索引。
	byCommand map[string]*Plugin
	// byKey 是按键签名 → 声明了该绑定的插件。
	byKey map[string][]*Plugin

	events chan UIEvent

	hostName    string
	hostVersion string
	workdir     string
}

// NewHost 按配置创建插件宿主。entries 为 nil 时返回可用但空的宿主。
func NewHost(entries []config.PluginConfig) *Host {
	h := &Host{
		byCommand: make(map[string]*Plugin),
		byKey:     make(map[string][]*Plugin),
		events:    make(chan UIEvent, 64),
	}
	for _, e := range entries {
		if e.Disabled || strings.TrimSpace(e.Command) == "" {
			continue
		}
		h.plugins = append(h.plugins, &Plugin{
			cfg: pluginConfig{
				Name: e.Name, Command: e.Command, Args: e.Args, Dir: e.Dir, Env: e.Env,
			},
			events: make(chan acp.IncomingMessage, 16),
			done:   make(chan struct{}),
		})
	}
	return h
}

// SetHostInfo 设置宿主标识（握手时发送给插件）。
func (h *Host) SetHostInfo(name, version, workdir string) {
	h.hostName, h.hostVersion, h.workdir = name, version, workdir
}

// Events 返回插件 → 宿主事件通道，由 app 事件循环消费。
func (h *Host) Events() <-chan UIEvent { return h.events }

// Start 启动全部插件并完成 initialize 握手。启动失败的插件被标记不可用并
// 上报事件，不影响其它插件与主程序。可重复调用（幂等）。
func (h *Host) Start(parent context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.plugins) == 0 {
		return nil
	}
	var firstErr error
	for _, p := range h.plugins {
		if p.tr != nil || p.fail != nil {
			continue
		}
		tr, err := startTransport(parent, p.cfg, func(m acp.IncomingMessage) { h.dispatch(p, m) },
			func(err error) { h.onPluginError(p, err) })
		if err != nil {
			p.fail = err
			h.reportEvent(UIEvent{Kind: EventFailed, Plugin: h.displayNameLocked(p), Text: fmt.Sprintf("插件 %s 启动失败: %v", p.cfg.Name, err), IsErr: true})
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.tr = tr
		go h.forwardIncoming(p)

		ctx, cancel := context.WithTimeout(parent, initializeTimeout)
		info, err := h.initialize(p, ctx)
		cancel()
		if err != nil {
			p.fail = err
			h.reportEvent(UIEvent{Kind: EventFailed, Plugin: h.displayNameLocked(p), Text: fmt.Sprintf("插件 %s 握手失败: %v", p.cfg.Name, err), IsErr: true})
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.info = info
		h.index(p)
	}
	return firstErr
}

// index 把已握手插件的命令与按键绑定加入查找表（命令冲突时后者被忽略）。
func (h *Host) index(p *Plugin) {
	info := p.info
	if info == nil {
		return
	}
	for _, c := range info.Commands {
		name := c.Name
		if name != "" && name[0] != '/' {
			name = "/" + name
		}
		if name == "" || name == "/" {
			continue
		}
		if _, dup := h.byCommand[name]; !dup {
			h.byCommand[name] = p
		}
	}
	for _, kb := range info.KeyBindings {
		sig := keyBindingSig(kb)
		if sig == "" {
			continue
		}
		h.byKey[sig] = append(h.byKey[sig], p)
	}
}

// initialize 向插件发送 initialize 请求并解析应答。调用方已持有 h.mu。
func (h *Host) initialize(p *Plugin, ctx context.Context) (*pbv1.InitializeResult, error) {
	req := &pbv1.InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		HostName:        h.hostName, HostVersion: h.hostVersion, Cwd: h.workdir,
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal initialize: %w", err)
	}
	raw, err := p.tr.request(ctx, "initialize", data)
	if err != nil {
		return nil, err
	}
	var res pbv1.InitializeResult
	if err := proto.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode initialize result: %w", err)
	}
	if res.Name == "" {
		res.Name = p.cfg.Name
	}
	if res.Name == "" {
		res.Name = "plugin"
	}
	return &res, nil
}

// displayName 返回用于界面展示的插件名（读取受 h.mu 保护，可与 Start 握手
// 并发安全）。
func (h *Host) displayName(p *Plugin) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.displayNameLocked(p)
}

// displayNameLocked 在调用方已持有 h.mu 时返回插件展示名。
func (h *Host) displayNameLocked(p *Plugin) string {
	if p.info != nil && p.info.Name != "" {
		return p.info.Name
	}
	return p.cfg.Name
}

// forwardIncoming 从 transport 的入站 handler 转发通道读取插件请求，
// 解析并回复（notify/status/log），UI 事件推给 Host.events。
func (h *Host) forwardIncoming(p *Plugin) {
	defer close(p.done)
	for {
		select {
		case <-p.tr.doneCh():
			return
		case m := <-p.events:
			h.handleIncoming(p, m)
		}
	}
}

// dispatch 是 transport 的入站 handler（readLoop goroutine 内，不得阻塞）：
// 直接推入插件本地队列，由 forwardIncoming goroutine 处理。
func (h *Host) dispatch(p *Plugin, m acp.IncomingMessage) {
	if m.Request == nil {
		return // 插件不应向宿主发 notification；忽略。
	}
	select {
	case p.events <- m:
	default:
		_ = p.tr.respondError(m.Request.ID, acp.RPCError{Code: -32600, Message: "host event queue full"})
	}
}

// handleIncoming 处理插件 → 宿主的 JSON-RPC 请求。
func (h *Host) handleIncoming(p *Plugin, m acp.IncomingMessage) {
	req := m.Request
	if req == nil {
		return
	}
	switch req.Method {
	case "notify":
		var in pbv1.NotifyRequest
		if err := decodeParams(req.Params, &in); err != nil {
			h.rpcErr(p, req, -32602, err)
			return
		}
		h.reportEvent(UIEvent{Kind: EventNotify, Plugin: h.displayName(p), Text: in.Text, IsErr: in.Kind == "error"})
	case "status":
		var in pbv1.StatusRequest
		if err := decodeParams(req.Params, &in); err != nil {
			h.rpcErr(p, req, -32602, err)
			return
		}
		h.reportEvent(UIEvent{Kind: EventStatus, Plugin: h.displayName(p), Text: in.Text})
	case "log":
		var in pbv1.LogRequest
		if err := decodeParams(req.Params, &in); err != nil {
			h.rpcErr(p, req, -32602, err)
			return
		}
		fmt.Fprintf(os.Stderr, "[plugin:%s] %s: %s\n", h.displayName(p), in.Level, in.Text)
	default:
		h.rpcErr(p, req, -32601, fmt.Errorf("method not found: %s", req.Method))
		return
	}
	_ = p.tr.respond(req.ID, nil)
}

func (h *Host) rpcErr(p *Plugin, req *acp.RPCRequest, code int, err error) {
	_ = p.tr.respondError(req.ID, acp.RPCError{Code: code, Message: err.Error()})
}

// reportEvent 非阻塞推送事件到宿主事件通道。
func (h *Host) reportEvent(ev UIEvent) {
	select {
	case h.events <- ev:
	default:
	}
}

// onPluginError 是 transport 非预期终止的回调（transport goroutine 内）。
func (h *Host) onPluginError(p *Plugin, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.fail != nil {
		return
	}
	p.fail = err
	h.reportEvent(UIEvent{Kind: EventFailed, Plugin: h.displayNameLocked(p), Text: fmt.Sprintf("插件 %s 已终止: %v", h.displayNameLocked(p), err), IsErr: true})
}

// available 返回可用（已握手、未失败、transport 存活）的插件列表。
func (h *Host) available() []*Plugin {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*Plugin
	for _, p := range h.plugins {
		if p.tr != nil && p.fail == nil {
			out = append(out, p)
		}
	}
	return out
}

// PromptHook 依次调用各插件的 prompt hook；返回最终文本、是否被拦截与拦截
// 原因。任一插件返回 BLOCK 即停止并返回 blocked=true。
func (h *Host) PromptHook(ctx context.Context, sessionID, text string) (string, bool, string, error) {
	out := text
	for _, p := range h.available() {
		if p.info == nil || !p.info.HooksPrompt {
			continue
		}
		req := &pbv1.PromptRequest{SessionId: sessionID, Prompt: out}
		data, err := proto.Marshal(req)
		if err != nil {
			return out, false, "", err
		}
		raw, err := p.tr.request(ctx, "hook/prompt", data)
		if err != nil {
			h.onHookError(p, err)
			continue
		}
		var res pbv1.PromptResult
		if err := proto.Unmarshal(raw, &res); err != nil {
			h.onHookError(p, err)
			continue
		}
		switch res.Action {
		case pbv1.PromptResult_ACTION_BLOCK:
			return out, true, res.Reason, nil
		case pbv1.PromptResult_ACTION_REWRITE:
			if res.Rewritten != "" {
				out = res.Rewritten
			}
		default:
			// ALLOW / 未指定：原样。
		}
	}
	return out, false, "", nil
}

// KeyHook 依次调用声明了该按键的插件的 key hook；任一返回 handled 即消费。
func (h *Host) KeyHook(ctx context.Context, ke input.KeyEvent, view, modal string, inputEmpty bool, input string) bool {
	plugins := h.matchedPlugins(ke)
	if len(plugins) == 0 {
		return false
	}
	req := &pbv1.KeyRequest{
		Type: keyTypeName(ke.Type), Rune: uint32(ke.Rune),
		Ctrl: ke.IsCtrl(), Alt: ke.IsAlt(), Shift: ke.IsShift(),
		View: view, Modal: modal, InputEmpty: inputEmpty, Input: input,
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return false
	}
	for _, p := range plugins {
		raw, err := p.tr.request(ctx, "hook/key", data)
		if err != nil {
			h.onHookError(p, err)
			continue
		}
		var res pbv1.KeyResult
		if err := proto.Unmarshal(raw, &res); err != nil {
			h.onHookError(p, err)
			continue
		}
		if res.Handled {
			return true
		}
	}
	return false
}

func (h *Host) onHookError(p *Plugin, err error) {
	h.mu.Lock()
	if p.fail == nil {
		p.fail = err
		h.reportEvent(UIEvent{Kind: EventFailed, Plugin: h.displayNameLocked(p), Text: fmt.Sprintf("插件 %s hook 调用失败，已停用: %v", h.displayNameLocked(p), err), IsErr: true})
	}
	h.mu.Unlock()
}

// NotifyUpdate 把 ACP 事件以 notification 广播给订阅的插件（异步、不等待）。
func (h *Host) NotifyUpdate(ev acp.Event) {
	req := updateRequest(ev)
	if req == nil {
		return
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return
	}
	for _, p := range h.available() {
		if p.info == nil || !p.info.HooksUpdate {
			continue
		}
		_ = p.tr.notify("hook/update", data)
	}
}

// RunCommand 调用注册了该命令的插件；返回命令是否被执行。
func (h *Host) RunCommand(ctx context.Context, name, args, sessionID string) bool {
	h.mu.Lock()
	p := h.byCommand[name]
	if p != nil && p.fail != nil {
		p = nil
	}
	h.mu.Unlock()
	if p == nil {
		return false
	}
	req := &pbv1.CommandRequest{Command: name, Args: args, SessionId: sessionID}
	data, err := proto.Marshal(req)
	if err != nil {
		return false
	}
	raw, err := p.tr.request(ctx, "command/run", data)
	if err != nil {
		h.onHookError(p, err)
		return false
	}
	var res pbv1.CommandResult
	if err := proto.Unmarshal(raw, &res); err != nil {
		h.onHookError(p, err)
		return false
	}
	return res.Handled
}

// CommandNames 返回全部插件注册的斜杠命令（已按 / 规范化）。
func (h *Host) CommandNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.byCommand))
	for name := range h.byCommand {
		out = append(out, name)
	}
	return out
}

// CommandInfo 返回命令的描述与参数提示。
func (h *Host) CommandInfo(name string) (desc, argsHint string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.byCommand[name]
	if p == nil || p.info == nil {
		return "", "", false
	}
	for _, c := range p.info.Commands {
		cname := c.Name
		if cname != "" && cname[0] != '/' {
			cname = "/" + cname
		}
		if cname == name {
			return c.Description, c.ArgsHint, true
		}
	}
	return "", "", false
}

// WantsKey 报告是否有插件声明了该按键绑定（用于避免无谓 IPC）。
func (h *Host) WantsKey(ke input.KeyEvent) bool {
	return len(h.matchedPlugins(ke)) > 0
}

// matchedPlugins 返回声明了该按键绑定的插件（按启动顺序）。
func (h *Host) matchedPlugins(ke input.KeyEvent) []*Plugin {
	sig := keyEventSig(ke)
	if sig == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	plugins := h.byKey[sig]
	out := make([]*Plugin, 0, len(plugins))
	for _, p := range plugins {
		if p.tr != nil && p.fail == nil {
			out = append(out, p)
		}
	}
	return out
}

// Close 依次通知并回收全部插件进程。
func (h *Host) Close() {
	h.mu.Lock()
	plugins := append([]*Plugin(nil), h.plugins...)
	h.mu.Unlock()
	for _, p := range plugins {
		if p.tr == nil {
			continue
		}
		_ = p.tr.shutdownGraceful(defaultShutdownTimeout)
		select {
		case <-p.done:
		default:
		}
	}
}

// updateRequest 把 ACP 事件序列化为 UpdateRequest；无法序列化的事件返回 nil。
func updateRequest(ev acp.Event) *pbv1.UpdateRequest {
	if ev == nil {
		return nil
	}
	// 内部事件类型按字段构造 JSON 快照。
	switch e := ev.(type) {
	case *acp.MessageChunkEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "message_chunk", Payload: mustJSON(map[string]any{"sessionId": e.SessionID, "messageId": e.MessageID, "isUser": e.IsUser, "isThought": e.IsThought, "text": e.Text})}
	case *acp.MessageUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "message_update", Payload: mustJSON(map[string]any{"sessionId": e.SessionID, "isUser": e.IsUser, "isThought": e.IsThought})}
	case *acp.ToolCallUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "tool_call_update", Payload: e.RawInput}
	case *acp.PlanUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "plan_update", Payload: mustJSON(map[string]any{"sessionId": e.SessionID})}
	case *acp.PermissionRequestEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "request_permission", Payload: mustJSON(map[string]any{"sessionId": e.SessionID, "requestId": e.Request.RequestID})}
	case *acp.StateChangeEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "state_update", Payload: mustJSON(map[string]any{"sessionId": e.SessionID, "state": string(e.State)})}
	case *acp.UsageUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "usage_update", Payload: mustJSON(map[string]any{"sessionId": e.SessionID, "used": e.Used, "size": e.Size})}
	case *acp.CommandsUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "commands_update", Payload: e.Raw}
	case *acp.ConfigOptionUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "config_option_update", Payload: e.Raw}
	case *acp.SessionInfoUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "session_info_update", Payload: e.Raw}
	case *acp.TerminalUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "terminal_update", Payload: e.Raw}
	case *acp.UnknownSessionUpdateEvent:
		return &pbv1.UpdateRequest{SessionId: e.SessionID, Method: "unknown_" + e.Discriminator, Payload: e.Raw}
	case *acp.NewSessionEvent:
		return &pbv1.UpdateRequest{SessionId: e.Session.ID(), Method: "session_new", Payload: mustJSON(map[string]any{"sessionId": e.Session.ID()})}
	default:
		return nil
	}
}

// mustJSON 尽力把值编码为 JSON。
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// keyTypeName 把 input.KeyType 映射为协议按键类型名。
func keyTypeName(t input.KeyType) string {
	switch t {
	case input.KeyRune:
		return "rune"
	case input.KeyEnter:
		return "enter"
	case input.KeyTab:
		return "tab"
	case input.KeyBackspace:
		return "backspace"
	case input.KeyDelete:
		return "delete"
	case input.KeyEsc:
		return "esc"
	case input.KeyUp:
		return "up"
	case input.KeyDown:
		return "down"
	case input.KeyLeft:
		return "left"
	case input.KeyRight:
		return "right"
	case input.KeyHome:
		return "home"
	case input.KeyEnd:
		return "end"
	case input.KeyPageUp:
		return "pageup"
	case input.KeyPageDown:
		return "pagedown"
	case input.KeyInsert:
		return "insert"
	case input.KeyF1:
		return "f1"
	case input.KeyF2:
		return "f2"
	case input.KeyF3:
		return "f3"
	case input.KeyF4:
		return "f4"
	case input.KeyF5:
		return "f5"
	case input.KeyF6:
		return "f6"
	case input.KeyF7:
		return "f7"
	case input.KeyF8:
		return "f8"
	case input.KeyF9:
		return "f9"
	case input.KeyF10:
		return "f10"
	case input.KeyF11:
		return "f11"
	case input.KeyF12:
		return "f12"
	default:
		return ""
	}
}

// keyBindingSig 把协议 KeyBinding 规范化为签名；非法绑定返回 ""。
func keyBindingSig(kb *pbv1.KeyBinding) string {
	if kb == nil || kb.Type == "" {
		return ""
	}
	return sig(strings.ToLower(kb.Type), rune(kb.Rune), kb.Ctrl, kb.Alt, kb.Shift)
}

// keyEventSig 把 input.KeyEvent 规范化为签名。
func keyEventSig(ke input.KeyEvent) string {
	typ := keyTypeName(ke.Type)
	if typ == "" {
		return ""
	}
	return sig(typ, ke.Rune, ke.IsCtrl(), ke.IsAlt(), ke.IsShift())
}

func sig(typ string, r rune, ctrl, alt, shift bool) string {
	return fmt.Sprintf("%s|%d|%t|%t|%t", typ, r, ctrl, alt, shift)
}
