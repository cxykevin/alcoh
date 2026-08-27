package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/term"
	"github.com/cxykevin/alcoh/internal/view"
)

// setConfigDir 将本地配置目录指向一个临时目录并返回该目录。
// Windows 上 config.Path() 读取 AppData 而非 XDG_CONFIG_HOME，两者需一并设置，
// 否则 Save 会落到真实用户目录，测试也读不到文件。
func setConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	}
	return dir
}

// trackBackend 包装 demo backend，记录会话 Cancel 调用次数，用于验证
// /clear 默认会取消运行中的会话、/clear on 不取消。
type trackBackend struct {
	*demo.Backend
	mu      sync.Mutex
	cancels int
}

func (b *trackBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &trackSession{Session: s, b: b}, nil
}

func (b *trackBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &trackSession{Session: s, b: b}, nil
}

func (b *trackBackend) cancelCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancels
}

// trackSession 包装 acp.Session，在 Cancel 时对 backend 计数。
type trackSession struct {
	acp.Session
	b *trackBackend
}

func (s *trackSession) Cancel(ctx context.Context) error {
	s.b.mu.Lock()
	s.b.cancels++
	s.b.mu.Unlock()
	return s.Session.Cancel(ctx)
}

// fakeTerm 是可注入事件的 Terminal 实现。
type fakeTerm struct {
	evCh   chan term.Event
	out    []byte
	w, h   int
	raw    bool
	copied string
}

func newFakeTerm() *fakeTerm {
	return &fakeTerm{evCh: make(chan term.Event, 64), w: 100, h: 30}
}

func (f *fakeTerm) EnterRaw() error           { f.raw = true; return nil }
func (f *fakeTerm) ExitRaw() error            { f.raw = false; return nil }
func (f *fakeTerm) Size() (int, int)          { return f.w, f.h }
func (f *fakeTerm) Events() <-chan term.Event { return f.evCh }
func (f *fakeTerm) Write(p []byte) error {
	f.out = append(f.out, p...)
	return nil
}

// copied 记录 CopyToClipboard 收到的文本（测试断言用）。
func (f *fakeTerm) CopyToClipboard(text string) error {
	f.copied = text
	return nil
}

// sendKey 注入一次按键事件。
func (f *fakeTerm) sendKey(ke input.KeyEvent) {
	f.evCh <- term.Event{Kind: term.EventKey, Key: ke}
}

// sendMouse 注入一次鼠标事件。
func (f *fakeTerm) sendMouse(me input.MouseEvent) {
	f.evCh <- term.Event{Kind: term.EventMouse, Mouse: me}
}

func runApp(t *testing.T, a *App) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	return done
}

func waitRun(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for app to exit")
	}
}

