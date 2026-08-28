// Package app 负责组装终端、后端、模型与视图，运行主事件循环。
package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/plugin"
	"github.com/cxykevin/alcoh/internal/provider"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/term"
	"github.com/cxykevin/alcoh/internal/view"
	"github.com/cxykevin/alcoh/internal/widget"
)

// App 是 TUI 应用主体。
type App struct {
	term    term.Terminal
	backend acp.Backend
	model   *model.AppModel
	view    *view.AppView

	front *renderer.Buffer // 已显示帧
	back  *renderer.Buffer // 绘制目标
	mode  renderer.ColorMode

	// modelMu 保护模型状态的并发访问。生产代码中模型由事件循环单 goroutine
	// 独占写入（Run 循环内在 modelMu.Lock 保护区内）；测试需要在应用运行期间
	// 轮询模型状态时，经 App.snapshot 并发读取，避免 data race。
	modelMu sync.RWMutex

	spinFrame int
	sess      acp.Session // 当前活动 backend 会话
	// preSession 是主页预创建会话的 backend 句柄。它只在主页存活：用户恢复旧
	// 会话、新建会话或程序退出时被删除（见 ensurePreSession / discardPreSession /
	// deletePreSessionAtExit）。主页时 sess 指向它，使 /effort 与 /model 可写。
	preSession acp.Session
	// discardPre 为 true 表示 discardPreSession 已在后台发起删除预创建会话。其
	// 删除响应不应触发重建（用户正在进入真实会话）；只有用户主动按 d 删除会话
	// 后仍停留在主页时才重建。
	discardPre    bool
	workdir       string // 新建会话的工作目录（绝对路径；空表示未指定）
	initialRender bool
	commands      chan commandResult
	runCtx        context.Context
	cancelRun     context.CancelFunc
	commandWG     sync.WaitGroup
	sessionOpID   uint64
	// Session list pagination state; accessed by the event loop and guarded by modelMu.
	sessionNextCursor string
	sessionLoading    bool
	sessionGeneration uint64

	// serverCfgFocus 是服务端配置编辑器新增项后的重定向目标路径：新增写回
	// （config/set）成功后据此触发整配置重载（config/get），重载完成后再经
	// SetServerConfig 重建的配置树 Focus 到该路径。非 nil 表示有待重载定位。
	serverCfgFocus []string

	// plugins 是前端插件宿主（本地子进程 + JSON-RPC + protobuf hooks）。
	// pluginEvents 是插件 → 宿主事件通道，由 Run 事件循环应用。
	plugins      *plugin.Host
	pluginEvents <-chan plugin.UIEvent

	// cfgGetSeq 是 config/get 请求序号（仅事件循环主 goroutine 访问）。
	// 每次发起 get 递增；结果带响应序号，晚回的旧序号结果被丢弃，避免覆盖
	// 更新的配置（如新增写回后触发的重载被打开时较早发出的 get 晚回覆盖）。
	cfgGetSeq uint64

	// cfgWriteBusy/cfgWriteQueue 串行化服务端配置写回（仅主循环访问）。
	// 编辑即自动保存会连续产生多个 config/set（如新增的完整对象 patch 之后
	// 紧接着字段编辑）；并发发送乱序到达时，旧的完整对象 patch 可能覆盖新
	// 编辑的值。因此写回按发出顺序逐个应用：仅当前一个完成后才发送队列中
	// 的下一个，全部完成后才触发新增后的整配置重载（确保 get 读到最终值）。
	cfgWriteBusy  bool
	cfgWriteQueue []json.RawMessage

	// onboardingEnabled 为 true 表示本次启动未指定 backend 参数（走默认 alkaid0
	// WebSocket 连接）：若服务端声明 alkaid0 能力且配置里没有任何模型，启动即
	// 进入新手引导（与 /connect 向导同义，见 maybeStartOnboarding）。
	onboardingEnabled bool
	// initialPrompt 是 One Shot 模式的启动消息：启动流程完成后自动复用预创建
	// 会话或新建会话并发送该消息，无需手动输入（见 sendInitialPrompt）。
	initialPrompt string
	// firstSessionOpID 是"用户第一个会话"的 create 请求序号：创建成功后把新手
	// 引导里选的 effort 应用到该会话（恢复旧会话不应用）。
	firstSessionOpID uint64

	lastCtrlCAt time.Time
}

type commandKind uint8

const (
	commandSession commandKind = iota
	commandSessionAction
	commandSessionDelete
	commandConfigGet
	commandConfigSet
	commandConnectFetch
	commandConnectSubmit
	commandTerminalStop
)

