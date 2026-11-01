package exprcheck

import (
	"strings"
	"testing"
)

// TestCompileValid 验证 alkaid0 规则里常见的写法能通过编译：变量、字段访问、
// 自定义函数、内置 len 与多行注释都在客户端检查范围内。
func TestCompileValid(t *testing.T) {
	cases := []string{
		"",
		"  \n\t",
		"ToolCall.Name == 'read'",
		"hasParam(ToolCall, \"safe\") && param(ToolCall, \"safe\") == true",
		"ToolCall.Name == 'run' &&\n  regex('^/tmp', Workdir)",
		"Agent.AutoApprove != '' && len(ToolCalls) > 0",
		"ToolCall.Name == 'read' // read only\n|| Workdir == ''",
	}
	for _, src := range cases {
		if d := Compile(src); d != nil {
			t.Errorf("Compile(%q) = %+v, want nil", src, d)
		}
	}
}

// TestCompileUnknownName 验证未定义的函数/变量在对应行列报错——这正是
// 规则编辑器"不认识的函数显示报错"的依据。
func TestCompileUnknownName(t *testing.T) {
	d := Compile("ToolCall.Name == 'run' &&\n  nope(1)")
	if d == nil {
		t.Fatal("Compile should reject unknown function")
	}
	if d.Line != 2 || d.Column != 3 {
		t.Fatalf("position = (%d,%d), want (2,3)", d.Line, d.Column)
	}
	if !strings.Contains(d.Message, "unknown name nope") {
		t.Fatalf("message = %q, want unknown name nope", d.Message)
	}

	if d := Compile("ToolCal.Name == 'run'"); d == nil ||
		!strings.Contains(d.Message, "ToolCal") {
		t.Fatalf("typo variable diagnostic = %+v, want unknown ToolCal", d)
	}
}

// TestCompileSyntaxError 验证不完整表达式的语法错误带位置返回。
func TestCompileSyntaxError(t *testing.T) {
	d := Compile("ToolCall.Name == ")
	if d == nil {
		t.Fatal("Compile should reject incomplete expression")
	}
	if d.Line != 1 || d.Column < 1 || d.Message == "" {
		t.Fatalf("diagnostic = %+v, want line 1 with message", d)
	}
}
