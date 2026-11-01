package model

import (
	"strings"
	"unicode"

	"github.com/cxykevin/alcoh/internal/i18n"
)

// RuleCompletion 是规则编辑器下拉补全的一项：Insert 是接受的文本，
// Detail 是右侧灰字说明（为空时不显示）。
type RuleCompletion struct {
	Insert string
	Detail string
}

// RuleCompleteOpen 等字段在 ConfigEditor 上，见 configtree.go。
// 这里只放补全的候选与匹配逻辑。

// ruleWordCompletions 返回表达式里可用的变量与函数（函数只插入名字，
// 参数由用户自己输入）。顺序与文档栏一致：变量在前、函数按字母序。
func ruleWordCompletions() []RuleCompletion {
	return []RuleCompletion{
		{"ToolCalls", i18n.T("全部待审批调用")},
		{"ToolCall", i18n.T("当前工具调用")},
		{"Agent", i18n.T("当前 Agent 配置")},
		{"Workdir", i18n.T("当前工作目录")},
		{"contains", i18n.T("包含子串")},
		{"hasParam", i18n.T("参数是否存在")},
		{"param", i18n.T("取参数值")},
		{"regex", i18n.T("正则匹配，p 在前")},
		{"truthy", i18n.T("按 Go 语义转布尔")},
	}
}

// ruleMemberCompletions 返回 `base.` 之后的字段候选。目前只认识
// ToolCall（AsMap 暴露 Name/ID/Parameters）；其它变量的字段随服务端
// 版本变化，不猜测。
func ruleMemberCompletions(base, prefix string) []RuleCompletion {
	var fields []RuleCompletion
	switch base {
	case "ToolCall":
		fields = []RuleCompletion{
			{"Name", i18n.T("工具名")},
			{"ID", i18n.T("调用 ID")},
			{"Parameters", i18n.T("参数表")},
		}
	case "Agent":
		// 当前 alkaid0 的 cfgStructs.AgentConfig 字段（表达式按 Go 字段名访问）。
		fields = []RuleCompletion{
			{"Color", i18n.T("颜色")},
			{"AgentName", i18n.T("代理名称")},
			{"AgentDescription", i18n.T("代理描述")},
			{"AgentPrompt", i18n.T("代理提示词")},
			{"AgentModel", i18n.T("代理模型")},
			{"AgentShortDescription", i18n.T("代理简介")},
			{"AutoApprove", i18n.T("自动审批")},
			{"AutoReject", i18n.T("自动拒绝")},
			{"DisableSandbox", i18n.T("禁用沙箱")},
		}
	case "Agent.Color":
		// cfgStructs.Color 的三个通道。
		fields = []RuleCompletion{
			{"Red", i18n.T("红")},
			{"Green", i18n.T("绿")},
			{"Blue", i18n.T("蓝")},
		}
	default:
		return nil
	}
	return filterRuleCompletions(fields, prefix)
}

// filterRuleCompletions 按大小写不敏感的前缀过滤候选。
func filterRuleCompletions(items []RuleCompletion, prefix string) []RuleCompletion {
	if prefix == "" {
		return items
	}
	lower := strings.ToLower(prefix)
	out := make([]RuleCompletion, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(strings.ToLower(it.Insert), lower) {
			out = append(out, it)
		}
	}
	return out
}

