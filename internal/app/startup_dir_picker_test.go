package app

// 启动目录选择器（远端位置参数只给主机、没给对方路径）的用例：启动完成即打开
// 选择器、选中目录后切换工作区并写入历史；未置位时不打开。

import (
	"slices"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/input"
)

// TestStartupDirPickerOpensAndChooses 验证远端连接无路径时启动即打开选择器，
// 选中目录后 workdir 与后端 CWD 一起更新，并写入该连接的历史桶。
func TestStartupDirPickerOpensAndChooses(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newDirBackend()
	a := New(ft, b)
	// 远端默认路径是对方根目录；远程连接不展示本机主目录。
	a.SetWorkdir("/")
	a.SetDirHistoryKey("ws://remote:7433/acp")
	a.SetLocalHome("")
	a.SetStartupDirPicker(true)
	done := runApp(t, a)

	// 逻辑根：没有本机主目录、没有历史，只有系统根。
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerOpen && !s.DirPickerLoading && s.DirPickerDir == "" && len(s.DirPickerRight) == 1
	})
	if got := snapPaths(a.snapshot().DirPickerRight); !slices.Equal(got, []string{"/"}) {
		t.Fatalf("logical root right = %v, want [/]", got)
	}

	// 逻辑根 → 系统根 → work。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerDir == "/" && !s.DirPickerLoading && len(s.DirPickerRight) == 3
	})
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.DirPickerDir == "/work" && !s.DirPickerLoading
	})
	ft.sendKey(input.RuneKey('s', input.ModNone))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return !s.DirPickerOpen && s.Workdir == "/work"
	})

	quitApp(ft)
	waitRun(t, done)
	if got := b.workdir(); got != "/work" {
		t.Fatalf("backend cwd = %q, want /work", got)
	}
	history := config.LoadDirHistory("ws://remote:7433/acp")
	if len(history) != 1 || history[0] != "/work" {
		t.Fatalf("dir history = %v, want [/work]", history)
	}
}

// TestStartupDirPickerDisabled 验证未置位时启动不会打开选择器（默认本地启动）。
func TestStartupDirPickerDisabled(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := newDirBackend()
	a := New(ft, b)
	a.SetWorkdir("/start")
	a.SetDirHistoryKey("test-conn")
	done := runApp(t, a)

	time.Sleep(300 * time.Millisecond)
	if s := a.snapshot(); s.DirPickerOpen {
		t.Fatalf("dir picker should not open at startup")
	}
	quitApp(ft)
	waitRun(t, done)
}
