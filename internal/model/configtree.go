package model

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/widget"
)

// ConfigKind 是配置值的类型。类型完全由 config/get 返回的 JSON 推断，
// 客户端不内置服务端 schema，避免与服务器版本脱节。
type ConfigKind int

const (
	// ConfigObject 对象（结构体或 map）。
	ConfigObject ConfigKind = iota
	// ConfigArray 数组。
	ConfigArray
	// ConfigString 字符串。
	ConfigString
	// ConfigNumber 数字。
	ConfigNumber
	// ConfigBool 布尔。
	ConfigBool
	// ConfigNull null（如 map 中新增、尚未赋值的键）。
	ConfigNull
)

// configKeyLabels 是服务端配置字段名的中文显示名。键为 alkaid0 服务端 Go
// 结构体硬编码的 JSON 字段名（见 alkaid0 README 配置示例）。编辑器用它作为
// 显示键；未收录的键（如 map 的用户自定义键、数字模型 ID）直接显示原始名称。
// 翻译只做展示，写回的 patch 始终使用原始字段名（Key）。
var configKeyLabels = map[string]string{
	// Config 顶层。
	"Version":       "配置版本",
	"ThemeID":       "主题 ID",
	"Model":         "模型",
	"Agent":         "代理",
	"ignoreSignals": "忽略系统 Signal",
	"Context":       "上下文",
	"Server":        "服务端",
	"Feedback":      "反馈",
	"JSONSchema":    "JSON 模式",
	"DataMask":      "数据掩码",
	// 本地配置（/plugins 弹窗，见 config.Values）。
	"version":             "配置版本",
	"colorMode":           "颜色模式",
	"language":            "界面语言",
	"thinkingExpanded":    "思考默认展开",
	"toolsExpanded":       "工具默认展开",
	"terminalOutputLimit": "终端输出上限",
	"onboardingEffort":    "首个会话推理强度",
	"plugins":             "插件",
	"name":                "名称",
	"command":             "命令",
	"args":                "参数",
	"dir":                 "工作目录",
	"env":                 "环境变量",
	"disabled":            "禁用",
	// Model 与模型项。
	"ProviderURL":            "提供方 URL",
	"ProviderKey":            "提供方密钥",
	"DefaultModelID":         "默认模型 ID",
	"Models":                 "模型集合",
	"ModelName":              "模型名称",
	"ModelID":                "模型 ID",
	"ModelDescription":       "模型描述",
	"ModelAddPrompt":         "附加系统提示词",
	"ModelTopP":              "Top-P",
	"ModelTopK":              "Top-K",
	"ModelTemperature":       "温度",
	"TokenLimit":             "Token 上限",
	"EnableThinking":         "启用思考",
	"EnableToolCalling":      "启用工具调用",
	"CompressSize":           "压缩阈值",
	"Hide":                   "隐藏",
	"Type":                   "类型",
	"ProviderSpecificConfig": "提供方专属配置",
	// ProviderSpecificConfig。
	"EnableDeepseekThinking": "Deepseek 思考",
	"EnableReasoningEffort":  "推理强度",
	"EnableTopP":             "Top-P",
	"EnableTopK":             "Top-K",
	"EnableTemperature":      "温度",
	"EnableUsage":            "用量统计",
	"Dimension":              "向量维度",
	"ToolPromptEnhance":      "工具提示词增强",
	// Agent 与子代理。
	"Agents":                  "子代理",
	"IgnoreBuiltinAgents":     "忽略内置代理",
	"GlobalPrompt":            "全局提示词",
	"SummaryModel":            "摘要模型",
	"MaxCallCount":            "最大调用次数",
	"AutoApprove":             "自动审批",
	"AutoReject":              "自动拒绝",
	"IgnoreDefaultRules":      "忽略默认规则",
	"DisablePromptPreprocess": "禁用提示词预处理",
	"UseShell":                "使用的 Shell",
	"TerminalEnvs":            "终端环境变量",
	"DisableSandbox":          "禁用沙箱",
	"AgentName":               "代理名称",
	"AgentDescription":        "代理描述",
	"AgentShortDescription":   "代理简介",
	"AgentPrompt":             "代理提示词",
	"AgentModel":              "代理模型",
	// Server（README 手写配置用大写，config/get 返回小写，同时收录）。
	"Key":                "访问密钥",
	"key":                "访问密钥",
	"Path":               "路径",
	"path":               "路径",
	"Host":               "主机",
	"host":               "主机",
	"Port":               "端口",
	"port":               "端口",
	"DisableStdioServer": "禁用 stdio 服务",
	"disableStdioServer": "禁用 stdio 服务",
	"SessionTimeout":     "会话超时",
	"sessionTimeout":     "会话超时",
	"protocol":           "协议",
	// Context。
	"LSP":                "语言服务器",
	"Enabled":            "启用",
	"IdleTimeout":        "空闲超时",
	"LanguageServers":    "语言服务器",
	"Command":            "命令",
	"Args":               "参数",
	"EmbeddingModelID":   "嵌入模型 ID",
	"SearchSummaryModel": "搜索摘要模型",
	"OnlineSearch":       "联网搜索",
	"Codebase":           "代码库索引",
	// OnlineSearch 供应商（小写字段）。
	"timeout":             "超时",
	"proxy_url":           "代理 URL",
	"retry_count":         "重试次数",
	"bing":                "Bing",
	"github":              "GitHub",
	"arxiv":               "arXiv",
	"tavily":              "Tavily",
	"enable":              "启用",
	"min_delay":           "最小延迟",
	"max_delay":           "最大延迟",
	"max_results":         "最大结果数",
	"token":               "令牌",
	"api_key":             "API 密钥",
	"search_depth":        "搜索深度",
	"include_answer":      "包含答案",
	"include_raw_content": "包含原始内容",
	// Codebase。
	"BM25Weight":          "BM25 权重",
	"VectorMinSimilarity": "向量最小相似度",
	"BM25RetentionScore":  "BM25 保留分数",
	// Feedback。
	"DisableAutoTelemetry": "禁用自动遥测",
	// 其他已知字段。
	"TitleModel":        "标题模型",
	"FetchProxy":        "抓取代理",
	"Color":             "颜色",
	"Red":               "红",
	"Green":             "绿",
	"Blue":              "蓝",
	"Phrase":            "短语",
	"Price":             "价格",
	"Timeout":           "超时",
	"Flags":             "标志",
	"Enable":            "启用",
	"Ratio":             "比例",
	"Temperature":       "温度",
	"System":            "系统提示",
	"AllowAutoCompact":  "自动压缩会话",
	"AllowSendFeedback": "允许反馈",
	"LocalCommands":     "本地命令",
	"Phrases":           "短语",
	"MaskIPWhitelist":   "IP 白名单",
	"ExitTimeout":       "退出超时",
	"UseFastMode":       "快速模式",
	"Deny":              "拒绝",
	"URLs":              "URL",
	"PerAgent":          "按代理",
	"Global":            "全局",
	"SystemPrompt":      "系统提示",
	"UserPrompt":        "用户提示",
	"Default":           "默认",
	"MergeSimilar":      "合并相似",
	"MinInterval":       "最小间隔",
	"MaxTimes":          "最大次数",
}