// TestRunFullFlow 集成测试：按键驱动完整流程。所有模型断言均在 Run 退出后
// 执行，避免测试代码与 UI 主循环并发读取 model。
func TestRunFullFlow(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(50 * time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	// fast demo 在权限请求前约需一秒；额外余量避免慢 CI 误判。
	time.Sleep(1500 * time.Millisecond)
	ft.sendKey(input.RuneKey('a', input.ModNone))
	time.Sleep(400 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if ft.raw {
		t.Error("terminal should be restored after quit")
	}
	if a.model.Active == nil {
		t.Fatal("active session should exist")
	}
	s := a.model.Active
	hasThought, hasTool, hasAssistant := false, len(s.ToolOrder) > 0, false
	for _, m := range s.Messages {
		if m.Kind == model.MsgThought {
			hasThought = true
			if !m.Collapsed() {
				t.Error("thought should be collapsed after idle")
			}
		}
		if m.Kind == model.MsgAssistant {
			hasAssistant = true
		}
	}
	if !hasThought || !hasTool || !hasAssistant {
		t.Errorf("expected thought/tool/assistant, got thought=%v tool=%v assistant=%v", hasThought, hasTool, hasAssistant)
	}
}

// TestCtrlOExpandAll 验证 Ctrl+O 展开会话中全部思维链（思考在 idle 后折叠）。
// 工具调用展开逻辑由 model.TestExpandAll 覆盖，此处只验证按键分发。
func TestCtrlOExpandAll(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	// 等待权限请求出现并批准。
	time.Sleep(1600 * time.Millisecond)
	ft.sendKey(input.RuneKey('a', input.ModNone))
	// 等待会话结束（idle → 思考自动折叠）。
	time.Sleep(500 * time.Millisecond)
	ft.sendKey(input.RuneKey('o', input.ModCtrl))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.Active == nil {
		t.Fatal("active session should exist")
	}
	expanded := 0
	for _, m := range a.model.Active.Messages {
		if m.Kind == model.MsgThought {
			if !m.Expanded {
				t.Errorf("thought %s should be expanded after Ctrl+O", m.MessageID)
			}
			expanded++
		}
	}
	if expanded == 0 {
		t.Error("no thought messages found in demo session")
	}
}

// TestCtrlOCollapseAll 验证 Ctrl+O 再按一次收回：全部展开后再次按
// Ctrl+O 时全部折叠（思考与工具调用均收起）。
func TestCtrlOCollapseAll(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	// 等待权限请求出现并批准。
	time.Sleep(1600 * time.Millisecond)
	ft.sendKey(input.RuneKey('a', input.ModNone))
	// 等待会话结束（idle → 思考自动折叠）。
	time.Sleep(500 * time.Millisecond)
	ft.sendKey(input.RuneKey('o', input.ModCtrl))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('o', input.ModCtrl))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.Active == nil {
		t.Fatal("active session should exist")
	}
	s := a.model.Active
	if !s.HasCollapsible() {
		t.Fatal("expected collapsible thoughts/tools in demo session")
	}
	expanded := 0
	for _, m := range s.Messages {
		if m.Kind == model.MsgThought {
			if m.Expanded {
				t.Errorf("thought %s should be collapsed after second Ctrl+O", m.MessageID)
			}
			expanded++
		}
	}
	if expanded == 0 {
		t.Error("no thought messages found in demo session")
	}
	for id, tc := range s.ToolCalls {
		if tc.Expanded {
			t.Errorf("tool call %s should be collapsed after second Ctrl+O", id)
		}
	}
}

// TestHomeFlow 验证首页会话恢复。断言同样在 app 退出后执行。
func TestHomeFlow(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.View != model.ViewSession {
		t.Errorf("expected session view after enter, got %v", a.model.View)
	}
	if a.model.Active == nil {
		t.Error("active session should be set")
	}
}

func TestSettingsAndSlashFlow(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)
	for _, r := range "/settings" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if !a.model.Settings.ThinkingExpanded {
		t.Error("thinking setting should be enabled")
	}
	if a.model.Modal == model.ModalSettings {
		t.Errorf("settings modal should have closed")
	}
}

// TestSettingsPersistToDisk 验证 /settings 修改会落盘到 $XDG_CONFIG_HOME/alcoh/config.json，
// 而非仅停留在内存。修改 ThinkingExpanded 后应能读到 thinkingExpanded=true。
func TestSettingsPersistToDisk(t *testing.T) {
	dir := setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/settings" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 切换"展开思考内容"为开启
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if !a.model.Settings.ThinkingExpanded {
		t.Fatal("thinking setting should be toggled on")
	}
	path := filepath.Join(dir, "alcoh", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file not persisted: %v", err)
	}
	if !strings.Contains(string(data), `"thinkingExpanded": true`) {
		t.Errorf("config.json missing thinkingExpanded=true, got:\n%s", data)
	}
	// 落盘内容重新加载后应与内存一致，验证下次启动可恢复。
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !loaded.ThinkingExpanded {
		t.Error("reloaded config should have thinkingExpanded=true")
	}
}

// TestHomeSlashSettingsFlow 验证主页输入框的命令面板能正确处理 /settings 客户端命令：
// 输入 /settings 回车应打开本地设置弹窗，而不是创建会话或进入会话视图。
func TestHomeSlashSettingsFlow(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/settings" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.View != model.ViewHome {
		t.Errorf("expected home view after /settings on home, got %v", a.model.View)
	}
	if a.model.Active != nil {
		t.Error("active session should be nil; /settings must not create a session")
	}
	if a.model.Modal == model.ModalSettings {
		t.Errorf("settings modal should have closed, got %v", a.model.Modal)
	}
}

