package plugin

// 端到端测试：真实子进程 + NDJSON JSON-RPC + protobuf payload。
// 测试用 helper 进程（TestPluginHelperProcess）扮演插件。

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/config"
	"github.com/cxykevin/alcoh/internal/input"
	pbv1 "github.com/cxykevin/alcoh/pb/plugin/v1"
	"google.golang.org/protobuf/proto"
)

// helperEnv 是 helper 进程识别自身的环境变量。
const helperEnv = "ALCOH_PLUGIN_HELPER=1"

// pluginHelperCmd 构造启动测试 helper 进程的 PluginConfig。
func pluginHelperCmd(name string) config.PluginConfig {
	return config.PluginConfig{
		Name:    name,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestPluginHelperProcess", "--"},
		Env:     []string{helperEnv},
	}
}

// TestPluginHelperProcess 是宿主测试复用的插件子进程入口：以 JSON-RPC 2.0
// NDJSON 方式与宿主通信。env=ALCOH_PLUGIN_HELPER=1 时执行协议循环。
func TestPluginHelperProcess(t *testing.T) {
	if os.Getenv("ALCOH_PLUGIN_HELPER") != "1" {
		return
	}
	runPluginHelperLoop()
	os.Exit(0)
}

// runPluginHelperLoop 实现一个最小插件：读 stdin NDJSON 行，处理
// initialize / hook/prompt / hook/key / hook/update / command/run。
func runPluginHelperLoop() {
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
		var env envelope
		_ = unmarshalEnvelope(raw.Params, &env)
		if len(raw.ID) == 0 {
			helperHandleNotification(raw.Method, env.Data)
			continue
		}
		helperHandleRequest(raw.ID, raw.Method, env.Data)
	}
}

// helperRespond 向宿主回复 protobuf 结果。
func helperRespond(id json.RawMessage, msg proto.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": envelope{Data: data}})
	if err != nil {
		return
	}
	os.Stdout.Write(append(line, '\n'))
}

// helperCallHost 向宿主发起一次请求并丢弃响应（notify/status 等）。
func helperCallHost(method string, msg proto.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": envelope{Data: data}})
	if err != nil {
		return
	}
	os.Stdout.Write(append(line, '\n'))
}

func helperHandleNotification(method string, data []byte) {
	if method == "hook/update" {
		// 每次收到 update 都向宿主回发一条状态事件，测试据此断言通知送达。
		helperCallHost("status", &pbv1.StatusRequest{Text: "updated"})
		return
	}
}

func helperHandleRequest(id json.RawMessage, method string, data []byte) {
	switch method {
	case "initialize":
		helperRespond(id, &pbv1.InitializeResult{
			Name: "testplug", Description: "测试插件", Version: "1.0",
			HooksPrompt: true, HooksUpdate: true,
			KeyBindings: []*pbv1.KeyBinding{{Type: "rune", Rune: 'g', Ctrl: true, Alt: true}},
			Commands:    []*pbv1.CommandInfo{{Name: "/testcmd", Description: "测试命令", ArgsHint: "[x]"}},
		})
	case "hook/prompt":
		var in pbv1.PromptRequest
		if proto.Unmarshal(data, &in) != nil {
			helperRespond(id, &pbv1.PromptResult{})
			return
		}
		switch {
		case in.Prompt == "blockme":
			helperRespond(id, &pbv1.PromptResult{Action: pbv1.PromptResult_ACTION_BLOCK, Reason: "blocked by testplug"})
		case strings.Contains(in.Prompt, "surely"):
			helperRespond(id, &pbv1.PromptResult{Action: pbv1.PromptResult_ACTION_REWRITE, Rewritten: "Please be concise: " + in.Prompt})
		default:
			helperRespond(id, &pbv1.PromptResult{})
		}
	case "hook/key":
		helperCallHost("notify", &pbv1.NotifyRequest{Kind: "info", Text: "key consumed"})
		helperRespond(id, &pbv1.KeyResult{Handled: true})
	case "command/run":
		var in pbv1.CommandRequest
		if proto.Unmarshal(data, &in) == nil {
			helperCallHost("notify", &pbv1.NotifyRequest{Kind: "success", Text: "ran " + in.Args})
		}
		helperRespond(id, &pbv1.CommandResult{Handled: true})
	default:
		helperRespond(id, &pbv1.KeyResult{})
	}
}

// drainEvent 在超时内从宿主事件通道取一个事件。
func drainEvent(t *testing.T, h *Host, what string) UIEvent {
	t.Helper()
	select {
	case ev := <-h.Events():
		return ev
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
		return UIEvent{}
	}
}

