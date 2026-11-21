package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func useTempConfigDir(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("filesystem unavailable")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	}
}

// TestDirHistoryRecordAndLimit 验证去重置顶、64 条上限与跨桶隔离。
func TestDirHistoryRecordAndLimit(t *testing.T) {
	useTempConfigDir(t)

	RecordDirHistory("localhost", "/work/a")
	got := RecordDirHistory("localhost", "/work/b")
	if len(got) != 2 || got[0] != "/work/b" || got[1] != "/work/a" {
		t.Fatalf("record = %v", got)
	}
	got = RecordDirHistory("localhost", "/work/a")
	if len(got) != 2 || got[0] != "/work/a" || got[1] != "/work/b" {
		t.Fatalf("dedupe reorder = %v", got)
	}

	for i := range DirHistoryLimit + 10 {
		RecordDirHistory("localhost", fmt.Sprintf("/work/%03d", i))
	}
	loaded := LoadDirHistory("localhost")
	if len(loaded) != DirHistoryLimit {
		t.Fatalf("len = %d, want %d", len(loaded), DirHistoryLimit)
	}
	if loaded[0] != fmt.Sprintf("/work/%03d", DirHistoryLimit+9) {
		t.Fatalf("newest = %q", loaded[0])
	}

	// 另一个连接 key 的桶互不影响。
	if other := LoadDirHistory("ws://example/acp"); len(other) != 0 {
		t.Fatalf("other bucket = %v", other)
	}
	RecordDirHistory("ws://example/acp", "/remote/x")
	if loaded := LoadDirHistory("localhost"); len(loaded) != DirHistoryLimit {
		t.Fatalf("localhost polluted: %d", len(loaded))
	}
	if other := LoadDirHistory("ws://example/acp"); len(other) != 1 || other[0] != "/remote/x" {
		t.Fatalf("remote bucket = %v", other)
	}
}

// TestDirHistoryFileLocationAndFormat 验证 dir.history 与 config.json 同目录，
// 且是带 version/buckets 的 JSON。
func TestDirHistoryFileLocationAndFormat(t *testing.T) {
	useTempConfigDir(t)
	if err := SaveDirHistory("localhost", []string{"/a", "/b", "", "/a"}); err != nil {
		t.Fatal(err)
	}
	path, err := DirHistoryPath()
	if err != nil {
		t.Fatal(err)
	}
	cfgPath, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Dir(cfgPath) {
		t.Fatalf("dir.history dir = %q, config dir = %q", filepath.Dir(path), filepath.Dir(cfgPath))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f dirHistoryFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != dirHistoryVersion || len(f.Buckets["localhost"]) != 2 {
		t.Fatalf("file = %#v", f)
	}
}

// TestDirHistoryCorruptFileIsIgnored 验证损坏文件回退为空而不是 panic。
func TestDirHistoryCorruptFileIsIgnored(t *testing.T) {
	useTempConfigDir(t)
	path, err := DirHistoryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadDirHistory("localhost"); len(got) != 0 {
		t.Fatalf("corrupt load = %v", got)
	}
	if got := RecordDirHistory("localhost", "/ok"); len(got) != 1 || got[0] != "/ok" {
		t.Fatalf("record after corrupt = %v", got)
	}
}
