package model

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/cxykevin/alcoh/internal/provider"
)

// ConnectStep 是 /connect 向导的步骤。
type ConnectStep int

const (
	// ConnectStepProvider 选择服务商模板（自动预填 base_url）。
	ConnectStepProvider ConnectStep = iota
	// ConnectStepForm 填写 base_url 与 API key（提交后拉取模型列表）。
	ConnectStepForm
	// ConnectStepSelect 从服务商拉取的模型列表中选择一个写入配置。
	ConnectStepSelect
	// ConnectStepDone 写入完成（Enter/Esc 关闭）。
	ConnectStepDone
)

// ConnectState 是 /connect 向导的状态。纯状态机：网络请求由 app 层发起，
// 结果经 ConnectApplyModels / ConnectFetchError / ConnectMarkResult 回填。
type ConnectState struct {
	Step        ConnectStep
	ProviderSel int // 服务商模板选中索引
	// BaseURL/Key 是表单字段（Key 掩码显示，写回为明文）。
	BaseURL   string
	Key       string
	FormFocus int    // 0=BaseURL 1=Key
	FormError string // 表单校验/拉取错误
	Fetching  bool   // 正在拉取模型列表（阻塞编辑）
	Models    []provider.Model
	ModelSel  int
	// Result 是 Done 步骤展示的写入结果（如 "模型已添加并设为默认模型"）。
	Result string
	// FromOnboarding 表示向导由新手引导触发（服务端无模型首次启动）：
	// 完成后继续引导剩余步骤（选推理强度 → 操作教学）而非直接关闭。
	FromOnboarding bool
}

// OpenConnect 打开 /connect 向导并重置状态。
func (m *AppModel) OpenConnect() {
	m.Connect = &ConnectState{Step: ConnectStepProvider}
	m.CloseSlash()
	m.SetModal(ModalConnect)
}

// CloseConnect 关闭 /connect 向导。
func (m *AppModel) CloseConnect() {
	m.Connect = nil
	m.SetModal(NoModal)
}

// ConnectTemplates 返回内置服务商模板副本。
func ConnectTemplates() []provider.Provider {
	return append([]provider.Provider(nil), provider.Templates...)
}

// ConnectSetForm 用服务商模板预填 base_url（自定义模板为空时保持用户输入）。
func (cs *ConnectState) ConnectSetForm(baseURL string) {
	if baseURL != "" || strings.TrimSpace(cs.BaseURL) == "" {
		cs.BaseURL = baseURL
	}
	cs.FormFocus = 1 // 直接聚焦 key 字段
	cs.FormError = ""
}

// ConnectApplyModels 拉取成功：进入模型选择步骤。
func (cs *ConnectState) ConnectApplyModels(models []provider.Model) {
	cs.Models = models
	cs.ModelSel = 0
	cs.Fetching = false
	cs.Step = ConnectStepSelect
}

// ConnectFetchError 拉取失败：回到表单步骤并显示错误（保留已填内容）。
func (cs *ConnectState) ConnectFetchError(err error) {
	cs.Fetching = false
	cs.Step = ConnectStepForm
	cs.FormError = err.Error()
}

// ConnectMarkResult 写入成功：进入完成步骤。
func (cs *ConnectState) ConnectMarkResult(msg string) {
	cs.Result = msg
	cs.Step = ConnectStepDone
}

// ConnectModelPatch 从 config/get 的完整配置中计算 Model.Models 的下一个数字键
//（现有最大数字键 + 1，首个为 "0"），并构造把选中模型写入服务端配置的
// config/set patch（含设为默认模型 DefaultModelID，该字段为数值类型）。
// 上下文长度未知时使用默认值 128000，压缩阈值取其一半。
func ConnectModelPatch(cfg json.RawMessage, p provider.Model, baseURL, key string) (json.RawMessage, error) {
	var parsed struct {
		Model *struct {
			Models map[string]json.RawMessage `json:"Models"`
		} `json:"Model"`
	}
	next := 0
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &parsed); err != nil {
			return nil, err
		}
		if parsed.Model != nil {
			for k := range parsed.Model.Models {
				if idx, err := strconv.Atoi(k); err == nil && idx >= next {
					next = idx + 1
				}
			}
		}
	}
	tokenLimit := p.TokenLimit
	if tokenLimit <= 0 {
		tokenLimit = 128000
	}
	compress := tokenLimit / 2
	if compress < 1 {
		compress = 1
	}
	keyStr := strconv.Itoa(next)
	name := p.Name
	if name == "" {
		name = p.ID
	}
	patch := map[string]any{
		"Model": map[string]any{
			"Models": map[string]any{
				keyStr: map[string]any{
					"ProviderURL": strings.TrimSpace(baseURL),
					"ProviderKey": strings.TrimSpace(key),
					"ModelName":   name,
					"ModelID":     p.ID,
					"TokenLimit":  tokenLimit,
					"CompressSize": compress,
				},
			},
			"DefaultModelID": next,
		},
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return b, nil
}
