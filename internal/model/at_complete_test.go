package model

// 输入框 @ 文件引用补全的纯状态测试：token 解析、前缀过滤、选择移动与
// 文件/目录两种补全结果。

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/widget"
)

func atCaps(enabled bool) acp.AgentCapabilities {
	raw := map[string]json.RawMessage{}
	if enabled {
		raw[acp.Alkaid0CapabilityV04] = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(raw)
	return acp.AgentCapabilities{Raw: data}
}

func newAtModel() *AppModel {
	m := New()
	m.AgentCaps = atCaps(true)
	return m
}

func typeAt(t *testing.T, m *AppModel, text string) {
	t.Helper()
	m.Input = widget.NewInputBuffer()
	m.Input.InsertText(text)
	m.UpdateAtState()
}

// TestAtTokenParsing 验证 @ token 的识别规则：只在行首/空白之后开始，
// 且 "/" 之前是目录、之后是过滤前缀。
func TestAtTokenParsing(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@")
	if !m.AtOpen || m.AtDir != "" || m.AtPrefix != "" {
		t.Fatalf("bare @ should open root list, got open=%v dir=%q prefix=%q", m.AtOpen, m.AtDir, m.AtPrefix)
	}

	m.Input = widget.NewInputBuffer()
	m.Input.InsertText("hello @src/ma")
	m.UpdateAtState()
	if !m.AtOpen || m.AtDir != "src" || m.AtPrefix != "ma" {
		t.Fatalf("token parse = open=%v dir=%q prefix=%q", m.AtOpen, m.AtDir, m.AtPrefix)
	}

	// 邮箱等紧贴字母的 @ 不触发补全。
	typeAt(t, m, "mail@example.com")
	if m.AtOpen {
		t.Fatal("@ not preceded by whitespace must not open completion")
	}

	// 空白结束 token：选择框收起。
	typeAt(t, m, "@src/main.go rest")
	if m.AtOpen {
		t.Fatal("whitespace should close @ completion")
	}
}

// TestAtFilterAndMove 验证前缀过滤（不区分大小写）与循环移动选中项。
func TestAtFilterAndMove(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@ma")
	m.ApplyAtEntries("", []AtEntry{
		{Name: "main.go"},
		{Name: "Makefile"},
		{Name: "src", IsDir: true},
	}, nil)
	entries := m.AtFilteredEntries()
	if len(entries) != 2 || entries[0].Name != "main.go" || entries[1].Name != "Makefile" {
		t.Fatalf("filtered entries = %#v", entries)
	}
	if _, ok := m.AtSelectedEntry(); !ok || m.AtSelected != 0 {
		t.Fatalf("default selection = %d", m.AtSelected)
	}
	m.AtMove(-1)
	if m.AtSelected != 1 {
		t.Fatalf("AtMove(-1) should wrap to last, got %d", m.AtSelected)
	}
	m.AtMove(1)
	if m.AtSelected != 0 {
		t.Fatalf("AtMove(1) should wrap to first, got %d", m.AtSelected)
	}
}

// TestAtCompleteFile 验证文件补全写入相对路径并追加空格，随后关闭选择框。
func TestAtCompleteFile(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "look @ma")
	m.ApplyAtEntries("", []AtEntry{{Name: "main.go"}}, nil)
	if !m.CompleteAt() {
		t.Fatal("CompleteAt should succeed with a selection")
	}
	if got := m.Input.Text(); got != "look @main.go " {
		t.Fatalf("completed text = %q", got)
	}
	if m.AtOpen {
		t.Fatal("file completion with trailing space should close the panel")
	}
}

// TestAtCompleteDir 验证目录补全保留尾部 "/" 并继续列出该目录。
func TestAtCompleteDir(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@sr")
	m.ApplyAtEntries("", []AtEntry{{Name: "src", IsDir: true}}, nil)
	if !m.CompleteAt() {
		t.Fatal("CompleteAt should complete a directory")
	}
	if got := m.Input.Text(); got != "@src/" {
		t.Fatalf("completed dir text = %q", got)
	}
	if !m.AtOpen || m.AtDir != "src" || m.AtPrefix != "" || !m.AtLoading {
		t.Fatalf("after dir completion open=%v dir=%q prefix=%q loading=%v", m.AtOpen, m.AtDir, m.AtPrefix, m.AtLoading)
	}

	// 目录内容到达后继续补全文件。
	m.ApplyAtEntries("src", []AtEntry{{Name: "main.go"}, {Name: "lib", IsDir: true}}, nil)
	m.AtMove(0)
	if !m.CompleteAt() {
		t.Fatal("CompleteAt should complete inside the directory")
	}
	if got := m.Input.Text(); got != "@src/main.go " {
		t.Fatalf("nested completion text = %q", got)
	}
}

