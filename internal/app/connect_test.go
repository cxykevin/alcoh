package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/provider"
)

// TestConnectCommandFlow 验证 /connect 完整流程：打开向导 → 选择服务商模板
// → 填 base_url/key → 拉取模型列表 → 选择模型 → config/set 写入服务端配置
// （模型键为下一个数字、设为默认）。alcohol 端到端使用 httptest 模拟服务商
// /models 接口。
func TestConnectCommandFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"deepseek-chat","context_length":65536},
			{"id":"deepseek-reasoner"}
		]}`))
	}))
	defer srv.Close()

	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	// /connect 打开向导。
	for _, r := range "/connect" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(150 * time.Millisecond)
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.Modal == model.ModalConnect
	})

	// 选择「自定义」模板（最后一项），自己填 base_url。
	templates := model.ConnectTemplates()
	for i := 0; i < len(templates)-1; i++ {
		ft.sendKey(input.SimpleKey(input.KeyDown))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(100 * time.Millisecond)

	// 表单：Tab 切到 base_url，输入服务商地址；Tab 切到 key，输入密钥；回车拉取。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	for _, r := range srv.URL {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyTab))
	for _, r := range "sk-test-123" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))

	// 拉取成功进入模型选择步骤。
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepSelect && len(cs.Models) == 2
	})

	// 选中第一个模型并确认写入。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitAtomic(t, &b.sets, 1, "config/set calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepDone
	})

	// 写入的 patch：demoConfigJSON 已有 Models 1/2 → 新键 3，设为默认。
	b.mu.Lock()
	patches := append([]json.RawMessage(nil), b.patches...)
	b.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("patches = %d, want 1", len(patches))
	}
	var got struct {
		Model struct {
			Models map[string]struct {
				ProviderURL  string `json:"ProviderURL"`
				ProviderKey  string `json:"ProviderKey"`
				ModelID      string `json:"ModelID"`
				TokenLimit   int    `json:"TokenLimit"`
				CompressSize int    `json:"CompressSize"`
			} `json:"Models"`
			DefaultModelID int `json:"DefaultModelID"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(patches[0], &got); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	m3, ok := got.Model.Models["3"]
	if !ok {
		t.Fatalf("patch Models keys = %v, want 3 (next after 1/2)", mapKeys(got.Model.Models))
	}
	if m3.ModelID != "deepseek-chat" || m3.ProviderURL != srv.URL || m3.ProviderKey != "sk-test-123" {
		t.Errorf("model3 = %+v", m3)
	}
	if m3.TokenLimit != 65536 || m3.CompressSize != 52428 {
		t.Errorf("TokenLimit/CompressSize = %d/%d, want 65536/52428 (80%%)", m3.TokenLimit, m3.CompressSize)
	}
	if got.Model.DefaultModelID != 3 {
		t.Errorf("DefaultModelID = %d, want 3", got.Model.DefaultModelID)
	}

	// 关闭向导并退出。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

func mapKeys[M ~map[string]T, T any](m M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestConnectManualCompressFlow 验证 /connect 手动设置压缩阈值路径：服务商
// 未公布模型上下文长度 → 选择模型后进入手动输入步骤 → 输入压缩阈值 → 写回
// patch（TokenLimit 默认 128000，CompressSize 为用户输入）。
func TestConnectManualCompressFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-no-ctx"}]}`))
	}))
	defer srv.Close()

	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{"Models":{"0":{"ModelName":"old","ModelID":"old"}}}}`)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/connect" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(150 * time.Millisecond)
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.Modal == model.ModalConnect })

	connectInWizard(t, ft, srv.URL)
	waitCondition(t, "models fetched without ctx", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepSelect && len(cs.Models) == 1 && cs.Models[0].TokenLimit == 0
	})

	// 选择模型 → 进入手动设置压缩阈值步骤。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "manual step", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepManual
	})

	// 手动步骤：默认聚焦上下文长度字段，输入 512000（压缩阈值联动预填 256000）；
	// Tab 切到压缩阈值字段改为 200000 后提交。
	for _, r := range "512000" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyTab))
	for _, r := range "200000" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "config set received", func() bool { return b.lastPatch() != nil })

	// 校验 patch：新键 1（已有键 0），TokenLimit 与 CompressSize 均为手动输入。
	var got struct {
		Model struct {
			Models map[string]struct {
				TokenLimit   int `json:"TokenLimit"`
				CompressSize int `json:"CompressSize"`
			} `json:"Models"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(b.lastPatch(), &got); err != nil {
		t.Fatalf("bad patch: %v", err)
	}
	m1, ok := got.Model.Models["1"]
	if !ok {
		t.Fatalf("patch Models keys = %v, want 1", mapKeys(got.Model.Models))
	}
	if m1.TokenLimit != 512000 || m1.CompressSize != 200000 {
		t.Errorf("TokenLimit/CompressSize = %d/%d, want 512000/200000", m1.TokenLimit, m1.CompressSize)
	}

	// 完成步骤 → 关闭向导。
	waitCondition(t, "done step", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepDone
	})
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "wizard closed", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Modal == model.NoModal && a.model.Connect == nil
	})

	quitApp(ft)
	waitRun(t, done)
}

