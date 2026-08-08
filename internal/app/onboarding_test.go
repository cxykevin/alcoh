package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// alkaid0Caps 声明 alkaid0 扩展能力 + session.delete（与真实 alkaid0 一致），
// 用于触发新手引导。
var alkaid0Caps = acp.AgentCapabilities{Raw: json.RawMessage(`{"session":{"delete":{}},"alk.cxykevin.top/alkaid0/v0.4":{}}`)}

// onboardingBackend 包装 demo backend，声明 alkaid0 能力、可控 config/get，
// 并记录 config/set patch 与会话 SetConfigOption 调用。
type onboardingBackend struct {
	*demo.Backend
	caps acp.AgentCapabilities
	cfg  json.RawMessage

	mu              sync.Mutex
	created         int
	patches         []json.RawMessage
	setConfigOptions []setConfigOptionCall
}

type setConfigOptionCall struct {
	ID, Type, Value string
}

func (b *onboardingBackend) AgentCapabilities() acp.AgentCapabilities { return b.caps }

func (b *onboardingBackend) GetConfig(ctx context.Context) (json.RawMessage, error) {
	return b.cfg, nil
}

func (b *onboardingBackend) SetConfig(ctx context.Context, patch json.RawMessage) error {
	b.mu.Lock()
	b.patches = append(b.patches, append(json.RawMessage(nil), patch...))
	b.mu.Unlock()
	return nil
}

func (b *onboardingBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.created++
	b.mu.Unlock()
	return &onboardingSession{Session: s, b: b}, nil
}

func (b *onboardingBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &onboardingSession{Session: s, b: b}, nil
}

func (b *onboardingBackend) createdCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.created
}

func (b *onboardingBackend) lastPatch() json.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.patches) == 0 {
		return nil
	}
	return b.patches[len(b.patches)-1]
}

func (b *onboardingBackend) hasSetConfigOption(id, value string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.setConfigOptions {
		if c.ID == id && c.Value == value {
			return true
		}
	}
	return false
}

// onboardingSession 包装 demo session，记录 SetConfigOption 调用。
type onboardingSession struct {
	acp.Session
	b *onboardingBackend
}

func (s *onboardingSession) SetConfigOption(ctx context.Context, configID, configType, value string) error {
	s.b.mu.Lock()
	s.b.setConfigOptions = append(s.b.setConfigOptions, setConfigOptionCall{ID: configID, Type: configType, Value: value})
	s.b.mu.Unlock()
	return s.Session.SetConfigOption(ctx, configID, configType, value)
}

func onboardingActive(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.Modal == model.ModalOnboarding
}

func homeReady(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.View == model.ViewHome && a.model.Modal == model.NoModal
}

func onboardingAtResult(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.Onboarding != nil && a.model.Onboarding.Step == model.OnboardStepResult
}

// quitApp 通过 Ctrl+q → y 退出应用。
func quitApp(ft *fakeTerm) {
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
}

// TestOnboardingTriggered 验证触发条件：无参数启动（enabled）+ 服务端声明 alkaid0
// 能力 + config 无模型 → 启动即进入全屏新手引导，且引导期间不创建主页预创建会话。
func TestOnboardingTriggered(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "onboarding active", func() bool { return onboardingActive(a) })
	// 引导期间不创建主页预创建会话（等引导结束才创建）。
	if got := b.createdCount(); got != 0 {
		t.Errorf("sessions created during onboarding = %d, want 0", got)
	}

	quitApp(ft)
	waitRun(t, done)
	if a.model.Onboarding == nil {
		t.Error("onboarding should remain set after quit from onboarding")
	}
}

// TestOnboardingNotTriggeredWhenDisabled 验证未启用引导（指定了 backend 参数）时
// 即使服务端无模型也不进入引导，正常进主页并创建预创建会话。
func TestOnboardingNotTriggeredWhenDisabled(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b) // SetOnboardingEnabled 未调用，默认 false
	done := runApp(t, a)

	waitCondition(t, "home ready", func() bool { return homeReady(a) })
	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })

	quitApp(ft)
	waitRun(t, done)
	if a.model.Onboarding != nil {
		t.Error("onboarding should not be active when disabled")
	}
}

