// Package exprcheck 在客户端预编译自动审批/拒绝规则，供规则编辑器实时
// 显示语法错误与未定义的变量/函数。
//
// 编译环境与 alkaid0 的 provider/request.compileExpr 保持一致（同样的
// 表达式引擎版本、变量形状与自定义函数），因此这里报错的表达式在服务端
// 求值时同样会失败；能通过这里的表达式不会因为客户端检查而误报。
package exprcheck

import (
	"errors"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/file"
)

// Diagnostic 是规则表达式的一条编译诊断，位置从 1 开始计数。
type Diagnostic struct {
	Line    int
	Column  int
	Message string
}

// Compile 编译规则表达式，返回 nil 表示语法与名称检查通过。
// 空白表达式是合法配置（表示不启用该规则），直接视为通过。
func Compile(src string) *Diagnostic {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	opts := []expr.Option{ruleEnv()}
	opts = append(opts, ruleFunctions()...)
	if _, err := expr.Compile(src, opts...); err != nil {
		return diagnostic(err)
	}
	return nil
}

// diagnostic 把 expr 的编译错误转成面向界面的诊断。expr 的错误通常带
// 源码位置（*file.Error），退化为无位置错误时从第一行第一列提示。
func diagnostic(err error) *Diagnostic {
	var fe *file.Error
	if errors.As(err, &fe) {
		return &Diagnostic{
			Line: max(fe.Line, 1),
			// expr 的 Column 从 0 开始计数，展示时加一（与其错误文案一致）。
			Column:  max(fe.Column+1, 1),
			Message: fe.Message,
		}
	}
	return &Diagnostic{Line: 1, Column: 1, Message: err.Error()}
}

// ruleEnv 与 alkaid0 注入的表达式变量一致：ToolCalls 是全部待审批调用、
// ToolCall 是当前调用（都按 AsMap 的形状暴露 Name/ID/Parameters），
// Agent 是当前 Agent 配置，Workdir 是规则生效的工作目录。
func ruleEnv() expr.Option {
	return expr.Env(map[string]any{
		"ToolCalls": []map[string]any{},
		"ToolCall":  map[string]any{},
		"Agent":     map[string]any{},
		"Workdir":   "",
	})
}

// ruleFunctions 与 alkaid0 注册的自定义函数一致。服务端用变参签名注册，
// 编译期不检查参数类型/个数，这里保持同样的宽松度，避免出现服务端能编译
// 而客户端报错的误报。
func ruleFunctions() []expr.Option {
	return []expr.Option{
		expr.Function("truthy", func(params ...any) (any, error) { return false, nil }),
		expr.Function("regex", func(params ...any) (any, error) { return false, nil }),
		expr.Function("contains", func(params ...any) (any, error) { return false, nil }),
		expr.Function("hasParam", func(params ...any) (any, error) { return false, nil }),
		expr.Function("param", func(params ...any) (any, error) { return nil, nil }),
	}
}
