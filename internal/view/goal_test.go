package view

// /goal 会话目标看板的排版测试：只读态展示多行目标与字符计数，空目标
// 显示空状态，编辑态把输入框嵌入面板并保留底部操作提示。

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

func drawGoalModal(t *testing.T, m *model.AppModel, w int) *renderer.Buffer {
	t.Helper()
	theme := renderer.DefaultTheme()
	modalH := goalModalHeight()
	if modalH != 12 {
		t.Fatalf("goalModalHeight = %d, want 12", modalH)
	}
	contentH := modalH - 1 // DrawSheet 的标题行占 1 行
	b := renderer.NewBuffer(w, contentH)
	goalContent(theme, m).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, contentH))
	return b
}

// TestGoalContentReadOnly 验证多行目标逐行展示，顶部字符计数与底部操作提示。
func TestGoalContentReadOnly(t *testing.T) {
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.Goal = "第一行目标\n第二行目标"
	m.SetModal(model.ModalGoal)

	const w = 64
	b := drawGoalModal(t, m, w)
	if hint := effortRowText(b, 0, w); !strings.Contains(hint, "当前会话目标") || !strings.Contains(hint, "/1024") {
		t.Errorf("hint row = %q", hint)
	}
	if row := effortRowText(b, 2, w); !strings.Contains(row, "第一行目标") {
		t.Errorf("first goal row = %q", row)
	}
	if row := effortRowText(b, 3, w); !strings.Contains(row, "第二行目标") {
		t.Errorf("second goal row = %q", row)
	}
	if footer := effortRowText(b, 10, w); !strings.Contains(footer, "e 编辑") || !strings.Contains(footer, "d 清除") {
		t.Errorf("footer row = %q", footer)
	}
}

// TestGoalContentEmptyState 验证未设置目标时的空状态文案。
func TestGoalContentEmptyState(t *testing.T) {
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.SetModal(model.ModalGoal)

	const w = 64
	b := drawGoalModal(t, m, w)
	if row := effortRowText(b, 2, w); !strings.Contains(row, "未设置目标") {
		t.Errorf("empty state row = %q", row)
	}
}

// TestGoalContentEditing 验证编辑态显示输入框与保存提示，输入内容可见。
func TestGoalContentEditing(t *testing.T) {
	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.Goal = "旧目标"
	m.SetModal(model.ModalGoal)
	m.GoalInput = widget.NewInputBuffer()
	m.GoalInput.InsertText("新目标")
	m.GoalEditing = true

	const w = 64
	b := drawGoalModal(t, m, w)
	if hint := effortRowText(b, 0, w); !strings.Contains(hint, "编辑会话目标") {
		t.Errorf("edit hint row = %q", hint)
	}
	if row := effortRowText(b, 2, w); !strings.Contains(row, "新目标") {
		t.Errorf("input row = %q", row)
	}
	if footer := effortRowText(b, 10, w); !strings.Contains(footer, "Enter 保存") || !strings.Contains(footer, "Esc 取消") {
		t.Errorf("edit footer row = %q", footer)
	}
}
