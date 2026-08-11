package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	mu               sync.Mutex
	created          int
	patches          []json.RawMessage
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

// connectActive 报告 /connect 向导是否已打开（引导触发时 FromOnboarding=true）。
func connectActive(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.Modal == model.ModalConnect && a.model.Connect != nil && a.model.Connect.FromOnboarding
}

func homeReady(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.View == model.ViewHome && a.model.Modal == model.NoModal
}

func onboardingAtEffort(a *App) bool {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.model.Onboarding != nil && a.model.Onboarding.Step == model.OnboardStepEffort
}

// quitApp 通过 Ctrl+q → y 退出应用。
func quitApp(ft *fakeTerm) {
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
}

// connectInWizard 填写向导的表单（自定义服务商 + httptest 提供的 base_url）并
// 拉取模型列表，返回拉取完成。配合 full flow 使用。
func connectInWizard(t *testing.T, ft *fakeTerm, srvURL string) {
	t.Helper()
	// 选择「自定义」模板（最后一项）。
	templates := model.ConnectTemplates()
	for i := 0; i < len(templates)-1; i++ {
		ft.sendKey(input.SimpleKey(input.KeyDown))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(50 * time.Millisecond)
	// Tab 切到 base_url 输入服务商地址；Tab 切到 key 输入密钥；回车拉取。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	for _, r := range srvURL {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyTab))
	for _, r := range "sk-test-123" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
}

// TestOnboardingTriggered 验证触发条件：无参数启动（enabled）+ 服务端声明 alkaid0
// 能力 + config 无模型 → 启动即进入 /connect 向导（新手引导与其同义），且引导
// 期间不创建主页预创建会话。
func TestOnboardingTriggered(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "connect wizard active", func() bool { return connectActive(a) })
	// 引导期间不创建主页预创建会话（等引导结束才创建）。
	if got := b.createdCount(); got != 0 {
		t.Errorf("sessions created during onboarding = %d, want 0", got)
	}

	quitApp(ft)
	waitRun(t, done)
	if a.model.Connect == nil || !a.model.Connect.FromOnboarding {
		t.Error("connect wizard should remain set after quit from onboarding")
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
	if a.model.Connect != nil || a.model.Onboarding != nil {
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
	if a.model.Connect != nil || a.model.Onboarding != nil {
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
	if a.model.Connect != nil || a.model.Onboarding != nil {
		t.Error("onboarding should not be active when models already configured")
	}
}

// TestOnboardingFullFlow 验证引导与 /connect 向导同义的完整流程：触发 → 向导
// 选自定义服务商 → 填 base_url/key → 拉取模型 → 选择模型 → 写入服务端配置
//（键 0 + 设为默认）→ 完成步骤 Enter → 选 effort(high) → 教学 → 完成进主页。
// 最后验证 effort 写入本地 config、第一个会话应用 effort。
func TestOnboardingFullFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"deepseek-chat","context_length":65536},
			{"id":"deepseek-reasoner"}
		]}`))
	}))
	defer srv.Close()

	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "connect wizard active", func() bool { return connectActive(a) })

	// 向导内填表单并拉取模型列表。
	connectInWizard(t, ft, srv.URL)
	waitCondition(t, "models fetched", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepSelect && len(cs.Models) == 2
	})

	// 选中第一个模型 → 确认写入服务端配置。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "config set received", func() bool { return b.lastPatch() != nil })

	// 校验写回 patch：Model.Models.0（config 无模型 → 键 0）+ DefaultModelID=0。
	var got map[string]any
	if err := json.Unmarshal(b.lastPatch(), &got); err != nil {
		t.Fatalf("bad patch: %v", err)
	}
	mo := got["Model"].(map[string]any)
	if mo["DefaultModelID"] != float64(0) {
		t.Errorf("DefaultModelID = %v, want 0 (numeric)", mo["DefaultModelID"])
	}
	m0 := mo["Models"].(map[string]any)["0"].(map[string]any)
	if m0["ProviderURL"] != srv.URL {
		t.Errorf("ProviderURL = %v, want %q", m0["ProviderURL"], srv.URL)
	}
	if m0["ProviderKey"] != "sk-test-123" {
		t.Errorf("ProviderKey = %v, want sk-test-123", m0["ProviderKey"])
	}
	if m0["ModelID"] != "deepseek-chat" {
		t.Errorf("ModelID = %v, want deepseek-chat", m0["ModelID"])
	}

	// 完成步骤 Enter → 引导剩余步骤（选推理强度）。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "onboarding at effort", func() bool { return onboardingAtEffort(a) })

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

// TestOnboardingSkipAtProvider 验证向导服务商步骤按 Esc 跳过整个引导：直接进入
// 主页并创建预创建会话。
func TestOnboardingSkipAtProvider(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b)
	a.SetOnboardingEnabled(true)
	done := runApp(t, a)

	waitCondition(t, "connect wizard active", func() bool { return connectActive(a) })
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitCondition(t, "home after skip", func() bool { return homeReady(a) })
	waitCondition(t, "pre-session created after skip", func() bool { return b.createdCount() == 1 })

	quitApp(ft)
	waitRun(t, done)
	if a.model.Connect != nil || a.model.Onboarding != nil {
		t.Error("onboarding state should be cleared after skip")
	}
}