// sensitiveConfigKeys 是服务端配置中的敏感字段名（密钥类）。这些名称是
// alkaid0 服务端 Go 结构体硬编码的：Server.Key 的 JSON key 为 "key"，
// 模型密钥字段名为 "ProviderKey"（Model 顶层与每个 Models.* 均有），
// 联网搜索的 "token"/"api_key" 为第三方服务密钥。敏感字符串在编辑器中脱敏展示。
var sensitiveConfigKeys = map[string]bool{
	"key":         true,
	"Key":         true, // Server.Key（README 大写形式）
	"ProviderKey": true,
	"token":       true, // OnlineSearch.github.token
	"api_key":     true, // OnlineSearch.tavily.api_key
}

// maskSecret 把密钥值脱敏为前若干字符 + "***"。短值全掩码。
func maskSecret(v string) string {
	if len(v) <= 4 {
		return "***"
	}
	return v[:4] + "***"
}

// ConfigNode 是配置树中的一个节点。Key 为 JSON 字段名（服务端硬编码名称）
// 或数组元素索引字符串；Path 为从根到本节点的字段路径片段（根节点 Path 为空、
// Key 为空）；Parent 指向父节点（根为 nil）。
type ConfigNode struct {
	Key       string
	Path      []string
	Kind      ConfigKind
	Sensitive bool
	Parent    *ConfigNode
	Children  []*ConfigNode
	Str       string
	Num       float64
	Bool      bool
	// Pending 表示本地配置（/plugins）中新增但尚未写回的插件条目：条目加入
	// 配置树（用于展示与编辑）但不参与持久化，直到其任一字段被编辑时清空
	// 该标记并随整体数组写回——避免仅按一次 Enter 就把空配置写入 config.json。
	Pending bool
}

// DisplayKey 返回该键在界面上的显示名：数组元素显示位置 "[i]"；其余键优先
// 取翻译表（configKeyLabels），未收录的显示原始字段名。中文标签按当前语言
// 翻译（渲染时调用，切换语言即时生效）。
func (n *ConfigNode) DisplayKey() string {
	if n.Parent != nil && n.Parent.Kind == ConfigArray {
		return "[" + n.Key + "]"
	}
	if label, ok := configKeyLabels[n.Key]; ok {
		return i18n.T(label)
	}
	return n.Key
}

// ModelPreview 返回该节点是否为 Model.Models 集合下的模型项，及其 ModelName
// 值（用于 Models 集合页行尾的灰色预览）。非模型项或 ModelName 为空时
// ok=false。ModelName 可能是字符串或 null（新增未赋值），只有非空字符串才算预览。
func (n *ConfigNode) ModelPreview() (name string, ok bool) {
	p := n.Parent
	if p == nil || p.Key != "Models" || p.Parent == nil || p.Parent.Key != "Model" {
		return "", false
	}
	for _, c := range n.Children {
		if c.Key == "ModelName" && c.Kind == ConfigString && c.Str != "" {
			return c.Str, true
		}
	}
	return "", false
}