type commandResult struct {
	kind       commandKind
	session    acp.Session
	sessionID  string
	opID       uint64
	config     json.RawMessage
	page       *sessionPageResult
	cfgSeq     uint64 // config/get 请求序号，用于丢弃乱序晚回的旧结果
	models     []provider.Model
	terminalID string
	err        error
}

type sessionPageResult struct {
	page       acp.SessionPage
	appendPage bool
}

// New 创建使用默认本地配置的 App。
func New(t term.Terminal, b acp.Backend) *App {
	return NewWithConfig(t, b, config.Defaults())
}

// NewWithConfig 创建带有已加载本地配置的 App。
func NewWithConfig(t term.Terminal, b acp.Backend, values config.Values) *App {
	m := model.New()
	m.Settings = values
	a := &App{
		term:     t,
		backend:  b,
		model:    m,
		view:     view.NewAppView(renderer.DefaultTheme()),
		mode:     colorMode(values.ColorMode),
		commands: make(chan commandResult, 32),
	}
	a.plugins = plugin.NewHost(values.Plugins)
	a.pluginEvents = a.plugins.Events()
	return a
}

// SetWorkdir 设置新建会话使用的默认工作目录（启动时解析后的绝对路径）。
// 未设置时回退到 "."，保持历史行为。
func (a *App) SetWorkdir(dir string) {
	a.workdir = dir
}

// SetOnboardingEnabled 启停新手引导触发判断。main 在未指定 backend 参数
// （走默认 alkaid0 连接）时置 true。
func (a *App) SetOnboardingEnabled(v bool) { a.onboardingEnabled = v }

// SetInitialPrompt 设置 One Shot 模式的启动消息：启动完成后自动进入会话视图
// 并发送该消息。空串表示普通交互启动。
func (a *App) SetInitialPrompt(text string) { a.initialPrompt = text }

// applyFirstSessionEffort 把新手引导里选择的 effort 应用到"用户第一个会话"：
// 经 session/set_config_option 写 thought_level，随后清空本地字段——只对首个
// 会话生效一次，之后由 /effort 命令正常管理。不依赖该会话的 config_option_update
// 是否已应用（引导后首个会话可能刚创建，config 事件尚未到达）。
func (a *App) applyFirstSessionEffort(s acp.Session) {
	effort := a.model.Settings.OnboardingEffort
	if effort == "" || s == nil {
		return
	}
	a.model.Settings.OnboardingEffort = ""
	_ = config.Save(a.model.Settings)
	a.startCommand(commandResult{kind: commandSessionAction, sessionID: s.ID()}, func(ctx context.Context) (acp.Session, error) {
		return nil, s.SetConfigOption(ctx, "thought_level", "select", effort)
	})
}

// sessionCWD 返回新建会话的工作目录：优先使用启动时传入的绝对目录，
// 不因 session/list 失败而回滚到进程当前目录。
func (a *App) sessionCWD() string {
	if a.workdir != "" {
		return a.workdir
	}
	return "."
}

// ensurePreSession 加锁创建主页预创建会话（若尚不存在且服务端支持 session.delete）。
// 供启动流程（Run）调用；调用方未持有 modelMu。
func (a *App) ensurePreSession() {
	a.modelMu.Lock()
	defer a.modelMu.Unlock()
	a.ensurePreSessionLocked()
}

// ensurePreSessionLocked 在调用方已持有 modelMu 时创建主页预创建会话。同步创建
// （一次网络往返，带 3s 超时），因此主页 /effort 与 /model 的可用性在返回时确定。
// 创建失败不致命：仅主页这两个命令暂不可用。会话进入视图后（Active 非 nil）不
// 再预创建——preSession 只在主页存在。
func (a *App) ensurePreSessionLocked() {
	if a.preSession != nil || a.model.PreSession != nil {
		return
	}
	if a.model.Active != nil || !a.model.SupportsSessionDelete() {
		return
	}
	ctx, cancel := context.WithTimeout(a.runCtx, 3*time.Second)
	s, err := a.backend.NewSession(ctx, a.sessionCWD())
	cancel()
	if err != nil {
		return
	}
	a.preSession = s
	// 主页时 sess 指向预创建会话，使 /effort 与 /model 能经 session/set_config_option
	// 写入；进入真实会话后 applyCommandResult 会覆盖为活动会话句柄。
	a.sess = s
	a.model.SetPreSession(s.ID(), s.Title())
}

