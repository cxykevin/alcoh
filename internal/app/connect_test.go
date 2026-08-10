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
)

// TestConnectCommandFlow 验证 /connect 完整流程：打开向导 → 选择服务商模板
// → 填 base_url/key → 拉取模型列表 → 选择模型 → config/set 写入服务端配置
//（模型键为下一个数字、设为默认）。alcohol 端到端使用 httptest 模拟服务商
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
		a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepSelect && len(cs.Models) == 2
	})

	// 选中第一个模型并确认写入。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitAtomic(t, &b.sets, 1, "config/set calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		a.modelMu.RUnlock()
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
	if m3.TokenLimit != 65536 || m3.CompressSize != 32768 {
		t.Errorf("TokenLimit/CompressSize = %d/%d", m3.TokenLimit, m3.CompressSize)
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
		a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepSelect && len(cs.Models) == 1 && cs.Models[0].TokenLimit == 0
	})

	// 选择模型 → 进入手动设置压缩阈值步骤。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "manual step", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		a.modelMu.RUnlock()
		return cs != nil && cs.Step == model.ConnectStepManual
	})

	// 输入压缩阈值并提交。
	for _, r := range "20000" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "config set received", func() bool { return b.lastPatch() != nil })

	// 校验 patch：新键 1（已有键 0），TokenLimit 默认 128000，CompressSize 手动输入。
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
	if m1.TokenLimit != 128000 || m1.CompressSize != 20000 {
		t.Errorf("TokenLimit/CompressSize = %d/%d, want 128000/20000", m1.TokenLimit, m1.CompressSize)
	}

	// 完成步骤 → 关闭向导。
	waitCondition(t, "done step", func() bool {
		a.modelMu.RLock()
		cs := a.model.Connect
		a.modelMu.RUnlock()
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
