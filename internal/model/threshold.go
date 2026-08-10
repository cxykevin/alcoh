package model

import (
	"encoding/json"
	"errors"
	"strconv"
)

// errNoDefaultModel 表示服务端配置中未配置默认模型（无 Model.Models 或
// DefaultModelID 指向的模型不存在）。
var errNoDefaultModel = errors.New("server has no default model")

// ThresholdInfo 是 /threshold 定位到的目标模型信息（来自 config/get）。
type ThresholdInfo struct {
	Key          string // Model.Models.<key>
	ModelName    string
	CompressSize int
}

// ThresholdState 是 /threshold 弹窗状态：修改默认模型的压缩阈值。
// 纯状态机：config/get 拉取当前值由 app 层发起，结果经预填字段回填。
type ThresholdState struct {
	// Loading 为 true 表示正在拉取当前压缩阈值（预填输入框），期间阻塞编辑。
	Loading bool
	// ModelKey/ModelName 是目标模型（config/get 后确定）。
	ModelKey  string
	ModelName string
	// Input 是输入框文本（预填当前值，用户可修改）。
	Input string
	// Error 是校验/写回错误。
	Error string
}

// OpenThreshold 打开 /threshold 弹窗并开始拉取当前值。
func (m *AppModel) OpenThreshold() {
	m.Threshold = &ThresholdState{Loading: true}
	m.CloseSlash()
	m.SetModal(ModalThreshold)
}

// CloseThreshold 关闭 /threshold 弹窗。
func (m *AppModel) CloseThreshold() {
	m.Threshold = nil
	m.SetModal(NoModal)
}

// ThresholdTarget 从 config/get 的完整配置中定位默认模型（DefaultModelID 指向
// Model.Models.<key>），返回其键、名称与当前 CompressSize。配置缺失默认模型时
// 返回错误。
func ThresholdTarget(cfg json.RawMessage) (*ThresholdInfo, error) {
	var parsed struct {
		Model *struct {
			DefaultModelID int `json:"DefaultModelID"`
			Models         map[string]struct {
				ModelName    string `json:"ModelName"`
				CompressSize int    `json:"CompressSize"`
			} `json:"Models"`
		} `json:"Model"`
	}
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &parsed); err != nil {
			return nil, err
		}
	}
	if parsed.Model == nil {
		return nil, errNoDefaultModel
	}
	key := strconv.Itoa(parsed.Model.DefaultModelID)
	m, ok := parsed.Model.Models[key]
	if !ok {
		return nil, errNoDefaultModel
	}
	return &ThresholdInfo{
		Key:          key,
		ModelName:    m.ModelName,
		CompressSize: m.CompressSize,
	}, nil
}