// TestHostEndToEnd 验证完整链路：进程启动 → 握手 → 全部 hooks → 关闭。
func TestHostEndToEnd(t *testing.T) {
	h := NewHost([]config.PluginConfig{pluginHelperCmd("testplug")})
	h.SetHostInfo("alcoh", "test", "/tmp")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer h.Close()

	// 握手后命令注册表应包含 /testcmd。
	if names := h.CommandNames(); len(names) != 1 || names[0] != "/testcmd" {
		t.Fatalf("CommandNames = %v, want [/testcmd]", names)
	}
	if desc, hint, ok := h.CommandInfo("/testcmd"); !ok || desc != "测试命令" || hint != "[x]" {
		t.Fatalf("CommandInfo = %q %q %v", desc, hint, ok)
	}

	// prompt hook：放行。
	out, blocked, _, err := h.PromptHook(ctx, "s1", "plain text")
	if err != nil || blocked || out != "plain text" {
		t.Fatalf("PromptHook(plain) = %q blocked=%v err=%v", out, blocked, err)
	}
	// prompt hook：改写。
	out, blocked, _, err = h.PromptHook(ctx, "s1", "hello surely world")
	if err != nil || blocked || out != "Please be concise: hello surely world" {
		t.Fatalf("PromptHook(rewrite) = %q blocked=%v err=%v", out, blocked, err)
	}
	// prompt hook：拦截。
	_, blocked, reason, err := h.PromptHook(ctx, "s1", "blockme")
	if err != nil || !blocked || reason != "blocked by testplug" {
		t.Fatalf("PromptHook(block) blocked=%v reason=%q err=%v", blocked, reason, err)
	}

	// key hook：命中绑定被消费；未绑定按键无 IPC。
	ke := input.RuneKey('g', input.ModAlt|input.ModCtrl)
	if !h.WantsKey(ke) {
		t.Fatal("WantsKey(ctrl+alt+g) = false, want true")
	}
	if !h.KeyHook(ctx, ke, "session", "none", true, "") {
		t.Fatal("KeyHook(ctrl+alt+g) = false, want true (handled)")
	}
	if h.WantsKey(input.RuneKey('x', 0)) {
		t.Fatal("WantsKey(plain x) = true, want false")
	}
	// key hook 触发插件 → 宿主 notify 事件。
	if ev := drainEvent(t, h, "notify from key hook"); ev.Kind != EventNotify || ev.Text != "key consumed" {
		t.Fatalf("event = %+v, want notify 'key consumed'", ev)
	}

	// 命令调用 + notify 事件。
	if !h.RunCommand(ctx, "/testcmd", "abc", "s1") {
		t.Fatal("RunCommand(/testcmd abc) = false, want true")
	}
	if ev := drainEvent(t, h, "notify from command"); ev.Kind != EventNotify || ev.Text != "ran abc" {
		t.Fatalf("event = %+v, want notify 'ran abc'", ev)
	}
	if h.RunCommand(ctx, "/nonexistent", "", "") {
		t.Fatal("RunCommand(/nonexistent) = true, want false")
	}

	// update 通知：插件收到后回发 status 事件。
	h.NotifyUpdate(&acp.StateChangeEvent{SessionID: "s1"})
	if ev := drainEvent(t, h, "status from update hook"); ev.Kind != EventStatus || ev.Text != "updated" {
		t.Fatalf("event = %+v, want status 'updated'", ev)
	}
}

// TestHostStartFailureIsNonFatal 验证启动失败的插件被标记不可用且不影响其余插件。
func TestHostStartFailureIsNonFatal(t *testing.T) {
	h := NewHost([]config.PluginConfig{
		{Name: "broken", Command: "/nonexistent/plugin-binary"},
		pluginHelperCmd("testplug"),
	})
	h.SetHostInfo("alcoh", "test", "/tmp")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Start(ctx); err == nil {
		t.Fatal("Start with broken plugin should return an error")
	}
	defer h.Close()

	if names := h.CommandNames(); len(names) != 1 || names[0] != "/testcmd" {
		t.Fatalf("CommandNames = %v, want [/testcmd] (healthy plugin only)", names)
	}
	// 失败事件应已上报。
	ev := drainEvent(t, h, "failure event")
	if ev.Kind != EventFailed || !ev.IsErr {
		t.Fatalf("event = %+v, want failed error event", ev)
	}
}

// TestHostZeroValue 验证零值 Host 可用且全部方法为 no-op。
func TestHostZeroValue(t *testing.T) {
	var h Host
	h.SetHostInfo("alcoh", "test", "/tmp")
	ctx := context.Background()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if out, blocked, _, err := h.PromptHook(ctx, "s", "x"); err != nil || blocked || out != "x" {
		t.Fatalf("PromptHook = %q blocked=%v err=%v", out, blocked, err)
	}
	if h.KeyHook(ctx, input.RuneKey('g', 0), "session", "none", true, "") {
		t.Fatal("KeyHook on empty host = true, want false")
	}
	if h.RunCommand(ctx, "/x", "", "") {
		t.Fatal("RunCommand on empty host = true, want false")
	}
	h.NotifyUpdate(&acp.StateChangeEvent{SessionID: "s"})
	h.Close()
}

// TestHelperProcessIsNotRun 防止误把 helper 当测试执行（环境变量未设置时为空操作）。
func TestHelperProcessIsNotRun(t *testing.T) {
	if os.Getenv(helperEnvSplit()) != "" {
		t.Fatal("helper env leaked into parent test")
	}
}

func helperEnvSplit() string { return "ALCOH_PLUGIN_HELPER" }
