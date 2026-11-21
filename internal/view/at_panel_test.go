package view

// @ 文件引用选择框的绘制测试：条目窗口、选中标记、目录后缀与提示文案。

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

func drawAtPanel(m *model.AppModel, w, h int) string {
	b := renderer.NewBuffer(w, h)
	(&AtPanel{Theme: renderer.DefaultTheme()}).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)
	return strings.Join(bufferText(b), "\n")
}

// TestAtPanelListsEntries 验证条目按顺序绘制、选中项带 "❯ " 前缀、
// 目录带 "/" 后缀。
func TestAtPanelListsEntries(t *testing.T) {
	old := i18n.Current()
	i18n.SetLang(i18n.Zh)
	t.Cleanup(func() { i18n.SetLang(old) })

	m := model.New()
	m.AtOpen = true
	m.AtEntries = []model.AtEntry{
		{Name: "src", IsDir: true},
		{Name: "main.go"},
	}
	m.AtSelected = 1
	got := drawAtPanel(m, 40, 4)
	if !strings.Contains(got, "src/") {
		t.Fatalf("directory should be rendered with a trailing slash:\n%s", got)
	}
	if !strings.Contains(got, "❯ main.go") {
		t.Fatalf("selected entry should use the ❯ prefix:\n%s", got)
	}
	if strings.Contains(got, "❯ src/") {
		t.Fatalf("unselected directory should not use the ❯ prefix:\n%s", got)
	}
}

// TestAtPanelEmptyAndLoading 验证空列表与加载中的提示行。
func TestAtPanelEmptyAndLoading(t *testing.T) {
	old := i18n.Current()
	i18n.SetLang(i18n.Zh)
	t.Cleanup(func() { i18n.SetLang(old) })

	m := model.New()
	m.AtOpen = true
	m.AtLoaded = true
	if got := drawAtPanel(m, 40, 2); !strings.Contains(got, "无匹配文件") {
		t.Fatalf("empty panel should show the no-match hint:\n%s", got)
	}
	m.AtOpen = true
	m.AtLoading = true
	if got := drawAtPanel(m, 40, 2); !strings.Contains(got, "加载中…") {
		t.Fatalf("loading panel should show the loading hint:\n%s", got)
	}
}

// TestAtPanelPreviewColumn 验证选中文件时右侧分栏绘制代码预览，窄面板
// 不显示预览（列表占满整行）。
func TestAtPanelPreviewColumn(t *testing.T) {
	old := i18n.Current()
	i18n.SetLang(i18n.Zh)
	t.Cleanup(func() { i18n.SetLang(old) })

	m := model.New()
	m.AtOpen = true
	m.AtLoaded = true
	m.AtEntries = []model.AtEntry{{Name: "main.go"}}
	m.AtPreviewPath = "main.go"
	m.AtPreview = "package main\n\nfunc main() {}\n"

	wide := drawAtPanel(m, 100, 6)
	for _, want := range []string{"main.go", "package main", "func main()", "│"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("wide panel should render preview %q:\n%s", want, wide)
		}
	}

	narrow := drawAtPanel(m, 60, 6)
	if strings.Contains(narrow, "package main") || strings.Contains(narrow, "│") {
		t.Fatalf("narrow panel should not render the preview column:\n%s", narrow)
	}
}

// TestAtPanelPreviewStates 验证预览分栏的加载中、空文件与错误提示。
func TestAtPanelPreviewStates(t *testing.T) {
	old := i18n.Current()
	i18n.SetLang(i18n.Zh)
	t.Cleanup(func() { i18n.SetLang(old) })

	m := model.New()
	m.AtOpen = true
	m.AtLoaded = true
	m.AtEntries = []model.AtEntry{{Name: "empty.go"}}
	m.AtPreviewPath = "empty.go"

	if got := drawAtPanel(m, 100, 4); !strings.Contains(got, "（空文件）") {
		t.Fatalf("empty file should show the empty hint:\n%s", got)
	}

	m.AtPreviewLoading = true
	if got := drawAtPanel(m, 100, 4); !strings.Contains(got, "加载中…") {
		t.Fatalf("loading preview should show the loading hint:\n%s", got)
	}

	m.AtPreviewLoading = false
	m.AtPreviewErr = "无法预览该文件"
	if got := drawAtPanel(m, 100, 4); !strings.Contains(got, "无法预览该文件") {
		t.Fatalf("preview error should be shown:\n%s", got)
	}
}

// TestHomeAtPanelFixedHeightShowsPreview 验证首页 @ 面板只有一个文件时也
// 按 8 行布局，右侧预览分栏能完整显示多行内容（不再按条目数收缩成 1 行）。
func TestHomeAtPanelFixedHeightShowsPreview(t *testing.T) {
	old := i18n.Current()
	i18n.SetLang(i18n.Zh)
	t.Cleanup(func() { i18n.SetLang(old) })

	m := model.New()
	m.View = model.ViewHome
	m.Input = widget.NewInputBuffer()
	m.Input.InsertText("@")
	m.AtOpen = true
	m.AtLoaded = true
	m.AtEntries = []model.AtEntry{{Name: "main.go"}}
	m.AtPreviewPath = "main.go"
	m.AtPreview = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(1)\n}\n"

	b := renderer.NewBuffer(120, 30)
	NewAppView(renderer.DefaultTheme()).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, 120, 30), m)
	got := strings.Join(bufferText(b), "\n")
	// 8 行面板应完整容纳 7 行预览内容（含最后的函数体行）。
	for _, want := range []string{"package main", "import \"fmt\"", "func main()", "fmt.Println(1)", "}"} {
		if !strings.Contains(got, want) {
			t.Fatalf("preview line %q should be visible in the fixed-height panel:\n%s", want, got)
		}
	}
}

// TestAtPanelHeightFixed 验证选择框打开时固定占满 8 行（预览分栏需要行数，
// 条目不足也不收缩），关闭时为 0。
func TestAtPanelHeightFixed(t *testing.T) {
	m := model.New()
	m.AtOpen = true
	if got := atPanelHeight(m); got != 8 || model.AtPanelRows != 8 {
		t.Fatalf("atPanelHeight = %d, want 8 (AtPanelRows = %d)", got, model.AtPanelRows)
	}
	// 只有一两个条目或正在加载时同样占满 8 行，避免面板高度跳动。
	m.AtEntries = []model.AtEntry{{Name: "main.go"}}
	if got := atPanelHeight(m); got != 8 {
		t.Fatalf("single-entry height = %d, want 8", got)
	}
	m.AtLoading = true
	if got := atPanelHeight(m); got != 8 {
		t.Fatalf("loading height = %d, want 8", got)
	}
	m.AtOpen = false
	if got := atPanelHeight(m); got != 0 {
		t.Fatalf("closed height = %d, want 0", got)
	}
}
