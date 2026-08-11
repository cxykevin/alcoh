// Package provider 提供 /connect 命令的模型服务商模板与 OpenAI 兼容
// /models 接口的模型列表获取。纯客户端 HTTP，不经过 ACP agent。
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cxykevin/alcoh/internal/i18n"
)

// Provider 是一个内置服务商模板。
type Provider struct {
	Name    string
	BaseURL string
}

// Templates 是 /connect 内置的服务商模板：选中自动预填 base_url。
// 末尾的"自定义"BaseURL 为空，由用户自行填写。
var Templates = []Provider{
	{Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1"},
	{Name: "OpenAI", BaseURL: "https://api.openai.com/v1"},
	{Name: "OpenCode Go", BaseURL: "https://opencode.ai/zen/go/v1"},
	{Name: "Moonshot Kimi", BaseURL: "https://api.moonshot.cn/v1"},
	{Name: "智谱 GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4"},
	{Name: "通义千问 Qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	{Name: "S3AI Api", BaseURL: "https://ai.furry.vg/v1"},
	{Name: "自定义", BaseURL: ""},
}

// Model 是服务商 /models 返回的一个模型。
type Model struct {
	ID string
	// Name 是展示名：优先取 API 返回的 name 字段，否则回退 ID。
	Name string
	// TokenLimit 是从 API 元数据读取的上下文长度（context_length /
	// max_model_len / context_window 中的首个非零值）；0 表示未知。
	TokenLimit int
}

// FetchModels 请求 {baseURL}/models（OpenAI 兼容接口）获取模型列表。
// 请求携带 Authorization: Bearer <key>；成功响应按 data[].id 顺序返回。
func FetchModels(ctx context.Context, baseURL, key string) ([]Model, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New(i18n.T("base_url 为空"))
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New(i18n.T("API key 为空"))
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(i18n.T("服务商返回 HTTP %d"), resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			// 常见上下文长度字段，兼容各家实现。
			ContextLength any `json:"context_length"`
			MaxModelLen   any `json:"max_model_len"`
			ContextWindow any `json:"context_window"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("解析 /models 响应失败"), err)
	}
	if len(out.Data) == 0 {
		return nil, errors.New(i18n.T("服务商未返回任何模型"))
	}
	models := make([]Model, 0, len(out.Data))
	for _, d := range out.Data {
		if d.ID == "" {
			continue
		}
		m := Model{ID: d.ID, Name: d.ID}
		if d.Name != "" {
			m.Name = d.Name
		}
		if n := toInt(d.ContextLength); n > 0 {
			m.TokenLimit = n
		} else if n := toInt(d.MaxModelLen); n > 0 {
			m.TokenLimit = n
		} else if n := toInt(d.ContextWindow); n > 0 {
			m.TokenLimit = n
		}
		models = append(models, m)
	}
	if len(models) == 0 {
		return nil, errors.New(i18n.T("服务商未返回任何模型"))
	}
	return models, nil
}

// toInt 把 JSON 数字字段（可能是 float64/string）转为 int。
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		var i int
		_, _ = fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}