// buildConfigNode 从解码后的 JSON 值递归构建节点。根节点 key 为空、parent 为 nil。
func buildConfigNode(key string, path []string, parent *ConfigNode, v any) *ConfigNode {
	n := &ConfigNode{Key: key, Path: append([]string(nil), path...), Parent: parent}
	switch val := v.(type) {
	case map[string]any:
		n.Kind = ConfigObject
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sortConfigKeys(keys)
		for _, k := range keys {
			if k == "$schema" {
				continue // $schema 是 schema 元数据，不参与配置编辑（隐藏）。
			}
			n.Children = append(n.Children, buildConfigNode(k, append(n.Path, k), n, val[k]))
		}
	case []any:
		n.Kind = ConfigArray
		for i, item := range val {
			ik := strconv.Itoa(i)
			n.Children = append(n.Children, buildConfigNode(ik, append(n.Path, ik), n, item))
		}
	case string:
		n.Kind = ConfigString
		n.Str = val
		n.Sensitive = sensitiveConfigKeys[key]
	case float64:
		n.Kind = ConfigNumber
		n.Num = val
	case int, int8, int16, int32, int64:
		// json.Unmarshal 产生 float64；但新增项（AddModelsItem/ConfirmAddKey）
		// 直接构造 map[string]any 时数字是 Go int，需一并识别为数字，
		// 否则会落入 default 变成 ConfigNull（表现为新建内容显示 null）。
		n.Kind = ConfigNumber
		n.Num = float64(reflect.ValueOf(val).Int())
	case uint, uint8, uint16, uint32, uint64:
		n.Kind = ConfigNumber
		n.Num = float64(reflect.ValueOf(val).Uint())
	case bool:
		n.Kind = ConfigBool
		n.Bool = val
	default:
		n.Kind = ConfigNull
	}
	return n
}

// sortConfigKeys 按自然顺序排序对象键：数字字符串按数值比较，其余按字典序。
// 使 Models/Agents 等数字键的模型集合按 ID 顺序展示。
func sortConfigKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool { return configKeyLess(keys[i], keys[j]) })
}

// configKeyLess 是配置对象键的排序比较：数字字符串按数值比较，其余按字典序。
func configKeyLess(a, b string) bool {
	ai, erra := strconv.Atoi(a)
	bi, errb := strconv.Atoi(b)
	if erra == nil && errb == nil {
		return ai < bi
	}
	return a < b
}

// IsLocalConfig 报告该编辑器编辑的是本地 config.json（/plugins）而非服务端
// 配置。本地配置的根页支持直接新增 plugins 段。
func (ed *ConfigEditor) IsLocalConfig() bool { return ed.isLocalConfig }

// HasPluginsArray 报告本地配置根下是否存在可编辑的 plugins 数组（缺失或为
// null 等非数组值时返回 false，此时根页提供「(新增)」入口来新建该段）。
func (ed *ConfigEditor) HasPluginsArray() bool {
	if ed.Root == nil {
		return false
	}
	for _, c := range ed.Root.Children {
		if c.Key == "plugins" && c.Kind == ConfigArray {
			return true
		}
	}
	return false
}

// AsValue 返回节点作为 JSON 值的表示（供序列化 patch）。
func (n *ConfigNode) AsValue() any {
	switch n.Kind {
	case ConfigObject:
		m := make(map[string]any, len(n.Children))
		for _, c := range n.Children {
			m[c.Key] = c.AsValue()
		}
		return m
	case ConfigArray:
		out := make([]any, 0, len(n.Children))
		for _, c := range n.Children {
			out = append(out, c.AsValue())
		}
		return out
	case ConfigString:
		return n.Str
	case ConfigNumber:
		return n.Num
	case ConfigBool:
		return n.Bool
	default:
		return nil
	}
}

// nodePatch 构造把 path 指向的字段设为 val 的部分更新 JSON。
// 例如 path=["Model","DefaultModelID"]、val=1 → {"Model":{"DefaultModelID":1}}。
func nodePatch(path []string, val any) json.RawMessage {
	if len(path) == 0 {
		b, _ := json.Marshal(val)
		return b
	}
	var build func(i int) any
	build = func(i int) any {
		if i == len(path)-1 {
			return map[string]any{path[i]: val}
		}
		return map[string]any{path[i]: build(i + 1)}
	}
	b, _ := json.Marshal(build(0))
	return b
}

// ConfigEditor 是配置树编辑器状态（/server 服务端配置弹窗与 /plugins 本地
// config.json 弹窗共用）。采用子页面导航：每次只展示一个集合（对象/数组）的
// 直接子项作为一页，Stack 记录从根到当前页面的路径，Enter 进入子页面、
// Back 返回上一页。编辑即自动保存：每次修改后调用方构造 patch 并写回。
type ConfigEditor struct {
	Root     *ConfigNode
	Stack    []*ConfigNode // 导航栈；Stack[0] 恒为根对象
	EntryIdx []int         // 与 Stack 平行：进入该层时父层选中行索引
	Selected int           // 当前页面内的选中行索引

	Editing   bool
	EditNode  *ConfigNode
	EditInput *widget.InputBuffer

	AddingKey bool
	AddInput  *widget.InputBuffer

	// Saving 表示配置写回（config/set）与随后的全量重载（config/get）进行中。
	// 期间阻塞新的改动，界面底部显示"保存中…"，直到重载完成解除。
	Saving bool

	// isLocalConfig 表示编辑的是本地 config.json（/plugins）而非服务端配置。
	// 用于根页新增 plugins 段、plugins 数组整体替换写回与 pending 延迟写回等
	// 本地配置特有的语义。
	isLocalConfig bool
}

// NewConfigEditor 从 config/get 的完整配置 JSON 构建配置编辑器。
func NewConfigEditor(raw json.RawMessage) *ConfigEditor {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		root = map[string]any{}
	}
	ed := &ConfigEditor{Root: buildConfigNode("", nil, nil, root)}
	ed.Stack = []*ConfigNode{ed.Root}
	return ed
}

// Current 返回当前页面所属的集合节点（对象或数组）。
func (ed *ConfigEditor) Current() *ConfigNode {
	return ed.Stack[len(ed.Stack)-1]
}

// CurrentChildren 返回当前页面的行（当前集合节点的直接子项）。
func (ed *ConfigEditor) CurrentChildren() []*ConfigNode {
	return ed.Current().Children
}