// discardPreSession 在进入真实会话（新建/恢复）时丢弃主页预创建的空会话：本地状态
// 立即清空（/effort 与 /model 随即从命令面板消失），删除在后台异步执行。调用方已
// 持有 modelMu。
func (a *App) discardPreSession() {
	var id string
	if a.preSession != nil {
		id = a.preSession.ID()
		a.preSession = nil
	} else if a.model.PreSession != nil {
		id = a.model.PreSession.ID
	}
	if id == "" {
		return
	}
	if a.sess != nil && a.sess.ID() == id {
		a.sess = nil
	}
	a.model.ClearPreSession()
	// 若服务端曾把预创建会话列入会话列表，一并移除。
	a.model.RemoveSession(id)
	if a.model.SupportsSessionDelete() {
		// 标记此次删除是进入真实会话前的清理：删除响应不得重建预创建会话。
		a.discardPre = true
		a.startCommand(commandResult{kind: commandSessionDelete, sessionID: id}, func(ctx context.Context) (acp.Session, error) {
			return nil, a.backend.DeleteSession(ctx, id)
		})
	}
}

// deletePreSessionAtExit 在程序退出时删除主页预创建的空会话，避免服务端残留，
// 并清空本地 PreSession 状态。带独立超时，不依赖 Run 的 runCtx（它已在退出时被取消）。
func (a *App) deletePreSessionAtExit() {
	a.modelMu.Lock()
	var id string
	if a.preSession != nil {
		id = a.preSession.ID()
		a.preSession = nil
	} else if a.model.PreSession != nil {
		id = a.model.PreSession.ID
	}
	support := a.model.SupportsSessionDelete()
	if id != "" {
		// 退出即丢弃预创建会话：清空本地状态；若服务端曾把它列入会话列表一并移除。
		a.model.ClearPreSession()
		a.model.RemoveSession(id)
	}
	a.modelMu.Unlock()
	if id == "" || !support {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = a.backend.DeleteSession(ctx, id)
}

// goHome 返回主页并确保主页预创建会话存在（/effort 与 /model 在主页命令面板
// 继续可用）。调用方必须已持有 modelMu（homeKey / tryLocalSlashCommand）。
func (a *App) goHome() {
	// 回到主页时先前的 discardPreSession 清理标记已过时（即将创建新的预创建
	// 会话），复位以免残留的删除回调误判而跳过重建。
	a.discardPre = false
	a.model.GoHome()
	a.ensurePreSessionLocked()
}

// maybeStartOnboarding 在满足触发条件时进入新手引导，否则按正常主页流程
// 创建主页预创建会话。触发条件：启动未指定 backend 参数（默认连接 alkaid0）、
// 服务端声明 alkaid0 能力、且 config/get 返回的配置里没有任何模型。config/get
// 失败或服务端已有模型时回退正常主页（拉取失败仅提示，不阻塞启动）。
//
// 引导与 /connect 向导同义：直接打开 ConnectState（FromOnboarding=true）走
// 服务商模板 → 填 key → 拉取模型 → 写入配置；完成后继续引导剩余步骤（选推理
// 强度 → 操作教学）。引导期间不创建主页预创建会话；引导结束（完成/跳过）后
// 经 goHome 创建。
func (a *App) maybeStartOnboarding() {
	a.modelMu.RLock()
	onboarding := a.onboardingEnabled && a.model.SupportsAlkaid0()
	a.modelMu.RUnlock()
	if !onboarding {
		a.ensurePreSession()
		return
	}
	ctx, cancel := context.WithTimeout(a.runCtx, 3*time.Second)
	cfg, err := a.backend.GetConfig(ctx)
	cancel()
	a.modelMu.Lock()
	defer a.modelMu.Unlock()
	if err != nil {
		a.model.ShowError(i18n.T("获取服务端配置失败: %s", err.Error()))
		a.ensurePreSessionLocked()
		return
	}
	if model.HasConfiguredModels(cfg) {
		a.ensurePreSessionLocked()
		return
	}
	// 服务端尚无模型：进入 /connect 向导（新手引导与其同义）。
	a.model.OpenConnect()
	if a.model.Connect != nil {
		a.model.Connect.FromOnboarding = true
	}
}

// sendInitialPrompt 在启动流程完成后发送 One Shot 模式的启动消息：优先复用主页
// 预创建会话（不删除、不新建，与主页输入 prompt 回车同一条路径），否则新建会话
// 并把消息记为 PendingInitialPrompt——会话建立后由 applyCommandResult 自动发送
// （见 applyCommandResult 中 PendingInitialPrompt 分支）。新手引导进行中时不发送。
func (a *App) sendInitialPrompt() {
	if a.initialPrompt == "" {
		return
	}
	a.modelMu.Lock()
	defer a.modelMu.Unlock()
	if a.model.Onboarding != nil || a.model.Connect != nil {
		return
	}
	if a.usePreSession(a.initialPrompt) {
		return
	}
	a.model.PendingInitialPrompt = a.initialPrompt
	a.createSession()
}

// closeServerEditor 关闭 /server 配置编辑器。
func (a *App) closeServerEditor() {
	a.model.CloseServer()
}

// modelSnapshot 是测试在应用运行期间轮询模型状态所需的字段快照。事件循环在
// modelMu 写锁保护下独占写入模型；测试经 App.snapshot 在读锁内拷贝所需标量，
// 避免与事件循环并发读写的 data race。模型断言优先放在 Run 退出后直接读 a.model。
type modelSnapshot struct {
	Quitting      bool
	Modal         model.ModalKind
	HasActive     bool
	ActiveState   acp.SessionState
	ActiveRunning bool
	ActiveScroll  int
	FollowBottom  bool
	ServerCfg     bool   // 服务端配置树已加载（ServerCfg != nil）
	ServerSaving  bool   // 服务端配置写回/全量重载进行中（编辑被阻塞）
	ServerCurKey  string // 服务端配置编辑器当前页 Key（根为空串）
	ServerSelKey  string // 当前页选中行节点 Key（无选中行或非对象键时为空串）
	BodyScroll    int    // 最近一帧正文滚动偏移
	ThoughtRow    int    // 最近一帧首个思考标题行（contentY），无则 -1
}

// snapshot 返回当前模型状态快照，供测试在应用运行期间安全轮询。
func (a *App) snapshot() modelSnapshot {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	s := modelSnapshot{
		Quitting:   a.model.Quitting,
		Modal:      a.model.Modal,
		BodyScroll: a.view.BodyScroll,
		ThoughtRow: firstThoughtRow(a.view.BodyToggles),
	}
	if a.model.ServerCfg != nil {
		s.ServerCfg = true
		s.ServerSaving = a.model.ServerCfg.Saving
		if c := a.model.ServerCfg.Current(); c != nil {
			s.ServerCurKey = c.Key
		}
		if n := a.model.ServerCfg.SelectedNode(); n != nil {
			s.ServerSelKey = n.Key
		}
	}
	if a.model.Active != nil {
		s.HasActive = true
		s.ActiveState = a.model.Active.State
		s.ActiveRunning = a.model.Active.Running()
		s.ActiveScroll = a.model.Active.Scroll
		s.FollowBottom = a.model.Active.FollowBottom
	}
	return s
}

// firstThoughtRow 返回 Toggles 中第一个思考标题行的 contentY；无则 -1。
func firstThoughtRow(toggles map[int]view.ToggleRef) int {
	row := -1
	for r, ref := range toggles {
		if ref.Kind == view.ToggleThought && (row < 0 || r < row) {
			row = r
		}
	}
	return row
}

func colorMode(value string) renderer.ColorMode {
	switch value {
	case "mono":
		return renderer.ColorModeMono
	case "16":
		return renderer.ColorMode16
	case "256":
		return renderer.ColorMode256
	case "truecolor":
		return renderer.ColorModeTrueColor
	default:
		return renderer.DetectColorMode()
	}
}

// Run 启动事件循环，直到用户退出。结束时恢复终端。
func (a *App) Run() error {
	if err := a.term.EnterRaw(); err != nil {
		return err
	}
	a.runCtx, a.cancelRun = context.WithCancel(context.Background())
	defer func() {
		a.cancelRun()
		// 程序退出时删除主页预创建的空会话，避免服务端残留。必须在 backend.Close()
		// 之前执行（删除需要活动 transport），且不依赖任何 in-flight 命令。
		a.deletePreSessionAtExit()
		// 通知并回收全部插件进程（shutdown notification + 超时强杀）。
		a.plugins.Close()
		_ = a.backend.Close()
		a.commandWG.Wait()
		_ = a.term.ExitRaw()
	}()

	// 先启动插件（握手带 3s 超时），再初始化后端：插件命令随即进入命令面板。
	a.startPlugins()

	if err := a.backend.Initialize(a.runCtx); err != nil {
		return err
	}
	// 记录握手得到的服务端标识与能力声明，供按能力门控 /server 等命令使用。
	// refreshSessions 内部自行加锁。
	a.modelMu.Lock()
	a.model.SetAgentInfo(a.backend.AgentInfo(), a.backend.AgentCapabilities())
	a.modelMu.Unlock()
	// Initial refresh is asynchronous; the event loop applies its page result.
	a.refreshSessions()
	// 判断是否进入新手引导；否则按正常主页流程预创建一个会话（仅当服务端支持
	// session.delete）。预创建会话承载 agent 广播的 config，使 /effort 与 /model
	// 在主页命令面板可用；用户新建/恢复会话或程序退出时该空会话被删除。
	a.maybeStartOnboarding()
	// One Shot 模式：自动进入会话视图并发送启动消息。
	a.sendInitialPrompt()

	w, h := a.term.Size()
	a.resetBuffers(w, h)
	a.render()

	eventsCh := a.backend.Events()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		// hasAnimation 只读模型；模型写入仅在下方持锁区执行，等待 select 期间
		// 无写入，因此此处读取不受并发影响。
		needsRender := false
		anim := a.hasAnimation()

		select {
		case ev, ok := <-a.term.Events():
			if !ok {
				return nil
			}
			a.modelMu.Lock()
			a.handleTermEvent(ev)
			a.modelMu.Unlock()
			needsRender = true
		case ev, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				a.modelMu.Lock()
				if a.model.Error == "" {
					a.model.ShowError(i18n.T("ACP backend 已关闭"))
				}
				a.modelMu.Unlock()
				needsRender = true
			} else {
				a.modelMu.Lock()
				a.model.ApplyEvent(ev)
				a.modelMu.Unlock()
				// 事件观察 hook：异步广播给订阅的插件（不等待响应）。
				a.plugins.NotifyUpdate(ev)
				needsRender = true
			}
		case ev, ok := <-a.pluginEvents:
			if !ok {
				a.pluginEvents = nil
			} else {
				a.modelMu.Lock()
				a.applyPluginEvent(ev)
				a.modelMu.Unlock()
				needsRender = true
			}
		case result := <-a.commands:
			a.modelMu.Lock()
			a.applyCommandResult(result)
			a.modelMu.Unlock()
			needsRender = true
		case <-ticker.C:
			if anim {
				a.spinFrame++
				needsRender = true
			}
		}
		// drain 积压后端事件，合并到一帧。
		a.modelMu.Lock()
		if a.drain(eventsCh) {
			needsRender = true
		}
		// 清理已过期的底部提示（错误/临时信息）：3 秒后自动消失，不再一直残留。
		if a.model.ExpireError(time.Now()) {
			needsRender = true
		}
		// render 会同步部分模型字段（如 Scroll/FollowBottom，见 view/message_list.go），
		// 必须在锁内执行，避免与测试的 snapshot 并发读产生 data race。
		if needsRender {
			a.render()
		}
		quitting := a.model.Quitting
		a.modelMu.Unlock()

		if quitting {
			break
		}
	}
	return nil
}

