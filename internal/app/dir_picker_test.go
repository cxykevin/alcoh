package app

// /cd 目录选择器的交互测试：逻辑根（最近使用）列表与固定主目录、从逻辑根
// 进入系统根、切换工作区并写入历史；新建文件夹后定位新目录；`/cd <路径>`
// 直达切换与失败提示；会话视图内拒绝切换。

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// dirBackend 包装 demo backend，提供无会话的绝对路径目录列举（fs/read）与
// run 期切换默认工作目录（/cd），并记录 mkdir 结果。
type dirBackend struct {
	*demo.Backend
	mu     sync.Mutex
	dirs   map[string][]acp.FSDirEntry
	cwd    string
	mkdirs []string
}

func newDirBackend() *dirBackend {
	return &dirBackend{
		Backend: demo.New(true),
		dirs: map[string][]acp.FSDirEntry{
			"/": {
				{Name: "work", Type: "directory"},
				{Name: "home", Type: "directory"},
				{Name: "README.md", Type: "file"},
			},
			"/work":          {{Name: "proj", Type: "directory"}},
			"/work/proj":     {},
			"/home":          {{Name: "cxykevin", Type: "directory"}},
			"/home/cxykevin": {},
		},
	}
}

func (b *dirBackend) ListDirAbs(ctx context.Context, dir string) ([]acp.FSDirEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries, ok := b.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("no such directory: %s", dir)
	}
	return append([]acp.FSDirEntry(nil), entries...), nil
}

func (b *dirBackend) SetCWD(dir string) {
	b.mu.Lock()
	b.cwd = dir
	b.mu.Unlock()
}

func (b *dirBackend) workdir() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cwd
}

func (b *dirBackend) mkdirLog() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.mkdirs...)
}

// NewSession 包装会话以实现 fs/mkdir（必须带 sessionId、路径相对会话 cwd）。
func (b *dirBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &dirSession{Session: s, b: b, cwd: cwd}, nil
}

type dirSession struct {
	acp.Session
	b   *dirBackend
	cwd string
}

func (s *dirSession) Mkdir(ctx context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	path := s.cwd + "/" + name
	s.b.dirs[s.cwd] = append(s.b.dirs[s.cwd], acp.FSDirEntry{Name: name, Type: "directory"})
	if _, ok := s.b.dirs[path]; !ok {
		s.b.dirs[path] = nil
	}
	s.b.mkdirs = append(s.b.mkdirs, path)
	return nil
}

// snapPaths 返回目录条目路径，便于整体比较。
func snapPaths(entries []model.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

// openDirPickerHome 在主页输入 /cd 并回车，等待选择器停在逻辑根。
func openDirPickerHome(t *testing.T, ft *fakeTerm, a *App) modelSnapshot {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	typeText(ft, "/cd")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerOpen && !s.DirPickerLoading && s.DirPickerDir == "" && len(s.DirPickerRight) > 0
	})
	return a.snapshot()
}

// TestDirPickerLogicalRoot 验证打开即停在逻辑根：左栏留空，右栏依次是本机
// 主目录、系统根与常用目录；Tab 不切空栏，回车进入系统根后左栏是逻辑根。
func TestDirPickerLogicalRoot(t *testing.T) {
	setConfigDir(t)
	if err := config.SaveDirHistory("test-conn", []string{"/work"}); err != nil {
		t.Fatalf("save dir history: %v", err)
	}
	ft := newFakeTerm()
	b := newDirBackend()
	a := New(ft, b)
	a.SetWorkdir("/start")
	a.SetDirHistoryKey("test-conn")
	a.SetLocalHome("/home/cxykevin")
	done := runApp(t, a)

	s := openDirPickerHome(t, ft, a)
	if len(s.DirPickerLeft) != 0 {
		t.Fatalf("logical root left = %v, want empty", s.DirPickerLeft)
	}
	want := []string{"/home/cxykevin", "/", "/work"}
	if got := snapPaths(s.DirPickerRight); !slices.Equal(got, want) {
		t.Fatalf("logical root right = %v, want %v", got, want)
	}
	// 逻辑根左栏为空：Tab 不应切走焦点（否则随后的 ↓ 不会移动右栏）。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	ft.sendKey(input.SimpleKey(input.KeyDown))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.DirPickerSel == 1 })

	// 回车进入系统根：左栏是逻辑根列表（当前目录 / 标记在第 2 项）。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerDir == "/" && !s.DirPickerLoading &&
			len(s.DirPickerRight) == 3 && s.DirPickerLeftSel == 1
	})
	if got := snapPaths(a.snapshot().DirPickerLeft); !slices.Equal(got, want) {
		t.Fatalf("root left = %v, want logical root %v", got, want)
	}

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.DirPickerOpen })
	quitApp(ft)
	waitRun(t, done)
}

