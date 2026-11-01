package model

import (
	"encoding/json"
	"testing"
)

// ruleCompleteEditor 构造一个 AutoApprove 为空、光标停在文件开头的全屏 IDE。
func ruleCompleteEditor(t *testing.T) *ConfigEditor {
	t.Helper()
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":""}}`))
	ed.Focus([]string{"Agent"})
	if !ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should accept AutoApprove")
	}
	return ed
}

// typeRuleText 把文本插入编辑缓冲并把光标移到文本末尾，然后刷新补全。
func typeRuleText(ed *ConfigEditor, s string) {
	ed.EditInput.InsertText(s)
	ed.RefreshRuleComplete()
}

// TestRuleCompletePrefix 验证普通标识符前缀补全：变量/函数候选按前缀过滤，
// 接受时替换整个前缀并收起下拉框。
func TestRuleCompletePrefix(t *testing.T) {
	ed := ruleCompleteEditor(t)
	typeRuleText(ed, "has")
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 1 {
		t.Fatalf("items = %+v (open=%v), want only hasParam", ed.RuleCompleteItems, ed.RuleCompleteOpen)
	}
	if got := ed.RuleCompleteItems[0].Insert; got != "hasParam" {
		t.Fatalf("candidate = %q, want hasParam", got)
	}
	if !ed.AcceptRuleComplete() {
		t.Fatal("AcceptRuleComplete should succeed")
	}
	if got := ed.EditInput.Text(); got != "hasParam" {
		t.Fatalf("text = %q, want hasParam", got)
	}
	if ed.RuleCompleteOpen {
		t.Fatal("accept should close the popup")
	}
}

// TestRuleCompleteMember 验证 `ToolCall.` 后的字段候选与选中移动。
func TestRuleCompleteMember(t *testing.T) {
	ed := ruleCompleteEditor(t)
	typeRuleText(ed, "ToolCall.")
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 3 {
		t.Fatalf("items = %+v, want Name/ID/Parameters", ed.RuleCompleteItems)
	}
	if got := ed.RuleCompleteItems[0].Insert; got != "Name" {
		t.Fatalf("first candidate = %q, want Name", got)
	}
	ed.MoveRuleComplete(1)
	if got := ed.RuleCompleteItems[ed.RuleCompleteSel].Insert; got != "ID" {
		t.Fatalf("selected after move = %q, want ID", got)
	}
	if !ed.AcceptRuleComplete() {
		t.Fatal("AcceptRuleComplete should succeed")
	}
	if got := ed.EditInput.Text(); got != "ToolCall.ID" {
		t.Fatalf("text = %q, want ToolCall.ID", got)
	}

	// 字段前缀过滤：ToolCall.Na → 只有 Name。
	ed.EditInput.Clear()
	typeRuleText(ed, "ToolCall.Na")
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 1 ||
		ed.RuleCompleteItems[0].Insert != "Name" {
		t.Fatalf("member items = %+v, want only Name", ed.RuleCompleteItems)
	}
}

// TestRuleCompleteReplacesPrefix 验证接受候选只替换光标前的标识符，保留后面的文本。
func TestRuleCompleteReplacesPrefix(t *testing.T) {
	ed := ruleCompleteEditor(t)
	ed.EditInput.InsertText("Tru == 1")
	ed.EditInput.CX = 3
	ed.RefreshRuleComplete()
	if !ed.RuleCompleteOpen {
		t.Fatal("popup should open for Tru")
	}
	ed.AcceptRuleComplete()
	if got := ed.EditInput.Text(); got != "truthy == 1" {
		t.Fatalf("text = %q, want truthy == 1", got)
	}
	if ed.EditInput.CX != 6 {
		t.Fatalf("cursor = %d, want 6", ed.EditInput.CX)
	}
}

// TestRuleCompleteSuppressed 验证字符串/注释里与无匹配前缀时不弹补全。
func TestRuleCompleteSuppressed(t *testing.T) {
	for _, src := range []string{`'Tru`, `"Tru`, "`Tru", `// Tru`, `/* Tru`, "zzz", "Tru "} {
		ed := ruleCompleteEditor(t)
		typeRuleText(ed, src)
		if ed.RuleCompleteOpen {
			t.Errorf("open for %q: %+v, want closed", src, ed.RuleCompleteItems)
		}
	}
}

// TestRuleCompleteWrap 验证上下移动循环、关闭后状态清空。
func TestRuleCompleteWrap(t *testing.T) {
	ed := ruleCompleteEditor(t)
	typeRuleText(ed, "ToolCall.")
	ed.MoveRuleComplete(-1)
	if ed.RuleCompleteSel != 2 {
		t.Fatalf("sel after up = %d, want 2 (wrap to Parameters)", ed.RuleCompleteSel)
	}
	ed.MoveRuleComplete(1)
	if ed.RuleCompleteSel != 0 {
		t.Fatalf("sel after down = %d, want 0", ed.RuleCompleteSel)
	}
	ed.CloseRuleComplete()
	if ed.RuleCompleteOpen || ed.RuleCompleteItems != nil {
		t.Fatalf("close left state: open=%v items=%v", ed.RuleCompleteOpen, ed.RuleCompleteItems)
	}
}

// TestRuleCompleteAgentFields 验证 Agent 变量按当前 alkaid0 的
// cfgStructs.AgentConfig 字段枚举，并支持 Agent.Color.* 的嵌套字段。
func TestRuleCompleteAgentFields(t *testing.T) {
	ed := ruleCompleteEditor(t)
	typeRuleText(ed, "Agent.")
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 9 {
		t.Fatalf("items = %+v, want 9 AgentConfig fields", ed.RuleCompleteItems)
	}
	names := make(map[string]bool, len(ed.RuleCompleteItems))
	for _, it := range ed.RuleCompleteItems {
		names[it.Insert] = true
	}
	for _, want := range []string{"AgentName", "AgentModel", "AutoApprove", "AutoReject", "DisableSandbox", "Color"} {
		if !names[want] {
			t.Errorf("Agent fields missing %q: %+v", want, ed.RuleCompleteItems)
		}
	}

	// 前缀过滤：Agent.AutoA → AutoApprove。
	ed.EditInput.Clear()
	typeRuleText(ed, "Agent.AutoA")
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 1 ||
		ed.RuleCompleteItems[0].Insert != "AutoApprove" {
		t.Fatalf("Agent.AutoA items = %+v, want only AutoApprove", ed.RuleCompleteItems)
	}

	// 嵌套字段：Agent.Color. → Red/Green/Blue。
	ed.EditInput.Clear()
	typeRuleText(ed, "Agent.Color.")
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 3 {
		t.Fatalf("Agent.Color items = %+v, want Red/Green/Blue", ed.RuleCompleteItems)
	}
}
