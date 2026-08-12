package app

// 插件系统与 TUI 的集成测试：真实插件子进程 + demo backend。
// 插件行为由 TestPluginHelperProcess（本包测试二进制）扮演。

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
	pbv1 "github.com/cxykevin/alcoh/pb/plugin/v1"
	"google.golang.org/protobuf/proto"
)

// pluginHelperEnv 是插件 helper 进程识别自身的环境变量。
const pluginHelperEnv = "ALCOH_APP_PLUGIN_HELPER=1"

// pluginConfigForTest 构造指向本测试二进制的插件配置。
func pluginConfigForTest(name string) config.PluginConfig {
	return config.PluginConfig{
		Name:    name,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestAppPluginHelperProcess", "--"},
		Env:     []string{pluginHelperEnv},
	}
}

// TestAppPluginHelperProcess 是插件集成测试的子进程入口。
func TestAppPluginHelperProcess(t *testing.T) {
	if os.Getenv("ALCOH_APP_PLUGIN_HELPER") != "1" {
		return
	}
	runAppPluginHelperLoop()
	os.Exit(0)
}

type appHelperEnvelope struct {
	Data []byte `json:"data"`
}

// runAppPluginHelperLoop 实现测试插件协议循环（与 internal/plugin 的 helper
// 行为一致）：initialize 声明 prompt/key/update hooks 与 /testcmd 命令。
func runAppPluginHelperLoop() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var raw struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &raw) != nil || raw.Method == "" {
			continue
		}
		if raw.Method == "shutdown" {
			os.Exit(0)
		}
		var env appHelperEnvelope
		_ = json.Unmarshal(raw.Params, &env)
		if len(raw.ID) == 0 {
			if raw.Method == "hook/update" {
				appHelperCallHost("status", &pbv1.StatusRequest{Text: "updated"})
			}
			continue
		}
		appHelperHandleRequest(raw.ID, raw.Method, env.Data)
	}
}

func appHelperRespond(id json.RawMessage, msg proto.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": appHelperEnvelope{Data: data}})
	if err != nil {
		return
	}
	os.Stdout.Write(append(line, '\n'))
}

func appHelperCallHost(method string, msg proto.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": appHelperEnvelope{Data: data}})
	if err != nil {
		return
	}
	os.Stdout.Write(append(line, '\n'))
}

func appHelperHandleRequest(id json.RawMessage, method string, data []byte) {
	switch method {
	case "initialize":
		appHelperRespond(id, &pbv1.InitializeResult{
			Name: "testplug", Description: "测试插件", Version: "1.0",
			HooksPrompt: true, HooksUpdate: true,
			KeyBindings: []*pbv1.KeyBinding{{Type: "rune", Rune: 'g', Ctrl: true, Alt: true}},
			Commands:    []*pbv1.CommandInfo{{Name: "/testcmd", Description: "测试命令", ArgsHint: "[x]"}},
		})
	case "hook/prompt":
		var in pbv1.PromptRequest
		if proto.Unmarshal(data, &in) != nil {
			appHelperRespond(id, &pbv1.PromptResult{})
			return
		}
		switch {
		case in.Prompt == "blockme":
			appHelperRespond(id, &pbv1.PromptResult{Action: pbv1.PromptResult_ACTION_BLOCK, Reason: "blocked by testplug"})
		case strings.Contains(in.Prompt, "surely"):
			appHelperRespond(id, &pbv1.PromptResult{Action: pbv1.PromptResult_ACTION_REWRITE, Rewritten: "Please be concise: " + in.Prompt})
		default:
			appHelperRespond(id, &pbv1.PromptResult{})
		}
	case "hook/key":
		appHelperCallHost("notify", &pbv1.NotifyRequest{Kind: "info", Text: "key consumed"})
		appHelperRespond(id, &pbv1.KeyResult{Handled: true})
	case "command/run":
		var in pbv1.CommandRequest
		if proto.Unmarshal(data, &in) == nil {
			appHelperCallHost("notify", &pbv1.NotifyRequest{Kind: "success", Text: "ran " + in.Args})
		}
		appHelperRespond(id, &pbv1.CommandResult{Handled: true})
	default:
		appHelperRespond(id, &pbv1.KeyResult{})
	}
}