// Crumb 返回从根到当前页面的面包屑片段（根显示 "Config"）。进入数组后
// 片段为位置 "[i]"。
func (ed *ConfigEditor) Crumb() []string {
	out := make([]string, 0, len(ed.Stack))
	out = append(out, "Config")
	for i := 1; i < len(ed.Stack); i++ {
		out = append(out, ed.Stack[i].DisplayKey())
	}
	return out
}

// SelectedNode 返回当前页面选中的行；页面为空时返回 nil。
func (ed *ConfigEditor) SelectedNode() *ConfigNode {
	children := ed.CurrentChildren()
	if ed.Selected < 0 || ed.Selected >= len(children) {
		return nil
	}
	return children[ed.Selected]
}

// Move 上下移动当前页面选中行（带环绕）。「(新增)」/「(删除该项)」都是普通
// 可选行，参与同一环绕。
func (ed *ConfigEditor) Move(delta int) {
	n := ed.RowCount()
	if n == 0 {
		return
	}
	ed.Selected = (ed.Selected + delta + n) % n
}

// CanAdd 报告当前页面（集合）是否允许新增条目。开放的集合：Model.Models
// （模型集合，键为数字字符串）、Agent.Agents（子代理集合，键为名称）与
// Context.LSP.LanguageServers（语言服务器集合，键为文件扩展名），以及
// Context.Phrase.Phrases（短语数组，直接追加元素）与本地配置的 plugins
// 数组（本地插件，/plugins 弹窗）。本地配置根页且尚无 plugins 段时也允许
// 新增——直接创建 plugins 数组并追加首条目（见 AddPluginsArray），使无插件
// 配置也能进入插件管理而不凭空生成空段。
func (ed *ConfigEditor) CanAdd() bool {
	n := ed.Current()
	if n == nil {
		return false
	}
	switch n.Kind {
	case ConfigObject:
		// 本地配置（/plugins）根页且尚无 plugins 数组（缺失或为 null 等非数组值）。
		if ed.isLocalConfig && n.Parent == nil && !ed.HasPluginsArray() {
			return true
		}
		switch n.Key {
		case "Models":
			return n.Parent != nil && n.Parent.Key == "Model"
		case "Agents":
			return n.Parent != nil && n.Parent.Key == "Agent"
		case "LanguageServers":
			return n.Parent != nil && n.Parent.Key == "LSP"
		}
	case ConfigArray:
		return n.Key == "plugins" || (n.Key == "Phrases" && n.Parent != nil && n.Parent.Key == "Phrase")
	}
	return false
}

// IsModels 报告当前页面是否为 Model.Models（数字键的模型集合）。
func (ed *ConfigEditor) IsModels() bool {
	n := ed.Current()
	return n != nil && n.Key == "Models" && n.Parent != nil && n.Parent.Key == "Model"
}

// CanDelete 报告当前页面是否提供「(删除该项)」行：当前页面是单个模型项
// （Model.Models.*）、子代理项（Agent.Agents.*）、语言服务器项
// （Context.LSP.LanguageServers.*）、短语项（Context.Phrase.Phrases[*]）
// 或本地插件项（plugins[*]）的子页时，可删除该项本身。
func (ed *ConfigEditor) CanDelete() bool {
	n := ed.Current()
	if n == nil {
		return false
	}
	p := n.Parent
	if p == nil {
		return false
	}
	return (p.Key == "Models" && p.Parent != nil && p.Parent.Key == "Model") ||
		(p.Key == "Agents" && p.Parent != nil && p.Parent.Key == "Agent") ||
		(p.Key == "LanguageServers" && p.Parent != nil && p.Parent.Key == "LSP") ||
		(p.Key == "Phrases" && p.Kind == ConfigArray && p.Parent != nil && p.Parent.Key == "Phrase") ||
		(p.Key == "plugins" && p.Kind == ConfigArray)
}

// AddRowIndex 返回「(新增)」行的行索引；当前页面不允许新增时返回 -1。
func (ed *ConfigEditor) AddRowIndex() int {
	if ed.CanAdd() {
		return len(ed.CurrentChildren())
	}
	return -1
}

// DelRowIndex 返回「(删除该项)」行的行索引；当前页面不可删除时返回 -1。
// 删除行排在所有普通行之后（「(新增)」行之后，若两者都存在）。
func (ed *ConfigEditor) DelRowIndex() int {
	if !ed.CanDelete() {
		return -1
	}
	idx := len(ed.CurrentChildren())
	if ed.CanAdd() {
		idx++
	}
	return idx
}

// RowCount 返回当前页面的总行数（含末尾「(新增)」/「(删除该项)」行，若存在）。
func (ed *ConfigEditor) RowCount() int {
	n := len(ed.CurrentChildren())
	if ed.AddRowIndex() >= 0 {
		n++
	}
	if ed.DelRowIndex() >= 0 {
		n++
	}
	return n
}

// OnAddRow 报告选中行是否为末尾的「(新增)」行。
func (ed *ConfigEditor) OnAddRow() bool {
	return ed.Selected == ed.AddRowIndex()
}

// OnDeleteRow 报告选中行是否为末尾的「(删除该项)」行。
func (ed *ConfigEditor) OnDeleteRow() bool {
	return ed.Selected == ed.DelRowIndex()
}

// Enter 进入当前选中行（对象/数组）的子页面，返回是否进入。标量行不动作。
func (ed *ConfigEditor) Enter() bool {
	n := ed.SelectedNode()
	if n == nil || (n.Kind != ConfigObject && n.Kind != ConfigArray) {
		return false
	}
	ed.EntryIdx = append(ed.EntryIdx, ed.Selected)
	ed.Stack = append(ed.Stack, n)
	ed.Selected = 0
	return true
}

