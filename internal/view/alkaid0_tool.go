package view

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
)

// 本文件实现 alkaid0 私有协议 v0.4 下工具调用的客户端渲染。
//
// 服务端声明 alk.cxykevin.top/alkaid0/v0.4 后，工具调用标题不再是
// "[Call <name>]<callId>"，而是由工具名与关键参数拼出的可读签名（如
// Edit(src/main.go)、Run shell*(go test ./...)、Search online(golang)）；
// 正文（展开后）只展示标题没有消费的其余参数，服务端随 content 下发的
// 全参数文本块不再重复渲染。参数来自 content 里的
// alk.cxykevin.top/calling_info 块（alkaid0 不发 rawInput，见服务端
// docs/acp/extension.md §4.1）。没有该块（非 alkaid0 工具）时回退标准渲染。

// alkaid0Call 是 alkaid0 工具调用的私有展示形态。
type alkaid0Call struct {
	Title string          // 拼出的标题，例如 Run shell*(npm run dev)
	Used  map[string]bool // 标题已消费的参数键：正文不再重复展示
	Args  map[string]any  // calling_info.args 解析出的完整参数
}

// alkaid0TitleKeys 是各工具放进标题的主参数（按顺序拼接）；未列出的工具
// 标题只有工具名（Name()），全部参数留给正文。run / read / search 格式
// 特殊，在 alkaid0CallSignature 中单独处理。
var alkaid0TitleKeys = map[string][]string{
	"edit":             {"path"},
	"fetch":            {"method", "url"},
	"scope":            {"name"},
	"agent":            {"name"},
	"activate_agent":   {"name"},
	"deactivate_agent": {"name"},
}

// alkaid0TitleNames 覆盖标题里的工具显示名（默认把工具名首字母大写）。
// activate_agent / deactivate_agent 是子代理生命周期工具，展示为可读名。
var alkaid0TitleNames = map[string]string{
	"activate_agent":   "Use Agent",
	"deactivate_agent": "Deactivate Agent",
}

// alkaid0ToolCall 从工具调用的 calling_info 参数块构造私有展示形态。
// 没有该块或块里没有有效参数对象时返回 nil，调用方回退标准渲染。
func alkaid0ToolCall(tc *model.ToolCall) *alkaid0Call {
	name, args, ok := alkaid0CallingInfo(tc)
	if !ok {
		return nil
	}
	title, used := alkaid0CallSignature(name, args)
	return &alkaid0Call{Title: title, Used: used, Args: args}
}

// alkaid0CallingInfo 在 content 中查找 alkaid0 私有 calling_info 块，
// 返回工具名与解析后的参数。工具名取块的 name 字段，缺失时回退服务端
// 标题 "[Call <name>]<id>" 里的名字。
func alkaid0CallingInfo(tc *model.ToolCall) (string, map[string]any, bool) {
	for _, ct := range tc.Content {
		if ct.Type != acp.ToolCallingInfoType || len(ct.Args) == 0 {
			continue
		}
		args := map[string]any{}
		if err := json.Unmarshal(ct.Args, &args); err != nil {
			continue
		}
		name := ct.Name
		if name == "" {
			name = alkaid0NameFromTitle(tc.Title)
		}
		if name == "" {
			continue
		}
		return name, args, true
	}
	return "", nil, false
}

// alkaid0NameFromTitle 从服务端标题 "[Call <name>]<callId>" 中取出工具名。
func alkaid0NameFromTitle(title string) string {
	const prefix = "[Call "
	if !strings.HasPrefix(title, prefix) {
		return ""
	}
	rest := title[len(prefix):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// alkaid0CallSignature 按工具名与参数拼出标题，并返回标题消费掉的参数键。
func alkaid0CallSignature(name string, args map[string]any) (string, map[string]bool) {
	used := map[string]bool{}
	switch name {
	case "run":
		// Run {type}({command})；后台任务在 type 后加 * 标记。
		star := ""
		if alkaid0Bool(args["background"]) {
			star = "*"
		}
		used["type"], used["command"], used["background"] = true, true, true
		return "Run " + alkaid0TitleValue(args["type"]) + star +
			"(" + alkaid0TitleValue(args["command"]) + ")", used
	case "search":
		// Search({query})；联网搜索显示为 Search online({query})。
		online := ""
		if alkaid0Bool(args["online"]) {
			online = " online"
		}
		used["query"], used["online"], used["keyword"] = true, true, true
		query := args["query"]
		if query == nil {
			query = args["keyword"]
		}
		return "Search" + online + "(" + alkaid0TitleValue(query) + ")", used
	case "read":
		// Read({path})；流式预览期 alkaid0 把 path 放在 name 键上（服务端
		// trace 工具 OnHook 的历史遗留），最终状态会归一化为 path。
		used["path"], used["name"] = true, true
		path := args["path"]
		if path == nil {
			path = args["name"]
		}
		return "Read(" + alkaid0TitleValue(path) + ")", used
	}
	keys, ok := alkaid0TitleKeys[name]
	if !ok {
		// 未收录的工具（插件/第三方工具）：标题只写工具名，参数全部留给正文。
		return alkaid0TitleName(name) + "()", used
	}
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		used[key] = true
		if value := alkaid0TitleValue(args[key]); value != "" {
			values = append(values, value)
		}
	}
	return alkaid0TitleName(name) + "(" + strings.Join(values, " ") + ")", used
}

// alkaid0TitleName 返回标题里的工具显示名：优先用 alkaid0TitleNames 的可读
// 名，否则把工具名首字母大写（edit → Edit）。
func alkaid0TitleName(name string) string {
	if display, ok := alkaid0TitleNames[name]; ok {
		return display
	}
	return alkaid0ToolName(name)
}

// alkaid0BodyArgs 返回标题未消费的其余参数，按参数名排序并渲染成
// "Key: value" 行（与服务端全参数文本块同形）。多行值的第一行带键名，
// 后续行缩进两格。
func alkaid0BodyArgs(call *alkaid0Call) []string {
	keys := make([]string, 0, len(call.Args))
	for key, value := range call.Args {
		if key == "" || call.Used[key] || value == nil {
			continue
		}
		if s, ok := value.(string); ok && s == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []string
	for _, key := range keys {
		parts := strings.Split(strings.TrimRight(alkaid0Value(call.Args[key]), "\r\n"), "\n")
		lines = append(lines, alkaid0ParamLabel(key)+": "+parts[0])
		for _, part := range parts[1:] {
			lines = append(lines, "  "+part)
		}
	}
	return lines
}

// alkaid0ToolName 把工具名首字母大写作为标题前缀：edit → Edit、
// activate_agent → Activate_agent。
func alkaid0ToolName(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	if first == utf8.RuneError && size == 0 {
		return name
	}
	return string(unicode.ToUpper(first)) + name[size:]
}

// alkaid0ParamLabel 参数名首字母大写，与服务端文本块（"Command: xxx"）同风格。
func alkaid0ParamLabel(key string) string {
	first, size := utf8.DecodeRuneInString(key)
	return string(unicode.ToUpper(first)) + key[size:]
}

// alkaid0Value 把参数值转成展示文本：字符串原样，其余类型用 JSON 表示。
func alkaid0Value(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// alkaid0TitleValue 把标题里的参数值压成单行：标题只占一行，值里的
// 换行/回车/制表符替换为空格（长值由渲染层按宽度截断）。
func alkaid0TitleValue(value any) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		return r
	}, alkaid0Value(value))
}

// alkaid0Bool 报告参数值是否为 true。
func alkaid0Bool(value any) bool {
	b, ok := value.(bool)
	return ok && b
}
