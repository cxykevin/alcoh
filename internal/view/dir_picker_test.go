package view

// /cd 目录选择器绘制测试：双栏标题、当前目录的 ● 标记与选中光标 ❯、
// 逻辑根（最近使用）与系统根浏览、编辑态输入框与窄终端的单栏退化。

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// dirRowText 提取一行中 [x0, x1) 列的文本；宽字符续列跳过。
func dirRowText(b *renderer.Buffer, y, x0, x1 int) string {
	var txt []rune
	for x := x0; x < x1 && x < b.W; x++ {
		cell := b.Get(x, y)
		if cell.Width == 0 {
			continue
		}
		txt = append(txt, cell.R)
	}
	return strings.TrimRight(string(txt), " ")
}

// dirPickerTestModel 构造一个位于 /work 的选择器，左栏为 / 的子目录（当前
// 目录在左栏第 2 项），右栏为 /work 的子目录。
func dirPickerTestModel() *DirPickerContent {
	st := &model.DirPickerState{
		Title: "切换工作目录 (/cd)",
		Root:  "/",
		Dir:   "/work",
		Left: []model.DirEntry{
			{Name: "home", Path: "/home", IsDir: true},
			{Name: "work", Path: "/work", IsDir: true},
		},
		LeftSel: 1,
		Right: []model.DirEntry{
			{Name: "proj", Path: "/work/proj", IsDir: true},
			{Name: "data", Path: "/work/data", IsDir: true},
			{Name: "notes.txt", Path: "/work/notes.txt"},
		},
		RightSel: 0,
		Focus:    model.DirPickerRight,
	}
	m := model.New()
	m.DirPicker = st
	m.SetModal(model.ModalDirPicker)
	return dirPickerContent(renderer.DefaultTheme(), m)
}

func TestDirPickerTwoColumnDraw(t *testing.T) {
	dc := dirPickerTestModel()
	const w, h = 80, 8
	b := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	leftW := (w - 1) / 2
	rightX := leftW + 1

	if got := dirRowText(b, 0, 0, leftW); got != "上一级目录" {
		t.Errorf("left title = %q", got)
	}
	if got := dirRowText(b, 0, rightX, w); !strings.Contains(got, "当前目录: /work") {
		t.Errorf("right title = %q", got)
	}
	if got := dirRowText(b, 1, 0, leftW); !strings.HasPrefix(got, "    home") {
		t.Errorf("left first row = %q", got)
	}
	// 当前目录在左栏被标记 ●；它同时是选中项，所以带 ❯ 光标。
	if got := dirRowText(b, 2, 0, leftW); !strings.HasPrefix(got, "❯ ● work") {
		t.Errorf("left current row = %q", got)
	}
	if got := dirRowText(b, 1, rightX, w); !strings.HasPrefix(got, "❯   proj") {
		t.Errorf("right selected row = %q", got)
	}
	if got := dirRowText(b, 2, rightX, w); !strings.HasPrefix(got, "    data") {
		t.Errorf("right second row = %q", got)
	}
	// 目录黄色、文件普通文字色（选中项的加粗不影响色相）。
	theme := renderer.DefaultTheme()
	if got, want := b.Get(rightX+4, 2).Style, theme.Style(theme.Warning); got != want {
		t.Errorf("directory style = %+v, want yellow %+v", got, want)
	}
	if got, want := b.Get(rightX+4, 3).Style, theme.Style(theme.Text); got != want {
		t.Errorf("file style = %+v, want text %+v", got, want)
	}
	// 分隔线只覆盖列表区域。
	if cell := b.Get(rightX-1, 0); cell.R != '│' {
		t.Errorf("divider at row 0 = %q, want │", cell.R)
	}
	if footer := dirRowText(b, h-1, 0, w); !strings.Contains(footer, "s 选中") ||
		!strings.Contains(footer, "/ 跳转") {
		t.Errorf("footer = %q", footer)
	}
}

