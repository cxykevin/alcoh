package model

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/widget"
)

// ErrorTimeout 是底部提示（错误/临时信息）自动消失的时长。
const ErrorTimeout = 3 * time.Second

// ViewMode 是顶层视图模式。
type ViewMode int

const (
	// ViewHome 首页：会话列表 + 欢迎提示。
	ViewHome ViewMode = iota
	// ViewSession 会话视图：消息列表 + 输入框 + 状态行。
	ViewSession
)

// ModalKind 是模态层类型。
type ModalKind int

const (
	// NoModal 无模态。
	NoModal ModalKind = iota
	// ModalPermission 权限请求弹窗。
	ModalPermission
	// ModalElicitation elicitation 请求弹窗。
	ModalElicitation
	// ModalHelp 帮助弹窗。
	ModalHelp
	// ModalExitConfirm 退出确认弹窗。
	ModalExitConfirm
	// ModalSettings 本地客户端设置。
	ModalSettings
	// ModalServer 服务端信息与操作弹窗（仅 alkaid0 扩展能力可用时展示）。
	ModalServer
	// ModalEffort 推理强度（thought_level）水平滑条弹窗。
	ModalEffort
	// ModalModel 模型选择菜单弹窗。
	ModalModel
	// ModalOnboarding 全屏新手引导（首次启动且服务端未配置任何模型时展示）。
	ModalOnboarding
	// ModalConnect /connect 向导：选择服务商 → 填 key → 拉取模型列表 → 写入配置。
	ModalConnect
	// ModalThreshold /threshold 弹窗：修改默认模型的压缩阈值。
	ModalThreshold
)

// Focus 表示会话视图的焦点区域。
type Focus int

const (
	// FocusInput 焦点在输入框。
	FocusInput Focus = iota
	// FocusMessage 焦点在消息内容区。
	FocusMessage
)

// AppModel 是 TUI 的顶层状态。纯状态机：只通过 ApplyEvent 与领域方法改变。
type AppModel struct {
	View     ViewMode
	Active   *SessionState
	// PreSession 是进入主页时预创建的会话（未激活）。它不进入会话视图，只承载
	// agent 在 session/new 后广播的 config（thought_level / model），使 /effort 与
	// /model 在主页命令面板可用。用户恢复旧会话、新建会话或程序退出时，由 app
	// 层删除这个空会话（见 app.ensurePreSession / app.discardPreSession）。
	PreSession *SessionState
	Sessions   []*acp.SessionInfo

	HomeSelected int // 首页会话列表选中索引（-1 表示未选中）
	// HomeListFocused 表示首页会话列表是否处于聚焦状态。默认不聚焦；在输入框
	// 按左键时置为 true（列表全屏显示并聚焦）。宽度不足时列表隐藏。
	HomeListFocused bool
	Focus           Focus

	Modal           ModalKind
	Permission      *acp.PermissionRequest
	PermSelected    int
	PermissionQueue []acp.PermissionRequest

	Elicitation         *ElicitationState
	ElicitationQueue    []acp.ElicitationCreateParams
	ElicitationRPCID    acp.RPCID
	ElicitationFormData map[string]string

	Input                *widget.InputBuffer
	PendingInitialPrompt string

	SlashOpen     bool
	SlashSelected int
	LocalCommands []string

	Settings         config.Values
	SettingsSelected int

	// AgentInfo / AgentCaps 记录 initialize 握手得到的服务端标识与能力声明，
	// 由 app 在 Initialize 成功后写入。用于按能力门控 /server 等命令。
	AgentInfo acp.AgentInfo
	AgentCaps acp.AgentCapabilities

	// EffortSelect 是推理强度滑条当前选中的索引（见 effortLevels）。
	EffortSelect int

	// ModelSelect 是模型选择菜单当前选中的候选索引。
	ModelSelect int

	// ServerCfg 是服务端配置树编辑器状态（/server 弹窗）。nil 表示配置
	// 尚未从 config/get 加载。
	ServerCfg *ConfigEditor

	// Onboarding 是全屏新手引导状态（见 onboarding.go）。nil 表示不在引导中。
	Onboarding *OnboardingState

	// Connect 是 /connect 向导状态（见 connect.go）。nil 表示不在向导中。
	Connect *ConnectState

	// Threshold 是 /threshold 弹窗状态（见 threshold.go）。nil 表示不在弹窗中。
	Threshold *ThresholdState

	Selection *Selection

	// pendingSessionEvents 缓存会话未激活时到达的初始事件。agent（如 alkaid0）在
	// session/new 响应返回前就会广播 available_commands_update / config_option_update
	// 等元数据；此刻会话尚未激活，直接应用会被丢弃。缓存后待会话激活时重放。
	pendingSessionEvents map[string][]acp.Event

	Error string
	// ErrorExpires 是底部提示自动消失的时刻；零值表示无过期时间。
	ErrorExpires time.Time
	// ErrorInfo 表示底部提示是信息性提示（非错误），渲染时用亮蓝色而非错误红。
	ErrorInfo bool
	Quitting  bool
}

// Selection 描述一次鼠标选择。坐标为屏幕单元格（0-based，X 列 / Y 行）。
// Anchor 为按下点，Cursor 为拖动终点；渲染时按行选择语义叠加反显
// （首行从 Anchor/Cur 到行尾、末行到 Anchor/Cur、中间整行，宽字符不切半），
// Ctrl+C 时按同一行选择从已渲染帧提取文本并复制。
type Selection struct {
	AnchorX, AnchorY int
	CurX, CurY       int
}

