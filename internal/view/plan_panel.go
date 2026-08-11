package view

import (
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// PlanPanel 绘制计划面板（Claude Code 风格），固定置于输入栏上方。
// 结构（最多 planMaxH 行）：概要一行 + 任务行。
// 任务排序：上方已完成 → 中间运行中 → 下方未完成；
// 若运行中任务超过 planMaxTasks，则整体退化为"全部未完成"列表。
type PlanPanel struct {
	Theme     renderer.Theme
	SpinFrame int
}

const (
	planMaxTasks = 5 // 任务行数上限
	planMaxH     = 6 // 面板高度上限（1 概要 + 5 任务）
)

// Height 返回面板需要的高度（0 表示无计划）。
func (p *PlanPanel) Height(s *model.SessionState) int {
	if s.Plan == nil || len(s.Plan.Entries) == 0 {
		return 0
	}
	h := 1 + minInt(len(s.Plan.Entries), planMaxTasks)
	if h > planMaxH {
		h = planMaxH
	}
	return h
}

// Draw 绘制计划面板到 rect。
func (p *PlanPanel) Draw(c *renderer.Canvas, r renderer.Rect, s *model.SessionState) {
	plan := s.Plan
	if plan == nil || r.H <= 0 {
		return
	}
	t := p.Theme

	// ---- 概要行 ----
	done, inProg := 0, 0
	for _, e := range plan.Entries {
		switch e.Status {
		case acp.PlanCompleted:
			done++
		case acp.PlanInProgress:
			inProg++
		}
	}
	summary := i18n.T("计划  %s/%s 完成", itoa(done), itoa(len(plan.Entries)))
	if inProg > 0 {
		summary += i18n.T("   ·  %s 进行中", itoa(inProg))
	}
	c.PutText(r.X, r.Y, renderer.Truncate(summary, r.W), t.Style(t.Secondary).WithBold(true))

	// ---- 任务行 ----
	y := r.Y + 1
	display := planDisplay(plan.Entries, planMaxTasks)
	for _, e := range display {
		if y >= r.Y+r.H {
			break
		}
		sym := "○"
		st := t.Style(t.TextMuted)
		switch e.Status {
		case acp.PlanInProgress:
			sym = "●"
			st = t.Style(t.ToolRunning)
		case acp.PlanCompleted:
			sym = "✓"
			st = t.Style(t.ToolDone)
		case acp.PlanCancelled:
			sym = "✗"
			st = t.Style(t.ToolFailed)
		}
		c.PutText(r.X, y, "  "+sym+" "+renderer.Truncate(e.Content, r.W-5), st)
		y++
	}
}

// planDisplay 计算显示的任务行（排序 + 截断）。
// 规则：
//   - 运行中 > maxTasks：整体退化为全部未完成（从上到下列出，状态标 pending）。
//   - 否则：上方取最近完成的，中间全部运行中，下方取未完成；总行数 ≤ maxTasks。
func planDisplay(entries []acp.PlanEntry, maxTasks int) []acp.PlanEntry {
	var completed, inProgress, pending []acp.PlanEntry
	for _, e := range entries {
		switch e.Status {
		case acp.PlanCompleted:
			completed = append(completed, e)
		case acp.PlanInProgress:
			inProgress = append(inProgress, e)
		default: // pending / cancelled / other → 未完成
			pending = append(pending, e)
		}
	}

	if len(inProgress) > maxTasks {
		// 退化：全部按未完成显示，从上到下填充
		n := minInt(maxTasks, len(entries))
		out := make([]acp.PlanEntry, 0, n)
		for i := 0; i < n; i++ {
			e := entries[i]
			e.Status = acp.PlanPending // 强制未完成样式
			out = append(out, e)
		}
		return out
	}

	remaining := maxTasks - len(inProgress)
	cShown := minInt(len(completed), remaining/2)    // 上方空间给已完成
	pShown := minInt(len(pending), remaining-cShown) // 剩余给未完成

	out := make([]acp.PlanEntry, 0, maxTasks)
	out = append(out, completed[len(completed)-cShown:]...)
	out = append(out, inProgress...)
	out = append(out, pending[:pShown]...)
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
