[![GitHub Repo stars](https://img.shields.io/github/stars/cxykevin/alcoh?style=flat&link=https%3A%2F%2Fgithub.com%2Fcxykevin%2Falcoh)](https://github.com/cxykevin/alcoh)
[![GitHub Release](https://img.shields.io/github/v/release/cxykevin/alcoh?include_prereleases&sort=semver&display_name=tag&style=flat)](https://github.com/cxykevin/alcoh/releases)
[![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/cxykevin/alcoh)](https://pkg.go.dev/github.com/cxykevin/alcoh)

# alcoh

alcoh 是一个全屏 TUI 的 ACP v2 客户端（类 opencode / Claude Code）。基于纯 Go 实现，手搓 ANSI 转义序列 + 双缓冲渲染，不使用任何 TUI 框架。默认通过 WebSocket 连接本地 alkaid0 ACP 服务；可用 `--demo` 切换为内置演示 backend。

设计理念：**轻量纯 Go** **手搓渲染** **跨平台** **协议兼容**

---

## 核心特性

- **ACP v2 双 transport**：JSON-RPC 2.0 over stdio 与 WebSocket 双 transport 客户端
- **完整 session 生命周期**：`initialize` / `initialized` 握手；`session/list`、`session/new`、`session/resume`、`session/prompt`、`session/cancel`、`session/delete`、`session/set_config_option`
- **时间线消息呈现**：以时间线顺序呈现消息、思考、工具调用、终端、计划、usage 与系统信息，工具不会统一堆到末尾；未知合法 update、content block 与枚举保留为 Raw，不破坏连接
- **权限弹窗**：`session/request_permission` 服务端请求与原始 JSON-RPC request ID 回包
- **健壮并发**：并发 pending request、乱序 response、EOF、agent 退出与 transport 关闭处理
- **纯 ANSI 双缓冲渲染**：CJK 宽字符、Markdown / 代码高亮、鼠标框选复制、1–6 行多行输入（`Shift+Enter` / 行尾 `\` 换行）
- **命令面板**：`/` 打开本地与 agent 命令面板；`/settings`、`Ctrl+,` 打开本地设置；`/effort`、`/model` 调整推理强度与切换模型
- **服务端配置编辑器**：`/server` 经 alkaid0 扩展 RPC 以多级子页面浏览/编辑服务端配置，编辑即自动保存
- **主页预创建会话**：让 `/effort` 与 `/model` 在主页命令面板直接可用
- **新手引导**：服务端无模型时启动进入全屏引导（选服务商 → 填模型表单 → 选推理强度 → 操作教学）
- **跨平台**：Linux、macOS、Windows Terminal 支持真实子进程与 WebSocket 连接；WASM/WASI 保留 demo

---

## 构建与运行

### 版本号（product）

版本信息由 `go:generate` 从 git 自动生成到 `product/build.go`（该文件进 `.gitignore`），
包含 `Version`（最新 tag，无 tag 时 `0.0.0`）、`VersionID`、`CommitID`、`BuildTime`
与 `BuildNote`（读环境变量 `ALCOH_BUILD_NOTE`）。每次发版打 tag 后重新生成即可：

```bash
go run ./product/gen.go
```

无 git 仓库或打 tag 前也能生成（版本回退 `0.0.0`、commit 回退 `unknown`），不中断构建。
生成的版本号已注入 ACP 握手 `ClientInfo.Version`，替代原先硬编码的 `"dev"`。

### Demo

```bash
go run ./cmd/alcoh --demo
go run ./cmd/alcoh --demo --fast
go run ./cmd/alcoh --dump 40
```

### 真实 ACP agent

真实模式需要显式提供 agent 可执行文件（stdio），或通过 `--ws-url` 连接远程 WebSocket agent。
**默认情况下**（不带任何 backend 相关参数）按 alkaid0/helper 的规则连接本地 WebSocket agent。命令参数必须逐项传入，不经过 shell 解析：

```bash
# 默认：连接本地 alkaid0 WebSocket 服务（规则与 alkaid0/helper 一致）
go run ./cmd/alcoh

# stdio 子进程
go run ./cmd/alcoh \
  --agent my-acp-agent \
  --agent-arg --stdio \
  --cwd "$PWD" \
  --env API_KEY=value

# 远程 WebSocket agent（如 alkaid0）
go run ./cmd/alcoh \
  --ws-url 'ws://127.0.0.1:7433/acp?token=a' \
  --cwd "$PWD"
```

可用参数：

- `--agent <path>`：ACP agent 可执行文件；与 `--ws-url` 互斥；
- `--agent-arg <arg>`：传给 agent 的单个 argv，可重复；
- `--ws-url <url>`：远程 ACP agent 的 WebSocket 地址（`ws://` 或 `wss://`），token 等认证参数并入 query string；与 `--agent` 互斥；
- `--config <path>`：WebSocket 配置文件路径（默认 `~/.config/alkaid0/config.json`），读取其中的 `server` 段；
- `--host <host>` / `--port <port>` / `--path <path>` / `--key <key>`：覆盖默认 WebSocket 连接的 host/端口/路径/认证 key；
- `--cwd <dir>`：新建 ACP 会话的工作目录，默认取 alcoh 进程当前目录（`--workdir` 为别名）；
- `--env KEY=VALUE`：追加环境变量，可重复；
- `--shutdown-timeout <duration>`：退出时等待 agent 的时长，默认 `2s`；
- `--demo`：使用内置 demo backend；
- `--dump N`：输出 N 帧 ANSI demo 渲染，不启动真实 agent。

#### 默认 WebSocket 连接

未指定 `--agent` / `--ws-url` / `--demo` 时，alcoh 默认连接本地 alkaid0 WebSocket 服务。地址解析优先级（低→高）与 `alkaid0/helper` 完全一致：

1. 硬编码默认值：`ws://127.0.0.1:7433/acp`；
2. 配置文件：系统级 `/etc/alkaid0/config.json` → 用户级 `~/.config/alkaid0/config.json`（后者覆盖前者，读取 `server` 段的 `host`/`port`/`path`/`key`）；
3. 环境变量：`ALKAID0_HELPER_HOST` / `ALKAID0_HELPER_PORT` / `ALKAID0_HELPER_PATH` / `ALKAID0_HELPER_KEY`（`ALKAID0_CONFIG_PATH` 可覆盖配置文件路径）；
4. 命令行 flag：`--config` / `--host` / `--port` / `--path` / `--key`（最高优先级，仅覆盖显式指定的 flag）。

认证 key 以 `key` 查询参数并入地址。未配置 key 时仍携带与 helper 一致的占位值 `<empty>`，使服务端能明确返回 401 而非无响应。连接日志中地址始终脱敏展示（`?key=***`）。

stdio agent 的 stdout 必须只输出换行分隔的 JSON-RPC 2.0 消息；日志请输出到 stderr。客户端会独立读取 stderr，不会把日志混入协议流。WebSocket transport 同样走 NDJSON 之外的 JSON-RPC 2.0 帧，且复用同一套 typed event 解码与生命周期管理。

---

## 配置

本地客户端设置保存到：

```bash
# Linux / macOS
~/.config/alcoh/config.json          # $XDG_CONFIG_HOME/alcoh/config.json，未设置时用 ~/.config
# Windows
%AppData%\alcoh\config.json
```

```json
{
    "version": 1,
    "colorMode": "auto",
    "terminalOutputLimit": 32768,
    "thinkingExpanded": true,
    "toolsExpanded": false,
    "onboardingEffort": ""
}
```

- `colorMode`：配色模式（`auto` / `light` / `dark`），本地设置里可切换；
- `thinkingExpanded` / `toolsExpanded`：思考与工具块默认展开状态；
- `terminalOutputLimit`：终端输出块截断阈值；
- `onboardingEffort`：新手引导选择的「第一个会话」推理强度，首个会话激活时应用后清空。

配置文件不存在、损坏或版本不兼容时安全回退默认值；保存采用原子写入。**ACP agent 配置只读展示，不写入本地配置。**

---

## 平台支持

| 平台 | 真实 ACP agent | 说明 |
|---|---:|---|
| Linux / macOS | ✅ | pure Go `os/exec` + stdio，或 WebSocket 远程连接 |
| Windows Terminal | ✅ | Windows Terminal VT + stdio，或 WebSocket 远程连接 |
| 浏览器 (GOOS=js) | ❌ | 可运行 demo；无本地子进程 |
| WASI (GOOS=wasip1) | ❌ | 可运行 demo；无本地子进程 |

---

## 会话管理

- **主页预创建会话**：进入主页（启动或 `/clear` 返回）时，若服务端公布 `session.delete` 能力，客户端同步预创建一个空会话（不进入会话视图），用它承载 agent 在 `session/new` 后广播的 `config_option_update`，使 `/effort` 与 `/model` 在主页命令面板直接可用。主页直接输入 prompt 回车时复用该会话作为用户的新会话（不删除、不新建）；恢复旧会话、程序退出或预创建会话确无用途时才把它删除，不在服务端残留。
- **会话恢复与删除**：首页选中会话按 `Enter` 恢复（`session/resume`），按 `d` 删除（`session/delete`，仅当 agent 声明对应能力时可用）。
- **打断与退出**：会话内按 `Esc` 打断正在进行的 AI 响应（`session/cancel`）；输入框为空时 `Ctrl+C` 首次提示、2 秒内再次按下才退出。

---

## 客户端命令

以 `/` 开头的输入会弹出命令面板，`Enter` / `Tab` 补全并执行本地命令。未匹配的斜杠命令原样作为 prompt 提交给 agent（按 agent 公布的能力出现）。

- `/alcoh_help`：显示命令帮助（输入框为空时按 `?` 亦可）
- `/clear [on]`：返回主页会话列表；默认先取消正在运行的会话，`on` 不取消直接返回
- `/effort [unset|low|medium|high|xhigh|max]`：设置推理强度。带参数直接经 `session/set_config_option` 写 `thought_level`；无参数弹出水平滑条（←→ 移动、Enter 确认、Esc 取消）。仅当 agent 公布 `thought_level` config 时可用
- `/model [value]`：切换模型。带参数直接经 `session/set_config_option`（`type=id`）写模型；无参数弹出垂直模型菜单（↑↓/滚轮 选择、Enter 确认、Esc 取消）。仅当 agent 公布 `category="model"`（或 `configId="model"`）config 时可用；候选值与当前值均取自服务端公布的 `options`/`currentValue`
- `/settings`：本地设置（`Ctrl+,` 亦可），切换 `colorMode` 等选项并即时保存
- `/server`：服务端配置编辑器，仅当服务端在 initialize 中声明 `alk.cxykevin.top/alkaid0/v0.4` 能力时出现

### 常用快捷键

| 按键 | 作用 |
|---|---|
| `?` | 命令帮助（输入框为空时） |
| `Ctrl+,` | 本地设置 |
| `Esc` | 会话内打断 AI 响应 / 关闭弹窗 |
| `Ctrl+C` | 复制选中文本 / 清空输入 / 连按两次退出 |
| `Ctrl+q` | 退出确认 |
| `Tab` | 切换输入框 / 消息区焦点 |
| `x` | 从会话视图返回主页（输入框为空时） |
| `d` | 主页删除选中会话（输入框为空时） |

### 服务端配置编辑器（`/server`）

经 `alk.cxykevin.top/config/get` 拉取完整配置，以**多级子页面**导航浏览（每次展示一个对象/数组的直接子项，`Enter` 进入下级、`←` 返回上级），字段名优先显示中文名，未收录的显示服务端 Go 结构体硬编码的原始 JSON 名称。`↑↓`/滚轮 选择、`Enter` 进入子页面/编辑标量、`←` 返回、`r` 重新拉取。

- 仅 `Model.Models`、`Agent.Agents`、`Context.LSP.LanguageServers` 与 `Context.Phrase.Phrases` 页末尾有「(新增)」行（灰色）；Models 自动分配下一个数字键（`0` 起）并带 `ModelName` 默认字段；Agents / LanguageServers 输入名称键（子代理名 / 文件扩展名如 `.go`）；Phrases 是数组，直接追加 `{Short,Text,Desc}` 空元素。新增写回成功后触发整配置重载（`config/get`）并重定向到新项子页。
- 任意模型项、子代理项、语言服务器项或短语项的子页末尾有「(删除该项)」行（红色），删除后自动返回集合页（对象键以 `null` 键写回，服务端 config/set 对 map 字段的 `null` 键真正删除）。
- **编辑即自动保存**：标量修改、数组增删、对象键增改立即经 `alk.cxykevin.top/config/set` 部分更新并持久化。多个连续改动排队串行写回（防止乱序覆盖），全部写回完成后触发整配置重载以服务端为准重建界面，期间底部显示「保存中…」、其他按键被忽略（仅 `Esc` 可关闭）。重载完成后恢复可编辑且导航位置、选中行与正在进行的编辑均保留。密钥字段（`key`/`ProviderKey`）值脱敏展示。
- 状态栏右侧的 model 名称优先取 session-info 的 `model` 字段，否则回退到该配置当前值的显示名。

---

## 新手引导

启动时未指定 backend 参数（走默认 alkaid0 WebSocket 连接）、服务端声明 alkaid0 能力、且 `alk.cxykevin.top/config/get` 返回的配置里没有任何模型（`Model.Models` 为空）时，启动即进入**全屏新手引导**：

欢迎 → 选服务商（Deepseek / OpenAI / S3AI Api，自动预填提供方 URL）→ 填模型表单（提供方 URL、密钥、模型名、模型 ID、Token 上限、压缩阈值，六项全部必填；密钥掩码显示，提交即写入 `Model.Models.<0>` 并设默认模型 `DefaultModelID=0`）→ 结果页可选中按钮「打开 /server 详细配置」（定位到 `Config/Model/Models`，关闭后回到引导）→ 选**第一个会话的推理强度**（写入本地配置，首个会话激活时自动应用，之后由 `/effort` 管理）→ 基本操作教学 → 完成进主页。

引导结束才创建主页预创建会话；引导中 `Esc` 可跳过且**不写标记**（只要服务端仍无模型，下次启动仍显示）。

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
GOOS=js GOARCH=wasm go build -o /tmp/alcoh.wasm ./cmd/alcoh
GOOS=wasip1 GOARCH=wasm go build -o /tmp/alcoh-wasi.wasm ./cmd/alcoh
```
