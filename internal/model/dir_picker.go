package model

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/widget"
)

// DirPickerFocus 是目录选择器当前聚焦的栏。
type DirPickerFocus uint8

const (
	// DirPickerLeft 左栏：当前目录的严格上一级；根目录没有上一级时左栏留空。
	DirPickerLeft DirPickerFocus = iota
	// DirPickerRight 右栏：当前目录。
	DirPickerRight
)

// DirPickerEdit 是目录选择器的底部输入状态。
type DirPickerEdit uint8

const (
	// DirPickerIdle 不在编辑。
	DirPickerIdle DirPickerEdit = iota
	// DirPickerNewFolder 正在输入新文件夹名。
	DirPickerNewFolder
	// DirPickerJump 正在输入要跳转的路径。
	DirPickerJump
)

// DirEntry 是目录选择器中的一项。Path 是服务端绝对路径；IsDir 区分目录
// 与文件（目录用黄色绘制，文件用普通文字色，只有目录可以被进入）。
type DirEntry struct {
	Name  string
	Path  string
	IsDir bool
}

// DirPickerState 是 /cd 目录选择器的纯状态。目录列举由 app 层经 ACP
// fs/read 异步完成，模型只负责选择、聚焦与输入态。
//
// 布局语义：
//   - Dir == "" 是逻辑根「最近使用」：Left 留空，Right 是本机主目录（仅本地
//     连接）、系统根 Root 与常用目录；
//   - 其他情况下 Right 是当前目录 Dir 的子目录，Left 是 Dir 上一级目录的子
//     目录（当前目录在列表中被标记）；系统根的上一级就是逻辑根。
type DirPickerState struct {
	// Title 是弹窗标题（/cd 与将来的 remote 选择器可不同）。
	Title string
	Root  string
	// Dir 是当前目录；空串表示逻辑根（最近使用，不对应真实目录）。
	Dir string
	// Home 是本机用户主目录：仅在服务端与本机同机时非空（远程连接为空）。
	Home string

	Left     []DirEntry
	LeftSel  int
	Right    []DirEntry
	RightSel int
	Focus    DirPickerFocus

	// Recent 是当前连接（历史 key）的常用目录，按最近使用排序。
	Recent []string

	// ShowHidden 控制是否显示 "." 开头的隐藏文件/目录（默认隐藏）。
	ShowHidden bool

	// leftAll/rightAll 是未过滤的原始列表；切换隐藏文件显示时按它重建
	// Left/Right，不必重新请求服务端。
	leftAll  []DirEntry
	rightAll []DirEntry

	Editing DirPickerEdit
	Input   *widget.InputBuffer
	Loading bool
	Err     string
}

// OpenDirPicker 打开目录选择器并返回状态。选择器总是从逻辑根（最近使用）
// 开始：Recent 与 Home 作为起点列表，系统根是其中的一项。
func (m *AppModel) OpenDirPicker(title, root, home string, recent []string) *DirPickerState {
	m.CloseSlash()
	st := &DirPickerState{
		Title:  title,
		Root:   root,
		Home:   home,
		Recent: append([]string(nil), recent...),
		Focus:  DirPickerRight,
	}
	m.DirPicker = st
	m.SetDirPickerLogicalRoot()
	m.SetModal(ModalDirPicker)
	return st
}

// AtLogicalRoot 报告当前是否停在逻辑根（最近使用）。
func (p *DirPickerState) AtLogicalRoot() bool {
	return p != nil && p.Dir == ""
}

// logicalRootEntries 构造逻辑根的条目：本机主目录（若有）→ 系统根 →
// 常用目录，重叠路径只保留首次出现。本机主目录固定第一项，系统根第二项。
func (p *DirPickerState) logicalRootEntries() []DirEntry {
	if p == nil {
		return nil
	}
	out := make([]DirEntry, 0, len(p.Recent)+2)
	seen := make(map[string]bool, len(p.Recent)+2)
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, DirEntry{Name: path, Path: path, IsDir: true})
	}
	add(p.Home)
	add(p.Root)
	for _, dir := range p.Recent {
		add(strings.TrimSpace(dir))
	}
	return out
}