// handleTermEvent 处理终端事件。
func (a *App) handleTermEvent(ev term.Event) {
	switch ev.Kind {
	case term.EventKey:
		a.dispatchKey(ev.Key)
	case term.EventMouse:
		a.dispatchMouse(ev.Mouse)
	case term.EventResize:
		a.resize(ev.W, ev.H)
	case term.EventQuit:
		a.model.Quitting = true
	}
}

func (a *App) applyCommandResult(result commandResult) {
	if result.kind == commandConfigGet {
		if result.cfgSeq != a.cfgGetSeq {
			// 过时结果：较早发出的 get 晚回，丢弃以免覆盖最新配置
			// （例如新增写回后触发的重载被打开时的首次 get 晚回覆盖）。
			return
		}
		if result.err != nil {
			a.model.ShowError(i18n.T("获取服务端配置失败: %s", result.err.Error()))
			a.closeServerEditor()
		} else if a.model.Modal != model.ModalServer {
			// 用户在拉取/重载期间已按 Esc 关闭编辑器：丢弃结果，避免复活编辑器。
			a.serverCfgFocus = nil
		} else {
			// 重建配置树。重建后的 Saving 归零（写回与全量重载完成，解除阻塞），
			// 编辑状态经 SetServerConfig 保留，可继续编辑。
			a.model.SetServerConfig(result.config)
			// 新增项写回后触发的整配置重载：完成后重定向到新项所在页面。
			// 若用户正在编辑（重载重建已保留其编辑状态），不强制覆盖导航，
			// 避免打断紧接的字段编辑（编辑写回会丢失）。
			if a.serverCfgFocus != nil {
				if ed := a.model.ServerCfg; ed != nil && !ed.IsEditing() {
					ed.Focus(a.serverCfgFocus)
				}
				a.serverCfgFocus = nil
			}
		}
		return
	}
	if result.kind == commandConfigSet {
		if result.err != nil {
			a.model.ShowError(i18n.T("保存配置失败: %s", result.err.Error()))
			// 写回失败：放弃重定向与队列中的后续写回（它们基于同一失败基态，
			// 继续应用只会加剧不一致）。队列清空、busy 复位，并解除 Saving
			// 阻塞（后续不会有重载 get 来解除它，避免界面永久卡在"保存中"）。
			a.serverCfgFocus = nil
			a.cfgWriteQueue = nil
			a.cfgWriteBusy = false
			if ed := a.model.ServerCfg; ed != nil {
				ed.Saving = false
			}
		} else {
			a.model.ShowInfo(i18n.T("配置已保存"))
			// 发送队列中下一个写回；全部完成后才触发新增后的整配置重载，
			// 保证 get 读到最终值（含新增对象与随后的字段编辑）。
			a.nextConfigSet()
		}
		return
	}
	if result.kind == commandConnectFetch {
		// /connect 拉取模型列表结果：成功进入模型选择步骤；失败回到表单页
		// 显示错误（保留已填内容，可修改后重试）。
		if cs := a.model.Connect; cs != nil && cs.Fetching {
			if result.err != nil {
				cs.ConnectFetchError(result.err)
			} else if len(result.models) > 0 {
				cs.ConnectApplyModels(result.models)
			} else {
				cs.ConnectFetchError(errors.New(i18n.T("服务商未返回任何模型")))
			}
		}
		return
	}
	if result.kind == commandConnectSubmit {
		// /connect 写入服务端配置结果：成功进入完成步骤；失败按当前步骤
		// 回填错误（自动路径回模型选择、手动路径留在输入页）。
		if cs := a.model.Connect; cs != nil {
			if result.err != nil {
				if cs.Step == model.ConnectStepManual {
					cs.ManualError = i18n.T("保存模型配置失败: %s", result.err.Error())
				} else {
					cs.FormError = i18n.T("保存模型配置失败: %s", result.err.Error())
				}
			} else {
				cs.ConnectMarkResult(i18n.T("模型已添加并设为默认模型"))
			}
		}
		return
	}
	if result.kind == commandSession && result.page != nil {
		p := *result.page
		if result.opID != a.sessionGeneration {
			return
		}
		if result.err != nil {
			a.sessionLoading = false
			a.model.ShowError(result.err.Error())
			return
		}
		if p.appendPage {
			a.model.Sessions = append(a.model.Sessions, p.page.Sessions...)
		} else {
			a.model.Sessions = p.page.Sessions
			if len(a.model.Sessions) == 0 {
				a.model.HomeSelected = -1
			} else if a.model.HomeSelected < 0 {
				a.model.HomeSelected = 0
			}
		}
		requestedCursor := result.sessionID
		a.sessionNextCursor = p.page.NextCursor
		if p.appendPage && requestedCursor == p.page.NextCursor {
			a.sessionNextCursor = ""
		}
		a.sessionLoading = false
		if p.appendPage {
			a.maybeLoadMoreSessions()
		}
		return
	}
	if result.kind == commandTerminalStop {
		if result.err != nil {
			a.model.ShowError(i18n.T("终端停止失败: %s", result.err.Error()))
			return
		}
		a.model.ShowInfo("终端停止请求已发送")
		return
	}
	if result.kind == commandSessionDelete {
		if result.err != nil {
			a.model.ShowError(i18n.T("删除会话失败: %s", result.err.Error()))
			a.discardPre = false
			return
		}
		a.model.DropPendingEvents(result.sessionID)
		a.model.RemoveSession(result.sessionID)
		a.model.ShowInfo(i18n.T("会话已删除"))
		// 删除的是当前活动会话时 RemoveSession 已回主页；回到主页需重建预创建
		// 会话，使 /effort 与 /model 在命令面板继续可用。但 discardPreSession 清理
		// 的删除（用户正在进入真实会话）不重建，等 goHome 时再创建。
		if !a.discardPre && a.model.View == model.ViewHome {
			a.ensurePreSessionLocked()
		}
		a.discardPre = false
		return
	}
	if result.kind == commandSession && result.opID != a.sessionOpID {
		return // 较晚返回的 create/resume 不能覆盖最后一次用户选择。
	}
	if result.kind == commandSessionAction && result.sessionID != "" && (a.sess == nil || a.sess.ID() != result.sessionID) {
		return // 已切换会话后的旧操作错误不污染当前 UI。
	}
	if result.err != nil {
		if result.kind == commandSession && a.model.PendingInitialPrompt != "" {
			a.model.Input.Lines = widget.SplitLines(a.model.PendingInitialPrompt)
			a.model.Input.CY = len(a.model.Input.Lines) - 1
			a.model.Input.CX = len(a.model.Input.Lines[a.model.Input.CY])
			a.model.PendingInitialPrompt = ""
		}
		a.model.ApplyEvent(&acp.BackendErrorEvent{Err: result.err})
		return
	}
	if result.session != nil {
		a.sess = result.session
		a.model.ClearSelection()
		a.model.ApplyEvent(&acp.NewSessionEvent{Session: result.session})
		if result.opID == a.firstSessionOpID {
			// 新手引导后的第一个新会话：应用引导里选的 effort。
			a.firstSessionOpID = 0
			a.applyFirstSessionEffort(result.session)
		}
		if prompt := a.model.PendingInitialPrompt; prompt != "" {
			a.model.PendingInitialPrompt = ""
			// 不本地回显：agent 会反射 user_message，避免重复显示。经插件
			// prompt hooks（可改写/拦截）后发送。
			a.sendPrompt(result.session, prompt)
		}
	}
}