// TestOnboardingNotTriggeredWhenNotAlkaid0 验证服务端未声明 alkaid0 能力时不进入
// 引导（即使是默认无参启动）。
func TestOnboardingNotTriggeredWhenNotAlkaid0(t *testing.T) {
	ft := newFakeTerm()
	// caps 仅声明 session.delete（demo 默认，无 alkaid0 能力）。
	b := &onboardingBackend{
		Backend: demo.New(true),
		caps:    acp.AgentCapabilities{Raw: json.RawMessage(`{"session":{"delete":{}}}`)},
		cfg:     json.RawMessage(`{"Model":{}}`),
	}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "home ready", func() bool { return homeReady(a) })
	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })

	quitApp(ft)
	waitRun(t, done)
	if a.model.Onboarding != nil {
		t.Error("onboarding should not be active without alkaid0 capability")
	}
}

// TestOnboardingNotTriggeredWhenHasModels 验证服务端已配置模型时不进入引导。
func TestOnboardingNotTriggeredWhenHasModels(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{
		Backend: demo.New(true),
		caps:    alkaid0Caps,
		cfg:     json.RawMessage(`{"Model":{"Models":{"0":{"ModelName":"m1","ModelID":"m1"}}}}`),
	}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "home ready", func() bool { return homeReady(a) })
	waitCondition(t, "pre-session created", func() bool { return b.createdCount() == 1 })

	quitApp(ft)
	waitRun(t, done)
	if a.model.Onboarding != nil {
		t.Error("onboarding should not be active when models already configured")
	}
}

// TestOnboardingFullFlow 完整流程：欢迎 → 选服务商(Deepseek 预填 URL) → 填六字段
// 表单并提交 → 结果页选「下一步」→ 选 effort(high) → 教学 → 完成进主页。验证
// config/set patch 结构、effort 写入本地 config、第一个会话应用 effort。
func TestOnboardingFullFlow(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "onboarding active", func() bool { return onboardingActive(a) })

	// 欢迎页 → Enter → 服务商选择。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(50 * time.Millisecond)
	// 服务商默认 Deepseek → Enter → 表单（ProviderURL 已预填）。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(50 * time.Millisecond)

	// 填其余五个字段：Tab 切换字段后输入字符。
	fill := func(text string) {
		ft.sendKey(input.SimpleKey(input.KeyTab))
		for _, r := range text {
			ft.sendKey(input.RuneKey(r, input.ModNone))
		}
	}
	fill("sk-test")       // ProviderKey
	fill("deepseek-chat") // ModelName
	fill("deepseek-chat") // ModelID
	fill("8192")          // TokenLimit
	fill("128000")        // CompressSize
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 提交

	// 等待 config/set 写回并进入结果页。
	waitCondition(t, "config set received", func() bool { return b.lastPatch() != nil })
	waitCondition(t, "onboarding at result", func() bool { return onboardingAtResult(a) })

	// 校验写回 patch：Model.Models.0 六字段 + DefaultModelID=0（数值类型）。
	var got map[string]any
	if err := json.Unmarshal(b.lastPatch(), &got); err != nil {
		t.Fatalf("bad patch: %v", err)
	}
	mo := got["Model"].(map[string]any)
	if mo["DefaultModelID"] != float64(0) {
		t.Errorf("DefaultModelID = %v, want 0 (numeric)", mo["DefaultModelID"])
	}
	m0 := mo["Models"].(map[string]any)["0"].(map[string]any)
	if m0["ProviderURL"] != "https://api.deepseek.com" {
		t.Errorf("ProviderURL = %v, want deepseek", m0["ProviderURL"])
	}
	if m0["ProviderKey"] != "sk-test" {
		t.Errorf("ProviderKey = %v, want sk-test", m0["ProviderKey"])
	}
	if m0["ModelName"] != "deepseek-chat" || m0["ModelID"] != "deepseek-chat" {
		t.Errorf("identity fields = %v", m0)
	}
	if m0["TokenLimit"].(float64) != 8192 || m0["CompressSize"].(float64) != 128000 {
		t.Errorf("numeric fields = %v", m0)
	}

	// 结果页默认选中「打开 /server」；↓ 选「下一步」→ Enter → effort。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(50 * time.Millisecond)
	// effort 候选 low/medium/high/xhigh/max；↓↓ 选 high → Enter → 教学。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(50 * time.Millisecond)
	// 教学 → Enter 完成 → 主页。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "home after onboarding", func() bool { return homeReady(a) })

	if got := a.model.Settings.OnboardingEffort; got != "high" {
		t.Errorf("OnboardingEffort = %q, want high", got)
	}
	// 引导结束后主页预创建会话（供 /effort 与 /model 与 prompt 复用）。
	waitCondition(t, "pre-session created after onboarding", func() bool { return b.createdCount() == 1 })

	// 主页输入 prompt 复用预创建会话：引导选的 effort 应用到第一个会话并清除本地字段。
	for _, r := range "你好" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "effort applied to first session", func() bool {
		return b.hasSetConfigOption("thought_level", "high")
	})
	if a.model.Settings.OnboardingEffort != "" {
		t.Errorf("OnboardingEffort should be cleared after applying, got %q", a.model.Settings.OnboardingEffort)
	}

	quitApp(ft)
	waitRun(t, done)
}