// ElicitationState 保存当前 elicitation 请求的状态。
type ElicitationState struct {
	Request      acp.ElicitationCreateParams
	Schema       map[string]interface{} // 解析后的 JSON Schema
	FieldOrder   []string               // 字段顺序
	FieldIndex   int                    // 当前选中的字段索引
	ErrorMessage string                 // 验证错误消息
}

// ClearSelection 清除当前选择。
func (m *AppModel) ClearSelection() { m.Selection = nil }

// New 创建初始 AppModel（首页视图）。
func New() *AppModel {
	return &AppModel{
		Modal:                NoModal,
		HomeSelected:         -1,
		HomeListFocused:      false,
		Input:                widget.NewInputBuffer(),
		LocalCommands:        []string{"/alcoh_help", "/clear", "/settings"},
		Settings:             config.Defaults(),
		pendingSessionEvents: map[string][]acp.Event{},
	}
}

// SlashCommandInfo 是可用于补全和展示的命令元数据。
type SlashCommandInfo struct {
	Name        string
	Description string
	ArgsHint    string
}

var localSlashCommandInfo = map[string]SlashCommandInfo{
	"/alcoh_help": {Name: "/alcoh_help", Description: "打开帮助"},
	"/connect":    {Name: "/connect", Description: "连接模型服务商（模板自动填 base_url，填 key 拉取模型列表）"},
	"/effort":     {Name: "/effort", Description: "设置推理强度", ArgsHint: "[unset|low|medium|high|xhigh|max]"},
	"/model":      {Name: "/model", Description: "切换模型"},
	"/clear":      {Name: "/clear", Description: "清除会话，返回会话列表", ArgsHint: "[on]"},
	"/settings":   {Name: "/settings", Description: "打开本地设置"},
	"/server":     {Name: "/server", Description: "服务端信息与操作弹窗"},
	"/threshold":  {Name: "/threshold", Description: "修改压缩阈值", ArgsHint: "[token数]"},
}

// effortLevels 是客户端硬编码的推理强度（ACP v2 thought_level 目录）候选值。
// 值不依赖服务端广播的 options 内容；服务端只负责声明是否支持 thought_level 配置。
var effortLevels = []string{"unset", "low", "medium", "high", "xhigh", "max"}

// EffortLevels 返回客户端硬编码的推理强度候选值副本（供视图渲染滑条）。
func EffortLevels() []string { return append([]string(nil), effortLevels...) }

func (m *AppModel) slashCommandInfos() []SlashCommandInfo {
	commands := m.SlashCommands()
	infos := make([]SlashCommandInfo, 0, len(commands))
	for _, name := range commands {
		if info, ok := localSlashCommandInfo[name]; ok {
			// 本地命令说明按当前语言翻译（渲染时调用，切换语言即时生效）。
			info.Description = i18n.T(info.Description)
			infos = append(infos, info)
			continue
		}
		info := SlashCommandInfo{Name: name}
		if m.Active != nil {
			for _, command := range m.Active.Commands {
				candidate := command.Name
				if candidate != "" && candidate[0] != '/' {
					candidate = "/" + candidate
				}
				if candidate == name {
					info.Description = command.Description
					break
				}
			}
		}
		infos = append(infos, info)
	}
	return infos
}

// SlashCompletion 返回当前命令的灰色补全文本和说明。
func (m *AppModel) SlashCompletion() (ghost, description string) {
	if m.Input == nil || m.Input.CY != 0 {
		return "", ""
	}
	line := string(m.Input.Lines[0])
	if len(line) == 0 || line[0] != '/' {
		return "", ""
	}
	end := len(line)
	for i, r := range line {
		if r == ' ' || r == '\t' {
			end = i
			break
		}
	}
	if m.Input.CX > end {
		return "", ""
	}
	token := line[:end]
	matches, _ := m.FilteredSlashCommands()
	infos := m.slashCommandInfos()
	for _, info := range infos {
		if info.Name != m.SlashSelectedCommand() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(info.Name), strings.ToLower(token)) && info.Name != token {
			ghost = info.Name[len(token):]
		}
		if len(matches) == 1 && info.Name == token {
			description = info.Description
		}
		return ghost, description
	}
	return "", ""
}

// 未列入硬编码列表的 agent 命令优先级为 0。
func slashCommandPriority(command string) int {
	switch command {
	case "/alcoh_help", "/connect", "/effort", "/clear", "/settings", "/server", "/threshold":
		return 1
	default:
		return 0
	}
}