func (a *App) startCommand(result commandResult, fn func(context.Context) (acp.Session, error)) {
	if a.runCtx == nil {
		return
	}
	ctx := a.runCtx
	a.commandWG.Add(1)
	go func() {
		defer a.commandWG.Done()
		session, err := fn(ctx)
		result.session = session
		result.err = err
		select {
		case a.commands <- result:
		case <-ctx.Done():
		}
	}()
}

// drain 非阻塞清空事件通道。
func (a *App) drain(eventsCh <-chan acp.Event) bool {
	changed := false
	for {
		select {
		case ev, ok := <-eventsCh:
			if !ok {
				return changed
			}
			a.model.ApplyEvent(ev)
			a.plugins.NotifyUpdate(ev)
			changed = true
		default:
			return changed
		}
	}
}

// resize 处理终端尺寸变化。
func (a *App) resize(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	a.resetBuffers(w, h)
	// resize 后 front 是 sentinel，render 会从左上角开始重写整屏，
	// 同时清掉旧尺寸下所有不可达行列的残留。
	a.render()
}

// resetBuffers 重新分配前后帧。首帧用"空哨兵"作为旧帧，
// 使 diff 时整屏（含全屏空格）都被视为变化而全量输出——实现"启动/resize 后先全屏填充一遍空格"。
func (a *App) resetBuffers(w, h int) {
	a.front = renderer.NewBuffer(w, h)
	a.back = renderer.NewBuffer(w, h)
	// 哨兵：R=0 的无效 cell，与任何真实内容都不相等 → 强制整屏输出
	sentinel := renderer.Cell{R: 0, Style: renderer.DefaultStyle(), Width: 0}
	for i := range a.front.Cells {
		a.front.Cells[i] = sentinel
	}
	a.initialRender = false
}

