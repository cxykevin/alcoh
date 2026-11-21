// alcoh 是一个全屏 TUI 的 ACP v2 Client。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/app"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/connspec"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/term"
	"github.com/cxykevin/alcoh/internal/wscfg"
	"github.com/cxykevin/alcoh/product"
)

// wsProbeTimeout 是启动前密钥预检（WebSocket 握手）的超时。
const wsProbeTimeout = 5 * time.Second

// maxKeyPromptTries 是认证失败后连续询问访问密钥的最多次数。
const maxKeyPromptTries = 3

type valuesFlag []string

func (v *valuesFlag) String() string { return strings.Join(*v, ",") }
func (v *valuesFlag) Set(value string) error {
	*v = append(*v, value)
	return nil
}

// emptyObj 是 Elicitation capability 的占位 JSON 对象。
var emptyObj = json.RawMessage("{}")

// flagExplicit 报告指定 flag 是否被显式设置。
func flagExplicit(name string) bool {
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			explicit = true
		}
	})
	return explicit
}

// workdirExplicit 报告用户是否显式指定了 --cwd / --workdir。
func workdirExplicit() bool {
	return flagExplicit("cwd") || flagExplicit("workdir")
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
			// 声明会话消息队列能力：服务端据此推送 msgqueue_update，并把
			// 会话执行中提交的输入入队而不是终止当前生成。是否真正启用
			// 队列交互还要看服务端是否同时声明 alkaid0 v0.6 与 msgqueue。
			MsgQueue: true,
		},
		CWD: cwd,
	})
}

// hostIsLoopback 报告 host 是否指向本机（localhost / 回环 IP / 空默认值）。
func hostIsLoopback(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// urlIsLoopback 报告 WebSocket URL 是否指向本机。
func urlIsLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return hostIsLoopback(u.Hostname())
}

// localUserHome 返回本机的用户主目录；失败时返回空串。
func localUserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// expandHome 展开 "~" 与 "~/" 前缀为当前用户主目录；其它路径原样返回。
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, filepath.FromSlash(p[2:]))
}

// resolveWSEndpoint 返回位置参数/默认连接使用的 WebSocket 端点路径：
// --url 优先，其次显式 --path，否则 /acp。二者同时给出且不同时报错。
func resolveWSEndpoint(urlValue string, opts *wscfg.Options) (string, error) {
	u := strings.TrimSpace(urlValue)
	urlSet := u != "" && flagExplicit("url")
	pathSet := flagExplicit("path")
	if urlSet && pathSet {
		if p, ok := opts.Path(); ok && strings.TrimSpace(p) != "" && strings.TrimSpace(p) != u {
			return "", errors.New(i18n.T("--url 与 --path 冲突（%s ≠ %s），请只选其一", u, p))
		}
	}
	if urlSet {
		return u, nil
	}
	if pathSet {
		if p, ok := opts.Path(); ok && strings.TrimSpace(p) != "" {
			return p, nil
		}
	}
	return connspec.DefaultEndpoint, nil
}

