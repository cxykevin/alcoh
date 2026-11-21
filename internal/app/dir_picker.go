package app

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/widget"
)

// ---- 服务端路径语义 ----
//
// 目录选择器浏览的是 *服务端* 文件系统：本地 alkaid0 与远程 alkaid0、
// Linux 与 Windows 都可能出现。路径运算不能使用客户端的 path/filepath，
// 否则 Linux 客户端连远程 Windows 时会把 "C:\Users" 的父目录算成 "."。

// serverPathOps 是服务端路径运算（POSIX 或 Windows）。
type serverPathOps interface {
	Root() string
	Clean(p string) string
	Parent(p string) string
	Join(dir, name string) string
	IsAbs(p string) bool
}

// posixPathOps 按 "/" 处理服务端路径。
type posixPathOps struct{}

func (posixPathOps) Root() string { return "/" }

func (posixPathOps) Clean(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func (posixPathOps) Parent(p string) string {
	p = posixPathOps{}.Clean(p)
	if p == "/" {
		return "/"
	}
	return path.Dir(p)
}

func (posixPathOps) Join(dir, name string) string {
	if dir == "" {
		dir = "/"
	}
	return path.Join(dir, name)
}

func (posixPathOps) IsAbs(p string) bool { return strings.HasPrefix(p, "/") }

// windowsPathOps 按 "\" 处理服务端路径，根为构造时推断的盘符根（如 C:\）。
type windowsPathOps struct{ root string }

func newWindowsPathOps(workdir string) windowsPathOps {
	root := `C:\`
	if len(workdir) >= 2 && workdir[1] == ':' {
		root = strings.ToUpper(workdir[:1]) + `:\`
	}
	return windowsPathOps{root: root}
}

func (w windowsPathOps) Root() string { return w.root }

func (w windowsPathOps) Clean(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "/", `\`)
	if p == "" {
		return w.root
	}
	if len(p) == 2 && p[1] == ':' {
		return strings.ToUpper(p[:1]) + `:\`
	}
	if len(p) >= 2 && p[1] == ':' {
		p = strings.ToUpper(p[:1]) + p[1:]
	}
	for len(p) > 3 && strings.HasSuffix(p, `\`) {
		p = p[:len(p)-1]
	}
	if len(p) == 2 && p[1] == ':' {
		return p + `\`
	}
	return p
}

func (w windowsPathOps) Parent(p string) string {
	p = w.Clean(p)
	if p == w.root || len(p) <= 3 {
		return w.root
	}
	i := strings.LastIndex(p, `\`)
	if i < 0 {
		return w.root
	}
	if i == 2 && p[1] == ':' {
		return p[:3]
	}
	return p[:i]
}

func (w windowsPathOps) Join(dir, name string) string {
	dir = w.Clean(dir)
	if strings.HasSuffix(dir, `\`) {
		return dir + name
	}
	return dir + `\` + name
}

func (w windowsPathOps) IsAbs(p string) bool {
	p = strings.ReplaceAll(p, "/", `\`)
	if strings.HasPrefix(p, `\\`) {
		return true
	}
	if len(p) >= 3 && p[1] == ':' && p[2] == '\\' {
		return true
	}
	return strings.HasPrefix(p, `\`)
}

// looksWindowsPath 根据工作目录推断服务端是否为 Windows 路径。
func looksWindowsPath(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return strings.HasPrefix(p, `\\`)
}

func serverPathsFor(workdir string) serverPathOps {
	if looksWindowsPath(workdir) {
		return newWindowsPathOps(workdir)
	}
	return posixPathOps{}
}

// ---- 文件系统提供者（全程 fs over ACP） ----

// dirFS 是目录选择器依赖的文件系统操作。实现全部走 ACP：
//   - List 使用 fs/read 不带 sessionId 的绝对路径只读访问；
//   - Mkdir 为 fs/mkdir 开一个以 dir 为 cwd 的临时会话，建完即关闭。
//
// 将来的 remote 启动选择器可以复用同一套 UI 与按键，只替换提供者。
type dirFS interface {
	List(ctx context.Context, dir string) ([]model.DirEntry, error)
	Mkdir(ctx context.Context, dir, name string) (tempSessionID string, err error)
}

// acpDirFS 是 dirFS 的 ACP 实现。
type acpDirFS struct {
	backend acp.Backend
	paths   serverPathOps
}

func (f acpDirFS) List(ctx context.Context, dir string) ([]model.DirEntry, error) {
	abs, ok := f.backend.(acp.FSAbsControl)
	if !ok {
		return nil, errors.New(i18n.T("当前后端不支持文件系统浏览（需要 alkaid0 扩展能力）"))
	}
	entries, err := abs.ListDirAbs(ctx, dir)
	if err != nil {
		return nil, err
	}
	// 目录与文件都列出：目录黄色可进入，文件仅用于区分展示（回车不进入）。
	out := make([]model.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name == "" {
			continue
		}
		out = append(out, model.DirEntry{
			Name:  e.Name,
			Path:  f.paths.Join(dir, e.Name),
			IsDir: e.Type == "directory",
		})
	}
	return out, nil
}

// Mkdir 在 dir 下新建 name。fs/mkdir 必须带 sessionId 且只接受相对路径，
// 因此先以 dir 为 cwd 开一个临时会话（与 Astrbot 插件的 _mkdir_remote 同
// 思路），执行后关闭。返回临时会话 ID，供 app 丢弃它的事件。
func (f acpDirFS) Mkdir(ctx context.Context, dir, name string) (string, error) {
	sess, err := f.backend.NewSession(ctx, dir)
	if err != nil {
		return "", err
	}
	id := sess.ID()
	writer, ok := sess.(acp.FSWriter)
	if !ok {
		_ = sess.Close(ctx)
		return id, errors.New(i18n.T("当前后端不支持 fs/mkdir（需要 alkaid0 扩展能力）"))
	}
	err = writer.Mkdir(ctx, name)
	// 临时会话只用于这次 fs/mkdir；关闭失败不影响建目录结果。
	_ = sess.Close(ctx)
	return id, err
}

// ---- 控制器 ----

// dirPickerRequest 是打开目录选择器所需的全部外部依赖。它把选择器与
// /cd 解耦：remote 启动时用同一份 UI，只替换 FS / Paths / HistoryKey / OnPick。
type dirPickerRequest struct {
	FS         dirFS
	Paths      serverPathOps
	HistoryKey string
	Title      string
	// Home 是逻辑根固定第一项的本机主目录；空表示不展示（远程连接）。
	Home   string
	OnPick func(dir string)
}

type dirPickerController struct {
	req dirPickerRequest
	seq uint64
}

const (
	dirSideLeft = iota
	dirSideRight
)

// openDirPicker 打开选择器：总是停在逻辑根（最近使用），系统根是列表里的一项。
func (a *App) openDirPicker(req dirPickerRequest) {
	if req.Home != "" {
		req.Home = req.Paths.Clean(req.Home)
	}
	recent := config.LoadDirHistory(req.HistoryKey)
	if a.model.OpenDirPicker(req.Title, req.Paths.Root(), req.Home, recent) == nil {
		return
	}
	a.dirPicker = &dirPickerController{req: req}
	a.loadDirPicker("", "")
}

// loadDirPicker 切换当前浏览目录并异步加载左右两栏：
//   - dir 为空表示逻辑根（最近使用）：列表全部来自本地数据（主目录 + 系统根
//   - 常用目录），不发 fs 请求，左栏留空。
//   - 否则左栏是上一级目录的子目录（系统根的上一级是逻辑根），右栏是当前
//     目录的子目录。
func (a *App) loadDirPicker(dir, prefer string) {
	c := a.dirPicker
	p := a.model.DirPicker
	if c == nil || p == nil {
		return
	}
	if dir == "" {
		// 递增 seq 让切换前在途的列举结果失效。
		c.seq++
		a.model.SetDirPickerLogicalRoot()
		return
	}
	dir = c.req.Paths.Clean(dir)
	c.seq++
	seq := c.seq
	a.model.DirPickerSetDir(dir)
	a.model.DirPickerSetLoading(true)
	if dir == p.Root {
		// 系统根的上一级是逻辑根：左栏直接展示逻辑根条目。
		a.model.SetDirPickerRootLeft()
	} else {
		a.startDirList(seq, c.req.Paths.Parent(dir), dirSideLeft, "")
	}
	a.startDirList(seq, dir, dirSideRight, prefer)
}

// startDirList 发起一次目录列举；seq 用于丢弃切换目录后晚回的旧结果。
func (a *App) startDirList(seq uint64, dir string, side int, prefer string) {
	c := a.dirPicker
	if c == nil {
		return
	}
	fs := c.req.FS
	a.startCommandResult(commandResult{
		kind:      commandDirList,
		dirSeq:    seq,
		dirSide:   side,
		dirPath:   dir,
		dirPrefer: prefer,
	}, func(ctx context.Context, result *commandResult) error {
		entries, err := fs.List(ctx, dir)
		result.dirEntries = entries
		return err
	})
}

// dirPickerKey 处理目录选择器按键。
func (a *App) dirPickerKey(ke input.KeyEvent) {
	m := a.model
	p := m.DirPicker
	if p == nil {
		m.SetModal(model.NoModal)
		return
	}
	if m.DirPickerEditing() {
		switch {
		case ke.Type == input.KeyEsc:
			m.DirPickerCancelEdit()
		case ke.Type == input.KeyEnter && !ke.IsShift():
			a.commitDirPickerEdit()
		default:
			if p.Input != nil {
				ib := &widget.InputBox{Buf: p.Input, Style: a.view.Theme.Style(a.view.Theme.Text)}
				ib.OnKey(ke)
			}
		}
		return
	}
	switch ke.Type {
	case input.KeyEsc:
		m.CloseDirPicker()
		a.dirPicker = nil
	case input.KeyUp:
		m.DirPickerMove(-1)
	case input.KeyDown:
		m.DirPickerMove(1)
	case input.KeyTab:
		if p.AtLogicalRoot() {
			// 逻辑根的左栏是空的（没有上一级），没有可切换的目标栏。
			return
		}
		m.DirPickerSwitchFocus()
	case input.KeyLeft, input.KeyBackspace:
		a.dirPickerUp()
	case input.KeyRight:
		if p.Focus == model.DirPickerLeft {
			m.DirPickerSwitchFocus()
			return
		}
		a.dirPickerEnter()
	case input.KeyEnter:
		a.dirPickerEnter()
	case input.KeyRune:
		if ke.IsCtrl() || ke.IsAlt() {
			return
		}
		switch ke.Rune {
		case 'h':
			if m.DirPickerToggleHidden() {
				m.ShowInfo(i18n.T("已显示隐藏文件"))
			} else {
				m.ShowInfo(i18n.T("已隐藏隐藏文件"))
			}
		case 'n':
			if p.AtLogicalRoot() {
				// 逻辑根不对应真实目录，不能在其中新建文件夹。
				return
			}
			m.DirPickerStartNewFolder()
		case '/', 'g':
			m.DirPickerStartJump()
		case 's', ' ':
			if p.AtLogicalRoot() {
				// 逻辑根本身不是目录，没有可切换的工作区。
				return
			}
			a.chooseDirPickerCurrent()
		}
	}
}

// dirPickerUp 返回上一级：系统根的上一级是逻辑根（最近使用），逻辑根本身
// 没有上一级，← 不做任何事。
func (a *App) dirPickerUp() {
	m := a.model
	p := m.DirPicker
	if p == nil || p.AtLogicalRoot() {
		return
	}
	if p.Dir == p.Root {
		a.loadDirPicker("", "")
		return
	}
	parent := a.dirPicker.req.Paths.Parent(p.Dir)
	a.loadDirPicker(parent, p.Dir)
	p.Focus = model.DirPickerRight
}

// dirPickerEnter 进入聚焦栏选中的目录。左栏选中当前目录本身时只切到右栏。
func (a *App) dirPickerEnter() {
	m := a.model
	p := m.DirPicker
	entry, ok := p.SelectedEntry()
	if !ok {
		return
	}
	if !entry.IsDir {
		// 文件只用于区分展示（黄色=目录），回车不进入。
		return
	}
	if p.Focus == model.DirPickerLeft && entry.Path == p.Dir {
		m.DirPickerSwitchFocus()
		return
	}
	a.loadDirPicker(entry.Path, "")
	p.Focus = model.DirPickerRight
}

// chooseDirPickerCurrent 选中当前目录作为工作区并关闭。
func (a *App) chooseDirPickerCurrent() {
	m := a.model
	c := a.dirPicker
	p := m.DirPicker
	if c == nil || p == nil || p.AtLogicalRoot() {
		return
	}
	dir := p.Dir
	m.CloseDirPicker()
	a.dirPicker = nil
	config.RecordDirHistory(c.req.HistoryKey, dir)
	if c.req.OnPick != nil {
		c.req.OnPick(dir)
	}
}

// commitDirPickerEdit 提交底部输入：新建文件夹或跳转路径。
func (a *App) commitDirPickerEdit() {
	m := a.model
	c := a.dirPicker
	p := m.DirPicker
	if c == nil || p == nil {
		return
	}
	text := m.DirPickerInputText()
	if text == "" {
		m.DirPickerSetError(i18n.T("请输入内容"))
		return
	}
	switch p.Editing {
	case model.DirPickerNewFolder:
		if strings.ContainsAny(text, `/\`) || text == "." || text == ".." {
			m.DirPickerSetError(i18n.T("文件夹名不能包含路径分隔符或为 . / .."))
			return
		}
		dir := p.Dir
		prefer := c.req.Paths.Join(dir, text)
		a.startCommandResult(commandResult{
			kind:      commandDirMkdir,
			dirPath:   dir,
			dirPrefer: prefer,
		}, func(ctx context.Context, result *commandResult) error {
			id, err := c.req.FS.Mkdir(ctx, dir, text)
			result.tempSessionID = id
			return err
		})
	case model.DirPickerJump:
		base := p.Dir
		if base == "" {
			// 逻辑根不对应真实目录：相对路径从系统根解析。
			base = p.Root
		}
		target := text
		if !c.req.Paths.IsAbs(target) {
			target = c.req.Paths.Join(base, target)
		}
		target = c.req.Paths.Clean(target)
		c.seq++
		seq := c.seq
		a.startCommandResult(commandResult{
			kind:    commandDirJump,
			dirSeq:  seq,
			dirPath: target,
		}, func(ctx context.Context, result *commandResult) error {
			entries, err := c.req.FS.List(ctx, target)
			result.dirEntries = entries
			return err
		})
	}
}

// applyDirListResult 把一次目录列举结果写入对应栏；晚回的旧序号直接丢弃。
func (a *App) applyDirListResult(result commandResult) {
	c := a.dirPicker
	if c == nil || result.dirSeq != c.seq {
		return
	}
	if result.err != nil {
		a.model.DirPickerSetError(i18n.T("读取目录失败: %s", result.err.Error()))
		return
	}
	if result.dirSide == dirSideLeft {
		a.model.SetDirPickerLeft(result.dirEntries)
		return
	}
	a.model.SetDirPickerRight(result.dirEntries, result.dirPrefer)
}

// applyDirJumpResult 处理选择器内的路径跳转（先列举校验，再切换目录）。
func (a *App) applyDirJumpResult(result commandResult) {
	c := a.dirPicker
	if c == nil || result.dirSeq != c.seq {
		return
	}
	if result.err != nil {
		a.model.DirPickerSetError(i18n.T("无法跳转到该路径: %s", result.err.Error()))
		return
	}
	a.model.DirPickerCancelEdit()
	a.loadDirPicker(result.dirPath, "")
	if p := a.model.DirPicker; p != nil {
		p.Focus = model.DirPickerRight
	}
}

// applyDirMkdirResult 处理新建文件夹结果：先忽略临时会话事件，再刷新并选中新目录。
func (a *App) applyDirMkdirResult(result commandResult) {
	if result.tempSessionID != "" {
		// 临时会话只为发一次 fs/mkdir；它的事件不能进入正文/待激活缓存。
		a.model.IgnoreSession(result.tempSessionID)
	}
	if result.err != nil {
		a.model.DirPickerSetError(i18n.T("新建文件夹失败: %s", result.err.Error()))
		return
	}
	a.model.DirPickerCancelEdit()
	a.loadDirPicker(result.dirPath, result.dirPrefer)
	a.model.ShowInfo(i18n.T("已新建文件夹: %s", result.dirPrefer))
}

// dirCommand 处理 /cd：空参打开选择器，带参直接切换（先经 fs/read 校验）。
func (a *App) dirCommand(rest string) {
	m := a.model
	if m.View != model.ViewHome {
		m.ShowError(i18n.T("会话中不能切换工作目录，请先返回主页"))
		return
	}
	paths := serverPathsFor(a.workdir)
	fs := acpDirFS{backend: a.backend, paths: paths}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		a.openDirPicker(dirPickerRequest{
			FS:         fs,
			Paths:      paths,
			HistoryKey: a.dirHistoryKey,
			Home:       a.localHome,
			Title:      i18n.T("切换工作目录 (/cd)"),
			OnPick:     a.applyWorkspace,
		})
		return
	}
	target := rest
	if !paths.IsAbs(target) {
		target = paths.Join(a.workdir, target)
	}
	target = paths.Clean(target)
	a.startCommandResult(commandResult{
		kind:      commandDirJump,
		dirPath:   target,
		dirDirect: true,
	}, func(ctx context.Context, result *commandResult) error {
		_, err := fs.List(ctx, target)
		return err
	})
}

// applyWorkspace 切换工作区：更新 workdir 与 backend 默认 cwd，重建主页预创建
// 会话并刷新会话列表。调用方必须已持有 modelMu，且仅在主页调用。
func (a *App) applyWorkspace(dir string) {
	if a.model.View != model.ViewHome {
		a.model.ShowError(i18n.T("会话中不能切换工作目录，请先返回主页"))
		return
	}
	a.workdir = dir
	if setter, ok := a.backend.(acp.CWDSetter); ok {
		setter.SetCWD(dir)
	}
	// 旧目录的预创建会话 cwd 已不匹配，丢弃后在新目录重建。
	a.discardPreSession()
	a.ensurePreSessionLocked()
	a.refreshSessionsLocked()
	a.model.ShowInfo(i18n.T("已切换工作目录: %s", dir))
}
