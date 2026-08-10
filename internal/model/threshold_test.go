package model

import (
	"encoding/json"
	"testing"
)

func TestThresholdTarget(t *testing.T) {
	cfg := json.RawMessage(`{"Version":1,"Model":{
		"DefaultModelID": 1,
		"Models":{
			"0":{"ModelName":"m0","CompressSize":64000},
			"1":{"ModelName":"deepseek-chat","CompressSize":32768}
		}
	}}`)
	info, err := ThresholdTarget(cfg)
	if err != nil {
		t.Fatalf("ThresholdTarget: %v", err)
	}
	if info.Key != "1" || info.ModelName != "deepseek-chat" || info.CompressSize != 32768 {
		t.Errorf("info = %+v", info)
	}
}

func TestThresholdTargetErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
	}{
		{"empty", ""},
		{"no model section", `{"Version":1}`},
		{"default id missing", `{"Model":{"DefaultModelID":5,"Models":{"0":{"CompressSize":1}}}}`},
		{"no models", `{"Model":{"DefaultModelID":0}}`},
		{"malformed", `{`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ThresholdTarget(json.RawMessage(c.cfg)); err == nil {
				t.Errorf("expected error for %q", c.name)
			}
		})
	}
}

// TestThresholdOpenClose 验证弹窗打开/关闭的 Modal 与状态。
func TestThresholdOpenClose(t *testing.T) {
	m := New()
	m.OpenThreshold()
	if m.Modal != ModalThreshold || m.Threshold == nil || !m.Threshold.Loading {
		t.Fatalf("threshold should open loading: modal=%v state=%+v", m.Modal, m.Threshold)
	}
	m.CloseThreshold()
	if m.Modal != NoModal || m.Threshold != nil {
		t.Error("threshold should close cleanly")
	}
}