// TestAtStaleDirResultIgnored 验证快速切换目录时旧请求晚回的结果被丢弃。
func TestAtStaleDirResultIgnored(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@src/ma")
	m.ApplyAtEntries("", []AtEntry{{Name: "stale.go"}}, nil)
	if len(m.AtEntries) != 0 {
		t.Fatalf("stale root result must be ignored, entries = %#v", m.AtEntries)
	}
	m.ApplyAtEntries("src", []AtEntry{{Name: "main.go"}}, nil)
	if len(m.AtFilteredEntries()) != 1 || m.AtFilteredEntries()[0].Name != "main.go" {
		t.Fatalf("current dir result should apply, entries = %#v", m.AtEntries)
	}
}

// TestAtErrorShown 验证目录读取失败时清空列表并保留错误文案。
func TestAtErrorShown(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@missing/")
	m.ApplyAtEntries("missing", nil, errors.New("no such directory"))
	if m.AtError != "no such directory" || len(m.AtEntries) != 0 || m.AtLoading {
		t.Fatalf("error state = %#v", m)
	}
}

// TestAtRequiresCapability 验证服务端未声明 alkaid0 扩展能力时 @ 不展开。
func TestAtRequiresCapability(t *testing.T) {
	m := New()
	m.AgentCaps = atCaps(false)
	typeAt(t, m, "@src")
	if m.AtOpen {
		t.Fatal("@ completion must be gated by the alkaid0 capability")
	}
}

// TestAtPreviewState 验证预览的加载/应用/清空，以及选中项已变化时旧结果
// 被忽略（快速移动选中项时晚回的结果不能覆盖新文件）。
func TestAtPreviewState(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@")
	m.ApplyAtEntries("", []AtEntry{{Name: "src", IsDir: true}, {Name: "main.go"}}, nil)

	// 选中目录不产生文件路径。
	if path, ok := m.AtSelectedPath(); ok || path != "" {
		t.Fatalf("directory selection should have no path, got %q", path)
	}
	m.AtSelected = 1
	path, ok := m.AtSelectedPath()
	if !ok || path != "main.go" {
		t.Fatalf("file selection path = %q ok=%v", path, ok)
	}

	m.BeginAtPreview("main.go")
	if !m.AtPreviewLoading || m.AtPreviewPath != "main.go" || m.AtPreview != "" {
		t.Fatalf("begin preview state = %#v", m)
	}
	m.ApplyAtPreview("main.go", "package main\n", nil)
	if m.AtPreviewLoading || m.AtPreviewErr != "" || m.AtPreview != "package main\n" {
		t.Fatalf("applied preview state = loading=%v err=%q text=%q",
			m.AtPreviewLoading, m.AtPreviewErr, m.AtPreview)
	}

	// 选中项切回目录后，main.go 的晚回结果不能写入。
	m.AtSelected = 0
	m.ApplyAtPreview("main.go", "stale\n", nil)
	if m.AtPreview != "package main\n" {
		t.Fatalf("stale preview must be ignored, got %q", m.AtPreview)
	}

	m.ClearAtPreview()
	if m.AtPreviewPath != "" || m.AtPreview != "" || m.AtPreviewErr != "" || m.AtPreviewLoading {
		t.Fatalf("clear preview state = %#v", m)
	}
}

// TestAtPreviewErrors 验证读取失败与二进制文件写入错误文案而不显示内容。
func TestAtPreviewErrors(t *testing.T) {
	m := newAtModel()
	typeAt(t, m, "@")
	m.ApplyAtEntries("", []AtEntry{{Name: "main.go"}}, nil)
	m.AtSelected = 0

	m.BeginAtPreview("main.go")
	m.ApplyAtPreview("main.go", "", errors.New("permission denied"))
	if m.AtPreview != "" || m.AtPreviewErr != "permission denied" || m.AtPreviewLoading {
		t.Fatalf("read error state = %#v", m)
	}

	m.BeginAtPreview("main.go")
	m.ApplyAtPreview("main.go", "a\x00b", nil)
	if m.AtPreview != "" || m.AtPreviewErr == "" {
		t.Fatalf("binary preview should show an error, got text=%q err=%q", m.AtPreview, m.AtPreviewErr)
	}
}

// TestPreviewTextTrimsIncompleteUTF8 验证按字节上限截断产生的不完整 UTF-8
// 尾巴被去掉，而 NUL / 中间非法 UTF-8 视为二进制。
func TestPreviewTextTrimsIncompleteUTF8(t *testing.T) {
	got, err := previewText("ab\xe4\xb8")
	if err != nil || got != "ab" {
		t.Fatalf("trim incomplete utf8 = %q err=%v", got, err)
	}
	if _, err := previewText("a\x00b"); err == nil {
		t.Fatal("NUL byte should be treated as binary")
	}
	if _, err := previewText("a\xffb"); err == nil {
		t.Fatal("invalid UTF-8 should be treated as binary")
	}
}