// SetDirPickerLogicalRoot 把选择器切到逻辑根（最近使用）：左栏留空，右栏为
// 本机主目录 → 系统根 → 常用目录。不做任何 fs 请求，也不再有"加载中"。
func (m *AppModel) SetDirPickerLogicalRoot() {
	p := m.DirPicker
	if p == nil {
		return
	}
	p.Dir = ""
	p.Left = nil
	p.LeftSel = 0
	p.leftAll = nil
	p.rightAll = p.logicalRootEntries()
	p.Right = p.filterEntries(p.rightAll)
	p.RightSel = 0
	p.Focus = DirPickerRight
	p.Loading = false
}

// CloseDirPicker 关闭目录选择器并释放状态。
func (m *AppModel) CloseDirPicker() {
	m.DirPicker = nil
	m.SetModal(NoModal)
}

// DirPickerFocusedEntries 返回当前聚焦栏的条目。
func (p *DirPickerState) FocusedEntries() []DirEntry {
	if p == nil {
		return nil
	}
	if p.Focus == DirPickerLeft {
		return p.Left
	}
	return p.Right
}

// FocusedSelected 返回当前聚焦栏的选中索引指针。
func (p *DirPickerState) FocusedSelected() *int {
	if p == nil {
		return nil
	}
	if p.Focus == DirPickerLeft {
		return &p.LeftSel
	}
	return &p.RightSel
}

// SelectedEntry 返回当前聚焦栏的选中条目。
func (p *DirPickerState) SelectedEntry() (DirEntry, bool) {
	if p == nil {
		return DirEntry{}, false
	}
	entries := p.FocusedEntries()
	sel := *p.FocusedSelected()
	if sel < 0 || sel >= len(entries) {
		return DirEntry{}, false
	}
	return entries[sel], true
}

// DirPickerMove 在当前聚焦栏内移动选择（clamp，不环绕）。
func (m *AppModel) DirPickerMove(delta int) {
	p := m.DirPicker
	if p == nil {
		return
	}
	entries := p.FocusedEntries()
	if len(entries) == 0 {
		return
	}
	sel := p.FocusedSelected()
	next := max(*sel+delta, 0)
	if next >= len(entries) {
		next = len(entries) - 1
	}
	*sel = next
}

// DirPickerSwitchFocus 在左右栏之间切换。
func (m *AppModel) DirPickerSwitchFocus() {
	if p := m.DirPicker; p != nil {
		if p.Focus == DirPickerLeft {
			p.Focus = DirPickerRight
		} else {
			p.Focus = DirPickerLeft
		}
	}
}

// SetDirPickerLeft 应用左栏（上一级目录）条目，并把选中光标对准当前目录。
func (m *AppModel) SetDirPickerLeft(entries []DirEntry) {
	p := m.DirPicker
	if p == nil {
		return
	}
	p.leftAll = entries
	p.Left = p.filterEntries(entries)
	p.LeftSel = indexDirEntry(p.Left, p.Dir)
}

// SetDirPickerRight 应用右栏（当前目录）条目；prefer 非空时选中该项。
func (m *AppModel) SetDirPickerRight(entries []DirEntry, prefer string) {
	p := m.DirPicker
	if p == nil {
		return
	}
	p.rightAll = entries
	p.Right = p.filterEntries(entries)
	p.RightSel = 0
	if prefer != "" {
		p.RightSel = indexDirEntry(p.Right, prefer)
	}
	p.Loading = false
}

// dirPickerAlwaysHidden 是无论隐藏文件开关如何都不显示的条目名：`.alkaid0`
// 是服务端/客户端的内部状态目录（沙箱挂载、技能、图片等），`.git` 是版本库
// 元数据，都不是工作区内容。
var dirPickerAlwaysHidden = map[string]bool{
	".alkaid0": true,
	".git":     true,
}

