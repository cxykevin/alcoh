package acp

// 文件系统扩展（alk.cxykevin.top/fs/read）的协议测试：能力门控、请求参数形状
// 与目录/文件两种响应的解码。

import (
	"context"
	"encoding/json"
	"testing"
)

// TestFSListDirRequiresCapability 验证未声明 alkaid0 扩展能力时不发请求。
func TestFSListDirRequiresCapability(t *testing.T) {
	ft := &fakeTransport{}
	b := probeClient(t, ft)
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.ListDir(context.Background(), "src"); err == nil {
		t.Fatal("expected error without the alkaid0 capability")
	}
	if len(ft.requests) != 1 {
		t.Fatalf("no fs RPC should be sent before the capability gate, requests = %v", ft.requests)
	}
}

// TestFSListDirDecodesAndSorts 验证请求带 sessionId/相对路径，条目按
// “目录优先、名称升序”排序。
func TestFSListDirDecodesAndSorts(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodFSRead, func(params, result any) error {
		p := params.(FSReadParams)
		if p.SessionID != "s1" || p.Path != "src" {
			t.Fatalf("params = %#v", p)
		}
		r := result.(*FSReadResult)
		r.Content = json.RawMessage(`[
			{"name":"zeta.go","type":"file","size":12},
			{"name":"Beta","type":"directory"},
			{"name":"alpha.go","type":"file"},
			{"name":"Alpha","type":"directory"}
		]`)
		return nil
	})
	b := probeClient(t, ft)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
	b.state = backendReady
	b.mu.Unlock()
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}

	entries, err := sess.ListDir(context.Background(), "src")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	want := []string{"Alpha", "Beta", "alpha.go", "zeta.go"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %#v", entries)
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Fatalf("entries[%d] = %q, want %q (%#v)", i, entries[i].Name, name, entries)
		}
	}
	if entries[0].Type != "directory" || entries[3].Size == nil || *entries[3].Size != 12 {
		t.Fatalf("entry fields = %#v", entries)
	}
}

// TestFSListDirRejectsFileContent 验证把文件路径传给 ListDir 时返回错误，
// 而不是把文件内容当目录条目解析。
func TestFSListDirRejectsFileContent(t *testing.T) {
	ft := &fakeTransport{}
	ft.set(MethodFSRead, func(params, result any) error {
		r := result.(*FSReadResult)
		r.Content = json.RawMessage(`"package main\n"`)
		return nil
	})
	b := probeClient(t, ft)
	b.mu.Lock()
	b.agentCaps = AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
	b.state = backendReady
	b.mu.Unlock()
	sess := &clientSession{baseSession: baseSession{id: "s1"}, backend: b}
	if _, err := sess.ListDir(context.Background(), "main.go"); err == nil {
		t.Fatal("expected an error when the path is a file")
	}
}
