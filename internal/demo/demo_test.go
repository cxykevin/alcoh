package demo

import (
	"context"
	"testing"
)

// TestDeleteSessionRemovesFromList 验证 DeleteSession 从可恢复会话列表移除，
// 且对不存在的会话静默成功（幂等，与 ACP v2 session/delete 语义一致）。
func TestDeleteSessionRemovesFromList(t *testing.T) {
	b := New(true)

	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "demo-0" {
		t.Fatalf("initial sessions = %#v, want [demo-0]", sessions)
	}

	// 新建会话应追加到列表。
	s, err := b.NewSession(context.Background(), ".")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	newID := s.ID()
	sessions, _ = b.ListSessions(context.Background())
	if len(sessions) != 2 {
		t.Fatalf("sessions after new = %d, want 2", len(sessions))
	}

	// 删除新建会话。
	if err := b.DeleteSession(context.Background(), newID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	sessions, _ = b.ListSessions(context.Background())
	if len(sessions) != 1 || sessions[0].SessionID != "demo-0" {
		t.Fatalf("sessions after delete = %#v, want [demo-0]", sessions)
	}

	// 删除不存在的会话：静默成功，列表不变。
	if err := b.DeleteSession(context.Background(), "nope"); err != nil {
		t.Fatalf("delete missing session: %v", err)
	}
	sessions, _ = b.ListSessions(context.Background())
	if len(sessions) != 1 {
		t.Fatalf("sessions after missing delete = %d, want 1", len(sessions))
	}
}

// TestBackendDeclaresSessionDeleteCapability 验证 demo backend 声明
// session.delete 能力，使首页按 d 删除会话在 demo 下可用。
func TestBackendDeclaresSessionDeleteCapability(t *testing.T) {
	b := New(true)
	if !b.AgentCapabilities().SupportsSessionDelete() {
		t.Fatal("demo backend should declare session.delete capability")
	}
}