// TestClearDefaultCancelsRunningSession 验证 /clear 默认会取消运行中的会话：
// 会话运行中输入 /clear 应发送 Cancel 并返回会话列表。
func TestClearDefaultCancelsRunningSession(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	tb := &trackBackend{Backend: demo.New(true)}
	a := New(ft, tb)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	for _, r := range "hello" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 提交 prompt，会话进入 running
	time.Sleep(300 * time.Millisecond)
	for _, r := range "/clear" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(200 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.View != model.ViewHome {
		t.Errorf("expected home view after /clear, got %v", a.model.View)
	}
	if a.model.Active != nil {
		t.Error("active session should be nil after /clear")
	}
	if tb.cancelCount() == 0 {
		t.Error("expected /clear to cancel the running session")
	}
}

// TestClearOnKeepsRunningSession 验证 /clear on 不取消会话直接返回会话列表。
func TestClearOnKeepsRunningSession(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	tb := &trackBackend{Backend: demo.New(true)}
	a := New(ft, tb)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	for _, r := range "hello" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)
	for _, r := range "/clear on" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(200 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.View != model.ViewHome {
		t.Errorf("expected home view after /clear on, got %v", a.model.View)
	}
	if a.model.Active != nil {
		t.Error("active session should be nil after /clear on")
	}
	if n := tb.cancelCount(); n != 0 {
		t.Errorf("expected /clear on NOT to cancel, got %d cancel(s)", n)
	}
}

// TestQuestionMarkHelpOnEmptyInput 验证 "?" 仅在输入框为空时触发帮助：
// 主页空输入按 ? 打开帮助弹窗；帮助打开期间普通字符被拦截，不进入输入框。
func TestQuestionMarkHelpOnEmptyInput(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('?', input.ModNone)) // 空输入框：触发帮助
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('a', input.ModNone)) // 帮助弹窗拦截普通字符
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('?', input.ModNone)) // 关闭帮助
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.Modal == model.ModalHelp {
		t.Errorf("help modal should have closed before exit, got %v", a.model.Modal)
	}
	if got := a.model.Input.Text(); got != "" {
		t.Errorf("help modal should have swallowed 'a', input = %q", got)
	}
}

// TestQuestionMarkTypedWhenInputNonEmpty 验证输入框非空时 "?" 作为普通字符输入：
// 不触发帮助弹窗，字符进入输入框。
func TestQuestionMarkTypedWhenInputNonEmpty(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "abc" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('?', input.ModNone)) // 输入框非空：应作为字符输入
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.Modal == model.ModalHelp {
		t.Errorf("'?' with non-empty input should not open help, got %v", a.model.Modal)
	}
	if got := a.model.Input.Text(); got != "abc?" {
		t.Errorf("expected '?' typed into input, got %q", got)
	}
}

