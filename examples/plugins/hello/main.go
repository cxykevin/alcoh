// hello 是 alcoh 插件系统的示例插件，演示全部协议能力：
//
//   - initialize 握手：声明名称、hooks 与斜杠命令
//   - hook/prompt：改写（"surely"）与拦截（"blockme"）
//   - hook/key：Ctrl+Alt+G 切换状态栏文本
//   - hook/update：观察 ACP 事件流并计数
//   - command/run：/hello [name] 弹提示
//   - 插件 → 宿主：notify（底部提示）与 status（状态栏）
//
// 协议：NDJSON JSON-RPC 2.0（stdin/stdout），payload 为 protobuf
// （schema 见 proto/plugin/v1/plugin.proto，本插件直接复用仓库内生成的
// Go 代码；外部插件可用 protoc 按相同 schema 生成任意语言的绑定）。
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	pbv1 "github.com/cxykevin/alcoh/pb/plugin/v1"
	"google.golang.org/protobuf/proto"
)

// envelope 是 JSON-RPC params/result 的单字段载体：protobuf 二进制以 base64
// 内嵌 JSON（Go 对 []byte 自动 base64）。
type envelope struct {
	Data []byte `json:"data"`
}

var (
	promptCount  int
	updateCount  int
	statusActive bool
)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var raw struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil || raw.Method == "" {
			continue
		}
		if raw.Method == "shutdown" {
			os.Exit(0)
		}
		var env envelope
		_ = json.Unmarshal(raw.Params, &env)
		if len(raw.ID) == 0 {
			handleNotification(raw.Method, env.Data)
			continue
		}
		handleRequest(raw.ID, raw.Method, env.Data)
	}
}

// handleRequest 处理宿主发来的 JSON-RPC request，处理后回复。
func handleRequest(id json.RawMessage, method string, data []byte) {
	switch method {
	case "initialize":
		respond(id, &pbv1.InitializeResult{
			Name: "hello", Description: "hello 示例插件", Version: "1.0.0",
			HooksPrompt: true, HooksUpdate: true,
			KeyBindings: []*pbv1.KeyBinding{{Type: "rune", Rune: 'g', Ctrl: true, Alt: true}},
			Commands: []*pbv1.CommandInfo{
				{Name: "/hello", Description: "向你打招呼", ArgsHint: "[name]"},
			},
		})
	case "hook/prompt":
		var in pbv1.PromptRequest
		if err := proto.Unmarshal(data, &in); err != nil {
			respond(id, &pbv1.PromptResult{})
			return
		}
		promptCount++
		switch {
		case in.Prompt == "blockme":
			// 拦截：输入框保留原文，原因展示在底部。
			respond(id, &pbv1.PromptResult{
				Action: pbv1.PromptResult_ACTION_BLOCK,
				Reason: "hello 插件拒绝发送 'blockme'",
			})
		case strings.Contains(in.Prompt, "surely"):
			// 改写：替换后发送。
			respond(id, &pbv1.PromptResult{
				Action:    pbv1.PromptResult_ACTION_REWRITE,
				Rewritten: "请务必简洁地回答：" + in.Prompt,
			})
		default:
			respond(id, &pbv1.PromptResult{})
		}
	case "hook/key":
		// Ctrl+Alt+G：切换状态栏文本。
		statusActive = !statusActive
		if statusActive {
			callHost("status", &pbv1.StatusRequest{Text: "hello on"})
		} else {
			callHost("status", &pbv1.StatusRequest{Text: ""})
		}
		respond(id, &pbv1.KeyResult{Handled: true})
	case "command/run":
		var in pbv1.CommandRequest
		_ = proto.Unmarshal(data, &in)
		name := strings.TrimSpace(in.Args)
		if name == "" {
			name = "world"
		}
		callHost("notify", &pbv1.NotifyRequest{Kind: "success", Text: "Hello, " + name + "!（已处理 " + itoa(promptCount) + " 个 prompt）"})
		respond(id, &pbv1.CommandResult{Handled: true})
	default:
		respond(id, &pbv1.KeyResult{})
	}
}

// handleNotification 处理宿主发来的 notification（不回复）。
func handleNotification(method string, data []byte) {
	if method != "hook/update" {
		return
	}
	var in pbv1.UpdateRequest
	if err := proto.Unmarshal(data, &in); err != nil {
		return
	}
	updateCount++
	if updateCount%10 == 0 {
		callHost("notify", &pbv1.NotifyRequest{Kind: "info", Text: "hello: 已观察 " + itoa(updateCount) + " 个事件（最新 " + in.Method + "）"})
	}
}

// respond 向宿主回复 protobuf 结果。
func respond(id json.RawMessage, msg proto.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": envelope{Data: data}})
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(append(line, '\n'))
}

// callHost 向宿主发起请求（notify/status/log），忽略响应。
func callHost(method string, msg proto.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": envelope{Data: data}})
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(append(line, '\n'))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
