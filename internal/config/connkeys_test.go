package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestConnKeySaveLoadDelete 验证按连接记忆密钥、覆盖与删除。
func TestConnKeySaveLoadDelete(t *testing.T) {
	useTempConfigDir(t)

	const conn = "ws://example.com:7433/acp"
	if got := LoadConnKey(conn); got != "" {
		t.Fatalf("empty load = %q", got)
	}
	if err := SaveConnKey(conn, "k1"); err != nil {
		t.Fatal(err)
	}
	if got := LoadConnKey(conn); got != "k1" {
		t.Fatalf("load = %q, want k1", got)
	}
	// 另一个连接互不影响。
	if got := LoadConnKey("ws://other:7433/acp"); got != "" {
		t.Fatalf("other load = %q", got)
	}
	// 覆盖写。
	if err := SaveConnKey(conn, "k2"); err != nil {
		t.Fatal(err)
	}
	if got := LoadConnKey(conn); got != "k2" {
		t.Fatalf("load after overwrite = %q, want k2", got)
	}
	// 空密钥删除条目。
	if err := SaveConnKey(conn, ""); err != nil {
		t.Fatal(err)
	}
	if got := LoadConnKey(conn); got != "" {
		t.Fatalf("load after delete = %q", got)
	}
}

// TestConnKeysFilePermissionAndLocation 验证文件与 config.json 同目录且权限 0600。
func TestConnKeysFilePermissionAndLocation(t *testing.T) {
	useTempConfigDir(t)
	if err := SaveConnKey("ws://example.com:7433/acp", "secret"); err != nil {
		t.Fatal(err)
	}
	path, err := ConnKeysPath()
	if err != nil {
		t.Fatal(err)
	}
	cfgPath, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Dir(cfgPath) {
		t.Fatalf("conn.keys dir = %q, config dir = %q", filepath.Dir(path), filepath.Dir(cfgPath))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("conn.keys perm = %o, want 600", perm)
		}
	}
}

// TestConnKeysCorruptFileIsIgnored 验证损坏文件回退为空，且后续仍可写入。
func TestConnKeysCorruptFileIsIgnored(t *testing.T) {
	useTempConfigDir(t)
	path, err := ConnKeysPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadConnKey("ws://example.com:7433/acp"); got != "" {
		t.Fatalf("corrupt load = %q", got)
	}
	if err := SaveConnKey("ws://example.com:7433/acp", "k"); err != nil {
		t.Fatal(err)
	}
	if got := LoadConnKey("ws://example.com:7433/acp"); got != "k" {
		t.Fatalf("load after corrupt = %q", got)
	}
}

// TestSaveConnKeyEmptyIdentity 验证空连接标识被拒绝。
func TestSaveConnKeyEmptyIdentity(t *testing.T) {
	useTempConfigDir(t)
	if err := SaveConnKey("  ", "k"); err == nil {
		t.Fatal("SaveConnKey with empty identity should fail")
	}
}