// Back 返回上一页，恢复进入时的选中行。已在根页面时返回 false。
func (ed *ConfigEditor) Back() bool {
	if len(ed.Stack) <= 1 {
		return false
	}
	ed.Stack = ed.Stack[:len(ed.Stack)-1]
	ed.Selected = ed.EntryIdx[len(ed.EntryIdx)-1]
	ed.EntryIdx = ed.EntryIdx[:len(ed.EntryIdx)-1]
	return true
}

// ValueText 返回节点的值显示文本。敏感字符串脱敏为前几字符 + "***"；
// 对象/数组由 view 用箭头指示可进入，返回空串（不展示子项数量）。
func (n *ConfigNode) ValueText() string {
	switch n.Kind {
	case ConfigObject, ConfigArray:
		return ""
	case ConfigString:
		if n.Sensitive {
			return maskSecret(n.Str)
		}
		return strconv.Quote(n.Str)
	case ConfigNumber:
		return strconv.FormatFloat(n.Num, 'f', -1, 64)
	case ConfigBool:
		return strconv.FormatBool(n.Bool)
	default:
		return "null"
	}
}

// ToggleBool 切换布尔叶子节点并返回写回 patch。
func (ed *ConfigEditor) ToggleBool() json.RawMessage {
	n := ed.SelectedNode()
	if n == nil || n.Kind != ConfigBool {
		return nil
	}
	n.Bool = !n.Bool
	return ed.patchForNode(n, n.Bool)
}

// patchForNode 返回把节点 n 的修改写回所需的 patch。
//
// 若 n 位于某数组祖先内（如 Context.Phrase.Phrases[*].Short 或字符串数组
// Args[*]），服务端对数组字段是整体替换语义，按字段路径构造的局部 patch
// （{"Phrases":{"0":{...}}}）会把数组段变成 map，服务端 applyPatch 类型不匹配
// 保存失败。因此必须整体替换最近的数组祖先（内含 n 的全部兄弟元素）。
// 否则按字段路径局部 patch（map 集合与 struct 字段均可局部更新）。
//
// 本地配置（/plugins）的 plugins 数组同理：其子树内（含条目内的 args/env
// 数组）任何编辑都写回整个 plugins 数组，且过滤尚未写回的 pending 空条目，
// 避免按字段路径的局部 patch 被 mergeJSON 误判为 map、以及把空配置持久化。
func (ed *ConfigEditor) patchForNode(n *ConfigNode, val any) json.RawMessage {
	for a := n.Parent; a != nil; a = a.Parent {
		if a.Kind == ConfigArray && ed.isLocalConfig && a.Key == "plugins" {
			ed.clearPendingEntry(n)
			return nodePatch(a.Path, ed.pluginsArrayValue(a))
		}
	}
	for a := n.Parent; a != nil; a = a.Parent {
		if a.Kind == ConfigArray {
			return nodePatch(a.Path, a.AsValue())
		}
	}
	return nodePatch(n.Path, val)
}

// pluginsArrayValue 返回 plugins 数组的 JSON 值，排除尚未写回的 pending 条目
// （新增后未编辑任何字段的空条目），避免把空配置持久化到 config.json。
func (ed *ConfigEditor) pluginsArrayValue(a *ConfigNode) any {
	out := make([]any, 0, len(a.Children))
	for _, c := range a.Children {
		if c.Pending {
			continue
		}
		out = append(out, c.AsValue())
	}
	return out
}

// clearPendingEntry 清除包含 n 的 plugins 数组条目的 pending 标记：编辑该条目
// 任一字段即视为提交，此后随整体数组写回。非 plugins 子树内调用为无操作。
func (ed *ConfigEditor) clearPendingEntry(n *ConfigNode) {
	for a := n; a != nil; a = a.Parent {
		if a.Parent != nil && a.Parent.Kind == ConfigArray && a.Parent.Key == "plugins" {
			a.Pending = false
			return
		}
	}
}

// BeginEdit 进入当前标量节点的值编辑模式。敏感字段输入框初始为空
// （展示已脱敏，编辑需重新输入完整值）。
func (ed *ConfigEditor) BeginEdit() {
	n := ed.SelectedNode()
	if n == nil {
		return
	}
	switch n.Kind {
	case ConfigString, ConfigNumber, ConfigNull:
		// 支持编辑
	default:
		return
	}
	ed.EditNode = n
	ed.Editing = true
	if ed.EditInput == nil {
		ed.EditInput = widget.NewInputBuffer()
	}
	ed.EditInput.Clear()
	if n.Kind != ConfigNull && !n.Sensitive {
		if n.Kind == ConfigString {
			ed.EditInput = inputFromString(n.Str)
		} else {
			ed.EditInput = inputFromString(strconv.FormatFloat(n.Num, 'f', -1, 64))
		}
	}
}

// inputFromString 构造预填文本的输入缓冲。
func inputFromString(s string) *widget.InputBuffer {
	b := widget.NewInputBuffer()
	b.Lines = widget.SplitLines(s)
	b.CY = len(b.Lines) - 1
	b.CX = len(b.Lines[b.CY])
	return b
}

// CancelEdit 取消值编辑。
func (ed *ConfigEditor) CancelEdit() {
	ed.Editing = false
	ed.EditNode = nil
}