// SlashCommands 返回当前会话可用的本地与 agent 命令，按硬编码优先级排序。
func (m *AppModel) SlashCommands() []string {
	out := append([]string(nil), m.LocalCommands...)
	// /connect 与 /threshold 仅在服务端声明 alkaid0 扩展能力时可用
	//（模型/压缩阈值写入服务端配置）。
	if m.SupportsAlkaid0() && !containsString(out, "/connect") {
		out = append(out, "/connect")
	}
	if m.SupportsAlkaid0() && !containsString(out, "/threshold") {
		out = append(out, "/threshold")
	}
	// /effort 仅在服务端公布 thought_level 配置时可用。
	if m.SupportsEffort() && !containsString(out, "/effort") {
		out = append(out, "/effort")
	}
	// /model 仅在服务端公布 model 配置时可用。
	if m.SupportsModel() && !containsString(out, "/model") {
		out = append(out, "/model")
	}
	// /server 仅在服务端声明 alkaid0 扩展能力时可用。
	if m.SupportsAlkaid0() && !containsString(out, "/server") {
		out = append(out, "/server")
	}
	if m.Active != nil {
		for _, command := range m.Active.Commands {
			if command.Name == "" {
				continue
			}
			name := command.Name
			if name[0] != '/' {
				name = "/" + name
			}
			duplicate := false
			for _, existing := range out {
				if existing == name {
					duplicate = true
					break
				}
			}
			if !duplicate {
				out = append(out, name)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return slashCommandPriority(out[i]) > slashCommandPriority(out[j])
	})
	return out
}

// slashCommandToken 提取输入首行中的命令 token，光标必须落在 token 范围内。
func (m *AppModel) slashCommandToken() (token string, inToken bool) {
	if m.Input == nil || m.Input.CY != 0 || len(m.Input.Lines) == 0 {
		return "", false
	}
	line := string(m.Input.Lines[0])
	if len(line) == 0 || line[0] != '/' {
		return "", false
	}
	end := len(line)
	for i, r := range line {
		if r == ' ' || r == '\t' {
			end = i
			break
		}
	}
	return line[:end], m.Input.CX <= end
}

// FilteredSlashCommands 返回按当前命令 token 过滤的命令及其原始索引。
func (m *AppModel) FilteredSlashCommands() ([]string, []int) {
	token, _ := m.slashCommandToken()
	query := strings.TrimPrefix(token, "/")
	commands := m.SlashCommands()
	filtered := make([]string, 0, len(commands))
	indices := make([]int, 0, len(commands))
	for i, command := range commands {
		if query != "" && !strings.Contains(strings.ToLower(command), strings.ToLower("/"+query)) {
			continue
		}
		filtered = append(filtered, command)
		indices = append(indices, i)
	}
	return filtered, indices
}

func (m *AppModel) UpdateSlashState() {
	token, inToken := m.slashCommandToken()
	m.SlashOpen = token != "" && inToken
	_, indices := m.FilteredSlashCommands()
	if len(indices) == 0 {
		m.SlashSelected = 0
		return
	}
	for _, index := range indices {
		if index == m.SlashSelected {
			return
		}
	}
	m.SlashSelected = indices[0]
}

func (m *AppModel) SlashMove(delta int) {
	_, indices := m.FilteredSlashCommands()
	if len(indices) == 0 {
		return
	}
	selected := 0
	for i, index := range indices {
		if index == m.SlashSelected {
			selected = i
			break
		}
	}
	m.SlashSelected = indices[(selected+delta+len(indices))%len(indices)]
}

// SlashCommandDescriptions 返回与 SlashCommands 顺序一一对应的描述列表。
func (m *AppModel) SlashCommandDescriptions() []string {
	infos := m.slashCommandInfos()
	out := make([]string, len(infos))
	for i, info := range infos {
		desc := info.Description
		if info.ArgsHint != "" {
			if desc != "" {
				desc = info.ArgsHint + "  " + desc
			} else {
				desc = info.ArgsHint
			}
		}
		out[i] = desc
	}
	return out
}

func (m *AppModel) SlashSelectedCommand() string {
	commands, indices := m.FilteredSlashCommands()
	for i, index := range indices {
		if index == m.SlashSelected {
			return commands[i]
		}
	}
	return ""
}

func (m *AppModel) CloseSlash() { m.SlashOpen = false }

// OpenSettings 打开本地设置页。
func (m *AppModel) OpenSettings() {
	m.CloseSlash()
	m.SettingsSelected = 0
	m.SetModal(ModalSettings)
}

// OpenServer 打开服务端配置编辑器弹窗。配置树由 app 在收到 config/get
// 响应后经 SetServerConfig 写入；未写入前 ServerCfg 为 nil（显示加载中）。
func (m *AppModel) OpenServer() {
	m.CloseSlash()
	m.SetModal(ModalServer)
}

// SetServerConfig 用 config/get 的完整配置 JSON 构建配置树。重建后保留此前
// 的导航位置与正在进行的编辑（新增写回后触发的整配置重载不应打断用户编辑，
// 否则紧接的字段编辑写回会丢失）。
func (m *AppModel) SetServerConfig(raw json.RawMessage) {
	old := m.ServerCfg
	m.ServerCfg = NewConfigEditor(raw)
	if old != nil {
		m.ServerCfg.RestoreState(old.CaptureState())
	}
}

// CloseServer 关闭服务端配置编辑器并释放其状态。
func (m *AppModel) CloseServer() {
	m.ServerCfg = nil
	m.SetModal(NoModal)
}

// SetAgentInfo 记录 initialize 握手得到的服务端标识与能力声明。由 app 在
// Initialize 成功后调用，供按能力门控 /server 等命令使用。
func (m *AppModel) SetAgentInfo(info acp.AgentInfo, caps acp.AgentCapabilities) {
	m.AgentInfo = info
	m.AgentCaps = caps
}

// SupportsAlkaid0 报告服务端是否声明 alkaid0 扩展协议能力
// （alk.cxykevin.top/alkaid0/v0.4）；仅当为 true 时 /server 可用。
func (m *AppModel) SupportsAlkaid0() bool {
	return m.AgentCaps.Has(acp.Alkaid0CapabilityV04)
}

// SupportsSessionDelete 报告服务端是否声明 session/delete 能力；仅当为 true
// 时首页按 d 删除会话可用。
func (m *AppModel) SupportsSessionDelete() bool {
	return m.AgentCaps.SupportsSessionDelete()
}

// MoveSettings 移动设置项选择。
func (m *AppModel) MoveSettings(delta, count int) {
	if count <= 0 {
		m.SettingsSelected = 0
		return
	}
	m.SettingsSelected = (m.SettingsSelected + delta + count) % count
}

// ToggleSetting 修改当前可布尔切换的本地设置。返回是否发生修改。
func (m *AppModel) ToggleSetting() bool {
	switch m.SettingsSelected {
	case 1:
		m.Settings.ThinkingExpanded = !m.Settings.ThinkingExpanded
	case 2:
		m.Settings.ToolsExpanded = !m.Settings.ToolsExpanded
	default:
		return false
	}
	return true
}

// CycleColorMode 在已支持的本地色彩模式之间切换。
func (m *AppModel) CycleColorMode(delta int) bool {
	if m.SettingsSelected != 0 {
		return false
	}
	modes := []string{"auto", "mono", "16", "256", "truecolor"}
	idx := 0
	for i, mode := range modes {
		if m.Settings.ColorMode == mode {
			idx = i
			break
		}
	}
	m.Settings.ColorMode = modes[(idx+delta+len(modes))%len(modes)]
	return true
}

// CycleLanguage 在支持的界面语言之间切换（写入本地配置，保存时应用）。
func (m *AppModel) CycleLanguage(delta int) bool {
	if m.SettingsSelected != 3 {
		return false
	}
	langs := []string{"zh", "en"}
	idx := 0
	for i, l := range langs {
		if m.Settings.Language == l {
			idx = i
			break
		}
	}
	m.Settings.Language = langs[(idx+delta+len(langs))%len(langs)]
	return true
}

func (m *AppModel) ActiveSession() *SessionState { return m.Active }

// HasActive 报告是否在会话视图且有活动会话。
func (m *AppModel) HasActive() bool { return m.Active != nil && m.View == ViewSession }

// SupportsEffort 报告服务端是否公布 thought_level 配置；仅当为 true 时 /effort 可用。
// 会话内看活动会话；主页时看预创建会话（PreSession）公布的 config。
func (m *AppModel) SupportsEffort() bool {
	return m.ActiveConfigOption("thought_level") != nil
}

// CurrentEffort 返回服务端公布的当前推理强度值；未知时返回空串。
func (m *AppModel) CurrentEffort() string {
	if opt := m.ActiveConfigOption("thought_level"); opt != nil {
		return opt.CurrentValue
	}
	return ""
}

// ActiveConfigOption 返回指定 configId 的配置项。优先活动会话；活动会话未公布
// 时回退到主页预创建会话（PreSession）——后者承载 /effort 与 /model 所需 config。
func (m *AppModel) ActiveConfigOption(configID string) *acp.ConfigOption {
	if m.Active != nil {
		if opt := m.Active.ConfigOption(configID); opt != nil {
			return opt
		}
	}
	if m.PreSession != nil {
		return m.PreSession.ConfigOption(configID)
	}
	return nil
}

// ValidEffortValue 判断给定值是否为客户端硬编码的合法推理强度。
func (m *AppModel) ValidEffortValue(value string) bool {
	for _, v := range effortLevels {
		if v == value {
			return true
		}
	}
	return false
}

// OpenEffortModal 打开推理强度滑条，选中项初始化为服务端当前值。
func (m *AppModel) OpenEffortModal() {
	if !m.SupportsEffort() {
		return
	}
	cur := m.CurrentEffort()
	m.EffortSelect = 0
	for i, v := range effortLevels {
		if v == cur {
			m.EffortSelect = i
			break
		}
	}
	m.SetModal(ModalEffort)
}

// EffortMove 左右移动滑条选择。
func (m *AppModel) EffortMove(delta int) {
	m.EffortSelect = (m.EffortSelect + delta + len(effortLevels)) % len(effortLevels)
}

// EffortSelectedValue 返回滑条当前选中值并关闭弹窗。
func (m *AppModel) EffortSelectedValue() string {
	if m.Modal != ModalEffort {
		return ""
	}
	idx := m.EffortSelect
	if idx < 0 || idx >= len(effortLevels) {
		idx = 0
	}
	value := effortLevels[idx]
	m.SetModal(NoModal)
	return value
}

// CancelEffort 关闭推理强度弹窗且不提交。
func (m *AppModel) CancelEffort() {
	if m.Modal == ModalEffort {
		m.SetModal(NoModal)
	}
}

// SupportsModel 报告服务端是否公布 model 配置；仅当为 true 时 /model 可用。
// 会话内看活动会话；主页时看预创建会话（PreSession）公布的 config。
func (m *AppModel) SupportsModel() bool {
	return m.ActiveConfigOption("model") != nil
}

// CurrentModel 返回服务端公布的当前模型值；未知时返回空串。
func (m *AppModel) CurrentModel() string {
	if opt := m.ActiveConfigOption("model"); opt != nil {
		return opt.CurrentValue
	}
	return ""
}

// ValidModelValue 判断给定值是否在服务端公布的候选列表中。
func (m *AppModel) ValidModelValue(value string) bool {
	opt := m.ActiveConfigOption("model")
	if opt == nil {
		return false
	}
	for _, v := range opt.Options {
		if v.Value == value {
			return true
		}
	}
	return false
}

// ModelConfig 返回活动会话的模型配置项。可用于获取当前模型及候选列表。
func (m *AppModel) ModelConfig() *acp.ConfigOption {
	return m.ActiveConfigOption("model")
}

// ActiveModelConfig 返回活动会话的模型配置项（keymap 设置模型时使用）。
func (m *AppModel) ActiveModelConfig() *acp.ConfigOption {
	return m.ActiveConfigOption("model")
}

// ModelOptions 返回模型候选值的字符串列表（用于菜单渲染）。
func (m *AppModel) ModelOptions() []string {
	opt := m.ActiveConfigOption("model")
	if opt == nil {
		return nil
	}
	out := make([]string, len(opt.Options))
	for i, v := range opt.Options {
		out[i] = v.Value
	}
	return out
}

// OpenModelModal 打开模型选择菜单，选中项初始化为服务端当前值。
func (m *AppModel) OpenModelModal() {
	if !m.SupportsModel() {
		return
	}
	cur := m.CurrentModel()
	opts := m.ModelOptions()
	m.ModelSelect = 0
	for i, v := range opts {
		if v == cur {
			m.ModelSelect = i
			break
		}
	}
	m.SetModal(ModalModel)
}

// ModelMove 上下移动模型选择菜单（带环绕）。
func (m *AppModel) ModelMove(delta int) {
	opts := m.ModelOptions()
	if len(opts) == 0 {
		return
	}
	m.ModelSelect = (m.ModelSelect + delta + len(opts)) % len(opts)
}

// ModelSelectedValue 返回菜单当前选中值并关闭弹窗。
func (m *AppModel) ModelSelectedValue() string {
	if m.Modal != ModalModel {
		return ""
	}
	opts := m.ModelOptions()
	if len(opts) == 0 {
		m.SetModal(NoModal)
		return ""
	}
	idx := m.ModelSelect
	if idx < 0 || idx >= len(opts) {
		idx = 0
	}
	value := opts[idx]
	m.SetModal(NoModal)
	return value
}

// CancelModel 关闭模型选择菜单且不提交。
func (m *AppModel) CancelModel() {
	if m.Modal == ModalModel {
		m.SetModal(NoModal)
	}
}

// ApplyEvent 应用一个后端事件。
func (m *AppModel) ApplyEvent(ev acp.Event) {
	// 带 sessionId 的事件若不属于当前活动会话（尚未激活，或属于后台会话），
	// 先缓存待会话激活后重放，避免 agent 在 session/new 响应前广播的
	// available_commands / config_option 等初始元数据被丢弃。
	if id := eventSessionID(ev); id != "" && (m.Active == nil || m.Active.ID != id) {
		// 主页预创建会话的事件直接应用到 PreSession，不进入激活队列——
		// 它只用于在主页读取 config，永不激活为会话视图。
		if m.PreSession != nil && id == m.PreSession.ID {
			m.applyPreSessionEvent(ev)
			return
		}
		m.pendingSessionEvents[id] = append(m.pendingSessionEvents[id], ev)
		return
	}
	switch e := ev.(type) {
	case *acp.MessageChunkEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.AppendChunk(e)
		}
	case *acp.MessageUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.ApplyMessage(e)
		}
	case *acp.ToolCallUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.ApplyToolCall(e)
		}
	case *acp.PlanUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.ApplyPlan(e)
		}
	case *acp.StateChangeEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			s := m.Active
			s.State = e.State
			s.StopReason = e.StopReason
			// 私有扩展的错误/提示信息收敛为系统提示，不占正文。
			if e.Notice != nil && *e.Notice != "" {
				s.AppendSystemNotice("state:"+*e.Notice, *e.Notice)
			}
			if e.State == acp.StateIdle {
				// 流式完成后 agent 可能不补发完整 agent_message/agent_thought 块
				// （如 alkaid0 只发 chunk），必须把未完成消息标记为 done。
				s.MarkStreamingDone()
				s.CollapseThoughts()
				// 若权限或 elicitation 模态还挂着但会话已空闲，说明 agent 取消/结束；连同后续队列一并清理。
				if m.Modal == ModalPermission {
					m.Permission = nil
					m.PermissionQueue = nil
					m.Modal = NoModal
				}
				if m.Modal == ModalElicitation || m.Elicitation != nil {
					m.Elicitation = nil
					m.ElicitationQueue = nil
					m.ElicitationFormData = nil
					m.ElicitationRPCID = nil
					if m.Modal == ModalElicitation {
						m.Modal = NoModal
					}
				}
			}
		}
	case *acp.PermissionRequestEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.EnqueuePermission(e.Request)
		}
	case *acp.ElicitationRequestEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.EnqueueElicitation(e.RequestID, e.Request)
		}
	case *acp.UsageUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.Usage = acp.Usage{Used: e.Used, Size: e.Size, Cost: e.Cost}
		}
	case *acp.SessionInfoUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			if e.Title != nil {
				m.Active.Title = *e.Title
			}
			if e.Model != nil {
				m.Active.ModelName = *e.Model
			}
			if e.CWD != nil {
				m.Active.WorkingDir = *e.CWD
			}
			if e.UpdatedAt != nil {
				m.Active.UpdatedAt = *e.UpdatedAt
			}
			for _, info := range m.Sessions {
				if info.SessionID == e.SessionID {
					if e.Title != nil {
						info.Title = *e.Title
					}
					if e.UpdatedAt != nil {
						info.UpdatedAt = *e.UpdatedAt
					}
				}
			}
		}
	case *acp.CommandsUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.Commands = append([]acp.AvailableCommand(nil), e.Commands...)
			m.UpdateSlashState()
		}
	case *acp.ConfigOptionUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.applyAgentConfig(e.Options)
			m.Active.ProtocolUpdates = appendProtocolUpdate(m.Active.ProtocolUpdates, e.Raw)
		}
	case *acp.TerminalUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.ApplyTerminal(e.TerminalID, e.Title, e.Status, e.Output)
		}
	case *acp.UnknownSessionUpdateEvent:
		if m.HasActive() && e.SessionID == m.Active.ID {
			m.Active.ProtocolUpdates = appendProtocolUpdate(m.Active.ProtocolUpdates, e.Raw)
			label := e.Discriminator
			if label == "" {
				label = "unknown"
			}
			noticeKey := "unknown:" + label
			notice := i18n.T("▸ 已收到未知 session update: %s（保留在协议诊断中）", label)
			m.Active.AppendSystemNotice(noticeKey, notice)
		}
	case *acp.SessionListEvent:
		m.Sessions = e.Sessions
	case *acp.NewSessionEvent:
		if e.Session != nil {
			m.ActivateSession(e.Session.ID(), e.Session.Title())
		}
	case *acp.BackendErrorEvent:
		if e.Err != nil {
			m.ShowError(e.Err.Error())
		} else {
			m.ShowError("ACP backend error")
		}
	}
}