// resolveRemoteKey 决定远端连接使用的访问密钥：位置参数内联 > 本地记忆 >
// 交互输入。
//
// 先用候选密钥做一次 WebSocket 握手预检（不发送 ACP 消息）：
//   - 握手成功：记住该密钥（下次启动直接复用）并返回；
//   - 认证失败（401/403）：作废本地记忆的密钥，提示用户重新输入（最多
//     maxKeyPromptTries 次）；
//   - 其它错误（网络不可达/超时/端点不是 WS）：直接报错，不反复索要密钥。
func resolveRemoteKey(spec connspec.Spec, endpoint string) (string, error) {
	connID, err := spec.ConnID(endpoint)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(spec.Key)
	remembered := false
	if key == "" {
		if saved := config.LoadConnKey(connID); saved != "" {
			key, remembered = saved, true
		}
	}
	tries := 0
	for {
		probeURL, err := spec.WSURL(endpoint, key)
		if err != nil {
			return "", err
		}
		status, probeErr := probeWSAuth(probeURL)
		if probeErr == nil {
			// 认证通过（或该服务端不需要密钥）：把可用的密钥记住。
			if key != "" {
				if err := config.SaveConnKey(connID, key); err != nil {
					fmt.Fprintln(os.Stderr, i18n.T("保存连接密钥失败: %s", err))
				}
			}
			return key, nil
		}
		if !isWSAuthFailure(status) {
			return "", errors.New(i18n.T("预检连接 %s 失败: %s", connspec.MaskURL(probeURL), probeErr))
		}
		// 认证失败：旧密钥（内联或本地记忆）作废，重新询问。
		if remembered {
			_ = config.SaveConnKey(connID, "")
			remembered = false
		}
		key = ""
		if tries >= maxKeyPromptTries {
			return "", errors.New(i18n.T("访问密钥连续 %d 次认证失败", maxKeyPromptTries))
		}
		tries++
		entered, err := term.PromptSecret(i18n.T("请输入 %s 的访问密钥（输入不回显）: ", spec.HostPort()))
		if err != nil && strings.TrimSpace(entered) == "" {
			return "", errors.New(i18n.T("读取访问密钥失败: %s", err))
		}
		key = strings.TrimSpace(entered)
		if key == "" {
			return "", errors.New(i18n.T("访问密钥为空，已取消连接"))
		}
	}
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
	urlPath := flag.String("url", "", i18n.T("连接使用的 WebSocket 端点路径（默认 /acp；位置参数里的路径是对方文件系统路径）"))
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

	// ---- 位置参数：连接目标 ----
	// 支持本地目录（"/home/a"、"./a"、"~/a"）与远端服务
	//（"host[:port][/对方路径]"、"key@host[:port][/对方路径]"）。
	// 远端路径是对方文件系统路径（会话 cwd），与 WebSocket 端点路径无关。
	if args := flag.Args(); len(args) > 1 {
		var msg strings.Builder
		msg.WriteString(i18n.T("只支持一个位置参数（连接目标），收到 %d 个", len(args)))
		// Go 的 flag 解析在第一个位置参数处停止：放到位置参数之后的开关会被
		// 当成多余的位置参数，这里给出可直接照做的提示。
		for _, a := range args[1:] {
			if strings.HasPrefix(a, "-") {
				msg.WriteString("\n" + i18n.T("命令行开关需要写在位置参数之前，例如 alcoh --url /acp host:7433/path"))
				break
			}
		}
		fmt.Fprintln(os.Stderr, msg.String())
		os.Exit(2)
	}
	spec, err := connspec.Parse(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("解析连接目标失败: %s", err))
		os.Exit(2)
	}
	endpoint, err := resolveWSEndpoint(*urlPath, ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if flagExplicit("url") && (*wsURL != "" || *agent != "") {
		fmt.Fprintln(os.Stderr, i18n.T("--url 只用于位置参数/默认连接，不能与 --ws-url 或 --agent 同时使用"))
		os.Exit(2)
	}

	// sessionCwd 是会话工作目录：本地连接取本机绝对路径；远端连接取对方路径，
	// 原样传给 session/new（绝不做本机 filepath.Abs）。
	sessionCwd := ""
	// wantDirPicker 表示位置参数没给远端路径：启动后打开目录选择器选对方目录。
	wantDirPicker := false
	switch spec.Kind {
	case connspec.KindLocal:
		if workdirExplicit() {
			fmt.Fprintln(os.Stderr, i18n.T("位置参数已指定本地目录，不要再传 --cwd/--workdir"))
			os.Exit(2)
		}
		*cwd = expandHome(spec.Dir)
	case connspec.KindRemote:
		if *agent != "" || *wsURL != "" || *demoFlag {
			fmt.Fprintln(os.Stderr, i18n.T("位置参数是远端连接目标，不能同时使用 --agent / --ws-url / --demo"))
			os.Exit(2)
		}
		remotePath := strings.TrimSpace(spec.Path)
		if workdirExplicit() {
			flagCwd := filepath.ToSlash(strings.TrimSpace(*cwd))
			if remotePath != "" && flagCwd != remotePath {
				fmt.Fprintln(os.Stderr, i18n.T("位置参数里的对方路径 %s 与 --cwd %s 冲突", remotePath, *cwd))
				os.Exit(2)
			}
			if remotePath == "" {
				remotePath = flagCwd
			}
		}
		// 未给路径：先以对方根目录为默认 cwd，启动后让用户在选择器里挑。
		if remotePath == "" {
			remotePath = "/"
			wantDirPicker = true
		}
		sessionCwd = remotePath
	}
	if spec.Kind != connspec.KindRemote {
		// 本地语义：未显式指定时用进程当前目录，并取绝对路径。
		if *cwd == "" {
			wd, err := os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, i18n.T("获取当前工作目录失败: %s", err))
				os.Exit(1)
			}
			*cwd = wd
		}
		abs, err := filepath.Abs(expandHome(*cwd))
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("解析工作目录失败: %s", err))
			os.Exit(1)
		}
		*cwd = abs
		sessionCwd = abs
	}

	if *dump > 0 {
		backend := demo.New(*fast)
		t := term.Dump(os.Stdout, *width, *height)
		a := app.NewWithConfig(t, backend, settings)
		a.SetWorkdir(sessionCwd)
		a.SetDirHistoryKey("demo")
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
	// dirHistoryKey 是 /cd 常用目录的分桶标识：按连接 URL 隔离；无参且无
	// 环境变量启动时统一为 localhost（忽略端口）。认证 key 不进入该标识。
	dirHistoryKey := "localhost"
	// localHome 是 /cd 逻辑根固定第一项的本机主目录：只在服务端与本机同机
	// （stdio agent / 环回 WebSocket）时传入，远程连接保持为空。
	localHome := ""
	switch {
	case *demoFlag:
		backend = demo.New(*fast)
		dirHistoryKey = "demo"
	case spec.Kind == connspec.KindRemote:
		key, err := resolveRemoteKey(spec, endpoint)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("连接远端 agent 失败: %s", err))
			os.Exit(2)
		}
		urlStr, err := spec.WSURL(endpoint, key)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("构造 WebSocket 地址失败: %s", err))
			os.Exit(2)
		}
		connID, err := spec.ConnID(endpoint)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("构造连接标识失败: %s", err))
			os.Exit(2)
		}
		backend = newWSBackend(urlStr, sessionCwd)
		dirHistoryKey = connID
		// 远端服务恰好在本机（127.0.0.1 等）时，目录选择器仍展示本机主目录。
		if hostIsLoopback(spec.Host) {
			localHome = localUserHome()
		}
		fmt.Fprintf(os.Stderr, i18n.T("连接 WebSocket agent: %s")+"\n", connspec.MaskURL(urlStr))
	case *wsURL != "":
		if *agent != "" {
			fmt.Fprintln(os.Stderr, i18n.T("--agent 与 --ws-url 互斥，请只选其一"))
			os.Exit(2)
		}
		backend = newWSBackend(*wsURL, *cwd)
		dirHistoryKey = wscfg.NormalizeURL(*wsURL)
		if urlIsLoopback(*wsURL) {
			localHome = localUserHome()
		}
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
				// 与 WebSocket backend 一致：声明会话消息队列能力。
				MsgQueue: true,
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("无法配置真实 ACP client: %s", err))
			os.Exit(1)
		}
		backend = client
		// stdio agent 是本机子进程，文件系统就是 alcoh 所在的机器。
		localHome = localUserHome()
	default:
		// 未指定 agent / ws-url / demo 时，按 alkaid0/helper 规则连接本地 WebSocket 服务。
		useDefaultWS = true
		cfg, err := ws.Resolve()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("解析默认 WebSocket 配置失败: %s", err))
			os.Exit(2)
		}
		// --url 显式指定时覆盖端点路径（与 --path 语义一致，只是名字更直观）。
		if flagExplicit("url") {
			cfg.Path = endpoint
		}
		urlStr, err := wscfg.URL(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("构建 WebSocket 地址失败: %s", err))
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, i18n.T("连接 WebSocket agent: %s")+"\n", wscfg.DisplayURL(cfg))
		backend = newWSBackend(urlStr, *cwd)
		// 无参且无环境变量启动统一记为 localhost（忽略端口）；显式配置了
		// 连接参数时按规范化 URL 分桶。
		if ws.Explicit() || wscfg.EnvConfigured() {
			dirHistoryKey = wscfg.URLKey(cfg)
		}
		// 默认 alkaid0 连接：只有主机确实是本机时才展示本机主目录。
		if hostIsLoopback(cfg.Host) {
			localHome = localUserHome()
		}
	}

	t, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("无法打开终端: %s", err))
		os.Exit(1)
	}
	a := app.NewWithConfig(t, backend, settings)
	a.SetWorkdir(sessionCwd)
	a.SetDirHistoryKey(dirHistoryKey)
	a.SetLocalHome(localHome)
	// 远端位置参数没给路径：启动后先用目录选择器挑对方目录。
	a.SetStartupDirPicker(wantDirPicker)
	// /cd 常用目录直接读写 alcoh 本机文件：每次启动都把当前工作区写入历史
	// （按连接 URL 分桶、去重），逻辑根因此总是包含启动时的工作目录。
	// 等用户在选择器里挑了目录再记录，避免把占位的 "/" 写进历史。
	if !wantDirPicker {
		config.RecordDirHistory(dirHistoryKey, sessionCwd)
	}
	// One Shot 模式下用户直接发消息，不再需要新手引导。
	a.SetOnboardingEnabled(useDefaultWS && *message == "")
	a.SetInitialPrompt(*message)
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("运行错误: %s", err))
		os.Exit(1)
	}
}
