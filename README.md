<p align="center">
  <picture>
    <source srcset="https://raw.githubusercontent.com/cxykevin/alcoh/refs/heads/main/logo/wide140x50d.svg" media="(prefers-color-scheme: dark)">
    <img src="https://raw.githubusercontent.com/cxykevin/alcoh/refs/heads/main/logo/wide140x50l.svg" alt="alkaid0-logo">
  </picture>
</p>

[![GitHub Repo stars](https://img.shields.io/github/stars/cxykevin/alcoh?style=flat&link=https%3A%2F%2Fgithub.com%2Fcxykevin%2Falcoh)](https://github.com/cxykevin/alcoh)
[![GitHub Release](https://img.shields.io/github/v/release/cxykevin/alcoh?include_prereleases&sort=semver&display_name=tag&style=flat)](https://github.com/cxykevin/alcoh/releases)
[![GitHub License](https://img.shields.io/github/license/cxykevin/alkaid0?style=flat&cacheSeconds=100000&link=https%3A%2F%2Fgithub.com%2Fcxykevin%2Falkaid0)](https://github.com/cxykevin/alkaid0?tab=GPL-3.0-1-ov-file)
[![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/cxykevin/alcoh)](https://pkg.go.dev/github.com/cxykevin/alcoh)
[![Build and Package](https://github.com/cxykevin/alcoh/actions/workflows/build.yml/badge.svg)](https://github.com/cxykevin/alcoh/actions/workflows/build.yml)

# alcoh

alcoh 是一个全屏 TUI 的 ACP v2 客户端（类 opencode / Claude Code）。基于纯 Go 实现。默认通过 WebSocket 连接本地 alkaid0 ACP 服务。

alcoh 实现了大部分的 ACP v2 协议内容，因此可以通过命令行访问其它支持 ACP v2 的 Agent。

## 核心特性

- **ACP v2 双 transport**：JSON-RPC 2.0 over stdio 与 WebSocket 双 transport 客户端
- **终端体验**：CJK 宽字符、Markdown / 代码高亮、鼠标框选复制
- **命令面板**：`/` 打开本地与 agent 命令面板；`/settings` 打开本地设置；`/effort`、`/model` 调整推理强度与切换模型
- **服务端配置编辑器**：`/server` 经 alkaid0 扩展 RPC 浏览/编辑服务端配置，编辑即自动保存（只适配 alkaid0 后端）
- **新手引导**：启动进入引导（与 `/connect` 向导同义：选服务商 → 填 key → 拉取模型列表 → 选模型 → 推理强度 → 操作教学）（只适配 alkaid0 后端）
- **跨平台**：Linux、macOS、Windows Terminal

## 安装

```bash
# Linux
curl -sSL https://alk.cxykevin.top/c.sh | bash
```

```powershell
# Windows
irm https://alk.cxykevin.top/c.ps1 | iex
```

---

### ACP agent

**默认情况下** 程序会连接本地的 Alkaid0。

如果你需要链接其它 agent，必须传入 agent 可执行文件（通过stdio），或通过 `--ws-url` 连接远程 WebSocket agent。
命令参数必须逐项传入，不经过 shell 解析：

```bash
# 在当前目录连接本地 alkaid0
alcoh

# stdio 子进程
alcoh \
  --agent my-acp-agent \
  --agent-arg --stdio \
  --cwd "$PWD" \
  --env API_KEY=value

# 远程 WebSocket agent（如 alkaid0）
alcoh \
  --ws-url 'ws://127.0.0.1:7433/acp?token={token}' \
  --cwd "$PWD"

# One Shot 模式：启动后自动进入会话并发送消息
alcoh --message '帮我实现一个 TCP 服务器'
alcoh -m '你好'
```

可用参数：

- `--agent <path>`：ACP agent 可执行文件；与 `--ws-url` 互斥；
- `--agent-arg <arg>`：传给 agent 的单个 argv，可重复；
- `--ws-url <url>`：远程 ACP agent 的 WebSocket 地址（`ws://` 或 `wss://`），token 等认证参数并入 query string；与 `--agent` 互斥；
- `--config <path>`：WebSocket 配置文件路径（默认 `~/.config/alkaid0/config.json`），读取其中的 `server` 段；
- `--host <host>` / `--port <port>` / `--path <path>` / `--key <key>`：覆盖默认 WebSocket 连接的 host/端口/路径/认证 key；
- `--cwd <dir>`：新建 ACP 会话的工作目录，默认取 alcoh 进程当前目录（`--workdir` 为别名）；
- `--env KEY=VALUE`：追加环境变量，可重复；
- `--message <text>` / `-m <text>`：**One Shot 模式**——启动后自动进入会话视图并发送该消息，无需手动输入；复用主页预创建会话（无预创建会话时新建）；
- `--shutdown-timeout <duration>`：退出时等待 agent 的时长，默认 `2s`；

---

## 国际化（i18n）

界面语言按以下优先级确定：**本地配置**（`/settings` 中的「语言」项，持久化到 `~/.config/alcoh/config.json` 的 `language` 字段）→ **环境变量 `ALCOH_LANG`** → **系统 locale**（`LANG`/`LC_ALL`/`LC_MESSAGES`）→ 默认中文。

- 当前支持 `zh`（默认）与 `en` 两种语言；`/settings` 里可随时切换，保存后立即生效
- 翻译表位于 `internal/i18n/en.go`（键为中文原文，缺失条目回退中文）；新增文案只需把用户可见字符串包上 `i18n.T("…")` 并补充英文翻译

---

## 配置

本地客户端设置保存到：

```bash
# Linux / macOS
~/.config/alcoh/config.json
# Windows
%AppData%\alcoh\config.json
```

```json
{
  "version": 1,
  "plugins": [
    {
      "name": "hello",
      "command": "/abs/path/to/alcoh/examples/plugins/hello/hello"
    }
  ]
}
```

---

## 插件系统

alcoh 以**本地子进程 + NDJSON JSON-RPC 2.0 + protobuf payload** 的方式接入前端插件：
插件是独立的可执行进程，由 alcoh 在启动时拉起（`shutdown` 通知后超时强杀），
经 stdin/stdout 双向传输 NDJSON 格式的 JSON-RPC 2.0 消息，每个方法的
params/result 都是 `{"data": "<base64(protobuf)>"}` 单字段对象。

- **协议 schema**：`proto/plugin/v1/plugin.proto`（Go 绑定已生成到
  `pb/plugin/v1`；任意语言可用 protoc 按同一 schema 生成插件端代码）
- **参考实现**：`examples/plugins/hello`（演示全部 hooks 与回调）
- **配置**：`~/.config/alcoh/config.json` 的 `plugins` 数组（`name` /
  `command` / `args` / `dir` / `env` / `disabled`），命令参数逐项传入不经过 shell

### Host → Plugin 方法

| 方法 | 类型 | 说明 |
|---|---|---|
| `initialize` | request | 启动后首条消息；插件应答名称、hooks、按键绑定与斜杠命令 |
| `hook/prompt` | request | 提交 prompt 前裁决：`ALLOW` / `REWRITE` / `BLOCK`（拦截时原因展示给用户，输入框保留） |
| `hook/key` | request | 仅当按键命中插件声明的 `key_bindings` 时调用；`handled=true` 消费按键 |
| `hook/update` | notification | 每个 ACP 事件（`message_chunk`、`state_update`、`tool_call_update` 等）的只读快照 |
| `command/run` | request | 用户执行插件注册的斜杠命令（自动出现在 `/` 命令面板） |
| `shutdown` | notification | alcoh 退出，插件应尽快自行退出 |

### Plugin → Host 方法

| 方法 | 说明 |
|---|---|
| `notify` | TUI 底部提示（`kind`: `info`/`success`/`error`） |
| `status` | 设置状态栏左侧插件文本（空串清除） |
| `log` | 写入 alcoh stderr（不污染 TUI） |

### Hooks 注入点

- **prompt**：会话视图回车提交、主页回车提交、One Shot 初始消息全部经
  `hook/prompt` 裁决后才发送（见 `internal/app/plugins.go` 的 `sendPrompt`）
- **key**：按键分发前按插件绑定过滤，命中才发起 IPC，普通按键零开销
- **update**：事件循环对每个 ACP 事件异步广播（不阻塞渲染）

插件进程启动/握手失败、运行中崩溃或 hook 超时均不致命：该插件被停用并在
TUI 底部提示一次，其余插件与主程序不受影响。

---

## 平台支持

| 平台 | 真实 ACP agent | 说明 |
|---|---:|---|
| Linux / macOS | ✅ | pure Go `os/exec` + stdio，或 WebSocket 远程连接 |
| Windows Terminal | ✅ | Windows Terminal VT + stdio，或 WebSocket 远程连接 |

---

## 会话管理

- **主页预创建会话**：进入主页（启动或 `/clear` 返回）时，若服务端公布 `session.delete` 能力，客户端同步预创建一个空会话（不进入会话视图），用它承载 agent 在 `session/new` 后广播的 `config_option_update`，使 `/effort` 与 `/model` 在主页命令面板直接可用。主页直接输入 prompt 回车时复用该会话作为用户的新会话（不删除、不新建）；恢复旧会话、程序退出或预创建会话确无用途时才把它删除，不在服务端残留。
- **会话恢复与删除**：首页选中会话按 `Enter` 恢复（`session/resume`），按 `d` 删除（`session/delete`，仅当 agent 声明对应能力时可用）。
- **打断与退出**：会话内按 `Esc` 打断正在进行的 AI 响应（`session/cancel`）；输入框为空时 `Ctrl+C` 首次提示、2 秒内再次按下才退出。

---

## 客户端命令

以 `/` 开头的输入会弹出命令面板，`Enter` / `Tab` 补全并执行本地命令。未匹配的斜杠命令原样作为 prompt 提交给 agent（按 agent 公布的能力出现）。

- `/alcoh_help`：显示命令帮助（输入框为空时按 `?` 亦可）
- `/connect`：**连接模型服务商向导**——内置服务商模板（DeepSeek / OpenAI / OpenCode Go / Moonshot / GLM / Qwen / S3AI / 自定义）自动预填 base_url，填 API key 后自动调用服务商 `/models` 接口拉取模型列表，选择模型即写入服务端配置（自动分配模型键并设为默认）。**压缩阈值自动规则**（拉取到上下文长度时生效）：模型名为 gemini → 80000；deepseek-v4-flash → 140000（上下文已知 1M，TokenLimit 1000000，全自动无需手动输入）；上下文 ≥1M 且模型名不含 claude → 200000；其余取上下文长度的 80%。未公布上下文长度的模型进入手动输入步骤（上下文长度 + 压缩阈值，压缩阈值随上下文长度按同一规则联动预填、可改）。`/model` 切换到 deepseek-v4-flash 时也会自动把服务端配置中的压缩阈值更新为 140000。仅当服务端声明 alkaid0 扩展能力时可用
- `/clear [on]`：返回主页会话列表；默认先取消正在运行的会话，`on` 不取消直接返回
- `/effort [unset|low|medium|high|xhigh|max]`：设置推理强度。带参数直接经 `session/set_config_option` 写 `thought_level`；无参数弹出水平滑条（←→ 移动、Enter 确认、Esc 取消）。仅当 agent 公布 `thought_level` config 时可用
- `/model [value]`：切换模型。带参数直接经 `session/set_config_option`（`type=id`）写模型；无参数弹出垂直模型菜单（↑↓/滚轮 选择、Enter 确认、Esc 取消）。仅当 agent 公布 `category="model"`（或 `configId="model"`）config 时可用；候选值与当前值均取自服务端公布的 `options`/`currentValue`
- `/settings`：本地设置（`Ctrl+,` 亦可），切换 `colorMode` 等选项并即时保存
- `/server`：服务端配置编辑器，仅当服务端在 initialize 中声明 `alk.cxykevin.top/alkaid0/v0.4` 能力时出现

### 常用快捷键

| 按键 | 作用 |
|---|---|
| `Esc` | 会话内打断 AI 响应 / 关闭弹窗 |
| `Ctrl+C` | 复制选中文本 / 清空输入 / 连按两次退出 |
| `Ctrl+O` | 展开/折叠会话中全部思维链与工具调用 |
| 鼠标左键点击思考/工具标题 | 展开/折叠该单项 |
| `d` | 删除会话 |

常用命令和 Claude Code 基本一致，但个别命令有区别。

---

## 新手引导

启动时未指定 backend 参数（走默认 alkaid0 WebSocket 连接）、服务端声明 alkaid0 能力、且 `alk.cxykevin.top/config/get` 返回的配置里没有任何模型（`Model.Models` 为空）时，启动即进入**新手引导**。

引导与 `/connect` 向导**同义**：直接进入连接向导（选服务商模板 → 自动预填 base_url → 填 API key → 自动拉取模型列表 → 选择模型写入 `Model.Models.<n>` 并设为默认 `DefaultModelID`）→ 完成后选**第一个会话的推理强度**（写入本地配置，首个会话激活时自动应用，之后由 `/effort` 管理）→ 基本操作教学 → 完成进主页。向导中按 Esc 可随时跳过剩余步骤。

---

## ACP v2 兼容边界

协议 wire 类型和解码器位于 `internal/acp`。核心 JSON-RPC 和 session 生命周期已接入，未知合法 update、未知 content block 和未知枚举会被保留为 Raw 数据，不会静默破坏连接。畸形 JSON-RPC、无效 response 或 agent 异常退出会产生可诊断 backend error。

客户端当前声明的能力范围以 `ClientCapabilities` 为准。暂未实现的可选 agent→client 服务会收到 JSON-RPC `method not found`，不会伪造成功响应。

---

## 架构

```text
cmd/alcoh/main.go
  ├─ wscfg       默认 WebSocket 连接配置解析（alkaid0/helper 同源规则）
  └─ app
      ├─ term       跨平台终端/raw mode/尺寸事件
      ├─ view/model TUI 与纯状态机
      ├─ plugin     插件宿主：本地进程 + JSON-RPC + protobuf hooks
      │                （internal/plugin，协议见 proto/plugin/v1/plugin.proto）
      └─ acp
          ├─ rpc.go              JSON-RPC envelope 与 ID
          ├─ protocol_types.go   ACP method params/results
          ├─ updates.go          session/update 联合解码
          ├─ transport_desktop.go
          ├─ transport_wasm.go
          └─ client.go            Backend/Session
```

---

## 测试

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
GOOS=linux GOARCH=amd64 go build ./...
GOOS=darwin GOARCH=arm64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
```