// applyPreSessionEvent 把预创建会话的事件应用到 PreSession 状态。它只更新主页
// 命令面板所需的状态（config / commands / 标题），不处理消息与工具内容——该会话
// 不进入会话视图，其正文数据没有消费者。
func (m *AppModel) applyPreSessionEvent(ev acp.Event) {
	s := m.PreSession
	if s == nil {
		return
	}
	switch e := ev.(type) {
	case *acp.ConfigOptionUpdateEvent:
		s.applyAgentConfig(e.Options)
		s.ProtocolUpdates = appendProtocolUpdate(s.ProtocolUpdates, e.Raw)
	case *acp.CommandsUpdateEvent:
		s.Commands = append([]acp.AvailableCommand(nil), e.Commands...)
	case *acp.SessionInfoUpdateEvent:
		if e.Title != nil {
			s.Title = *e.Title
		}
		if e.Model != nil {
			s.ModelName = *e.Model
		}
		if e.CWD != nil {
			s.WorkingDir = *e.CWD
		}
	case *acp.StateChangeEvent:
		s.State = e.State
	}
}

// SetPreSession 登记主页预创建会话并重放该会话未激活时缓存的事件
// （agent 在 session/new 响应返回前广播的 config / commands）。
func (m *AppModel) SetPreSession(id, title string) {
	if m.PreSession != nil && m.PreSession.ID == id {
		return
	}
	m.PreSession = NewSession(id, title)
	if pending, ok := m.pendingSessionEvents[id]; ok {
		delete(m.pendingSessionEvents, id)
		for _, ev := range pending {
			m.ApplyEvent(ev)
		}
	}
}

