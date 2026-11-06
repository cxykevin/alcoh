package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// ruleEditorSample 构造含多行 AutoApprove 的配置编辑器并进入全屏 IDE。
func ruleEditorSample(t *testing.T) *model.ConfigEditor {
	t.Helper()
	ed := model.NewConfigEditor(json.RawMessage(`{"Agent":{
		"AutoApprove":"ToolCall.Name == \"read\" &&\n    regex(\"^/tmp\", param(ToolCall, \"path\"))",
		"AutoReject":"",
		"Name":"x"
	}}`))
	ed.Focus([]string{"Agent"})
	// 字段按显示名排序："Name" 在前，AutoApprove / AutoReject 随后。
	ed.Move(1)
	if n := ed.SelectedNode(); n == nil || n.Key != "AutoApprove" {
		t.Fatalf("selected = %v, want AutoApprove", n)
	}
	if !ed.BeginRuleEdit() {
		t.Fatal("BeginRuleEdit should accept AutoApprove")
	}
	return ed
}

// TestRuleEditorDraw 验证全屏 IDE 的顶栏、行号、代码与右侧文档栏。
func TestRuleEditorDraw(t *testing.T) {
	ed := ruleEditorSample(t)
	const w, h = 120, 18
	rows := drawConfigTree(ed, w, h)

	if !strings.Contains(rows[0], "规则编辑器") {
		t.Errorf("header = %q, want 规则编辑器", rows[0])
	}
	if !strings.Contains(rows[0], "行 1") || !strings.Contains(rows[0], "列 1") {
		t.Errorf("status = %q, want 行 1 · 列 1", rows[0])
	}
	if !strings.Contains(rows[2], "1 │") || !strings.Contains(rows[2], "ToolCall.Name") {
		t.Errorf("first code row = %q, want line number + ToolCall.Name", rows[2])
	}
	if !strings.Contains(rows[3], "2 │") || !strings.Contains(rows[3], "regex") {
		t.Errorf("second code row = %q, want line number + regex", rows[3])
	}
	// 右侧文档栏（宽屏显示）：标题、变量与函数条目。
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"表达式文档", "ToolCalls", "truthy(v)", "regex(p, s)", "Reject 优先"} {
		if !strings.Contains(joined, want) {
			t.Errorf("docs missing %q in:\n%s", want, joined)
		}
	}
	// 底部按键提示。
	if !strings.Contains(rows[h-1], "Ctrl+S 保存") {
		t.Errorf("footer = %q, want Ctrl+S 保存", rows[h-1])
	}
}

// TestRuleEditorShowsDiagnostic 验证编译诊断的呈现：底栏显示行列与原因，
// 修正后底栏恢复按键提示。
func TestRuleEditorShowsDiagnostic(t *testing.T) {
	ed := ruleEditorSample(t)
	ed.EditInput.Clear()
	ed.EditInput.InsertText("ToolCall.Name == 'run' &&\n  nope(1)")
	ed.RefreshRuleDiag()
	const w, h = 120, 18
	rows := drawConfigTree(ed, w, h)
	if footer := rows[h-1]; !strings.Contains(footer, "⚠ 第 2 行") || !strings.Contains(footer, "nope") {
		t.Fatalf("footer = %q, want line-2 unknown-name diagnostic", footer)
	}

	ed.EditInput.Clear()
	ed.EditInput.InsertText("ToolCall.Name == 'run'")
	ed.RefreshRuleDiag()
	rows = drawConfigTree(ed, w, h)
	if footer := rows[h-1]; !strings.Contains(footer, "Ctrl+S 保存") {
		t.Fatalf("footer after fix = %q, want key hints", footer)
	}
}

// TestRuleEditorCompletePopup 验证补全下拉框：候选与说明显示在光标下方，
// 整块使用弹层背景色，选中行用主题色高亮，底栏显示补全按键提示。
func TestRuleEditorCompletePopup(t *testing.T) {
	ed := ruleEditorSample(t)
	ed.EditInput.Clear()
	ed.EditInput.InsertText("ToolCall.")
	ed.RefreshRuleComplete()
	const w, h = 120, 18
	theme := renderer.DefaultTheme()
	b := renderer.NewBuffer(w, h)
	(&ConfigTree{Theme: theme, Tree: ed}).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))
	rows := make([]string, h)
	for y := range h {
		rows[y] = effortRowText(b, y, w)
	}
	// 弹层在光标行下方：Name / ID / Parameters 依次占 3 行（右侧文档栏同屏）。
	if !strings.Contains(rows[3], "Name") || !strings.Contains(rows[4], "ID") || !strings.Contains(rows[5], "Parameters") {
		t.Fatalf("popup rows = %q, want Name/ID/Parameters", rows[3:6])
	}
	if !strings.Contains(rows[h-1], "Enter/Tab 补全") {
		t.Errorf("footer = %q, want completion hint", rows[h-1])
	}
	hasPopupBg, hasSelBg := false, false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			switch bg := b.Get(x, y).Style.Bg; bg {
			case theme.PopupBg:
				hasPopupBg = true
			case theme.Primary:
				hasSelBg = true
			}
		}
	}
	if !hasPopupBg {
		t.Error("popup cells should be filled with PopupBg")
	}
	if !hasSelBg {
		t.Error("selected completion row should use the primary background")
	}
}