// TestDirPickerNavigateAndChoose 验证逻辑根 → 系统根 → 子目录的浏览路径、
// 左栏重新对准当前目录，以及 s 选中当前目录后切换工作区并写入历史。
func TestDirPickerNavigateAndChoose(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newDirBackend()
	a := New(ft, b)
	a.SetWorkdir("/start")
	a.SetDirHistoryKey("test-conn")
	done := runApp(t, a)

	s := openDirPickerHome(t, ft, a)
	// 远程/无本机主目录、无历史时逻辑根只有系统根。
	if got := snapPaths(s.DirPickerRight); !slices.Equal(got, []string{"/"}) {
		t.Fatalf("logical root right = %v, want [/]", got)
	}

	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		// 根目录同时列出目录（work/home）与文件（README.md）。
		return s.DirPickerDir == "/" && !s.DirPickerLoading && len(s.DirPickerRight) == 3 &&
			s.DirPickerLeftSel == 0
	})
	// 右栏第 1 项是 work，第 2 项是 home：↓ 选中 home 后回车进入。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.DirPickerSel == 1 })
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerDir == "/home" && !s.DirPickerLoading && len(s.DirPickerRight) == 1 &&
			s.DirPickerLeftSel == 1
	})

	ft.sendKey(input.RuneKey('s', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.DirPickerOpen && s.Workdir == "/home" })

	quitApp(ft)
	waitRun(t, done)
	if got := b.workdir(); got != "/home" {
		t.Fatalf("backend cwd = %q, want /home", got)
	}
	history := config.LoadDirHistory("test-conn")
	if len(history) != 1 || history[0] != "/home" {
		t.Fatalf("dir history = %v, want [/home]", history)
	}
}

// TestDirPickerNewFolder 验证 n 输入新文件夹名后经临时会话执行 fs/mkdir，
// 完成后刷新当前目录并选中新目录，底部提示创建结果。
func TestDirPickerNewFolder(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newDirBackend()
	a := New(ft, b)
	a.SetWorkdir("/start")
	a.SetDirHistoryKey("test-conn")
	done := runApp(t, a)

	openDirPickerHome(t, ft, a)
	// 逻辑根 → 系统根 → work。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerDir == "/" && !s.DirPickerLoading && len(s.DirPickerRight) == 3
	})
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.DirPickerDir == "/work" && !s.DirPickerLoading })

	ft.sendKey(input.RuneKey('n', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.DirPickerEdit == model.DirPickerNewFolder })
	typeText(ft, "newdir")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return !s.DirPickerLoading && len(s.DirPickerRight) == 2 &&
			s.DirPickerRight[s.DirPickerSel].Name == "newdir" &&
			strings.Contains(s.Error, "已新建文件夹")
	})
	if s := a.snapshot(); s.DirPickerEdit != model.DirPickerIdle || s.DirPickerErr != "" {
		t.Fatalf("edit/err = %v/%q after mkdir", s.DirPickerEdit, s.DirPickerErr)
	}

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.DirPickerOpen })
	quitApp(ft)
	waitRun(t, done)

	if got := b.mkdirLog(); !slices.Equal(got, []string{"/work/newdir"}) {
		t.Fatalf("mkdir calls = %v, want [/work/newdir]", got)
	}
}