// ClearPreSession 清空主页预创建会话状态并丢弃其缓存事件。删除该空会话时调用。
func (m *AppModel) ClearPreSession() {
	if m.PreSession != nil {
		delete(m.pendingSessionEvents, m.PreSession.ID)
	}
	m.PreSession = nil
}

// DropPendingEvents 丢弃指定会话未激活时缓存的事件（会话已被删除，不再激活）。
func (m *AppModel) DropPendingEvents(id string) {
	delete(m.pendingSessionEvents, id)
}

// eventSessionID 返回事件携带的会话 ID；无会话归属的事件返回空串。
func eventSessionID(ev acp.Event) string {
	switch e := ev.(type) {
	case *acp.MessageChunkEvent:
		return e.SessionID
	case *acp.MessageUpdateEvent:
		return e.SessionID
	case *acp.ToolCallUpdateEvent:
		return e.SessionID
	case *acp.PlanUpdateEvent:
		return e.SessionID
	case *acp.PermissionRequestEvent:
		return e.SessionID
	case *acp.ElicitationRequestEvent:
		return e.SessionID
	case *acp.StateChangeEvent:
		return e.SessionID
	case *acp.UsageUpdateEvent:
		return e.SessionID
	case *acp.SessionInfoUpdateEvent:
		return e.SessionID
	case *acp.CommandsUpdateEvent:
		return e.SessionID
	case *acp.ConfigOptionUpdateEvent:
		return e.SessionID
	case *acp.TerminalUpdateEvent:
		return e.SessionID
	case *acp.UnknownSessionUpdateEvent:
		return e.SessionID
	}
	return ""
}

