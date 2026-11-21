package app

// 输入框 @ 文件引用的交互测试：输入 @ 触发 fs/read 列目录，Tab 逐级补全，
// 文件补全后追加空格并关闭选择框。

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
)

// atBackend 包装 demo backend，声明 alkaid0 v0.4 能力并记录 fs/read 请求。
type atBackend struct {
	*demo.Backend
	mu      sync.Mutex
	paths   []string // ListDir 请求的目录
	reads   []string // ReadFile 请求的文件
	entries map[string][]acp.FSDirEntry
	files   map[string]string
}

func (b *atBackend) AgentCapabilities() acp.AgentCapabilities {
	return acp.AgentCapabilities{Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)}
}

func (b *atBackend) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	s, err := b.Backend.NewSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &atSession{Session: s, b: b}, nil
}

func (b *atBackend) ResumeSession(ctx context.Context, id string) (acp.Session, error) {
	s, err := b.Backend.ResumeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &atSession{Session: s, b: b}, nil
}

// atSession 实现 acp.FSControl；目录内容挂在 backend 上，便于断言请求顺序。
type atSession struct {
	acp.Session
	b *atBackend
}

func (s *atSession) ListDir(ctx context.Context, path string) ([]acp.FSDirEntry, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.paths = append(s.b.paths, path)
	return append([]acp.FSDirEntry(nil), s.b.entries[path]...), nil
}

// ReadFile 是选中文件的预览读取（fs/read 的前 length 字节）。
func (s *atSession) ReadFile(ctx context.Context, path string, length int) (string, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.reads = append(s.b.reads, path)
	content := s.b.files[path]
	if length > 0 && len(content) > length {
		content = content[:length]
	}
	return content, nil
}

// TestAtCompletionListsAndCompletes 验证 @ 触发列目录、Tab 补全目录后继续
// 列出子目录，再 Tab 补全文件（追加空格并关闭选择框）。
func TestAtCompletionListsAndCompletes(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &atBackend{Backend: demo.New(true)}
	b.entries = map[string][]acp.FSDirEntry{
		"": {
			{Name: "src", Type: "directory"},
			{Name: "main.go", Type: "file"},
		},
		"src": {
			{Name: "main.go", Type: "file"},
		},
	}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 恢复 demo-0
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "@")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtOpen && !s.AtLoading && len(s.AtEntries) == 2
	})
	if s := a.snapshot(); s.AtDir != "" || s.AtPrefix != "" {
		t.Fatalf("root token state = dir %q prefix %q", s.AtDir, s.AtPrefix)
	}

	// 目录优先排序：选中 src，Tab 补全为 "@src/" 并继续列子目录。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtOpen && s.AtDir == "src" && !s.AtLoading && s.InputText == "@src/"
	})

	// 子目录里的文件补全为相对路径 + 空格，选择框关闭。
	ft.sendKey(input.SimpleKey(input.KeyTab))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return !s.AtOpen && s.InputText == "@src/main.go "
	})

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	paths := append([]string(nil), b.paths...)
	b.mu.Unlock()
	if len(paths) != 2 || paths[0] != "" || paths[1] != "src" {
		t.Fatalf("fs/read paths = %v, want [\"\" \"src\"]", paths)
	}
}

// TestAtPreviewLoadsSelectedFile 验证 @ 选择框里选中文件时异步读取预览，
// 移到目录上时清空；同一文件不重复请求。
func TestAtPreviewLoadsSelectedFile(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &atBackend{Backend: demo.New(true)}
	b.entries = map[string][]acp.FSDirEntry{
		"": {
			{Name: "src", Type: "directory"},
			{Name: "main.go", Type: "file"},
		},
	}
	b.files = map[string]string{"main.go": "package main\n\nfunc main() {}\n"}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "@")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtOpen && !s.AtLoading && len(s.AtEntries) == 2
	})
	// 默认选中目录：不显示文件预览。
	if s := a.snapshot(); s.AtPreviewPath != "" || s.AtPreview != "" {
		t.Fatalf("directory selection should have no preview, got %q", s.AtPreviewPath)
	}

	// ↓ 选中 main.go：读取预览。
	ft.sendKey(input.SimpleKey(input.KeyDown))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtPreviewPath == "main.go" && !s.AtPreviewLoading &&
			s.AtPreview == "package main\n\nfunc main() {}\n"
	})

	// ↑ 回到目录：清空预览。
	ft.sendKey(input.SimpleKey(input.KeyUp))
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtPreviewPath == "" && s.AtPreview == ""
	})

	quitApp(ft)
	waitRun(t, done)

	b.mu.Lock()
	reads := append([]string(nil), b.reads...)
	b.mu.Unlock()
	if len(reads) != 1 || reads[0] != "main.go" {
		t.Fatalf("ReadFile paths = %v, want [\"main.go\"]", reads)
	}
}

// TestAtPreviewLoadsFirstFile 验证目录加载完成后自动选中首个文件时立即
// 加载预览，不需要用户再按方向键。
func TestAtPreviewLoadsFirstFile(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &atBackend{Backend: demo.New(true)}
	b.entries = map[string][]acp.FSDirEntry{
		"": {{Name: "main.go", Type: "file"}},
	}
	b.files = map[string]string{"main.go": "hello\n"}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "@")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtOpen && s.AtPreviewPath == "main.go" && !s.AtPreviewLoading &&
			s.AtPreview == "hello\n"
	})

	quitApp(ft)
	waitRun(t, done)
}

// TestAtCompletionFilter 验证继续输入会按前缀筛选当前目录列表。
func TestAtCompletionFilter(t *testing.T) {
	setConfigDir(t)
	ft := newFakeTerm()
	b := &atBackend{Backend: demo.New(true)}
	b.entries = map[string][]acp.FSDirEntry{
		"": {
			{Name: "README.md", Type: "file"},
			{Name: "main.go", Type: "file"},
		},
	}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(200 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(300 * time.Millisecond)

	typeText(ft, "@ma")
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.AtOpen && !s.AtLoading && s.AtPrefix == "ma" && len(s.AtEntries) == 2
	})
	if s := a.snapshot(); len(s.AtEntries) != 2 {
		t.Fatalf("full directory should stay loaded, entries = %d", len(s.AtEntries))
	}

	quitApp(ft)
	waitRun(t, done)
}