// TestDirPickerLogicalRootDraw 验证逻辑根（最近使用）没有上一级：左栏整块
// 留空，右栏标题为「最近使用」，光标停在第一项，底部不提示 n/s。
func TestDirPickerLogicalRootDraw(t *testing.T) {
	dc := dirPickerTestModel()
	st := dc.State
	st.Dir = ""
	st.Home = "/home/me"
	st.Left = nil
	st.LeftSel = 0
	st.Right = []model.DirEntry{
		{Name: "/home/me", Path: "/home/me"},
		{Name: "/", Path: "/"},
		{Name: "/work", Path: "/work"},
	}
	st.RightSel = 0
	const w, h = 80, 7
	b := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	leftW := (w - 1) / 2
	rightX := leftW + 1
	for y := range h - 1 {
		if got := dirRowText(b, y, 0, leftW); got != "" {
			t.Errorf("logical root left row %d = %q, want blank", y, got)
		}
	}
	if cell := b.Get(rightX-1, 0); cell.R != '│' {
		t.Errorf("divider at row 0 = %q, want │", cell.R)
	}
	if got := dirRowText(b, 0, rightX, w); !strings.Contains(got, "最近使用") {
		t.Errorf("right title = %q", got)
	}
	if got := dirRowText(b, 1, rightX, w); !strings.HasPrefix(got, "❯   /home/me") {
		t.Errorf("pinned home row = %q", got)
	}
	if got := dirRowText(b, 2, rightX, w); !strings.HasPrefix(got, "    /") {
		t.Errorf("system root row = %q", got)
	}
	if footer := dirRowText(b, h-1, 0, w); !strings.Contains(footer, "Enter 进入") ||
		strings.Contains(footer, "n 新建") {
		t.Errorf("logical root footer = %q", footer)
	}
}

func TestDirPickerEditingDraw(t *testing.T) {
	dc := dirPickerTestModel()
	st := dc.State
	st.Editing = model.DirPickerNewFolder
	st.Input = widget.NewInputBuffer()
	st.Input.InsertText("sub")
	const w, h = 80, 8
	b := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	if got := dirRowText(b, h-2, 0, w); !strings.Contains(got, "新建文件夹（/work）") {
		t.Errorf("new folder label = %q", got)
	}
	if got := dirRowText(b, h-1, 0, w); !strings.Contains(got, "> sub") {
		t.Errorf("input row = %q", got)
	}

	st.Editing = model.DirPickerJump
	st.Input.Clear()
	b2 := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b2), renderer.NewRect(0, 0, w, h))
	if got := dirRowText(b2, h-2, 0, w); !strings.Contains(got, "跳转路径") {
		t.Errorf("jump label = %q", got)
	}
}

func TestDirPickerErrorOverridesHint(t *testing.T) {
	dc := dirPickerTestModel()
	dc.State.Err = "读取目录失败: permission denied"
	const w, h = 80, 6
	b := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))
	footer := dirRowText(b, h-1, 0, w)
	if !strings.Contains(footer, "读取目录失败") || strings.Contains(footer, "s 选中") {
		t.Errorf("footer = %q, want the error instead of the hint", footer)
	}
}

func TestDirPickerNarrowFallback(t *testing.T) {
	st := &model.DirPickerState{
		Root: "/", Dir: "/work",
		Left: []model.DirEntry{
			{Name: "home", Path: "/home"},
			{Name: "work", Path: "/work"},
		},
		LeftSel: 1,
		Focus:   model.DirPickerLeft,
	}
	m := model.New()
	m.DirPicker = st
	m.SetModal(model.ModalDirPicker)
	dc := dirPickerContent(renderer.DefaultTheme(), m)
	const w, h = 40, 6
	b := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))

	if got := dirRowText(b, 0, 0, w); !strings.Contains(got, "上一级目录") {
		t.Errorf("narrow left title = %q", got)
	}
	// 右栏不绘制：整行不应出现右栏标题。
	for y := range h - 1 {
		if got := dirRowText(b, y, 0, w); strings.Contains(got, "当前目录") {
			t.Errorf("narrow draw shows right pane at row %d: %q", y, got)
		}
	}
	if footer := dirRowText(b, h-1, 0, w); !strings.Contains(footer, "Tab 切换栏") {
		t.Errorf("narrow footer = %q", footer)
	}
}

func TestDirPickerEmptyStates(t *testing.T) {
	dc := dirPickerTestModel()
	dc.State.Right = nil
	dc.State.RightSel = 0
	dc.State.Loading = true
	const w, h = 80, 6
	b := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h))
	if got := dirRowText(b, 1, (w-1)/2+1, w); !strings.Contains(got, "加载中…") {
		t.Errorf("loading row = %q", got)
	}

	dc.State.Loading = false
	b2 := renderer.NewBuffer(w, h)
	dc.Draw(renderer.NewCanvas(b2), renderer.NewRect(0, 0, w, h))
	if got := dirRowText(b2, 1, (w-1)/2+1, w); !strings.Contains(got, "（没有子目录）") {
		t.Errorf("empty row = %q", got)
	}
}