func appendProtocolUpdate(existing []json.RawMessage, raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return existing
	}
	existing = append(existing, append(json.RawMessage(nil), raw...))
	const maxProtocolUpdates = 32
	if len(existing) > maxProtocolUpdates {
		existing = existing[len(existing)-maxProtocolUpdates:]
	}
	return existing
}

// ActivateSession 激活一个会话并切换到会话视图。
func (m *AppModel) ActivateSession(id, title string) {
	// 幂等：同一会话已激活时不重建会话。命令完成的激活信号与后端事件可能
	// 并发到达（重复的 NewSessionEvent），重建会清空已重放的初始元数据。
	if m.Active != nil && m.Active.ID == id {
		if pending, ok := m.pendingSessionEvents[id]; ok {
			delete(m.pendingSessionEvents, id)
			for _, ev := range pending {
				m.ApplyEvent(ev)
			}
		}
		return
	}
	if m.PreSession != nil && m.PreSession.ID == id {
		// 主页预创建会话被用户直接用作新会话（主页输入 prompt 回车或按 n）：
		// 复用其状态对象，保留已应用的 config / commands，避免重建空状态丢失
		// agent 在 session/new 响应前广播的初始元数据。
		m.Active = m.PreSession
		m.PreSession = nil
	} else {
		m.Active = NewSession(id, title)
	}
	m.View = ViewSession
	m.Modal = NoModal
	m.Permission = nil
	m.ScrollBottom()
	// 重放该会话未激活时缓存的事件（available_commands / config / session-info 等）。
	if pending, ok := m.pendingSessionEvents[id]; ok {
		delete(m.pendingSessionEvents, id)
		for _, ev := range pending {
			m.ApplyEvent(ev)
		}
	}
}

