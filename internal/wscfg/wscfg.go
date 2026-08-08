// Package wscfg 解析 alcoh 默认 WebSocket 连接配置。
// 配置来源优先级（低→高）与 alkaid0/helper 一致：
//  1. 代码硬编码默认值 (Host=127.0.0.1, Port=7433, Path=/acp)
//  2. 配置文件（系统级 /etc/alkaid0/config.json → 用户级 ~/.config/alkaid0/config.json，后覆盖前）
//  3. 环境变量 (ALKAID0_HELPER_HOST/PORT/PATH/KEY)
//  4. 命令行 flag（最高优先级，仅覆盖显式指定的 flag）
package wscfg

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	envConfigPath = "ALKAID0_CONFIG_PATH"
	envHost       = "ALKAID0_HELPER_HOST"
	envPort       = "ALKAID0_HELPER_PORT"
	envPath       = "ALKAID0_HELPER_PATH"
	envKey        = "ALKAID0_HELPER_KEY"
)

// defaultKeySentinel 与 alkaid0/helper 保持一致的占位 key：未配置真实 key 时
// 仍携带该值，使服务端对不上 token 时返回明确的 401，而不是因无查询参数而无响应。
const defaultKeySentinel = "<empty>"

var defaultConfigPath = "~/.config/alkaid0/config.json"

// systemConfigPathFn 返回平台相关的系统级配置文件路径，测试时可替换。
var systemConfigPathFn = func() string {
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\alkaid0\config.json`
	}
	return "/etc/alkaid0/config.json"
}

// Config 是解析后的 WebSocket 连接配置。
type Config struct {
	Host string
	Port uint16
	Key  string
	Path string
}

// Options 持有命令行 flag 的显式覆盖值及其是否被设置的标记。
type Options struct {
	configPath string
	host       string
	port       uint
	path       string
	key        string

	configSet bool
	hostSet   bool
	portSet   bool
	pathSet   bool
	keySet    bool
}

// RegisterFlags 把默认 WebSocket 连接的覆盖 flag 注册到 fs。
func RegisterFlags(fs *flag.FlagSet) *Options {
	o := &Options{}
	fs.StringVar(&o.configPath, "config", "", "WebSocket 配置文件路径（默认 ~/.config/alkaid0/config.json）")
	fs.StringVar(&o.host, "host", "", "WebSocket 主机（默认 127.0.0.1）")
	fs.UintVar(&o.port, "port", 0, "WebSocket 端口（默认 7433）")
	fs.StringVar(&o.path, "path", "", "WebSocket 路径（默认 /acp）")
	fs.StringVar(&o.key, "key", "", "WebSocket 认证 key（默认读配置文件）")
	return o
}

// ApplyExplicit 记录 fs 中用户显式设置的 flag，供 Resolve 判断优先级。
func (o *Options) ApplyExplicit(fs *flag.FlagSet) {
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			o.configSet = true
		case "host":
			o.hostSet = true
		case "port":
			o.portSet = true
		case "path":
			o.pathSet = true
		case "key":
			o.keySet = true
		}
	})
}

// Resolve 依据优先级链解析最终配置：
// 硬编码默认值 → 配置文件（系统级 → 用户级）→ 环境变量 → 命令行 flag。
func (o *Options) Resolve() (Config, error) {
	cfg := Config{Host: "127.0.0.1", Port: 7433, Path: "/acp", Key: defaultKeySentinel}

	// 配置文件链：非显式指定时先加载系统级配置；显式指定（--config 或环境变量）时仅加载指定配置。
	configExplicit := false
	configPath := defaultConfigPath
	if env := os.Getenv(envConfigPath); env != "" {
		configPath = env
		configExplicit = true
	}
	if o.configSet {
		configPath = o.configPath
		configExplicit = true
	}
	loadConfigChain(&cfg, configPath, configExplicit)

	// 环境变量覆盖（优先级高于配置文件）。
	if v := os.Getenv(envHost); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv(envPort); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", envPort, err)
		}
		if p < 1 || p > 65535 {
			return cfg, fmt.Errorf("invalid %s %d: must be in range 1-65535", envPort, p)
		}
		cfg.Port = uint16(p)
	}
	if v := os.Getenv(envPath); v != "" {
		cfg.Path = v
	}
	if v := os.Getenv(envKey); v != "" {
		cfg.Key = v
	}

	// 命令行 flag 最高优先级覆盖（仅显式指定的 flag）。
	if o.hostSet {
		cfg.Host = o.host
	}
	if o.portSet {
		if o.port < 1 || o.port > 65535 {
			return cfg, fmt.Errorf("invalid port %d: must be in range 1-65535", o.port)
		}
		cfg.Port = uint16(o.port)
	}
	if o.pathSet {
		cfg.Path = o.path
	}
	if o.keySet {
		cfg.Key = o.key
	}

	// 校验必要参数。
	if cfg.Host == "" {
		return cfg, errors.New("host must be set")
	}
	if cfg.Port == 0 {
		return cfg, errors.New("port must be set")
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	return cfg, nil
}

// loadConfigChain 按优先级加载配置文件：未显式指定时先加载系统级配置作为基座，
// 再加载用户配置（或显式指定的配置）覆盖低优先级的值。
func loadConfigChain(cfg *Config, userPath string, configExplicit bool) {
	if !configExplicit {
		if loaded, ok := loadConfigFile(systemConfigPathFn()); ok {
			merge(cfg, loaded)
		}
	}
	if loaded, ok := loadConfigFile(userPath); ok {
		merge(cfg, loaded)
	}
}

// loadConfigFile 读取 alkaid0 配置文件中的 server 段。
// 文件不存在或格式损坏时视为未配置（ok=false），沿用已有值。
func loadConfigFile(path string) (Config, bool) {
	if path == "" {
		return Config{}, false
	}
	expanded, err := expandPath(path)
	if err != nil {
		return Config{}, false
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return Config{}, false
	}
	var full struct {
		Server struct {
			Host string `json:"host"`
			Port uint16 `json:"port"`
			Key  string `json:"key"`
			Path string `json:"path"`
		} `json:"server"`
	}
	if err := json.Unmarshal(data, &full); err != nil {
		return Config{}, false
	}
	return Config{
		Host: full.Server.Host,
		Port: full.Server.Port,
		Key:  full.Server.Key,
		Path: full.Server.Path,
	}, true
}

// merge 将 src 中的非空字段合并到 dst。
func merge(dst *Config, src Config) {
	if src.Host != "" {
		dst.Host = src.Host
	}
	if src.Port != 0 {
		dst.Port = src.Port
	}
	if src.Path != "" {
		dst.Path = src.Path
	}
	if src.Key != "" {
		dst.Key = src.Key
	}
}

// expandPath 展开路径中的 ~/ 为家目录。
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~+") {
		return "", errors.New("unsupported path expansion: " + path)
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// URL 依据配置构建 WebSocket 地址；key 非空时并入查询参数。
func URL(cfg Config) (string, error) {
	if cfg.Host == "" {
		return "", errors.New("host is empty")
	}
	if cfg.Port == 0 {
		return "", errors.New("port is empty")
	}
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	hostPort := net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	u := url.URL{Scheme: "ws", Host: hostPort, Path: path}
	if cfg.Key != "" {
		q := url.Values{}
		q.Set("key", cfg.Key)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// DisplayURL 返回用于日志展示的连接地址，认证 key 以 *** 掩盖，避免泄露到终端。
func DisplayURL(cfg Config) string {
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{
		Scheme: "ws",
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		Path:   path,
	}
	if cfg.Key != "" {
		u.RawQuery = "key=***"
	}
	return u.String()
}