// RefreshRuleComplete 根据光标前的上下文刷新补全候选，供 app 在编辑缓冲
// 变化后调用。字符串/注释里不弹补全；`ToolCall.` 后给字段，普通标识符给
// 变量与函数的前缀匹配。
func (ed *ConfigEditor) RefreshRuleComplete() {
	ed.CloseRuleComplete()
	if !ed.FullscreenEdit || ed.EditInput == nil {
		return
	}
	buf := ed.EditInput
	if buf.CY < 0 || buf.CY >= len(buf.Lines) {
		return
	}
	line := buf.Lines[buf.CY]
	if buf.CX < 0 || buf.CX > len(line) {
		return
	}
	if ruleInStringOrComment(buf.Lines, buf.CY, buf.CX) {
		return
	}
	start := buf.CX
	for start > 0 && ruleIdentPart(line[start-1]) {
		start--
	}
	prefix := string(line[start:buf.CX])

	var items []RuleCompletion
	if start > 0 && line[start-1] == '.' {
		// 点号前可以是一段字段路径（ToolCall / Agent / Agent.Color）：一路
		// 向左吃掉标识符与点号，拼出完整的 base。
		baseStart := start - 1
		for baseStart > 0 {
			r := line[baseStart-1]
			if !ruleIdentPart(r) && r != '.' {
				break
			}
			baseStart--
		}
		items = ruleMemberCompletions(string(line[baseStart:start-1]), prefix)
	} else if prefix != "" {
		items = filterRuleCompletions(ruleWordCompletions(), prefix)
	}
	if len(items) == 0 {
		return
	}
	ed.RuleCompleteOpen = true
	ed.RuleCompleteItems = items
	ed.RuleCompleteStart = start
	ed.RuleCompleteSel = 0
}

// CloseRuleComplete 收起补全下拉框。
func (ed *ConfigEditor) CloseRuleComplete() {
	ed.RuleCompleteOpen = false
	ed.RuleCompleteItems = nil
	ed.RuleCompleteSel = 0
	ed.RuleCompleteStart = 0
}

// MoveRuleComplete 在候选间移动（循环），d 为 -1 / +1。
func (ed *ConfigEditor) MoveRuleComplete(d int) {
	n := len(ed.RuleCompleteItems)
	if !ed.RuleCompleteOpen || n == 0 {
		return
	}
	ed.RuleCompleteSel = (ed.RuleCompleteSel + d%n + n) % n
}

// AcceptRuleComplete 用选中候选替换光标前的标识符（或点号后的字段前缀），
// 光标停在插入文本之后，并收起下拉框。
func (ed *ConfigEditor) AcceptRuleComplete() bool {
	if !ed.RuleCompleteOpen || ed.EditInput == nil ||
		ed.RuleCompleteSel < 0 || ed.RuleCompleteSel >= len(ed.RuleCompleteItems) {
		return false
	}
	buf := ed.EditInput
	if buf.CY < 0 || buf.CY >= len(buf.Lines) {
		return false
	}
	line := buf.Lines[buf.CY]
	start := min(max(ed.RuleCompleteStart, 0), len(line))
	end := min(max(buf.CX, 0), len(line))
	if start > end {
		start = end
	}
	ins := []rune(ed.RuleCompleteItems[ed.RuleCompleteSel].Insert)
	next := make([]rune, 0, len(line)-(end-start)+len(ins))
	next = append(next, line[:start]...)
	next = append(next, ins...)
	next = append(next, line[end:]...)
	buf.Lines[buf.CY] = next
	buf.CX = start + len(ins)
	ed.CloseRuleComplete()
	return true
}

// ruleIdentPart 与高亮/补全共用：标识符字符（字母、数字、下划线）。
func ruleIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ruleInStringOrComment 报告光标（cy,cx）是否位于字符串或注释里。
// 只做轻量扫描：引号（含反引号与转义）与 //、/* */ 注释。
func ruleInStringOrComment(lines [][]rune, cy, cx int) bool {
	var quote rune
	block := false
	for y := 0; y <= cy && y < len(lines); y++ {
		line := lines[y]
		limit := len(line)
		if y == cy {
			limit = min(cx, len(line))
		}
		lineComment := false
		for x := 0; x < limit; x++ {
			r := line[x]
			if lineComment {
				break
			}
			if block {
				if r == '*' && x+1 < limit && line[x+1] == '/' {
					block = false
					x++
				}
				continue
			}
			if quote != 0 {
				if r == '\\' && quote != '`' {
					x++
					continue
				}
				if r == quote {
					quote = 0
				}
				continue
			}
			switch r {
			case '/':
				if x+1 < limit && line[x+1] == '/' {
					lineComment = true
					x++
				} else if x+1 < limit && line[x+1] == '*' {
					block = true
					x++
				}
			case '\'', '"', '`':
				quote = r
			}
		}
		// 行注释一直持续到行尾：光标停在注释里（包括行尾）都算命中。
		if y == cy && lineComment {
			return true
		}
	}
	return quote != 0 || block
}