// TestDirCommandDirectPath 验证 `/cd <路径>` 先列举校验再切换工作区；
// 目标不存在时保留原工作区并提示失败；会话视图内拒绝切换。
func TestDirCommandDirectPath(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newDirBackend()
	a := New(ft, b)
	a.SetWorkdir("/start")
	a.SetDirHistoryKey("test-conn")
	done := runApp(t, a)
	time.Sleep(200 * time.Millisecond)

	typeText(ft, "/cd /work")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.Workdir == "/work" && !s.DirPickerOpen })
	waitSnapshot(t, a, func(s modelSnapshot) bool { return strings.Contains(s.Error, "已切换工作目录: /work") })

	typeText(ft, "/cd /missing")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return strings.Contains(s.Error, "无法切换到该目录") && s.Workdir == "/work"
	})

	// 会话视图内禁止切换。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	waitCondition(t, "session view", func() bool { return inView(a, model.ViewSession) })
	typeText(ft, "/cd")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return strings.Contains(s.Error, "会话中不能切换工作目录") && !s.DirPickerOpen
	})

	quitApp(ft)
	waitRun(t, done)
	if got := b.workdir(); got != "/work" {
		t.Fatalf("backend cwd = %q, want /work", got)
	}
	history := config.LoadDirHistory("test-conn")
	if len(history) != 1 || history[0] != "/work" {
		t.Fatalf("dir history = %v, want [/work]", history)
	}
}

// TestDirPickerFilesAndHidden 验证目录与文件都列出（文件不会被回车进入），
// 默认隐藏 "." 开头的条目，h 切换显示后再切换回隐藏。
func TestDirPickerFilesAndHidden(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newDirBackend()
	b.dirs["/"] = append(b.dirs["/"],
		acp.FSDirEntry{Name: ".git", Type: "directory"},
		acp.FSDirEntry{Name: ".env", Type: "file"},
		// 内部状态目录：永远不列出，即使按 h 显示隐藏文件。
		acp.FSDirEntry{Name: ".alkaid0", Type: "directory"},
	)
	a := New(ft, b)
	a.SetWorkdir("/start")
	a.SetDirHistoryKey("test-conn")
	done := runApp(t, a)

	openDirPickerHome(t, ft, a)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerDir == "/" && !s.DirPickerLoading && len(s.DirPickerRight) == 3
	})
	var hasFile bool
	for _, e := range a.snapshot().DirPickerRight {
		if strings.HasPrefix(e.Name, ".") {
			t.Fatalf("hidden entry %q should be filtered by default", e.Name)
		}
		if e.Name == "README.md" {
			hasFile = true
			if e.IsDir {
				t.Fatal("README.md should not be marked as a directory")
			}
		}
	}
	if !hasFile {
		t.Fatal("README.md file should be listed")
	}

	ft.sendKey(input.RuneKey('h', input.ModNone))
	// .git 与 .alkaid0 始终排除：只多出 .env 一项。
	waitSnapshot(t, a, func(s modelSnapshot) bool { return len(s.DirPickerRight) == 4 })
	if paths := snapPaths(a.snapshot().DirPickerRight); !slices.Contains(paths, "/.env") ||
		slices.Contains(paths, "/.git") || slices.Contains(paths, "/.alkaid0") {
		t.Fatalf("hidden toggle should only reveal .env: %v", paths)
	}

	// 移到 README.md（文件）后回车：不进入任何目录。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.DirPickerSel == 1 })
	ft.sendKey(input.SimpleKey(input.KeyDown))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.DirPickerSel == 2 })
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(100 * time.Millisecond)
	if s := a.snapshot(); s.DirPickerDir != "/" || !s.DirPickerOpen {
		t.Fatalf("file selection should not navigate: dir/open = %q/%v", s.DirPickerDir, s.DirPickerOpen)
	}

	ft.sendKey(input.RuneKey('h', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return len(s.DirPickerRight) == 3 })

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.DirPickerOpen })
	quitApp(ft)
	waitRun(t, done)
}