// TestMouseSelectThenCtrlC 验证：拖拽框选 → Ctrl+C 复制到剪贴板并清除选择。
func TestMouseSelectThenCtrlC(t *testing.T) {
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(50 * time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(1500 * time.Millisecond)
	ft.sendKey(input.RuneKey('a', input.ModNone))
	time.Sleep(400 * time.Millisecond)

	// 消息区左上角拖拽框选（坐标 1-based）。
	ft.sendMouse(input.MouseEvent{Button: input.MouseLeft, Action: input.MousePress, X: 1, Y: 1})
	ft.sendMouse(input.MouseEvent{Button: input.MouseLeft, Action: input.MouseMove, X: 50, Y: 3})
	ft.sendMouse(input.MouseEvent{Button: input.MouseLeft, Action: input.MouseRelease, X: 50, Y: 3})
	time.Sleep(80 * time.Millisecond)
	ft.sendKey(input.RuneKey('c', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if ft.copied == "" {
		t.Fatal("expected copied text after Ctrl+C, got empty")
	}
	if a.model.Selection != nil {
		t.Error("selection should be cleared after copy")
	}
}

// TestCopyBodyText 验证复制取渲染前的原始 markdown：行级插针从消息中间精确
// 截取、长行 wrap 续行去重、块级整块输出；选择只作用于正文区域、滚动正确映射。
func TestCopyBodyText(t *testing.T) {
	ft := newFakeTerm()
	a := New(ft, demo.New(true))

	// 行级：三条渲染行，第 3 条是第 2 条逻辑行的 wrap 续行。
	a.view.Body = []view.BodyBlock{{
		Src: []view.SrcLine{
			{Text: "# 标题", First: true},
			{Text: "正文 **加粗**", First: true},
			{Text: "正文 **加粗**", First: false},
		},
		Start: 0, End: 2,
	}}
	a.view.BodyRect = renderer.NewRect(0, 0, 80, 24)
	a.view.BodyScroll = 0

	// 从消息中间选中 wrap 续行（contentY=2）：仍输出该逻辑行一次。
	got := a.bodyText(&model.Selection{AnchorX: 0, AnchorY: 2, CurX: 0, CurY: 2})
	if got != "正文 **加粗**" {
		t.Fatalf("mid wrap row = %q", got)
	}
	// 选中 0..2：第 1 行 + 第 2 逻辑行一次（续行去重），原始 markdown 原样。
	got = a.bodyText(&model.Selection{AnchorX: 0, AnchorY: 0, CurX: 0, CurY: 2})
	if got != "# 标题\n正文 **加粗**" {
		t.Fatalf("line selection = %q", got)
	}
	// 选择越出正文区域会被裁剪。
	if got := a.bodyText(&model.Selection{AnchorX: 0, AnchorY: 0, CurX: 0, CurY: 100}); got != "# 标题\n正文 **加粗**" {
		t.Fatalf("clamped body = %q", got)
	}
	// 选择完全在正文外 → 空。
	if got := a.bodyText(&model.Selection{AnchorX: 0, AnchorY: 50, CurX: 0, CurY: 60}); got != "" {
		t.Fatalf("outside body = %q", got)
	}

	// 块级（工具/终端）：整块输出一次。
	a.view.Body = []view.BodyBlock{{Raw: "工具输出行1\n工具输出行2", Start: 0, End: 1}}
	a.view.BodyRect = renderer.NewRect(0, 0, 80, 24)
	got = a.bodyText(&model.Selection{AnchorX: 0, AnchorY: 0, CurX: 0, CurY: 1})
	if got != "工具输出行1\n工具输出行2" {
		t.Fatalf("block raw = %q", got)
	}

	// 滚动：正文滚动 10 行后，屏幕 y=20 命中 contentY=30 的块。
	a.view.Body = []view.BodyBlock{{Src: []view.SrcLine{{Text: "滚动后的内容", First: true}}, Start: 30, End: 30}}
	a.view.BodyScroll = 10
	got = a.bodyText(&model.Selection{AnchorX: 0, AnchorY: 20, CurX: 0, CurY: 20})
	if got != "滚动后的内容" {
		t.Fatalf("scrolled body = %q", got)
	}
}

// TestApplySelectionLine 验证行选择高亮：宽字符整字反显，不产生块状花屏。
func TestApplySelectionLine(t *testing.T) {
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	a.view.BodyRect = renderer.NewRect(0, 0, 80, 24)

	// 单行选中"世"字的续列（col9），应扩展到首列整字反显。
	a.back = renderer.NewBuffer(80, 24)
	a.back.PutText(2, 1, "hello 世界", renderer.DefaultStyle(), 80)
	a.model.Selection = &model.Selection{AnchorX: 9, AnchorY: 1, CurX: 9, CurY: 1}
	a.applySelection(a.back)
	if c := a.back.Cells[a.back.Index(8, 1)]; !c.Style.Reverse {
		t.Error("wide char first column should be reversed")
	}
	if c := a.back.Cells[a.back.Index(9, 1)]; !c.Style.Reverse {
		t.Error("wide char continuation column should be reversed")
	}
	if c := a.back.Cells[a.back.Index(7, 1)]; c.Style.Reverse {
		t.Error("cell before wide char should not be reversed")
	}

	// 行选择：首行从 anchor 到行尾，末行从行首到 cur。
	a.back = renderer.NewBuffer(80, 24)
	a.back.PutText(2, 1, "hello 世界", renderer.DefaultStyle(), 80)
	a.back.PutText(2, 2, "second line", renderer.DefaultStyle(), 80)
	a.model.Selection = &model.Selection{AnchorX: 4, AnchorY: 1, CurX: 2, CurY: 2}
	a.applySelection(a.back)
	if c := a.back.Cells[a.back.Index(11, 1)]; !c.Style.Reverse {
		t.Error("first row should extend to end of line")
	}
	if c := a.back.Cells[a.back.Index(2, 2)]; !c.Style.Reverse {
		t.Error("last row should start from beginning of line")
	}
	if c := a.back.Cells[a.back.Index(1, 1)]; c.Style.Reverse {
		t.Error("cell before anchor on first row should not be reversed")
	}
}

// TestEffortCommandFlow 验证 /effort 两条路径：
//   - 带参数：/effort high → session/set_config_option(thought_level=high)；
//   - 无参数：/effort → 打开滑条弹窗 → 右移 → Enter 确认。
//
// 与其它集成测试一致，模型断言全部放在 Run 退出后执行，避免测试 goroutine
// 与 UI 主循环并发读写 model。
func TestEffortCommandFlow(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	// 带参数路径。
	for _, r := range "/effort high" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	// 无参数路径：/effort → 弹窗（当前值 high 被选中）→ 右移（→xhigh）→ Enter 确认。
	for _, r := range "/effort" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(150 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyRight))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if !a.model.SupportsEffort() {
		t.Fatal("demo should advertise thought_level so /effort is enabled")
	}
	if a.model.Modal == model.ModalEffort {
		t.Error("effort modal should close after confirm")
	}
	// 带参数路径把值设为 high；弹窗路径从 high 右移到 xhigh 并确认 → 最终 xhigh。
	// 若弹窗未打开、右移未生效，最终值会停留在 high，从而暴露路径失败。
	if got := a.model.CurrentEffort(); got != "xhigh" {
		t.Errorf("effort after both paths = %q, want xhigh", got)
	}
}

// TestModelCommandFlow 验证 /model 两条路径：
//   - 带参数：/model demo-go-2 → session/set_config_option(model=demo-go-2)；
//   - 无参数：/model → 打开模型菜单 → 下移选中 demo-go-3 → Enter 确认。
//
// 与其它集成测试一致，模型断言全部放在 Run 退出后执行。
func TestModelCommandFlow(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	// 带参数路径。
	for _, r := range "/model demo-go-2" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	// 无参数路径：/model → 弹窗（当前 demo-go-2 被选中）→ 下移（→demo-go-3）→ Enter 确认。
	for _, r := range "/model" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(150 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if !a.model.SupportsModel() {
		t.Fatal("demo should advertise model config so /model is enabled")
	}
	if a.model.Modal == model.ModalModel {
		t.Error("model modal should close after confirm")
	}
	// 带参数路径把值设为 demo-go-2；弹窗路径从 demo-go-2 下移到 demo-go-3 并确认 → 最终 demo-go-3。
	if got := a.model.CurrentModel(); got != "demo-go-3" {
		t.Errorf("model after both paths = %q, want demo-go-3", got)
	}
}

// cwdRecordingBackend 包装 demo backend，记录 NewSession 收到的 cwd。
type cwdRecordingBackend struct {
	*demo.Backend
	newCWDs []string
}

func (b *cwdRecordingBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	b.newCWDs = append(b.newCWDs, cwd)
	return b.Backend.NewSession(ctx, cwd)
}

// TestCreateSessionUsesConfiguredWorkdir 验证主页进入会话使用的目录取自启动时
// 传入的绝对路径（预创建会话的 NewSession 即使用该目录），而不是在列出失败时
// 回退到 "."。
func TestCreateSessionUsesConfiguredWorkdir(t *testing.T) {
	ft := newFakeTerm()
	b := &cwdRecordingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	a.SetWorkdir("/work/example")
	done := runApp(t, a)

	time.Sleep(50 * time.Millisecond)
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(400 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if len(b.newCWDs) == 0 {
		t.Fatal("expected NewSession to be called")
	}
	if b.newCWDs[0] != "/work/example" {
		t.Errorf("NewSession cwd = %q, want /work/example", b.newCWDs[0])
	}
}

// alkaid0Backend 包装 demo backend，声明 alkaid0 扩展能力并记录 config/get 与
// config/set 调用，用于验证 /server 配置编辑器的加载与写回链路。
type alkaid0Backend struct {
	*demo.Backend
	gets int64
	sets int64
	mu   sync.Mutex
	// patches 记录每次 SetConfig 收到的部分更新 JSON，按调用顺序保存。
	patches []json.RawMessage
	// cfg 是 config/get 返回的当前配置（map 形式，惰性初始化为 demoConfigJSON）。
	// SetConfig 把 patch 递归合并进来，GetConfig 序列化返回，模拟服务端
	// 持久化后的读取——新增项后整配置重载能读到含新键的配置。
	cfg map[string]any
}

// demoConfigJSON 是 config/get 返回的固定配置。子页面导航布局
// （每层按字典序排序）须与 TestServerConfigEditor 的按键导航保持一致：
//
//	根页面: [Model, Server, Version]  →  Model 是行 0
//	Model 子页: [DefaultModelID, Models, ProviderKey]  →  DefaultModelID 是行 0
var demoConfigJSON = json.RawMessage(`{
  "Version": 1,
  "Model": {
    "DefaultModelID": 1,
    "ProviderKey": "sk-or-abc123",
    "Models": {
      "1": {"ModelName": "Kimi", "ModelID": "kimi-k2", "TokenLimit": 8192},
      "2": {"ModelName": "Deepseek", "ModelID": "deepseek-v3", "TokenLimit": 128000}
    }
  },
  "Server": {"host": "127.0.0.1", "port": 7433, "key": "alk-secret", "path": "/acp"}
}`)

func (b *alkaid0Backend) AgentInfo() acp.AgentInfo {
	return acp.AgentInfo{Name: "alkaid0", Version: "test"}
}

func (b *alkaid0Backend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
}

func (b *alkaid0Backend) GetConfig(ctx context.Context) (json.RawMessage, error) {
	atomic.AddInt64(&b.gets, 1)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cfg == nil {
		b.cfg = map[string]any{}
		_ = json.Unmarshal(demoConfigJSON, &b.cfg)
	}
	out, _ := json.Marshal(b.cfg)
	return out, nil
}

func (b *alkaid0Backend) SetConfig(ctx context.Context, patch json.RawMessage) error {
	atomic.AddInt64(&b.sets, 1)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.patches = append(b.patches, append(json.RawMessage(nil), patch...))
	if b.cfg == nil {
		b.cfg = map[string]any{}
		_ = json.Unmarshal(demoConfigJSON, &b.cfg)
	}
	var p map[string]any
	if err := json.Unmarshal(patch, &p); err == nil {
		mergeConfigMap(b.cfg, p)
	}
	return nil
}

// mergeConfigMap 把 patch 递归合并进 base（patch 的对象字段覆盖 base 中同名的
// 对象字段，标量整体覆盖），模拟服务端 json.Unmarshal 的部分更新语义。
// patch 值为 null 的键对应删除（真实服务端 config/set 在 unmarshal 后调用
// deleteNullMapKeys 真正删除 map 键；测试须模拟，否则整配置重载后被删项
// 以 null 值复活）。
func mergeConfigMap(base, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(base, k)
			continue
		}
		pv, pok := v.(map[string]any)
		bv, bok := base[k].(map[string]any)
		if pok && bok {
			mergeConfigMap(bv, pv)
		} else {
			base[k] = v
		}
	}
}

// TestServerCommandRequiresCapability 验证服务端未声明 alkaid0 扩展能力时，
// /server 命令被拒绝并给出错误提示，而不打开弹窗。
func TestServerCommandRequiresCapability(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	for _, r := range "/server" {
		a.model.Input.InsertRune(r)
	}
	if !a.tryLocalSlashCommand() {
		t.Fatalf("tryLocalSlashCommand should handle /server")
	}
	if a.model.Modal == model.ModalServer {
		t.Fatal("server modal must not open without alkaid0 capability")
	}
	if a.model.Error == "" {
		t.Fatal("expected an error hint when /server unavailable")
	}
}

// TestServerConfigEditor 验证 /server 打开配置编辑器：config/get 加载配置树，
// 导航到数字叶子（Version）编辑后经 config/set 写回部分更新 patch。
func TestServerConfigEditor(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开服务端配置编辑器

	// config/get 应被调用一次。
	waitAtomic(t, &b.gets, 1, "config/get calls")
	// 配置树加载完成（ServerCfg != nil）后才可导航。
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// 子页面导航（布局见 demoConfigJSON）：根页 Model 是行 0，Enter 进入其
	// 子页；DefaultModelID 是子页行 0，Enter 进入数字编辑（输入框预填原值 1），
	// Ctrl+U 清空后输入 7，Enter 提交 → patch {"Model":{"DefaultModelID":7}}。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入 Model 子页
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 编辑 DefaultModelID
	time.Sleep(30 * time.Millisecond)
	ft.sendKey(input.RuneKey('u', input.ModCtrl))
	ft.sendKey(input.RuneKey('7', input.ModNone))
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	// SetConfig 应被调用一次，patch 为 {"Model":{"DefaultModelID":7}}。
	waitAtomic(t, &b.sets, 1, "config/set calls")
	b.mu.Lock()
	patches := append([]json.RawMessage(nil), b.patches...)
	b.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("set patches = %d, want 1", len(patches))
	}
	var patch map[string]any
	if err := json.Unmarshal(patches[0], &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	modelObj, ok := patch["Model"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model = %v, want object", patch["Model"])
	}
	if v, ok := modelObj["DefaultModelID"].(float64); !ok || v != 7 {
		t.Errorf("patch Model.DefaultModelID = %v, want 7", modelObj["DefaultModelID"])
	}

	// Esc 关闭编辑器（退出后读取模型安全）。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if a.model.Modal == model.ModalServer {
		t.Fatal("server modal should close on Esc")
	}
}

// TestServerConfigEditorAddModel 验证通过「(新增)」行新增模型：Model.Models 集合页
// 末尾选中 (新增) 行 Enter → 自动分配数字键 3（现有最大键 2 的下一个），
// patch 含新键空对象。
func TestServerConfigEditorAddModel(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开服务端配置编辑器
	waitAtomic(t, &b.gets, 1, "config/get calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// 子页面导航：Model(行0) → Models(行1) → 集合页 [1, 2, (新增)]。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入 Model 子页
	ft.sendKey(input.SimpleKey(input.KeyDown))  // 选中 Models
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入 Models 集合页（选中键 1）
	ft.sendKey(input.SimpleKey(input.KeyDown))  // 键 2
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (新增) 行
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 新增模型

	// SetConfig 应被调用一次，patch 为 {"Model":{"Models":{"3":{}}}}。
	waitAtomic(t, &b.sets, 1, "config/set calls")
	b.mu.Lock()
	patches := append([]json.RawMessage(nil), b.patches...)
	b.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("set patches = %d, want 1", len(patches))
	}
	var patch map[string]any
	if err := json.Unmarshal(patches[0], &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	modelObj, ok := patch["Model"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model = %v, want object", patch["Model"])
	}
	modelsObj, ok := modelObj["Models"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model.Models = %v, want object", modelObj["Models"])
	}
	model3, ok := modelsObj["3"].(map[string]any)
	if !ok || model3["ModelName"] == nil {
		t.Errorf("patch Model.Models.3 = %v, want {ModelName:...} (key auto-assigned to 3)", modelsObj["3"])
	}

	// 新增写回成功后应触发整配置重载（config/get 第 2 次），重载完成后
	// 重定向到新模型子页（Current 为键 3，展示以服务端返回为准）。
	waitAtomic(t, &b.gets, 2, "config/get calls after add")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.ServerCfg && !s.ServerSaving && s.ServerCurKey == "3"
	})

	// Esc 关闭编辑器（退出后读取模型安全）。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// TestApplyConfigGetStaleSeq 验证过时序号的 config/get 结果被丢弃：新增写回后
// 触发的整配置重载（seq=2，含新键）应用后，打开时较早发出但晚回的首次 get
// （seq=1，旧配置）不得覆盖最新配置树，且重定向目标不被误清。
func TestApplyConfigGetStaleSeq(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	a.model.OpenServer()

	// 最新 seq=2：新增写回后触发的整配置重载，含新键 3。
	a.cfgGetSeq = 2
	a.serverCfgFocus = []string{"Model", "Models", "3"}
	newCfg := json.RawMessage(`{"Version":1,"Model":{"Models":{"1":{"ModelName":"Kimi"},"3":{"ModelName":"New"}}}}`)
	a.applyCommandResult(commandResult{kind: commandConfigGet, cfgSeq: 2, config: newCfg})
	if a.model.ServerCfg == nil {
		t.Fatal("latest get should rebuild config tree")
	}
	if cur := a.model.ServerCfg.Current(); cur == nil || cur.Key != "3" {
		t.Fatalf("after latest get, current = %v, want new model page (key 3)", cur)
	}
	if a.serverCfgFocus != nil {
		t.Fatal("focus should be consumed after latest get applied")
	}

	// 过时 seq=1 的 get（打开时较早发出、晚回的旧配置，无键 3）应被丢弃，
	// 不得覆盖最新配置树，也不得重新设置 ServerCfg。
	oldCfg := json.RawMessage(`{"Version":1,"Model":{"Models":{"1":{"ModelName":"Kimi"}}}}`)
	a.applyCommandResult(commandResult{kind: commandConfigGet, cfgSeq: 1, config: oldCfg})
	if cur := a.model.ServerCfg.Current(); cur == nil || cur.Key != "3" {
		t.Fatalf("stale get must not override: current = %v, want still key 3", cur)
	}
}

// TestServerConfigEditorDeleteModel 验证删除改为模型项子页末尾「(删除该项)」行：
// 进入 Model.Models.1 子页选中该行 Enter，删除当前模型并返回 Models 集合页
// （对象键置零写回）。
func TestServerConfigEditorDeleteModel(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开服务端配置编辑器
	waitAtomic(t, &b.gets, 1, "config/get calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// 子页面导航：Model(行0) → Models(行1) → 键 1 子页 [ModelID, ModelName,
	// TokenLimit, (删除该项)] → 删除行 Enter 删除模型 1。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入 Model 子页
	ft.sendKey(input.SimpleKey(input.KeyDown))  // 选中 Models
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入 Models 集合页（选中键 1）
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入键 1 的模型子页
	ft.sendKey(input.SimpleKey(input.KeyDown))  // ModelName（index1）
	ft.sendKey(input.SimpleKey(input.KeyDown))  // TokenLimit（index2）
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (复制)
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (重命名键)
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (删除该项)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 删除模型 1

	// SetConfig 应被调用一次，patch 为 {"Model":{"Models":{"1":null}}}。
	waitAtomic(t, &b.sets, 1, "config/set calls")
	b.mu.Lock()
	patches := append([]json.RawMessage(nil), b.patches...)
	b.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("set patches = %d, want 1", len(patches))
	}
	var patch map[string]any
	if err := json.Unmarshal(patches[0], &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	modelObj, ok := patch["Model"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model = %v, want object", patch["Model"])
	}
	modelsObj, ok := modelObj["Models"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model.Models = %v, want object", modelObj["Models"])
	}
	if v, exists := modelsObj["1"]; !exists || v != nil {
		t.Errorf("patch Model.Models.1 = %v, want null (key set zero)", modelsObj["1"])
	}

	// 删除写回成功后全量重载：重载完成（Saving 解除）后返回 Models 集合页，
	// 选中相邻项（键 2）。运行期间经锁内快照轮询，不在运行期直接读模型。
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.ServerCfg && !s.ServerSaving && s.ServerCurKey == "Models" && s.ServerSelKey == "2"
	})

	// Esc 关闭编辑器（退出后读取模型安全）。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// waitAtomic 轮询直到 atomic 计数器达到目标值。
func waitAtomic(t *testing.T, p *int64, want int64, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(p) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s = %d, want %d", what, atomic.LoadInt64(p), want)
}

// waitSnapshot 轮询模型快照直到条件满足。
func waitSnapshot(t *testing.T, a *App, cond func(modelSnapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond(a.snapshot()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("snapshot condition not met within 5s")
}

// TestHomeDeleteSession 验证首页按 d 删除选中会话：列表项移除、后端列表
// 反映删除。demo backend 初始列表含 demo-0；聚焦列表后按 d 删除它。
func TestHomeDeleteSession(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	// 聚焦会话列表（输入框为空时按左键），HomeSelected 初始化为 0。
	ft.sendKey(input.SimpleKey(input.KeyLeft))
	time.Sleep(100 * time.Millisecond)
	// 按 d 删除选中的 demo-0。
	ft.sendKey(input.RuneKey('d', input.ModNone))
	time.Sleep(300 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	// 本地列表应移除 demo-0。
	for _, s := range a.model.Sessions {
		if s.SessionID == "demo-0" {
			t.Error("demo-0 should be removed from local session list")
		}
	}
	// 后端列表也应反映删除。
	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, s := range sessions {
		if s.SessionID == "demo-0" {
			t.Error("demo-0 should be removed from backend session list")
		}
	}
}

// TestHomeDeleteSessionUnfocused 验证未聚焦列表时，输入框为空且有选中会话
// （先聚焦选中再返回输入框，HomeSelected 保留），按 d 同样删除。
func TestHomeDeleteSessionUnfocused(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	// 聚焦列表（HomeSelected 初始化为 0），再右键返回输入框（取消聚焦但保留选中）。
	ft.sendKey(input.SimpleKey(input.KeyLeft))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyRight))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('d', input.ModNone))
	time.Sleep(300 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	for _, s := range a.model.Sessions {
		if s.SessionID == "demo-0" {
			t.Error("demo-0 should be removed from local session list")
		}
	}
}

// TestHomeDeleteInputNotEmpty 验证输入框非空时按 d 不删除会话（d 作为普通字符输入）。
func TestHomeDeleteInputNotEmpty(t *testing.T) {
	ft := newFakeTerm()
	b := demo.New(true)
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyLeft)) // 聚焦，HomeSelected=0
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyRight)) // 返回输入框
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('h', input.ModNone)) // 输入框有内容
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.RuneKey('d', input.ModNone)) // d 应为普通字符
	time.Sleep(300 * time.Millisecond)

	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if got := a.model.Input.Text(); got != "hd" {
		t.Errorf("input text = %q, want \"hd\" (d typed as character)", got)
	}
	if len(a.model.Sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (delete must not trigger)", len(a.model.Sessions))
	}
}

// TestSettingsLanguageSwitchApplies 验证 /settings 中把语言切到 en 后，
// 全局 i18n 语言立即切换并写入本地配置；重启加载配置后仍为 en。
func TestSettingsLanguageSwitchApplies(t *testing.T) {
	i18n.SetLang(i18n.Zh)
	defer i18n.SetLang(i18n.Zh)
	dir := setConfigDir(t)
	ft := newFakeTerm()
	a := New(ft, demo.New(true))
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/settings" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(100 * time.Millisecond)
	// 第 3 行 = 语言：三次下移后按右切到 en。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyRight))
	time.Sleep(100 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)

	if i18n.Current() != i18n.En {
		t.Errorf("current language = %q, want en", i18n.Current())
	}
	// 落盘配置应包含 language: en。
	data, err := os.ReadFile(filepath.Join(dir, "alcoh", "config.json"))
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	if !strings.Contains(string(data), `"language": "en"`) {
		t.Errorf("config.json missing language=en, got:\n%s", data)
	}
	// 重新加载后配置保持 en。
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if loaded.Language != "en" {
		t.Errorf("reloaded language = %q, want en", loaded.Language)
	}
}