// CommitEdit 解析编辑输入为对应类型的值并更新节点，返回写回 patch。
// 成功时 ok=true；解析失败返回 errMsg（节点与树不变）。
func (ed *ConfigEditor) CommitEdit() (patch json.RawMessage, ok bool, errMsg string) {
	if !ed.Editing || ed.EditNode == nil {
		return nil, false, ""
	}
	text := ed.EditInput.Text()
	n := ed.EditNode
	switch n.Kind {
	case ConfigNumber:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, false, i18n.T("无效数字: %s", text)
		}
		n.Num = f
	case ConfigNull:
		// null 字段没有类型信息：按输入内容推断。数字→ConfigNumber、
		// true/false→ConfigBool、否则字符串——避免把数字/布尔字段发成字符串，
		// 否则服务端 unmarshal 类型不匹配会报错（"保存失败无法写入"）。
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			n.Kind = ConfigNumber
			n.Num = f
		} else if b, err := strconv.ParseBool(text); err == nil {
			n.Kind = ConfigBool
			n.Bool = b
		} else {
			n.Kind = ConfigString
			n.Str = text
		}
	default:
		// 字符串：按字符串处理。
		n.Kind = ConfigString
		n.Str = text
	}
	patch = ed.patchForNode(n, n.AsValue())
	ed.Editing = false
	ed.EditNode = nil
	return patch, true, ""
}

// AddModelsItem 在 Model.Models 集合页的「(新增)」行上新增一个模型。
// 键为下一个数字（现有最大数字键 + 1，首个为 "0"），加载 alkaid0 README
// 中的完整模型配置项（含默认值，-1 表示未设置），写回 patch 仅含新键。
// 新增后自动进入该模型的子页面并选中首行，便于立即编辑。
func (ed *ConfigEditor) AddModelsItem() (json.RawMessage, bool) {
	n := ed.Current()
	if n == nil || n.Key != "Models" {
		return nil, false
	}
	next := 0
	for _, c := range n.Children {
		if idx, err := strconv.Atoi(c.Key); err == nil && idx >= next {
			next = idx + 1
		}
	}
	key := strconv.Itoa(next)
	val := map[string]any{
		"ModelName":         "",
		"ModelID":           "",
		"ModelDescription":  "",
		"ModelAddPrompt":    "",
		"ModelTopP":         -1,
		"ModelTopK":         -1,
		"ModelTemperature":  -1,
		"TokenLimit":        8192,
		"ProviderURL":       "",
		"ProviderKey":       "",
		"EnableThinking":    true,
		"EnableToolCalling": false,
		"CompressSize":      128000,
		"Hide":              false,
		"Type":              "",
		"ProviderSpecificConfig": map[string]any{
			"EnableDeepseekThinking": false,
			"EnableReasoningEffort":  true,
			"EnableTopP":             false,
			"EnableTopK":             false,
			"EnableTemperature":      false,
			"EnableUsage":            true,
			"Dimension":              0,
			"ToolPromptEnhance":      "auto",
		},
	}
	child := buildConfigNode(key, append(n.Path, key), n, val)
	n.Children = append(n.Children, child)
	// 自动进入新模型子页面并选中首行，便于立即编辑。
	ed.EntryIdx = append(ed.EntryIdx, ed.Selected)
	ed.Stack = append(ed.Stack, child)
	ed.Selected = 0
	patch := nodePatch(n.Path, map[string]any{key: val})
	return patch, true
}

// AddPhrasesItem 在 Context.Phrase.Phrases 数组页的「(新增)」行上追加一个短语
// 元素（{Short:"", Text:"", Desc:""}），并自动进入其子页面选中首行。数组字段
// 服务端 config/set 整体替换，返回的 patch 为整个数组（含新元素与全部兄弟元素，
// 未修改的元素按当前树序列化）。数组新增不需要键名，交互与 map 集合不同。
func (ed *ConfigEditor) AddPhrasesItem() (json.RawMessage, bool) {
	n := ed.Current()
	if n == nil || n.Kind != ConfigArray || n.Key != "Phrases" {
		return nil, false
	}
	key := strconv.Itoa(len(n.Children))
	val := map[string]any{
		"Short": "",
		"Text":  "",
		"Desc":  "",
	}
	child := buildConfigNode(key, append(n.Path, key), n, val)
	n.Children = append(n.Children, child)
	// 自动进入新元素子页面并选中首行，便于立即编辑。
	ed.EntryIdx = append(ed.EntryIdx, ed.Selected)
	ed.Stack = append(ed.Stack, child)
	ed.Selected = 0
	return nodePatch(n.Path, n.AsValue()), true
}

// AddPluginsItem 在本地配置 plugins 数组页的「(新增)」行上追加一个插件条目
// （{Name:"", Command:"", Disabled:false}），并自动进入其子页面选中首行。
// 新条目标记为 pending（尚未写回）：不立即保存，直到该条目任一字段被编辑时
// 才随整体数组写回（见 patchForNode/pluginsArrayValue）——避免仅按一次 Enter
// 就把空配置持久化到 config.json。插件改动在重启 alcoh 后生效。
func (ed *ConfigEditor) AddPluginsItem() bool {
	n := ed.Current()
	if n == nil || n.Kind != ConfigArray || n.Key != "plugins" {
		return false
	}
	key := strconv.Itoa(len(n.Children))
	val := map[string]any{
		"Name":     "",
		"Command":  "",
		"Disabled": false,
	}
	child := buildConfigNode(key, append(n.Path, key), n, val)
	child.Pending = true
	n.Children = append(n.Children, child)
	// 自动进入新条目子页面并选中首行，便于立即编辑。
	ed.EntryIdx = append(ed.EntryIdx, ed.Selected)
	ed.Stack = append(ed.Stack, child)
	ed.Selected = 0
	return true
}