// TestConnectDeepseekV4Flash 验证 deepseek-v4-flash 特例：上下文已知 1M（TokenLimit
// 1000000）、压缩阈值固定 140000，即使服务商未公布上下文长度也全自动写入，
// 不进入手动输入步骤。
func TestConnectDeepseekV4Flash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v4-flash"}]}`))
	}))
	defer srv.Close()

	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{}}`)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/connect" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(150 * time.Millisecond)

	connectInWizard(t, ft, srv.URL)
	waitCondition(t, "models fetched", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		defer a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepSelect && len(cs.Models) == 1
	})

	// 选择模型：应跳过手动步骤直接写入。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "config set received", func() bool { return b.lastPatch() != nil })

	var got struct {
		Model struct {
			Models map[string]struct {
				TokenLimit   int `json:"TokenLimit"`
				CompressSize int `json:"CompressSize"`
			} `json:"Models"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(b.lastPatch(), &got); err != nil {
		t.Fatalf("bad patch: %v", err)
	}
	m0, ok := got.Model.Models["0"]
	if !ok {
		t.Fatalf("patch Models keys = %v, want 0", mapKeys(got.Model.Models))
	}
	if m0.TokenLimit != 1000000 || m0.CompressSize != 140000 {
		t.Errorf("TokenLimit/CompressSize = %d/%d, want 1000000/140000", m0.TokenLimit, m0.CompressSize)
	}
	// 应直接进入完成步骤（未走手动输入）。
	a.modelMu.RLock()
	step := model.ConnectStepSelect
	if cs := a.model.Connect; cs != nil {
		step = cs.Step
	}
	a.modelMu.RUnlock()
	if step != model.ConnectStepDone {
		t.Errorf("step = %v, want done (no manual step for deepseek-v4-flash)", step)
	}

	quitApp(ft)
	waitRun(t, done)
}

// TestEnsureCompressForModel 验证切换模型后的压缩阈值规则：deepseek-v4-flash
// 触发静默写回其 CompressSize=140000；其他模型不触发。
func TestEnsureCompressForModel(t *testing.T) {
	ft := newFakeTerm()
	b := &onboardingBackend{Backend: demo.New(true), caps: alkaid0Caps, cfg: json.RawMessage(`{"Model":{"Models":{
		"1":{"ModelName":"other","ModelID":"gpt-4o"},
		"2":{"ModelName":"flash","ModelID":"deepseek-v4-flash","CompressSize":99999}
	}}}`)}
	a := New(ft, b)
	done := runApp(t, a)
	time.Sleep(100 * time.Millisecond)

	// 非 flash 模型不触发写回。
	a.ensureCompressForModel("gpt-4o")
	time.Sleep(200 * time.Millisecond)
	if b.lastPatch() != nil {
		t.Fatal("non-flash model must not trigger compress write-back")
	}

	// flash 模型静默写回 Models.2 的 CompressSize=140000。
	a.ensureCompressForModel("deepseek-v4-flash")
	waitCondition(t, "flash compress written", func() bool { return b.lastPatch() != nil })
	var got struct {
		Model struct {
			Models map[string]struct {
				CompressSize int `json:"CompressSize"`
			} `json:"Models"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(b.lastPatch(), &got); err != nil {
		t.Fatalf("bad patch: %v", err)
	}
	m2, ok := got.Model.Models["2"]
	if !ok {
		t.Fatalf("patch Models keys = %v, want 2", mapKeys(got.Model.Models))
	}
	if m2.CompressSize != 140000 {
		t.Errorf("CompressSize = %d, want 140000", m2.CompressSize)
	}

	quitApp(ft)
	waitRun(t, done)
}

// TestConnectCompressForLimit 验证压缩阈值规则：
// deepseek-v4-flash 固定 140000；含 gemini 固定 80000（优先于 1M 规则）；
// 上下文 >=1M 且不含 claude 取 200000；1M 的 claude 走 80%；其余取 80%。
func TestConnectCompressForLimit(t *testing.T) {
	cases := []struct {
		name       string
		p          provider.Model
		tokenLimit int
		want       int
	}{
		{"deepseek fixed", provider.Model{ID: "deepseek-v4-flash"}, 2000000, 140000},
		{"gemini low ctx", provider.Model{ID: "gemini-2.5-flash"}, 500000, 80000},
		{"gemini 1M priority", provider.Model{ID: "gemini-2.5-pro", Name: "Gemini Pro"}, 1000000, 80000},
		{"1M no claude", provider.Model{ID: "qwen-max"}, 1000000, 200000},
		{"1M claude excluded", provider.Model{ID: "claude-sonnet-4"}, 1000000, 800000},
		{"claude in name", provider.Model{ID: "anthropic-model", Name: "Claude Sonnet"}, 1000000, 800000},
		{"under 1M", provider.Model{ID: "gpt-4o"}, 65536, 52428},
		{"edge exactly 1M", provider.Model{ID: "kimi"}, 1000000, 200000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := connectCompressForLimit(c.p, c.tokenLimit); got != c.want {
				t.Errorf("connectCompressForLimit(%+v, %d) = %d, want %d", c.p, c.tokenLimit, got, c.want)
			}
		})
	}
}
