package model

import (
	"encoding/json"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
)

func TestElicitationEnqueue(t *testing.T) {
	m := New()
	m.Active = NewSession("sess1", "测试会话")

	// 创建表单模式的 elicitation 请求
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
			"age": map[string]interface{}{
				"type": "number",
			},
		},
		"required": []interface{}{"name"},
	}
	schemaJSON, _ := json.Marshal(schema)

	req := acp.ElicitationCreateParams{
		SessionID: "sess1",
		Mode:      acp.ElicitationModeForm,
		Message:   "请填写信息",
		Schema:    schemaJSON,
	}

	rpcID := acp.NewRPCID(json.RawMessage(`"req1"`))

	// 入队请求
	m.EnqueueElicitation(rpcID, req)

	// 验证模态已打开
	if m.Modal != ModalElicitation {
		t.Errorf("expected modal to be ModalElicitation, got %v", m.Modal)
	}

	// 验证 elicitation 状态
	if m.Elicitation == nil {
		t.Fatal("elicitation should not be nil")
	}

	if m.Elicitation.Request.Mode != acp.ElicitationModeForm {
		t.Errorf("expected form mode, got %v", m.Elicitation.Request.Mode)
	}

	if len(m.Elicitation.FieldOrder) != 2 {
		t.Errorf("expected 2 fields, got %d", len(m.Elicitation.FieldOrder))
	}

	// 验证 RPCID 已保存
	if m.ElicitationRPCID == nil {
		t.Error("ElicitationRPCID should not be nil")
	}
}

func TestElicitationQueue(t *testing.T) {
	m := New()
	m.Active = NewSession("sess1", "测试会话")

	req1 := acp.ElicitationCreateParams{
		SessionID:     "sess1",
		Mode:          acp.ElicitationModeURL,
		Message:       "请求1",
		URL:           "https://example.com",
		ElicitationID: "elicit1",
	}

	req2 := acp.ElicitationCreateParams{
		SessionID:     "sess1",
		Mode:          acp.ElicitationModeURL,
		Message:       "请求2",
		URL:           "https://example.com/2",
		ElicitationID: "elicit2",
	}

	rpcID1 := acp.NewRPCID(json.RawMessage(`"req1"`))
	rpcID2 := acp.NewRPCID(json.RawMessage(`"req2"`))

	// 第一个请求应立即显示
	m.EnqueueElicitation(rpcID1, req1)
	if m.Elicitation == nil || m.Elicitation.Request.Message != "请求1" {
		t.Error("first request should be displayed")
	}

	// 第二个请求应进入队列
	m.EnqueueElicitation(rpcID2, req2)
	if len(m.ElicitationQueue) != 1 {
		t.Errorf("expected queue length 1, got %d", len(m.ElicitationQueue))
	}

	// 完成第一个请求
	m.AdvanceElicitationQueue()
	if m.Elicitation == nil || m.Elicitation.Request.Message != "请求2" {
		t.Error("second request should be displayed after advancing")
	}

	if len(m.ElicitationQueue) != 0 {
		t.Errorf("expected empty queue, got %d", len(m.ElicitationQueue))
	}
}

func TestElicitationStateIdle(t *testing.T) {
	m := New()
	m.Active = NewSession("sess1", "测试会话")
	m.View = ViewSession // 确保在会话视图

	req := acp.ElicitationCreateParams{
		SessionID:     "sess1",
		Mode:          acp.ElicitationModeURL,
		Message:       "请求",
		URL:           "https://example.com",
		ElicitationID: "elicit1",
	}

	rpcID := acp.NewRPCID(json.RawMessage(`"req1"`))
	m.EnqueueElicitation(rpcID, req)

	// 验证初始状态
	if m.Modal != ModalElicitation {
		t.Fatalf("modal should be ModalElicitation, got %v", m.Modal)
	}

	// 添加队列中的请求
	m.ElicitationQueue = append(m.ElicitationQueue, req)

	// 模拟会话空闲事件
	m.ApplyEvent(&acp.StateChangeEvent{
		SessionID: "sess1",
		State:     acp.StateIdle,
	})

	// 验证 elicitation 被清理
	if m.Modal == ModalElicitation {
		t.Error("modal should be closed on idle")
	}
	if m.Elicitation != nil {
		t.Error("elicitation should be nil")
	}
	if len(m.ElicitationQueue) != 0 {
		t.Errorf("elicitation queue should be cleared, got %d items", len(m.ElicitationQueue))
	}
}
