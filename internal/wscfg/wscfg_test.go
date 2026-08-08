package wscfg

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setEnv 设置环境变量并在测试结束后恢复。
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// writeConfig 在 dir 下写入 alkaid0 风格配置并返回路径。
func writeConfig(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolate 屏蔽系统级配置与默认用户配置，避免测试机上的真实配置干扰断言。
func isolate(t *testing.T) {
	t.Helper()
	systemConfigPathFn = func() string { return filepath.Join(t.TempDir(), "system-absent.json") }
	defaultConfigPath = filepath.Join(t.TempDir(), "user-absent.json")
}

func clearWsEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envConfigPath, envHost, envPort, envPath, envKey} {
		_ = os.Unsetenv(k)
	}
}

func resolveOptions(t *testing.T, configure func(fs *flag.FlagSet)) (Config, error) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o := RegisterFlags(fs)
	configure(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	o.ApplyExplicit(fs)
	return o.Resolve()
}

// TestResolveDefaults 无任何配置/环境/flag 时回退到硬编码默认值，且 key 占位与 helper 一致。
func TestResolveDefaults(t *testing.T) {
	isolate(t)
	clearWsEnv(t)
	cfg, err := resolveOptions(t, func(*flag.FlagSet) {})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 7433 || cfg.Path != "/acp" {
		t.Errorf("defaults = %+v, want host=127.0.0.1 port=7433 path=/acp", cfg)
	}
	if cfg.Key != defaultKeySentinel {
		t.Errorf("default key = %q, want sentinel %q", cfg.Key, defaultKeySentinel)
	}
}

// TestResolveConfigFile 用户配置文件覆盖默认值。
func TestResolveConfigFile(t *testing.T) {
	isolate(t)
	clearWsEnv(t)
	dir := t.TempDir()
	path := writeConfig(t, dir, "config.json", `{"server":{"host":"0.0.0.0","port":9000,"path":"/rpc","key":"alk-secret-1"}}`)
	cfg, err := resolveOptions(t, func(fs *flag.FlagSet) {
		if err := fs.Set("config", path); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 9000 || cfg.Path != "/rpc" || cfg.Key != "alk-secret-1" {
		t.Errorf("config override = %+v", cfg)
	}
}

// TestResolveConfigChain 未显式指定配置时按 系统级 → 用户级 顺序合并，用户级覆盖系统级。
func TestResolveConfigChain(t *testing.T) {
	clearWsEnv(t)
	dir := t.TempDir()
	sys := writeConfig(t, dir, "sys.json", `{"server":{"host":"sys.host","port":8001,"path":"/sys","key":"sys-key"}}`)
	usr := writeConfig(t, dir, "usr.json", `{"server":{"host":"usr.host","port":8002}}`)
	systemConfigPathFn = func() string { return sys }
	defaultConfigPath = usr
	cfg, err := resolveOptions(t, func(*flag.FlagSet) {})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// host/port 由用户级覆盖，path/key 保留系统级。
	if cfg.Host != "usr.host" || cfg.Port != 8002 {
		t.Errorf("host/port = %s:%d, want usr.host:8002", cfg.Host, cfg.Port)
	}
	if cfg.Path != "/sys" || cfg.Key != "sys-key" {
		t.Errorf("path/key = %q/%q, want /sys/sys-key", cfg.Path, cfg.Key)
	}
}

// TestResolveEnv 环境变量覆盖配置文件。
func TestResolveEnv(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := writeConfig(t, dir, "config.json", `{"server":{"host":"file.host","port":9000,"key":"file-key"}}`)
	setEnv(t, envConfigPath, path)
	setEnv(t, envHost, "env.host")
	setEnv(t, envPort, "7000")
	setEnv(t, envPath, "/env")
	setEnv(t, envKey, "env-key")
	cfg, err := resolveOptions(t, func(*flag.FlagSet) {})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Host != "env.host" || cfg.Port != 7000 || cfg.Path != "/env" || cfg.Key != "env-key" {
		t.Errorf("env override = %+v", cfg)
	}
}

// TestResolveFlags 命令行 flag 最高优先级覆盖环境变量。
func TestResolveFlags(t *testing.T) {
	isolate(t)
	clearWsEnv(t)
	setEnv(t, envHost, "env.host")
	setEnv(t, envPort, "7000")
	cfg, err := resolveOptions(t, func(fs *flag.FlagSet) {
		if err := fs.Set("host", "flag.host"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Set("port", "9000"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Set("key", "flag-key"); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Host != "flag.host" || cfg.Port != 9000 || cfg.Key != "flag-key" {
		t.Errorf("flag override = %+v", cfg)
	}
	// path 未显式指定，保留默认 /acp（不因环境未设置而变化）。
	if cfg.Path != "/acp" {
		t.Errorf("path = %q, want /acp", cfg.Path)
	}
}

// TestResolveInvalidPort 非法端口（flag 与环境变量）返回错误。
func TestResolveInvalidPort(t *testing.T) {
	isolate(t)
	clearWsEnv(t)
	if _, err := resolveOptions(t, func(fs *flag.FlagSet) {
		if err := fs.Set("port", "70000"); err != nil {
			t.Fatal(err)
		}
	}); err == nil {
		t.Error("out-of-range flag port should error")
	}
	if _, err := resolveOptions(t, func(fs *flag.FlagSet) {}); err != nil {
		t.Fatalf("baseline resolve: %v", err)
	}
	setEnv(t, envPort, "abc")
	if _, err := resolveOptions(t, func(fs *flag.FlagSet) {}); err == nil {
		t.Error("non-numeric env port should error")
	}
}

// TestURL 构建 WebSocket 地址：key 并入查询参数、路径规范化。
func TestURL(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"defaults", Config{Host: "127.0.0.1", Port: 7433, Path: "/acp", Key: "<empty>"},
			"ws://127.0.0.1:7433/acp?key=%3Cempty%3E"},
		{"no key", Config{Host: "localhost", Port: 9000, Path: ""},
			"ws://localhost:9000/"},
		{"path normalized", Config{Host: "example.com", Port: 80, Path: "acp", Key: "abc"},
			"ws://example.com:80/acp?key=abc"},
		{"ipv6", Config{Host: "::1", Port: 7433, Path: "/acp"},
			"ws://[::1]:7433/acp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := URL(c.cfg)
			if err != nil {
				t.Fatalf("URL: %v", err)
			}
			if got != c.want {
				t.Errorf("URL = %q, want %q", got, c.want)
			}
		})
	}
	if _, err := URL(Config{Port: 0, Host: "h"}); err == nil {
		t.Error("empty port should error")
	}
	if _, err := URL(Config{Port: 1, Host: ""}); err == nil {
		t.Error("empty host should error")
	}
}

// TestDisplayURL 展示地址掩盖 key，且可正确回显是否携带认证。
func TestDisplayURL(t *testing.T) {
	got := DisplayURL(Config{Host: "127.0.0.1", Port: 7433, Path: "/acp", Key: "super-secret"})
	if got != "ws://127.0.0.1:7433/acp?key=***" {
		t.Errorf("DisplayURL = %q", got)
	}
	if strings.Contains(got, "super-secret") {
		t.Error("DisplayURL leaked the key")
	}
	plain := DisplayURL(Config{Host: "127.0.0.1", Port: 7433, Path: "/acp"})
	if plain != "ws://127.0.0.1:7433/acp" {
		t.Errorf("DisplayURL (no key) = %q", plain)
	}
}