// newPluginApp 创建带测试插件的 App（demo backend + promptRecordingBackend）。
func newPluginApp(t *testing.T, initial string) (*App, *promptRecordingBackend, *fakeTerm, <-chan error) {
	t.Helper()
	ft := newFakeTerm()
	b := &promptRecordingBackend{Backend: demo.New(true)}
	a := NewWithConfig(ft, b, config.Values{Plugins: []config.PluginConfig{pluginConfigForTest("testplug")}})
	a.SetWorkdir(t.TempDir())
	a.SetOnboardingEnabled(false)
	a.SetInitialPrompt(initial)
	done := runApp(t, a)
	return a, b, ft, done
}

// quitApp 通过退出确认弹窗结束应用。
func quitPluginApp(t *testing.T, ft *fakeTerm, done <-chan error) {
	t.Helper()
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// TestPluginPromptRewriteIntegration 验证 One Shot 初始消息经插件改写后发送。
func TestPluginPromptRewriteIntegration(t *testing.T) {
	a, b, ft, done := newPluginApp(t, "hello surely world")
	// 插件改写后的文本应到达 backend。
	waitCondition(t, "rewritten prompt sent", func() bool {
		got := b.recordedPrompts()
		return len(got) == 1 && got[0].text == "Please be concise: hello surely world"
	})
	// 插件命令应进入命令面板。
	waitCondition(t, "plugin command in slash list", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return containsStr(a.model.SlashCommands(), "/testcmd")
	})
	quitPluginApp(t, ft, done)
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestPluginPromptBlockIntegration 验证插件拦截 prompt 时不发送且展示原因。
func TestPluginPromptBlockIntegration(t *testing.T) {
	a, b, ft, done := newPluginApp(t, "blockme")
	// 拦截后不应有 SendPrompt。
	time.Sleep(500 * time.Millisecond)
	if got := b.recordedPrompts(); len(got) != 0 {
		t.Fatalf("prompts = %v, want none (blocked)", got)
	}
	// 原因应展示在底部提示。
	waitCondition(t, "block reason shown", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Error == "blocked by testplug"
	})
	quitPluginApp(t, ft, done)
}

// TestPluginKeyHookIntegration 验证命中绑定的按键被插件消费并产生提示。
func TestPluginKeyHookIntegration(t *testing.T) {
	a, b, ft, done := newPluginApp(t, "hi")
	waitCondition(t, "prompt sent", func() bool { return len(b.recordedPrompts()) == 1 })
	// 会话视图中按下 ctrl+alt+g（插件绑定）：显示 key consumed 提示。
	ft.sendKey(input.RuneKey('g', input.ModAlt|input.ModCtrl))
	waitCondition(t, "key hook notify shown", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Error == "key consumed"
	})
	// 未绑定按键（普通 a）不产生 IPC，输入框正常收到字符。
	ft.sendKey(input.RuneKey('a', input.ModNone))
	waitCondition(t, "unbound key typed", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Input != nil && a.model.Input.Text() == "a"
	})
	quitPluginApp(t, ft, done)
}

// TestPluginCommandIntegration 验证插件斜杠命令触发 notify 并清空输入。
func TestPluginCommandIntegration(t *testing.T) {
	a, b, ft, done := newPluginApp(t, "hi")
	waitCondition(t, "prompt sent", func() bool { return len(b.recordedPrompts()) == 1 })

	// 输入 /testcmd foo 回车：命令交给插件执行。
	for _, r := range "/testcmd foo" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "command notify shown", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Error == "ran foo"
	})
	waitCondition(t, "input cleared after command", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Input != nil && a.model.Input.Text() == ""
	})
	quitPluginApp(t, ft, done)
}

// TestPluginUpdateHookIntegration 验证 ACP 事件经 update 通知插件。
func TestPluginUpdateHookIntegration(t *testing.T) {
	a, _, ft, done := newPluginApp(t, "hi")
	// 插件在收到 hook/update 后回发 status 事件：状态栏展示插件状态。
	waitCondition(t, "plugin status from update hook", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		lines := a.model.PluginStatusLines()
		return len(lines) == 1 && strings.Contains(lines[0], "updated")
	})
	quitPluginApp(t, ft, done)
}