// GoHome 返回首页。
func (m *AppModel) GoHome() {
	m.View = ViewHome
	m.Active = nil
	m.Input = widget.NewInputBuffer()
	m.PendingInitialPrompt = ""
	m.ClearError()
	m.HomeSelected = -1
	m.HomeListFocused = false
	m.ClearSelection()
}

// RemoveSession 从首页会话列表移除指定会话并调整选中索引。删除的会话同时
// 也是当前活动会话时回到首页。返回是否实际移除。
func (m *AppModel) RemoveSession(id string) bool {
	if len(m.Sessions) == 0 {
		return false
	}
	removedIndex := -1
	next := make([]*acp.SessionInfo, 0, len(m.Sessions)-1)
	for i, s := range m.Sessions {
		if s.SessionID == id {
			removedIndex = i
			continue
		}
		next = append(next, s)
	}
	if removedIndex < 0 {
		return false
	}
	m.Sessions = next
	// 调整选中索引：移除项在选中项之前时选中项前移一位；删除项恰为选中项时
	// 收敛到原选中位置（后一项顶上来）；列表空时置 -1。
	if len(m.Sessions) == 0 {
		m.HomeSelected = -1
	} else if removedIndex < m.HomeSelected {
		m.HomeSelected--
	} else if m.HomeSelected > len(m.Sessions)-1 {
		m.HomeSelected = len(m.Sessions) - 1
	}
	// 删除的是当前活动会话：回到首页（会话句柄已失效）。
	if m.Active != nil && m.Active.ID == id {
		m.GoHome()
	}
	return true
}

// ShowError 设置底部错误提示，并安排其在 ErrorTimeout 后自动消失。错误用红色渲染。
func (m *AppModel) ShowError(msg string) {
	m.Error = msg
	m.ErrorInfo = false
	m.ErrorExpires = time.Now().Add(ErrorTimeout)
}

// ShowInfo 设置底部信息提示（如操作成功提示），用亮蓝色渲染，3 秒后自动消失。
func (m *AppModel) ShowInfo(msg string) {
	m.Error = msg
	m.ErrorInfo = true
	m.ErrorExpires = time.Now().Add(ErrorTimeout)
}

// ClearError 立即清除底部提示并重置过期时间。
func (m *AppModel) ClearError() {
	m.Error = ""
	m.ErrorInfo = false
	m.ErrorExpires = time.Time{}
}

// ExpireError 清除已到期的底部提示；返回 true 表示发生了状态变化（需要重绘）。
func (m *AppModel) ExpireError(now time.Time) bool {
	if m.Error == "" || m.ErrorExpires.IsZero() || !now.After(m.ErrorExpires) {
		return false
	}
	m.Error = ""
	m.ErrorInfo = false
	m.ErrorExpires = time.Time{}
	return true
}

// InputEmpty 报告输入框是否为空（含 nil 防御）。
// 用于决定 "?" 是否作为帮助快捷键触发：仅在输入框为空时生效。
func (m *AppModel) InputEmpty() bool {
	return m.Input == nil || m.Input.Text() == ""
}

// SubmitHomeInput 提交首页草稿；创建会话成功前不回显为聊天消息。
// 若 strip 后为空则拒绝提交并保留原文。
func (m *AppModel) SubmitHomeInput() string {
	if m.View != ViewHome {
		return ""
	}
	if strings.TrimSpace(m.Input.Text()) == "" {
		return ""
	}
	return m.Input.Submit()
}

// AddUserMessage 已移除：ACP agent 在 session/prompt 后固定反射 user_message，
// 客户端本地回显会与反射消息重复。用户消息一律以服务端反射的 user_message 为准。