// render 绘制一帧并输出差异。
func (a *App) render() {
	w, h := a.front.W, a.front.H
	a.back.Clear()
	canvas := renderer.NewCanvas(a.back)
	a.view.SpinFrame = a.spinFrame
	a.view.Draw(canvas, renderer.NewRect(0, 0, w, h), a.model)
	a.applySelection(a.back)

	// 统一 diff：首帧时 front 是哨兵，等价于"从全空屏幕"开始 → 整屏输出；
	// 之后 front 是上一帧，增量输出。
	renderer.Render(a.front, a.back, a.mode, writerFunc(a.term.Write))
	a.initialRender = true
	a.front, a.back = a.back, a.front
}

// applySelection 给行选择区域叠加反显样式，实现"所见即所得"的高亮。
// 选区只作用于正文区域（BodyRect），计划/输入框/状态栏不高亮；
// 宽字符整字反显，不会只反半格导致花屏。
func (a *App) applySelection(buf *renderer.Buffer) {
	sel := a.model.Selection
	if sel == nil {
		return
	}
	y1, y2 := min(sel.AnchorY, sel.CurY), max(sel.AnchorY, sel.CurY)
	if y1 < 0 {
		y1 = 0
	}
	if y2 >= buf.H {
		y2 = buf.H - 1
	}
	rect := a.view.BodyRect
	if rect.H <= 0 {
		return
	}
	if y2 < rect.Y || y1 >= rect.Y+rect.H {
		return
	}
	if y1 < rect.Y {
		y1 = rect.Y
	}
	if y2 >= rect.Y+rect.H {
		y2 = rect.Y + rect.H - 1
	}
	for y := y1; y <= y2; y++ {
		lo, hi := lineSelectionBounds(buf, sel, y)
		if lo > hi {
			continue
		}
		for x := lo; x <= hi; x++ {
			if i := buf.Index(x, y); i >= 0 {
				buf.Cells[i].Style = buf.Cells[i].Style.WithReverse(true)
			}
		}
	}
}