// TestOnboardingCustomProvider 验证选择「自定义」服务商时不预填 URL，六个字段
// 全部由用户填写，提交后 patch 使用用户填写的提供方 URL。
func TestOnboardingCustomProvider(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{"Models":{}}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "onboarding active", func() bool { return onboardingActive(a) })
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 欢迎 → 服务商
	time.Sleep(50 * time.Millisecond)
	// 从 Deepseek(0) 连按三次 ↓ 到「自定义」(末尾)。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // → 表单
	time.Sleep(50 * time.Millisecond)

	// ProviderURL 未预填：直接输入自定义 URL。
	for _, r := range "https://my.example.com/v1" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	fill := func(text string) {
		ft.sendKey(input.SimpleKey(input.KeyTab))
		for _, r := range text {
			ft.sendKey(input.RuneKey(r, input.ModNone))
		}
	}
	fill("sk-custom")
	fill("my-model")
	fill("my-model-id")
	fill("4096")
	fill("64000")
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 提交

	waitCondition(t, "config set received", func() bool { return b.lastPatch() != nil })
	var got map[string]any
	if err := json.Unmarshal(b.lastPatch(), &got); err != nil {
		t.Fatalf("bad patch: %v", err)
	}
	m0 := got["Model"].(map[string]any)["Models"].(map[string]any)["0"].(map[string]any)
	if m0["ProviderURL"] != "https://my.example.com/v1" {
		t.Errorf("ProviderURL = %v, want custom URL", m0["ProviderURL"])
	}
	if m0["ProviderKey"] != "sk-custom" || m0["ModelID"] != "my-model-id" {
		t.Errorf("custom model fields = %v", m0)
	}

	quitApp(ft)
	waitRun(t, done)
}

// TestOnboardingServerEditorRoundTrip 验证从引导结果页打开 /server 编辑器定位到
// Config/Model/Models，关闭后回到引导结果页（而非主页）。
func TestOnboardingServerEditorRoundTrip(t *testing.T) {
	ft := newFakeTerm()
	// 保留空但存在的 Models 键，使 /server 打开后能 Focus 定位到 Config/Model/Models。
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{"Models":{}}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "onboarding active", func() bool { return onboardingActive(a) })
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 欢迎 → 服务商
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // Deepseek → 表单
	time.Sleep(50 * time.Millisecond)
	// 填完整表单（复用上面逻辑的缩写：六个字段）。
	fill := func(text string) {
		ft.sendKey(input.SimpleKey(input.KeyTab))
		for _, r := range text {
			ft.sendKey(input.RuneKey(r, input.ModNone))
		}
	}
	fill("sk-test")
	fill("deepseek-chat")
	fill("deepseek-chat")
	fill("8192")
	fill("128000")
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "onboarding at result", func() bool { return onboardingAtResult(a) })

	// 结果页默认选中「打开 /server 详细配置」→ Enter。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	// 等待 /server 编辑器打开且定位到 Model/Models（加载完成后 Focus）。
	waitCondition(t, "server editor open", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		ed := a.model.ServerCfg
		return a.model.Modal == model.ModalServer && ed != nil && ed.Current() != nil && ed.Current().Key == "Models"
	})
	// Esc 关闭编辑器 → 应回到引导结果页。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitCondition(t, "back to onboarding result", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Modal == model.ModalOnboarding &&
			a.model.Onboarding != nil && a.model.Onboarding.Step == model.OnboardStepResult
	})

	quitApp(ft)
	waitRun(t, done)
}
