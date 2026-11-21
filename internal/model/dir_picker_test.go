package model

// /cd 目录选择器的纯状态测试：逻辑根（最近使用）列表、隐藏文件过滤、
// 移动与聚焦切换、左右栏应用与底部输入态。

import "testing"

func dirEntries(paths ...string) []DirEntry {
	out := make([]DirEntry, 0, len(paths))
	for _, p := range paths {
		out = append(out, DirEntry{Name: p, Path: p, IsDir: true})
	}
	return out
}

func entryPaths(entries []DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

func assertEntryPaths(t *testing.T, entries []DirEntry, want ...string) {
	t.Helper()
	got := entryPaths(entries)
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

// TestOpenDirPickerLogicalRoot 验证打开即停在逻辑根：左栏留空，右栏是本机
// 主目录（仅本地）→ 系统根 → 常用目录，重叠路径去重。
func TestOpenDirPickerLogicalRoot(t *testing.T) {
	m := New()
	st := m.OpenDirPicker("切换工作目录", "/", "/home/me", []string{"/work", "", "/", "/home/me"})
	if st == nil || m.DirPicker != st {
		t.Fatal("picker state not attached to the model")
	}
	if m.Modal != ModalDirPicker {
		t.Fatalf("modal = %v, want ModalDirPicker", m.Modal)
	}
	if !st.AtLogicalRoot() || st.Dir != "" {
		t.Fatalf("dir = %q, want logical root", st.Dir)
	}
	if len(st.Left) != 0 {
		t.Fatalf("logical root left = %v, want empty", st.Left)
	}
	assertEntryPaths(t, st.Right, "/home/me", "/", "/work")
	for _, e := range st.Right {
		if !e.IsDir {
			t.Fatalf("logical root entry %q should be a directory", e.Path)
		}
	}
	if st.Focus != DirPickerRight || st.Loading {
		t.Fatalf("focus/loading = %v/%v, want right/false", st.Focus, st.Loading)
	}

	// 远程连接（没有本机主目录）时逻辑根第一项是系统根。
	remote := m.OpenDirPicker("t", "/", "", nil)
	assertEntryPaths(t, remote.Right, "/")

	m.CloseDirPicker()
	if m.DirPicker != nil || m.Modal != NoModal {
		t.Fatalf("close left state %v / modal %v", m.DirPicker, m.Modal)
	}
}

// TestDirPickerRootLeft 验证系统根的左栏是逻辑根（主目录 → 系统根 → 常用
// 目录），选中光标对准系统根。
func TestDirPickerRootLeft(t *testing.T) {
	m := New()
	m.OpenDirPicker("t", "/", "/home/me", []string{"/work"})
	m.DirPickerSetDir("/")
	m.SetDirPickerRootLeft()
	p := m.DirPicker
	assertEntryPaths(t, p.Left, "/home/me", "/", "/work")
	if p.LeftSel != 1 {
		t.Fatalf("left sel = %d, want 1 (system root)", p.LeftSel)
	}
}

// TestDirPickerHiddenFiles 验证默认隐藏 "." 开头的目录/文件，h 切换后显示，
// 再切换回隐藏；目录与文件都保留 IsDir 标记。
func TestDirPickerHiddenFiles(t *testing.T) {
	m := New()
	m.OpenDirPicker("t", "/", "", nil)
	m.DirPickerSetDir("/work")
	m.SetDirPickerRight([]DirEntry{
		{Name: "src", Path: "/work/src", IsDir: true},
		{Name: ".git", Path: "/work/.git", IsDir: true},
		{Name: "main.go", Path: "/work/main.go"},
		{Name: ".env", Path: "/work/.env"},
		// 服务端/客户端内部状态目录：即使显示隐藏文件也始终排除。
		{Name: ".alkaid0", Path: "/work/.alkaid0", IsDir: true},
	}, "")
	assertEntryPaths(t, m.DirPicker.Right, "/work/src", "/work/main.go")

	// 选中 main.go 后切换，仍然选中它。
	m.DirPickerMove(1)
	if !m.DirPickerToggleHidden() {
		t.Fatal("toggle should show hidden entries")
	}
	// .git 与 .alkaid0 始终排除，只放出其他隐藏项。
	assertEntryPaths(t, m.DirPicker.Right, "/work/src", "/work/main.go", "/work/.env")
	if e := m.DirPicker.Right[m.DirPicker.RightSel]; e.Path != "/work/main.go" {
		t.Fatalf("selection after show = %q, want /work/main.go", e.Path)
	}
	// 选中隐藏项后隐藏它：过滤后回到第一项。
	m.DirPickerMove(3)
	if m.DirPicker.Right[m.DirPicker.RightSel].Path != "/work/.env" {
		t.Fatalf("selection = %q, want /work/.env", m.DirPicker.Right[m.DirPicker.RightSel].Path)
	}
	if m.DirPickerToggleHidden() {
		t.Fatal("toggle should hide hidden entries again")
	}
	assertEntryPaths(t, m.DirPicker.Right, "/work/src", "/work/main.go")
	if m.DirPicker.RightSel != 0 {
		t.Fatalf("selection after hide = %d, want 0", m.DirPicker.RightSel)
	}
}

// TestDirPickerMoveClamps 验证选择在列表内 clamp、不环绕；空列表不移动。
func TestDirPickerMoveClamps(t *testing.T) {
	m := New()
	m.OpenDirPicker("t", "/", "", nil)
	m.DirPickerSetDir("/work")
	m.SetDirPickerRight(dirEntries("/a", "/b"), "")
	m.DirPickerMove(-1)
	if got := m.DirPicker.RightSel; got != 0 {
		t.Fatalf("sel after up = %d, want 0", got)
	}
	m.DirPickerMove(5)
	if got := m.DirPicker.RightSel; got != 1 {
		t.Fatalf("sel after down = %d, want 1", got)
	}
	m.SetDirPickerRight(nil, "")
	m.DirPickerMove(1)
	if got := m.DirPicker.RightSel; got != 0 {
		t.Fatalf("empty list sel = %d, want 0", got)
	}
	if _, ok := m.DirPicker.SelectedEntry(); ok {
		t.Fatal("empty list should have no selected entry")
	}
}

// TestDirPickerSwitchFocus 验证 Tab 在左右栏之间切换，且移动操作作用于聚焦栏。
func TestDirPickerSwitchFocus(t *testing.T) {
	m := New()
	m.OpenDirPicker("t", "/", "", nil)
	m.DirPickerSetDir("/work")
	m.SetDirPickerRight(dirEntries("/work/a", "/work/b"), "")
	m.SetDirPickerLeft(dirEntries("/home", "/work"))
	if m.DirPicker.LeftSel != 1 {
		t.Fatalf("left sel = %d, want the current directory index 1", m.DirPicker.LeftSel)
	}
	if got := m.DirPicker.FocusedEntries(); len(got) != 2 || got[0].Path != "/work/a" {
		t.Fatalf("right-focused entries = %v", got)
	}
	m.DirPickerSwitchFocus()
	if m.DirPicker.Focus != DirPickerLeft {
		t.Fatalf("focus = %v, want left", m.DirPicker.Focus)
	}
	m.DirPickerMove(-1)
	if m.DirPicker.LeftSel != 0 || m.DirPicker.RightSel != 0 {
		t.Fatalf("left/right sel = %d/%d, want 0/0", m.DirPicker.LeftSel, m.DirPicker.RightSel)
	}
	m.DirPickerSwitchFocus()
	if m.DirPicker.Focus != DirPickerRight {
		t.Fatalf("focus = %v, want right", m.DirPicker.Focus)
	}
}

// TestDirPickerRightPrefer 验证右栏优先选中指定目录（新建文件夹后定位），
// 目标不在列表时回到第一项。
func TestDirPickerRightPrefer(t *testing.T) {
	m := New()
	m.OpenDirPicker("t", "/", "", nil)
	m.DirPickerSetDir("/work")
	m.SetDirPickerRight(dirEntries("/work/a", "/work/b"), "/work/b")
	if m.DirPicker.RightSel != 1 || m.DirPicker.Loading {
		t.Fatalf("sel/loading = %d/%v, want 1/false", m.DirPicker.RightSel, m.DirPicker.Loading)
	}
	m.SetDirPickerRight(dirEntries("/work/a"), "/work/gone")
	if m.DirPicker.RightSel != 0 {
		t.Fatalf("missing prefer sel = %d, want 0", m.DirPicker.RightSel)
	}
}

// TestDirPickerEditing 验证底部输入态：新建/跳转共用输入框，文本去首尾空白，
// 取消后回到 Idle 并释放输入框。
func TestDirPickerEditing(t *testing.T) {
	m := New()
	m.OpenDirPicker("t", "/", "", nil)
	m.DirPickerSetDir("/work")
	if m.DirPickerEditing() {
		t.Fatal("picker should not start in editing state")
	}
	m.DirPickerStartNewFolder()
	if !m.DirPickerEditing() || m.DirPicker.Editing != DirPickerNewFolder {
		t.Fatalf("editing = %v, want new folder", m.DirPicker.Editing)
	}
	m.DirPicker.Input.InsertText("  sub  ")
	if got := m.DirPickerInputText(); got != "sub" {
		t.Fatalf("input text = %q, want trimmed sub", got)
	}
	m.DirPickerStartJump()
	if m.DirPicker.Editing != DirPickerJump || m.DirPicker.Input == nil {
		t.Fatal("jump should reset the input buffer")
	}
	if got := m.DirPickerInputText(); got != "" {
		t.Fatalf("jump input = %q, want empty", got)
	}
	m.DirPickerCancelEdit()
	if m.DirPickerEditing() || m.DirPicker.Input != nil {
		t.Fatal("cancel should leave editing state and drop input")
	}
	if m.DirPickerInputText() != "" {
		t.Fatal("input text should be empty after cancel")
	}

	// 异步错误直接显示在面板底部，不创建输入框、不保持加载态。
	m.DirPickerSetError("boom")
	if m.DirPicker.Err != "boom" || m.DirPicker.Loading {
		t.Fatalf("err/loading = %q/%v", m.DirPicker.Err, m.DirPicker.Loading)
	}
	if m.DirPicker.Input != nil {
		t.Fatal("error should not create an input buffer")
	}
}