// AddPluginsArray 在本地配置根页「(新增)」行上新增 plugins 数组并追加首个
// 插件条目（配置中还没有 plugins 段时）。根下 plugins 键若存在但非数组
// （如 null）则替换为数组；排序保持。新条目同样标记 pending 延迟写回。
// 成功进入新条目子页面时返回 true。
func (ed *ConfigEditor) AddPluginsArray() bool {
	if ed.Root == nil || ed.Root.Kind != ConfigObject || !ed.isLocalConfig {
		return false
	}
	for i, c := range ed.Root.Children {
		if c.Key != "plugins" {
			continue
		}
		if c.Kind == ConfigArray {
			return false // 已是数组，应走 AddPluginsItem。
		}
		ed.Root.Children = append(ed.Root.Children[:i], ed.Root.Children[i+1:]...)
		break
	}
	child := buildConfigNode("plugins", []string{"plugins"}, ed.Root, []any{})
	ed.Root.Children = append(ed.Root.Children, child)
	sort.Slice(ed.Root.Children, func(i, j int) bool {
		return configKeyLess(ed.Root.Children[i].Key, ed.Root.Children[j].Key)
	})
	// 进入 plugins 页并追加首条目（AddPluginsItem 会继续进入该条目子页）。
	ed.Focus([]string{"plugins"})
	return ed.AddPluginsItem()
}

// BeginAddKey 进入当前集合页（名称键的 map：Agent.Agents、Context.LSP.
// LanguageServers）的新增键输入模式（「(新增)」行）。仅在 CanAdd 时由 app 调用。
func (ed *ConfigEditor) BeginAddKey() bool {
	n := ed.Current()
	if n == nil || n.Kind != ConfigObject {
		return false
	}
	ed.AddingKey = true
	if ed.AddInput == nil {
		ed.AddInput = widget.NewInputBuffer()
	}
	ed.AddInput.Clear()
	return true
}

// CancelAddKey 取消新增键输入。
func (ed *ConfigEditor) CancelAddKey() {
	ed.AddingKey = false
}

// ConfirmAddKey 用输入框文本作为新键名，在当前集合下新增条目（加载该集合的
// 默认配置项），并自动进入其子页面选中首行，返回写回 patch（该键为完整对象；
// 服务端 config/set 对 map 字段新增键）。支持 Agent.Agents（子代理，加载 README
// 完整代理配置）与 Context.LSP.LanguageServers（语言服务器，Command+Args）。
// 键名为空时返回错误。
func (ed *ConfigEditor) ConfirmAddKey() (json.RawMessage, bool, string) {
	if !ed.AddingKey {
		return nil, false, ""
	}
	n := ed.Current()
	if n == nil || n.Kind != ConfigObject {
		return nil, false, ""
	}
	key := ed.AddInput.Text()
	if key == "" {
		return nil, false, i18n.T("键名不能为空")
	}
	var val map[string]any
	switch n.Key {
	case "Agents":
		val = map[string]any{
			"AgentName":             "",
			"AgentDescription":      "",
			"AgentShortDescription": "",
			"AgentPrompt":           "",
			"AgentModel":            0,
			"AutoApprove":           "",
			"AutoReject":            "",
			"Color":                 map[string]any{"Red": 128, "Green": 128, "Blue": 128},
			"DisableSandbox":        false,
		}
	case "LanguageServers":
		val = map[string]any{
			"Command": "",
			"Args":    []any{},
		}
	default:
		return nil, false, i18n.T("该集合不支持新增")
	}
	child := buildConfigNode(key, append(n.Path, key), n, val)
	n.Children = append(n.Children, child)
	ed.AddingKey = false
	// 自动进入新代理子页面并选中首行，便于立即编辑。
	ed.EntryIdx = append(ed.EntryIdx, ed.Selected)
	ed.Stack = append(ed.Stack, child)
	ed.Selected = 0
	patch := nodePatch(n.Path, map[string]any{key: val})
	return patch, true, ""
}

