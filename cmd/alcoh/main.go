// alcoh 是一个全屏 TUI 的 ACP v2 Client。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/app"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/term"
	"github.com/cxykevin/alcoh/internal/wscfg"
	"github.com/cxykevin/alcoh/product"
)

type valuesFlag []string

func (v *valuesFlag) String() string { return strings.Join(*v, ",") }
func (v *valuesFlag) Set(value string) error {
	*v = append(*v, value)
	return nil
}

// emptyObj 是 Elicitation capability 的占位 JSON 对象。
var emptyObj = json.RawMessage("{}")

// workdirExplicit 报告用户是否显式指定了 --cwd / --workdir。
func workdirExplicit() bool {
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "cwd" || f.Name == "workdir" {
			explicit = true
		}
	})
	return explicit
}

// newWSBackend 创建通过 WebSocket 连接远程 ACP agent 的 backend。
func newWSBackend(urlStr, cwd string) acp.Backend {
	return acp.NewWSClientBackend(acp.WSClientConfig{
		URL:        urlStr,
		ClientInfo: acp.ClientInfo{Name: "alcoh", Version: product.Version},
		Capabilities: acp.ClientCapabilities{
			Elicitation: &acp.ElicitationCapability{
				Form: &emptyObj,
				URL:  &emptyObj,
			},
		},
		CWD: cwd,
	})
}

func main() {
	settings, settingsErr := config.Load()
	// 先确定界面语言：配置 → ALCOH_LANG → 系统 locale，之后所有输出按此语言。
	i18n.SetLang(i18n.Detect(settings.Language))
	if settingsErr != nil {
		fmt.Fprintln(os.Stderr, i18n.T("读取本地配置失败，已使用默认值: %s", settingsErr))
	}
	demoFlag := flag.Bool("demo", false, i18n.T("使用内置演示 backend"))
	fast := flag.Bool("fast", false, i18n.T("加速演示脚本"))
	dump := flag.Int("dump", 0, i18n.T("无 TTY 渲染 N 帧 ANSI 输出（仅 demo）"))
	width := flag.Int("width", 80, i18n.T("dump 模式终端宽度"))
	height := flag.Int("height", 24, i18n.T("dump 模式终端高度"))
	agent := flag.String("agent", "", i18n.T("ACP agent 可执行文件（与 --ws-url 二选一）"))
	wsURL := flag.String("ws-url", "", i18n.T("远程 ACP agent 的 WebSocket 地址，如 ws://127.0.0.1:7433/acp?key=xxx"))
	ws := wscfg.RegisterFlags(flag.CommandLine)
	cwd := flag.String("cwd", "", i18n.T("新建 ACP 会话的工作目录（默认 alcoh 当前目录）"))
	flag.StringVar(cwd, "workdir", "", i18n.T("同 --cwd"))
	message := flag.String("message", "", i18n.T("One Shot 模式：启动后自动进入会话并发送该消息（可缩写 -m）"))
	flag.StringVar(message, "m", "", i18n.T("同 --message"))
	shutdownTimeout := flag.Duration("shutdown-timeout", 2*time.Second, i18n.T("关闭 agent 的最长等待时间"))
	var agentArgs valuesFlag
	var env valuesFlag
	flag.Var(&agentArgs, "agent-arg", i18n.T("传给 ACP agent 的单个 argv；可重复"))
	flag.Var(&env, "env", i18n.T("追加给 ACP agent 的 KEY=VALUE 环境变量；可重复"))
	flag.Parse()
	ws.ApplyExplicit(flag.CommandLine)

	if !workdirExplicit() && *cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("获取当前工作目录失败: %s", err))
			os.Exit(1)
		}
		*cwd = wd
	}
	if *cwd != "" {
		abs, err := filepath.Abs(*cwd)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("解析工作目录失败: %s", err))
			os.Exit(1)
		}
		*cwd = abs
	}

	if *dump > 0 {
		backend := demo.New(*fast)
		t := term.Dump(os.Stdout, *width, *height)
		a := app.NewWithConfig(t, backend, settings)
		a.SetWorkdir(*cwd)
		if err := a.RunDump("帮我实现一个 TCP 服务器", *dump); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("dump error: %s", err))
			os.Exit(1)
		}
		return
	}

	var backend acp.Backend
	// useDefaultWS 为 true 表示未指定任何 backend 参数，走默认 alkaid0 WebSocket
	// 连接。此时若服务端配置里没有任何模型，启动即进入新手引导（见 app.go）。
	useDefaultWS := false
	switch {
	case *demoFlag:
		backend = demo.New(*fast)
	case *wsURL != "":
		if *agent != "" {
			fmt.Fprintln(os.Stderr, i18n.T("--agent 与 --ws-url 互斥，请只选其一"))
			os.Exit(2)
		}
		backend = newWSBackend(*wsURL, *cwd)
	case *agent != "":
		client, err := acp.NewDesktopClientBackend(context.Background(), acp.ClientConfig{
			CommandConfig: acp.CommandConfig{
				Command:         *agent,
				Args:            agentArgs,
				Dir:             *cwd,
				Env:             env,
				ShutdownTimeout: *shutdownTimeout,
			},
			ClientInfo: acp.ClientInfo{Name: "alcoh", Version: product.Version},
			Capabilities: acp.ClientCapabilities{
				Elicitation: &acp.ElicitationCapability{
					Form: &emptyObj,
					URL:  &emptyObj,
				},
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("无法配置真实 ACP client: %s", err))
			os.Exit(1)
		}
		backend = client
	default:
		// 未指定 agent / ws-url / demo 时，按 alkaid0/helper 规则连接本地 WebSocket 服务。
		useDefaultWS = true
		cfg, err := ws.Resolve()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("解析默认 WebSocket 配置失败: %s", err))
			os.Exit(2)
		}
		urlStr, err := wscfg.URL(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("构建 WebSocket 地址失败: %s", err))
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, i18n.T("连接 WebSocket agent: %s")+"\n", wscfg.DisplayURL(cfg))
		backend = newWSBackend(urlStr, *cwd)
	}

	t, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("无法打开终端: %s", err))
		os.Exit(1)
	}
	a := app.NewWithConfig(t, backend, settings)
	a.SetWorkdir(*cwd)
	// One Shot 模式下用户直接发消息，不再需要新手引导。
	a.SetOnboardingEnabled(useDefaultWS && *message == "")
	a.SetInitialPrompt(*message)
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("运行错误: %s", err))
		os.Exit(1)
	}
}