// TestPluginSessionSubmitHook 验证会话视图输入框提交路径的 hooks：改写后发送、
// 拦截时输入框保留原文。
func TestPluginSessionSubmitHook(t *testing.T) {
	a, b, ft, done := newPluginApp(t, "hi")
	waitCondition(t, "initial prompt sent", func() bool { return len(b.recordedPrompts()) == 1 })

	// 输入 "say it surely" 回车：插件改写后发送。
	for _, r := range "say it surely" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "rewritten prompt sent", func() bool {
		got := b.recordedPrompts()
		return len(got) == 2 && got[1].text == "Please be concise: say it surely"
	})

	// 输入 "blockme" 回车：被拦截，不发送，输入框保留原文。
	for _, r := range "blockme" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "block reason shown", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Error == "blocked by testplug"
	})
	time.Sleep(200 * time.Millisecond)
	if got := b.recordedPrompts(); len(got) != 2 {
		t.Fatalf("prompts = %v, want still 2 (blocked prompt not sent)", got)
	}
	a.modelMu.RLock()
	input := a.model.Input.Text()
	a.modelMu.RUnlock()
	if input != "blockme" {
		t.Fatalf("input after block = %q, want blockme preserved", input)
	}
	quitPluginApp(t, ft, done)
}

// TestPluginCommandsReachable 验证插件命令在主页命令面板中可补全（模型层行为）。
func TestPluginCommandsReachable(t *testing.T) {
	ft := newFakeTerm()
	a := NewWithConfig(ft, &promptRecordingBackend{Backend: demo.New(true)}, config.Values{
		Plugins: []config.PluginConfig{pluginConfigForTest("testplug")},
	})
	a.model.SetPluginCommands([]string{"/testcmd"})
	a.model.SetPluginCommandInfo(map[string]model.SlashCommandInfo{
		"/testcmd": {Name: "/testcmd", Description: "测试命令", ArgsHint: "[x]"},
	})
	if !containsStr(a.model.SlashCommands(), "/testcmd") {
		t.Fatalf("SlashCommands = %v, want include /testcmd", a.model.SlashCommands())
	}
}

// TestPluginsCommandIntegration 验证 /plugins 打开本地配置编辑器、聚焦到
// plugins 段，编辑（切换 disabled）即保存到 config.json。
func TestPluginsCommandIntegration(t *testing.T) {
	cfgDir := setConfigDir(t)
	cfgPath := filepath.Join(cfgDir, "alcoh", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := `{"version":1,"colorMode":"auto","plugins":[{"name":"hello","command":"/bin/hello","disabled":false}]}`
	if err := os.WriteFile(cfgPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	ft := newFakeTerm()
	b := &promptRecordingBackend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)
	waitCondition(t, "home ready", func() bool { return homeCommandsReady(a) })

	// 输入 /plugins 回车：打开本地配置编辑器。
	for _, r := range "/plugins" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitCondition(t, "plugins editor open", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Modal == model.ModalPlugins && a.model.PluginsCfg != nil
	})
	// 应聚焦到 plugins 数组页。
	waitCondition(t, "focused on plugins page", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		ed := a.model.PluginsCfg
		return ed != nil && ed.Current() != nil && ed.Current().Key == "plugins"
	})
	// 进入第一个插件条目；下移到 disabled 行（command/disabled/name 排序第 2）并回车切换。
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.SimpleKey(input.KeyDown))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	// 编辑即保存：config.json 中 disabled 应为 true。
	waitCondition(t, "config saved with disabled=true", func() bool {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return false
		}
		return strings.Contains(string(data), `"disabled": true`)
	})
	// 编辑器打开时 Ctrl+q 被编辑器按键处理吞掉：先 Esc 关闭再退出。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitCondition(t, "plugins editor closed", func() bool {
		a.modelMu.RLock()
		defer a.modelMu.RUnlock()
		return a.model.Modal == model.NoModal
	})
	quitPluginApp(t, ft, done)
}