// TestRuleEditorShowsSaveConfirm 验证有编译错误时按 Ctrl+S 的确认提示：
// 底栏切换为"Enter 仍要保存 / Esc 继续编辑"，并指出出错行号。
func TestRuleEditorShowsSaveConfirm(t *testing.T) {
	ed := ruleEditorSample(t)
	ed.EditInput.Clear()
	ed.EditInput.InsertText("nope()")
	ed.RefreshRuleDiag()
	ed.RuleSaveConfirm = true
	const w, h = 120, 18
	rows := drawConfigTree(ed, w, h)
	if footer := rows[h-1]; !strings.Contains(footer, "第 1 行") || !strings.Contains(footer, "Enter 仍要保存") {
		t.Fatalf("footer = %q, want save confirmation for line 1", footer)
	}
}

// TestRuleEditorDrawsSoftCursor 验证块状软光标：光标下的字符用主色背景
// 反显（部分终端不显示硬件光标时也能看到插入点）。
func TestRuleEditorDrawsSoftCursor(t *testing.T) {
	ed := ruleEditorSample(t)
	const w, h = 120, 18
	theme := renderer.DefaultTheme()
	b := renderer.NewBuffer(w, h)
	(&ConfigTree{Theme: theme, Tree: ed}).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))
	// codeX = 6（行号栏），正文从 y=2 开始；光标在行首，'T' 被反显。
	cell := b.Get(6, 2)
	if cell.R != 'T' || cell.Style.Bg != theme.Primary || cell.Style.Fg != theme.Background {
		t.Fatalf("cursor cell = %+v, want 'T' with primary background", cell)
	}
}

// TestRuleEditorNarrowHidesDocs 验证窄屏不显示右侧文档栏，但编辑器仍可用。
func TestRuleEditorNarrowHidesDocs(t *testing.T) {
	ed := ruleEditorSample(t)
	const w, h = 70, 18
	rows := drawConfigTree(ed, w, h)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "表达式文档") || strings.Contains(joined, "参数是否存在") {
		t.Fatalf("narrow screen should hide docs:\n%s", joined)
	}
	if !strings.Contains(rows[2], "ToolCall.Name") {
		t.Errorf("code row = %q, want code still visible", rows[2])
	}
}

// TestExprSpans 验证 expr 高亮：字符串/注释/函数/变量/关键字/数字着色。
func TestExprSpans(t *testing.T) {
	theme := renderer.DefaultTheme()
	line := `ToolCall.Name == "read" && 42 > 0 || true && param(ToolCall, "path")`
	spans := exprSpans(line, theme)

	styleOf := func(text string) (renderer.Style, bool) {
		for _, sp := range spans {
			if sp.Text == text {
				return sp.Style, true
			}
		}
		return renderer.Style{}, false
	}
	if st, ok := styleOf(`"read"`); !ok || st.Fg != theme.Accent {
		t.Errorf("string style = %+v (ok=%v), want Accent", st, ok)
	}
	if st, ok := styleOf("true"); !ok || st.Fg != theme.Primary || !st.Bold {
		t.Errorf("keyword style = %+v (ok=%v), want primary bold", st, ok)
	}
	if st, ok := styleOf("42"); !ok || st.Fg != theme.Warning {
		t.Errorf("number style = %+v (ok=%v), want warning", st, ok)
	}
	if st, ok := styleOf("param"); !ok || st.Fg != theme.Secondary || !st.Bold {
		t.Errorf("function style = %+v (ok=%v), want secondary bold", st, ok)
	}
	// 行注释吞掉整行剩余部分。
	comment := exprSpans("// note", theme)
	if len(comment) != 1 || comment[0].Text != "// note" ||
		comment[0].Style.Fg != theme.TextMuted || !comment[0].Style.Italic {
		t.Errorf("comment spans = %+v, want one muted italic span", comment)
	}
}

// TestExprSpansVariables 验证 ToolCall/Workdir 等环境变量单独着色，字段访问
// （ToolCall.Name 的 Name）不当作变量。
func TestExprSpansVariables(t *testing.T) {
	theme := renderer.DefaultTheme()
	spans := exprSpans(`ToolCall.Name == "x" && Workdir != ""`, theme)
	var vars []string
	for _, sp := range spans {
		if sp.Style.Fg == theme.Info && sp.Style.Bold {
			vars = append(vars, sp.Text)
		}
	}
	if len(vars) != 2 || vars[0] != "ToolCall" || vars[1] != "Workdir" {
		t.Fatalf("variables = %v, want [ToolCall Workdir]", vars)
	}
}
