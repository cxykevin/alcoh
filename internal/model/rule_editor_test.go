package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBeginRuleEditMultiline 验证规则字段（AutoApprove）用全屏 IDE 编辑：
// 预填完整多行表达式、光标停在文件开头，提交后写回整个字符串并退出 IDE。
func TestBeginRuleEditMultiline(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{
		"AutoApprove":"ToolCall.Name == \"read\"\n// keep",
		"AutoReject":"",
		"Name":"x"
	}}`))
	ed.Focus([]string{"Agent"})
	if n := ed.SelectedNode(); n == nil || n.Key != "AutoApprove" {
		t.Fatalf("selected = %v, want AutoApprove", n)
	}

	if !ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should accept AutoApprove")
	}
	if !ed.Editing || !ed.FullscreenEdit {
		t.Fatalf("editing=%v fullscreen=%v, want both true", ed.Editing, ed.FullscreenEdit)
	}
	if got, want := ed.EditInput.Text(), "ToolCall.Name == \"read\"\n// keep"; got != want {
		t.Fatalf("prefilled text = %q, want %q", got, want)
	}
	if ed.EditInput.CY != 0 || ed.EditInput.CX != 0 {
		t.Fatalf("cursor = (%d,%d), want (0,0)", ed.EditInput.CX, ed.EditInput.CY)
	}

	// 编辑：在开头换行并追加一行注释。
	ed.EditInput.InsertText("// note\n")
	patch, ok, errMsg := ed.CommitEdit()
	if !ok {
		t.Fatalf("CommitEdit failed: %s", errMsg)
	}
	if ed.Editing || ed.FullscreenEdit {
		t.Fatal("CommitEdit should leave the rule IDE")
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(patch, &got); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	want := "// note\nToolCall.Name == \"read\"\n// keep"
	if got["Agent"]["AutoApprove"] != want {
		t.Fatalf("patch AutoApprove = %q, want %q", got["Agent"]["AutoApprove"], want)
	}
}

// TestBeginRuleEditRejectsOtherFields 验证只有 AutoApprove / AutoReject 走
// 全屏 IDE，其它字符串字段仍使用普通单行编辑。
func TestBeginRuleEditRejectsOtherFields(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"","AutoReject":"","Name":"x"}}`))
	ed.Focus([]string{"Agent"})
	ed.Selected = 2 // Name（AutoApprove/AutoReject 之后）
	if n := ed.SelectedNode(); n == nil || n.Key != "Name" {
		t.Fatalf("selected = %v, want Name", n)
	}
	if ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should reject non-rule fields")
	}
	if ed.Editing || ed.FullscreenEdit {
		t.Fatal("rejected field should not enter editing")
	}
}

// TestCancelRuleEdit 验证 Esc 取消后不写回、节点值保持不变。
func TestCancelRuleEdit(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name == \"read\""}}`))
	ed.Focus([]string{"Agent"})
	if !ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should accept AutoApprove")
	}
	ed.EditInput.InsertText("garbage")
	ed.CancelEdit()
	if ed.Editing || ed.FullscreenEdit {
		t.Fatal("CancelEdit should leave the rule IDE")
	}
	n := ed.SelectedNode()
	if n == nil || n.Str != `ToolCall.Name == "read"` {
		t.Fatalf("node value = %q, want unchanged", n.Str)
	}
}

// TestRuleDiagTracksCompileErrors 验证全屏 IDE 的编译诊断随缓冲更新：
// 打开时先检查现有表达式，改成未定义函数后在对应行列报错，修正后诊断消失。
func TestRuleDiagTracksCompileErrors(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name =="}}`))
	ed.Focus([]string{"Agent"})
	if !ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should accept AutoApprove")
	}
	if ed.EditDiag == nil || ed.EditDiag.Line != 1 {
		t.Fatalf("initial diag = %+v, want syntax error on line 1", ed.EditDiag)
	}

	ed.EditInput.Clear()
	ed.EditInput.InsertText("ToolCall.Name == 'run' &&\n  nope(1)")
	ed.RefreshRuleDiag()
	d := ed.EditDiag
	if d == nil || d.Line != 2 || !strings.Contains(d.Message, "nope") {
		t.Fatalf("diag after unknown call = %+v, want line 2 mentioning nope", d)
	}

	ed.EditInput.Clear()
	ed.EditInput.InsertText("ToolCall.Name == 'run'")
	ed.RefreshRuleDiag()
	if ed.EditDiag != nil {
		t.Fatalf("diag after fix = %+v, want nil", ed.EditDiag)
	}

	ed.CancelEdit()
	if ed.EditDiag != nil {
		t.Fatalf("diag after cancel = %+v, want nil", ed.EditDiag)
	}
}

// TestRestoreStateKeepsFullscreen 验证整配置重载恢复编辑状态时全屏 IDE
// 不退回单行输入框。
func TestRestoreStateKeepsFullscreen(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name == \"read\""}}`))
	ed.Focus([]string{"Agent"})
	if !ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should accept AutoApprove")
	}
	st := ed.CaptureState()

	next := NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name == \"read\""}}`))
	next.RestoreState(st)
	if !next.Editing || !next.FullscreenEdit {
		t.Fatalf("editing=%v fullscreen=%v, want both true", next.Editing, next.FullscreenEdit)
	}
	if next.EditInput.CY != 0 || next.EditInput.CX != 0 {
		t.Fatalf("restored cursor = (%d,%d), want (0,0)", next.EditInput.CX, next.EditInput.CY)
	}
}