// hasAnimation 报告当前是否需要持续重绘（spinner 动画）。
func (a *App) hasAnimation() bool {
	m := a.model
	if !m.HasActive() {
		return false
	}
	s := m.Active
	if s.Running() {
		return true
	}
	for _, tc := range s.ToolCalls {
		if tc.Running() {
			return true
		}
	}
	return false
}

// writerFunc 把 func(p []byte) error 适配为 io.Writer。
type writerFunc func(p []byte) error

func (f writerFunc) Write(p []byte) (int, error) {
	if err := f(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// refreshSessions 从头拉取会话列表；调用方通常在事件循环中持有 modelMu。
func (a *App) refreshSessions() {
	a.modelMu.Lock()
	a.refreshSessionsLocked()
	a.modelMu.Unlock()
}

// refreshSessionsLocked starts a first-page request while modelMu is held.
func (a *App) refreshSessionsLocked() {
	a.sessionGeneration++
	generation := a.sessionGeneration
	a.sessionNextCursor = ""
	a.sessionLoading = true
	a.startSessionPage(generation, "", false)
}

func (a *App) startSessionPage(generation uint64, cursor string, appendPage bool) {
	a.commandWG.Add(1)
	go func() {
		defer a.commandWG.Done()
		page, err := a.backend.ListSessionsPage(a.runCtx, cursor)
		select {
		case a.commands <- commandResult{kind: commandSession, opID: generation, sessionID: cursor, page: &sessionPageResult{page: page, appendPage: appendPage}, err: err}:
		case <-a.runCtx.Done():
		}
	}()
}

// loadMoreSessions fetches the next page when selection nears the list bottom.
func (a *App) loadMoreSessions() {
	if a.sessionLoading || a.sessionNextCursor == "" {
		return
	}
	cursor := a.sessionNextCursor
	generation := a.sessionGeneration
	a.sessionLoading = true
	a.startSessionPage(generation, cursor, true)
}

func (a *App) maybeLoadMoreSessions() {
	if len(a.model.Sessions) > 0 && a.model.HomeSelected >= len(a.model.Sessions)-5 {
		a.loadMoreSessions()
	}
}

// RunDump 在无 TTY 模式下渲染 frames 帧 ANSI 输出（冒烟测试）。
// 自动创建会话并发送 prompt，由 backend 驱动事件流。
func (a *App) RunDump(prompt string, frames int) error {
	_ = a.term.EnterRaw() // dump 终端为空操作
	defer a.term.ExitRaw()
	defer a.backend.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var promptDone chan struct{}
	defer func() {
		cancel()
		if promptDone != nil {
			<-promptDone
		}
	}()
	if err := a.backend.Initialize(ctx); err != nil {
		return err
	}
	a.refreshSessions()

	s, err := a.backend.NewSession(ctx, a.sessionCWD())
	if err != nil {
		return err
	}
	a.sess = s
	a.model.ApplyEvent(&acp.NewSessionEvent{Session: s})
	promptDone = make(chan struct{})
	go func() {
		defer close(promptDone)
		_ = s.SendPrompt(ctx, prompt)
	}()

	w, h := a.term.Size()
	a.resetBuffers(w, h)

	eventsCh := a.backend.Events()
	for i := 0; i < frames; i++ {
		// drain 全部积压事件
		a.drain(eventsCh)
		a.spinFrame = i
		a.render()
		time.Sleep(40 * time.Millisecond)
	}
	// 恢复终端（输出结束序列）
	a.term.ExitRaw()
	return nil
}