// SubmitInput 提交输入框内容。返回文本；空输入或不在会话视图时返回空。
// 提交后清空输入，但不本地回显——agent 会反射 user_message，避免重复。
func (m *AppModel) SubmitInput() string {
	if !m.HasActive() {
		return ""
	}
	if strings.TrimSpace(m.Input.Text()) == "" {
		return ""
	}
	text := m.Input.Submit()
	if text == "" {
		return ""
	}
	return text
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// EnqueuePermission 追加一个权限请求；若当前无活动请求则立即打开。
func (m *AppModel) EnqueuePermission(req acp.PermissionRequest) {
	if m.Permission == nil {
		clone := req
		m.Permission = &clone
		m.PermSelected = 0
		m.Modal = ModalPermission
		return
	}
	m.PermissionQueue = append(m.PermissionQueue, req)
}

// advancePermissionQueue 关闭当前弹窗；若队列还有条目则弹出下一条。
func (m *AppModel) advancePermissionQueue() {
	m.Permission = nil
	m.PermSelected = 0
	if len(m.PermissionQueue) == 0 {
		m.Modal = NoModal
		return
	}
	next := m.PermissionQueue[0]
	m.PermissionQueue = m.PermissionQueue[1:]
	clone := next
	m.Permission = &clone
	m.PermSelected = 0
	m.Modal = ModalPermission
}

// PendingPermissionCount 返回后续排队等待的权限请求数（不含当前弹窗）。
func (m *AppModel) PendingPermissionCount() int { return len(m.PermissionQueue) }

// EnqueueElicitation 将 elicitation 请求加入队列或立即显示。
func (m *AppModel) EnqueueElicitation(rpcID acp.RPCID, req acp.ElicitationCreateParams) {
	if m.Elicitation == nil && m.Modal != ModalPermission {
		m.showElicitation(rpcID, req)
		return
	}
	m.ElicitationQueue = append(m.ElicitationQueue, req)
}

// showElicitation 显示 elicitation 弹窗。
func (m *AppModel) showElicitation(rpcID acp.RPCID, req acp.ElicitationCreateParams) {
	state := &ElicitationState{
		Request: req,
	}
	// 解析表单模式的 schema
	if req.Mode == acp.ElicitationModeForm && len(req.Schema) > 0 {
		var schema map[string]interface{}
		if err := json.Unmarshal(req.Schema, &schema); err == nil {
			state.Schema = schema
			// 提取字段顺序
			if props, ok := schema["properties"].(map[string]interface{}); ok {
				for field := range props {
					state.FieldOrder = append(state.FieldOrder, field)
				}
				sort.Strings(state.FieldOrder)
			}
		}
	}
	m.Elicitation = state
	m.ElicitationFormData = make(map[string]string)
	m.ElicitationRPCID = rpcID
	m.Modal = ModalElicitation
}

// AdvanceElicitationQueue 关闭当前 elicitation 弹窗；若队列还有条目则弹出下一条。
func (m *AppModel) AdvanceElicitationQueue() {
	m.Elicitation = nil
	m.ElicitationFormData = nil
	m.ElicitationRPCID = nil
	if len(m.ElicitationQueue) == 0 {
		m.Modal = NoModal
		return
	}
	next := m.ElicitationQueue[0]
	m.ElicitationQueue = m.ElicitationQueue[1:]
	// 注意：队列中的请求没有保存 RPCID，这里传 nil
	m.showElicitation(nil, next)
}

// PendingElicitationCount 返回后续排队等待的 elicitation 请求数（不含当前弹窗）。
func (m *AppModel) PendingElicitationCount() int { return len(m.ElicitationQueue) }

// ApproveSelection 确认权限弹窗当前选项。返回 (outcome, optionID)。
func (m *AppModel) ApproveSelection() (acp.PermissionOutcome, string) {
	if m.Modal != ModalPermission || m.Permission == nil {
		return acp.OutcomeCancelled, ""
	}
	p := m.Permission
	idx := m.PermSelected
	if idx < 0 || idx >= len(p.Options) {
		return acp.OutcomeCancelled, ""
	}
	opt := p.Options[idx]
	m.advancePermissionQueue()
	return acp.OutcomeSelected, opt.OptionID
}

// CancelPermission 取消权限请求（Esc）。
func (m *AppModel) CancelPermission() {
	if m.Modal == ModalPermission {
		m.advancePermissionQueue()
	}
}

// SetModal 设置/清除模态。
func (m *AppModel) SetModal(k ModalKind) {
	m.Modal = k
	if k != ModalPermission {
		m.Permission = nil
	}
}

// ToggleFocusItem 展开/折叠会话中最近的可折叠项（思考/工具/计划）。
// 返回是否命中。
func (m *AppModel) ToggleFocusItem() bool {
	if !m.HasActive() {
		return false
	}
	s := m.Active
	// 优先最后一个未完成的思考或工具
	for i := len(s.Messages) - 1; i >= 0; i-- {
		msg := s.Messages[i]
		if msg.Kind == MsgThought {
			msg.Expanded = !msg.Expanded
			return true
		}
	}
	if len(s.ToolOrder) > 0 {
		id := s.ToolOrder[len(s.ToolOrder)-1]
		s.ToggleToolCall(id)
		return true
	}
	if s.Plan != nil {
		s.PlanExpanded = !s.PlanExpanded
		return true
	}
	return false
}

// TogglePlan 展开/折叠计划面板。
func (m *AppModel) TogglePlan() {
	if m.HasActive() {
		m.Active.PlanExpanded = !m.Active.PlanExpanded
	}
}

// ScrollUp / ScrollDown / ScrollTop / ScrollBottom 滚动消息区。
// Scroll 在渲染层被同步为当前实际偏移（见 view/message_list.go），
// 因此贴底状态解除后能立刻从上次可见位置继续滚动，而不是从大哨兵值减。
func (m *AppModel) ScrollUp(n int) {
	if m.HasActive() {
		s := m.Active
		s.FollowBottom = false
		s.Scroll -= n
		if s.Scroll < 0 {
			s.Scroll = 0
		}
	}
}

func (m *AppModel) ScrollDown(n int) {
	if m.HasActive() {
		s := m.Active
		s.FollowBottom = false
		s.Scroll += n
		if s.Scroll < 0 {
			s.Scroll = 0 // 溢出保护
		}
	}
}

func (m *AppModel) ScrollTop() {
	if m.HasActive() {
		m.Active.FollowBottom = false
		m.Active.Scroll = 0
	}
}

func (m *AppModel) ScrollBottom() {
	if m.HasActive() {
		s := m.Active
		s.FollowBottom = true
		// Scroll 的实际最大偏移由渲染层写入；这里只需解除旧偏移。
		s.Scroll = 0
	}
}

// NextPermissionOption / PrevPermissionOption 移动权限选项选择。
func (m *AppModel) NextPermissionOption() {
	if m.Permission != nil && m.PermSelected < len(m.Permission.Options)-1 {
		m.PermSelected++
	}
}

func (m *AppModel) PrevPermissionOption() {
	if m.Permission != nil && m.PermSelected > 0 {
		m.PermSelected--
	}
}

// SelectPermissionByKind 快捷选择第一个匹配 kind 的选项（如 a=allow, r=reject）。
func (m *AppModel) SelectPermissionByKind(kind acp.PermissionOptionKind) bool {
	if m.Permission == nil {
		return false
	}
	for i, opt := range m.Permission.Options {
		if opt.Kind == kind {
			m.PermSelected = i
			return true
		}
	}
	return false
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
