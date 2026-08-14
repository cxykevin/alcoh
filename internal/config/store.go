package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const currentVersion = 1

// PluginConfig 描述一个本地插件进程（alcoh 前端插件系统，见 internal/plugin）。
// Command/Args 直接作为 argv 传给 os/exec，不经过 shell 解析。
type PluginConfig struct {
	// Name 是展示名；空时用 Command 的最后一段。
	Name string `json:"name,omitempty"`
	// Command 是插件可执行文件路径。
	Command string `json:"command"`
	// Args 是传给插件的单个 argv，可重复。
	Args []string `json:"args,omitempty"`
	// Dir 是插件工作目录；空表示继承 alcoh。
	Dir string `json:"dir,omitempty"`
	// Env 是追加给插件的 KEY=VALUE 环境变量。
	Env []string `json:"env,omitempty"`
	// Disabled 为 true 时跳过启动该插件。不带 omitempty：false 也要持久化，
	// 否则 /plugins 编辑器里无法切换（false 在加载→保存往返中丢失）。
	Disabled bool `json:"disabled"`
}

// Values 是项目本地 TUI 配置。ACP agent 配置不写入此文件。
type Values struct {
	Version             int    `json:"version"`
	ColorMode           string `json:"colorMode,omitempty"`
	// Language 是界面语言（"zh"/"en"）；空表示未显式设置，
	// 启动时按 配置 → ALCOH_LANG → 系统 locale 检测（见 i18n.Detect）。
	Language            string `json:"language,omitempty"`
	ThinkingExpanded    bool   `json:"thinkingExpanded,omitempty"`
	ToolsExpanded       bool   `json:"toolsExpanded,omitempty"`
	TerminalOutputLimit int    `json:"terminalOutputLimit,omitempty"`
	// OnboardingEffort 是新手引导里选择的"用户第一个会话"的推理强度
	// （thought_level 值，如 high/xhigh）。首个会话激活时经 session/set_config_option
	// 应用后清空；空表示未设置。仅由引导流程写入。
	OnboardingEffort string `json:"onboardingEffort,omitempty"`
	// Plugins 是启动时加载的本地插件进程（前端插件系统）。
	Plugins []PluginConfig `json:"plugins,omitempty"`
}

func Defaults() Values {
	return Values{Version: currentVersion, ColorMode: "auto", TerminalOutputLimit: 32768}
}

// Path 返回跨平台用户配置路径。WASM/WASI 返回空路径，调用方应使用内存 store。
func Path() (string, error) {
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		return "", errors.New("config file is unavailable on wasm targets")
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("AppData")
		if base == "" {
			return "", errors.New("AppData is not set")
		}
		return filepath.Join(base, "alcoh", "config.json"), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "alcoh", "config.json"), nil
}

// Load 读取本地配置；不存在、损坏或版本不兼容时安全回退默认值。
func Load() (Values, error) {
	defaults := Defaults()
	path, err := Path()
	if err != nil {
		return defaults, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	var got Values
	if err := json.Unmarshal(data, &got); err != nil {
		return defaults, fmt.Errorf("decode config: %w", err)
	}
	if got.Version != currentVersion {
		return defaults, nil
	}
	if got.ColorMode == "" {
		got.ColorMode = defaults.ColorMode
	}
	if got.TerminalOutputLimit <= 0 {
		got.TerminalOutputLimit = defaults.TerminalOutputLimit
	}
	return got, nil
}

// Save 原子写入配置，避免程序中断留下半个 JSON 文件。
func Save(values Values) error {
	path, err := Path()
	if err != nil {
		return err
	}
	values.Version = currentVersion
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