// DeleteItem 删除当前页面本身（模型项、子代理项、语言服务器项或短语项），并从
// 导航栈返回其父页面。父为数组（Context.Phrase.Phrases）时整体替换写回（真删除）；
// 父为对象（Model.Models / Agent.Agents / Context.LSP.LanguageServers 均为 map）时
// 以 null 键写回——服务端 config/set 对 map 字段的 null 键会真正删除（返回的
// patch 为 {key: null}）。删除后父页面选中相邻项（被删项位置越界时 clamp 到末尾），
// 便于继续操作。
func (ed *ConfigEditor) DeleteItem() (json.RawMessage, bool) {
	n := ed.Current()
	if n == nil || n == ed.Root || n.Parent == nil {
		return nil, false
	}
	parent := n.Parent
	// 进入当前页时父页的选中行（EntryIdx 栈顶）。
	entry := 0
	if len(ed.EntryIdx) > 0 {
		entry = ed.EntryIdx[len(ed.EntryIdx)-1]
	}
	for i, c := range parent.Children {
		if c == n {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}
	// 返回父页面，选中相邻项。
	ed.Stack = ed.Stack[:len(ed.Stack)-1]
	ed.EntryIdx = ed.EntryIdx[:len(ed.EntryIdx)-1]
	if entry >= len(parent.Children) {
		entry = len(parent.Children) - 1
	}
	if entry < 0 {
		entry = 0
	}
	ed.Selected = entry
	if parent.Kind == ConfigArray {
		// 数组：删除后整体替换写回。数组元素键是索引字符串，删除后重排剩余
		// 元素的 Key 与整棵子树的 Path，保证索引连续（避免显示 "[1]" 等空洞）。
		for i, c := range parent.Children {
			ik := strconv.Itoa(i)
			c.Key = ik
			setPath(c, append(append([]string(nil), parent.Path...), ik))
		}
		// 本地 plugins 数组：过滤尚未写回的 pending 条目，避免把空条目一并落盘。
		if ed.isLocalConfig && parent.Key == "plugins" {
			return nodePatch(parent.Path, ed.pluginsArrayValue(parent)), true
		}
		return nodePatch(parent.Path, parent.AsValue()), true
	}
	// 对象：本地删除键，以 null 键写回（服务端 config/set 真正删除 map 键）。
	return nodePatch(parent.Path, map[string]any{n.Key: nil}), true
}

// setPath 把 node 及其整个子树的 Path 更新为 path（数组元素删除重排索引后，
// 键改变需要同步，否则后续编辑会基于旧的 Path 生成错误 patch）。
func setPath(n *ConfigNode, path []string) {
	n.Path = append([]string(nil), path...)
	for _, c := range n.Children {
		setPath(c, append(append([]string(nil), path...), c.Key))
	}
}

// Focus 从根重建导航栈，沿 path 逐层展开，最终把 path 末段对应的节点作为
// 当前页面（若末段是对象/数组则为该节点的子页）。用于新增项写回服务端后
// 触发整配置重载，并把视图重定向到新项页面。path 中任一段在当前树中缺失时
// 停在最近可达的页面；path 为空或 nil 时回到根页。
func (ed *ConfigEditor) Focus(path []string) {
	ed.Stack = []*ConfigNode{ed.Root}
	ed.EntryIdx = []int{}
	ed.Selected = 0
	cur := ed.Root
	for _, seg := range path {
		idx := -1
		for i, c := range cur.Children {
			if c.Key == seg {
				idx = i
				break
			}
		}
		if idx < 0 {
			return // 段不存在（键已变化/被删），停留在最近可达页面。
		}
		// 只进入可展开的容器（对象/数组）；标量/null 停留在父页，避免聚焦进
		// 无内容的子页（如本地配置的 plugins:null，其修复入口在根页「(新增)」）。
		if k := cur.Children[idx].Kind; k != ConfigObject && k != ConfigArray {
			return
		}
		ed.EntryIdx = append(ed.EntryIdx, idx)
		ed.Stack = append(ed.Stack, cur.Children[idx])
		ed.Selected = 0
		cur = cur.Children[idx]
	}
}

// configState 是整配置重载前捕获的导航与编辑状态，用于重建后恢复。
// 新增写回成功后触发的重载不应打断用户正在进行的字段编辑。
type configState struct {
	path     []string // 当前导航栈各层 Key（Stack[i].Key）
	selected int      // 当前页面内的选中行索引（重载后恢复，避免停在行首）
	editPath []string // 正在编辑的节点 Path；nil 表示未在编辑
	editText string   // 编辑输入框当前文本
	adding   bool     // 是否在新增键输入模式
	addText  string   // 新增键输入框文本
}

// CaptureState 捕获当前导航与编辑状态，供整配置重载（SetServerConfig 重建）
// 后经 RestoreState 恢复。
func (ed *ConfigEditor) CaptureState() configState {
	s := configState{selected: ed.Selected}
	for _, n := range ed.Stack {
		if n.Key == "" {
			continue // 根节点（Key 为空）不参与 Focus 路径定位。
		}
		s.path = append(s.path, n.Key)
	}
	if ed.Editing && ed.EditNode != nil {
		s.editPath = append([]string(nil), ed.EditNode.Path...)
		s.editText = ed.EditInput.Text()
	}
	if ed.AddingKey {
		s.adding = true
		s.addText = ed.AddInput.Text()
	}
	return s
}

// RestoreState 在整配置重载重建后的新树上恢复捕获的导航与编辑状态。
// 导航按 Key 重新定位；正在编辑的节点若仍存在则重新进入编辑并恢复输入文本。
func (ed *ConfigEditor) RestoreState(s configState) {
	if len(s.path) > 0 {
		ed.Focus(s.path)
	}
	if s.editPath != nil {
		ed.focusNode(s.editPath)
		ed.BeginEdit()
		if s.editText != "" {
			ed.EditInput = inputFromString(s.editText)
		}
		return
	}
	// 恢复当前页选中行（Focus 默认停在行首）。重载后行数可能变化（增删项），
	// clamp 到有效范围；editPath 分支已由 focusNode 定位，不再覆盖。
	if s.selected > 0 {
		if n := ed.RowCount(); s.selected >= n {
			s.selected = n - 1
		}
		if s.selected < 0 {
			s.selected = 0
		}
		ed.Selected = s.selected
	}
	if s.adding {
		ed.AddingKey = true
		ed.AddInput = inputFromString(s.addText)
	}
}

// focusNode 把指定路径的节点作为当前选中行（导航到其所在页并选中它）。
func (ed *ConfigEditor) focusNode(path []string) {
	if len(path) == 0 {
		return
	}
	ed.Focus(path[:len(path)-1])
	last := path[len(path)-1]
	for i, c := range ed.CurrentChildren() {
		if c.Key == last {
			ed.Selected = i
			return
		}
	}
}

// IsEditing 报告当前是否处于值编辑或新增键输入模式（重载重建后不应被覆盖）。
func (ed *ConfigEditor) IsEditing() bool {
	return ed.Editing || ed.AddingKey
}