// filterEntries 按隐藏文件开关过滤 "." 开头的条目；ShowHidden 时只保留
// dirPickerAlwaysHidden 的排除（.alkaid0 始终不出现）。
func (p *DirPickerState) filterEntries(entries []DirEntry) []DirEntry {
	if p == nil {
		return entries
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		if dirPickerAlwaysHidden[e.Name] {
			continue
		}
		if !p.ShowHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// DirPickerToggleHidden 切换 "." 开头的隐藏文件/目录显示，返回切换后的状态。
// 两侧列表都从原始列表重建，并按路径尽量保持选中项（被隐藏时回到第一项）。
func (m *AppModel) DirPickerToggleHidden() bool {
	p := m.DirPicker
	if p == nil {
		return false
	}
	leftPath, rightPath := "", ""
	if e, ok := entryAt(p.Left, p.LeftSel); ok {
		leftPath = e.Path
	}
	if e, ok := entryAt(p.Right, p.RightSel); ok {
		rightPath = e.Path
	}
	p.ShowHidden = !p.ShowHidden
	p.Left = p.filterEntries(p.leftAll)
	p.Right = p.filterEntries(p.rightAll)
	p.LeftSel = selectDirEntry(p.Left, leftPath)
	p.RightSel = selectDirEntry(p.Right, rightPath)
	return p.ShowHidden
}

// entryAt 返回 entries[sel]；越界或空列表时 ok=false。
func entryAt(entries []DirEntry, sel int) (DirEntry, bool) {
	if sel < 0 || sel >= len(entries) {
		return DirEntry{}, false
	}
	return entries[sel], true
}

// selectDirEntry 返回 path 在过滤后列表中的下标；找不到或列表为空时返回 0。
func selectDirEntry(entries []DirEntry, path string) int {
	if len(entries) == 0 {
		return 0
	}
	if path == "" {
		return 0
	}
	for i, e := range entries {
		if e.Path == path {
			return i
		}
	}
	return 0
}

// SetDirPickerRootLeft 在系统根时把左栏设为逻辑根（本机主目录 → 系统根 →
// 常用目录），并把选中光标对准系统根。
func (m *AppModel) SetDirPickerRootLeft() {
	p := m.DirPicker
	if p == nil {
		return
	}
	entries := p.logicalRootEntries()
	p.Left = entries
	p.LeftSel = indexDirEntry(entries, p.Dir)
}

// DirPickerStartNewFolder 进入新建文件夹输入态。
func (m *AppModel) DirPickerStartNewFolder() {
	p := m.DirPicker
	if p == nil {
		return
	}
	p.Editing = DirPickerNewFolder
	p.Input = widget.NewInputBuffer()
	p.Err = ""
}

// DirPickerStartJump 进入路径跳转输入态。
func (m *AppModel) DirPickerStartJump() {
	p := m.DirPicker
	if p == nil {
		return
	}
	p.Editing = DirPickerJump
	p.Input = widget.NewInputBuffer()
	p.Err = ""
}

// DirPickerCancelEdit 取消底部输入态。
func (m *AppModel) DirPickerCancelEdit() {
	p := m.DirPicker
	if p == nil {
		return
	}
	p.Editing = DirPickerIdle
	p.Input = nil
	p.Err = ""
}

// DirPickerEditing 报告是否处于底部输入态。
func (m *AppModel) DirPickerEditing() bool {
	return m.DirPicker != nil && m.DirPicker.Editing != DirPickerIdle
}

// DirPickerInputText 返回底部输入内容（去除首尾空白）。
func (m *AppModel) DirPickerInputText() string {
	p := m.DirPicker
	if p == nil || p.Input == nil {
		return ""
	}
	return strings.TrimSpace(p.Input.Text())
}

// DirPickerSetError 在面板底部显示一条错误（不关闭面板）。
func (m *AppModel) DirPickerSetError(msg string) {
	if p := m.DirPicker; p != nil {
		p.Err = msg
		p.Loading = false
	}
}

// DirPickerSetLoading 标记当前目录的列举是否在进行中。
func (m *AppModel) DirPickerSetLoading(loading bool) {
	if p := m.DirPicker; p != nil {
		p.Loading = loading
	}
}

// DirPickerSetDir 更新当前浏览目录（不修改列表；列表由 app 异步加载）。
func (m *AppModel) DirPickerSetDir(dir string) {
	if p := m.DirPicker; p != nil {
		p.Dir = dir
	}
}

// indexDirEntry 返回 path 在 entries 中的索引；不存在时返回 0（列表非空）
// 或 -1（列表为空）。
func indexDirEntry(entries []DirEntry, path string) int {
	if len(entries) == 0 {
		return -1
	}
	for i, e := range entries {
		if e.Path == path {
			return i
		}
	}
	return 0
}
