package acp

// 会话 goal（alk.cxykevin.top/goal）的协议测试：v0.6 门控、RPC 参数形状与
// session/update 快照解码。

import (
	"context"
	"encoding/json"
	"testing"
)

func goalTestCaps(t *testing.T, b *ClientBackend, supported bool) {
	t.Helper()
	raw := map[string]json.RawMessage{}
	if supported {
		raw[Alkaid0CapabilityV06] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: data}
	b.state = backendReady
	b.mu.Unlock()
}

// TestGoalRequiresCapability 验证 goal RPC 只在服务端声明 goal 能力时发送。
func TestGoalRequiresCapability(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.GetGoal(context.Background()); err == nil {
		t.Fatal("expected error without goal capability")
	}
	if err := sess.SetGoal(context.Background(), "x"); err == nil {
		t.Fatal("expected error without goal capability")
	}
	for _, method := range ft.requests {
		if method == MethodGoalGet || method == MethodGoalSet {
			t.Fatalf("goal RPC must not be sent before capability gate, requests = %v", ft.requests)
		}
	}
}

// TestGoalGetSetParams 验证 get/set 带 sessionId，set 原样发送 goal（空串清除）。
func TestGoalGetSetParams(t *testing.T) {
	ft := &fakeTransport{}
	var gets []GoalGetParams
	var sets []GoalSetParams
	ft.set(MethodGoalGet, func(params, result any) error {
		gets = append(gets, params.(GoalGetParams))
		result.(*GoalResult).Goal = "现有目标"
		return nil
	})
	ft.set(MethodGoalSet, func(params, result any) error {
		sets = append(sets, params.(GoalSetParams))
		result.(*GoalResult).Goal = params.(GoalSetParams).Goal
		return nil
	})
	b := probeClient(t, ft)
	goalTestCaps(t, b, true)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}

	goal, err := sess.GetGoal(context.Background())
	if err != nil || goal != "现有目标" {
		t.Fatalf("GetGoal = %q, %v", goal, err)
	}
	if err := sess.SetGoal(context.Background(), "新目标"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if err := sess.SetGoal(context.Background(), ""); err != nil {
		t.Fatalf("SetGoal(clear): %v", err)
	}
	if len(gets) != 1 || gets[0].SessionID != "s1" {
		t.Fatalf("get params = %#v", gets)
	}
	if len(sets) != 2 || sets[0].SessionID != "s1" || sets[0].Goal != "新目标" || sets[1].Goal != "" {
		t.Fatalf("set params = %#v", sets)
	}
}

// TestDecodeGoalUpdate 验证 goal 快照事件解码：非空目标与空串（清除）都保留。
func TestDecodeGoalUpdate(t *testing.T) {
	for _, goal := range []string{"完成 goal 支持", ""} {
		raw, _ := json.Marshal(map[string]any{
			"sessionUpdate": "alk.cxykevin.top/session/goal",
			"goal":          goal,
		})
		ev, err := DecodeSessionUpdatePayload("s1", raw)
		if err != nil {
			t.Fatalf("decode goal=%q: %v", goal, err)
		}
		u, ok := ev.(*GoalUpdateEvent)
		if !ok {
			t.Fatalf("event = %T, want *GoalUpdateEvent", ev)
		}
		if u.SessionID != "s1" || u.Goal != goal {
			t.Fatalf("event = %#v", u)
		}
	}
}
