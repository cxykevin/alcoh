package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/provider"
)

func TestConnectFlow(t *testing.T) {
	m := New()
	m.OpenConnect()
	if m.Modal != ModalConnect || m.Connect == nil || m.Connect.Step != ConnectStepProvider {
		t.Fatalf("connect should open at provider step: modal=%v step=%v", m.Modal, m.Connect)
	}
	cs := m.Connect

	// 选择服务商模板预填 base_url 并聚焦 key 字段。
	cs.ConnectSetForm(provider.Templates[0].BaseURL)
	if cs.BaseURL != provider.Templates[0].BaseURL || cs.FormFocus != 1 {
		t.Errorf("baseURL=%q focus=%d, want prefill + key focus", cs.BaseURL, cs.FormFocus)
	}
	// 自定义模板（空 URL）不覆盖已有输入。
	cs.ConnectSetForm("")
	if cs.BaseURL != provider.Templates[0].BaseURL {
		t.Errorf("empty template should keep baseURL=%q", cs.BaseURL)
	}

	// 拉取成功 → 模型选择步骤。
	models := []provider.Model{{ID: "m1", Name: "M1"}, {ID: "m2", TokenLimit: 8192}}
	cs.ConnectApplyModels(models)
	if cs.Step != ConnectStepSelect || len(cs.Models) != 2 || cs.Fetching {
		t.Errorf("after apply: step=%v models=%d fetching=%v", cs.Step, len(cs.Models), cs.Fetching)
	}

	// 拉取失败 → 回表单并保留内容。
	cs.ConnectFetchError(jsonErr("boom"))
	if cs.Step != ConnectStepForm || cs.FormError != "boom" {
		t.Errorf("after error: step=%v err=%q", cs.Step, cs.FormError)
	}

	// 写入完成。
	cs.ConnectMarkResult("ok")
	if cs.Step != ConnectStepDone || cs.Result != "ok" {
		t.Errorf("after mark: step=%v result=%q", cs.Step, cs.Result)
	}

	// 关闭。
	m.CloseConnect()
	if m.Modal != NoModal || m.Connect != nil {
		t.Error("connect should close cleanly")
	}
}

func jsonErr(msg string) error { return &testErr{msg} }

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestConnectModelPatch(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		want int // 期望的下一个模型键（数字）
	}{
		{"empty config", "", 0},
		{"no models", `{"Version":1,"Server":{"port":7433}}`, 0},
		{"models 0 and 2", `{"Model":{"Models":{"0":{},"2":{}}}}`, 3},
		{"non-numeric keys ignored", `{"Model":{"Models":{"0":{},"abc":{}}}}`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			patch, err := ConnectModelPatch(json.RawMessage(c.cfg), provider.Model{ID: "deepseek-chat", TokenLimit: 65536}, "https://api.deepseek.com/v1", "sk-123", 65536, 32768)
			if err != nil {
				t.Fatalf("patch: %v", err)
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
			if err := json.Unmarshal(patch, &got); err != nil {
				t.Fatalf("unmarshal patch: %v", err)
			}
			key := itoaTest(c.want)
			entry, ok := got.Model.Models[key]
			if !ok {
				t.Fatalf("patch Models keys = %v, want %q", keysOf(got.Model.Models), key)
			}
			if entry.ModelID != "deepseek-chat" || entry.ProviderURL != "https://api.deepseek.com/v1" || entry.ProviderKey != "sk-123" {
				t.Errorf("entry = %+v", entry)
			}
			if entry.TokenLimit != 65536 || entry.CompressSize != 32768 {
				t.Errorf("TokenLimit/CompressSize = %d/%d", entry.TokenLimit, entry.CompressSize)
			}
			// DefaultModelID 必须为数字且等于新键。
			if got.Model.DefaultModelID != c.want {
				t.Errorf("DefaultModelID = %d, want %d", got.Model.DefaultModelID, c.want)
			}
		})
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func keysOf[M ~map[string]T, T any](m M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestConnectModelPatchManualCompress 验证手动路径：上下文长度未知时调用方
// 传入用户输入的 TokenLimit 与压缩阈值，patch 原样写入。
func TestConnectModelPatchManualCompress(t *testing.T) {
	patch, err := ConnectModelPatch(nil, provider.Model{ID: "m"}, "u", "k", 128000, 20000)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	var got struct {
		Model struct {
			Models map[string]struct {
				TokenLimit   int `json:"TokenLimit"`
				CompressSize int `json:"CompressSize"`
			} `json:"Models"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(patch, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, m := range got.Model.Models {
		if m.TokenLimit != 128000 || m.CompressSize != 20000 {
			t.Errorf("values = %d/%d, want 128000/20000", m.TokenLimit, m.CompressSize)
		}
	}
}

// TestConnectModelPatchDefaults 验证传入非法值时回退兜底（TokenLimit 128000、
// 压缩阈值取其半；正常流程调用方总是传入显式值）。
func TestConnectModelPatchDefaults(t *testing.T) {
	patch, err := ConnectModelPatch(nil, provider.Model{ID: "m"}, "u", "k", 0, 0)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	var got struct {
		Model struct {
			Models map[string]struct {
				TokenLimit   int `json:"TokenLimit"`
				CompressSize int `json:"CompressSize"`
			} `json:"Models"`
		} `json:"Model"`
	}
	if err := json.Unmarshal(patch, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, m := range got.Model.Models {
		if m.TokenLimit != 128000 || m.CompressSize != 102400 {
			t.Errorf("defaults = %d/%d, want 128000/102400 (80%%)", m.TokenLimit, m.CompressSize)
		}
	}
}
