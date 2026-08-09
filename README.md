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

# alcoh

alcoh 是一个全屏 TUI 的 ACP v2 客户端（类 opencode / Claude Code）。基于纯 Go 实现。默认通过 WebSocket 连接本地 alkaid0 ACP 服务。

alcoh 实现了大部分的 ACP v2 协议内容，因此可以通过命令行

---

## 核心特性

- **ACP v2 双 transport**：JSON-RPC 2.0 over stdio 与 WebSocket 双 transport 客户端
- **终端体验**：CJK 宽字符、Markdown / 代码高亮、鼠标框选复制
- **命令面板**：`/` 打开本地与 agent 命令面板；`/settings` 打开本地设置；`/effort`、`/model` 调整推理强度与切换模型
- **服务端配置编辑器**：`/server` 经 alkaid0 扩展 RPC 浏览/编辑服务端配置，编辑即自动保存（只适配 alkaid0 后端）
- **新手引导**：启动进入全屏引导（选服务商 → 填模型表单 → 选推理强度 → 操作教学）（只适配 alkaid0 后端）
- **跨平台**：Linux、macOS、Windows Terminal

---

### 真实 ACP agent

真实模式需要显式提供 agent 可执行文件（stdio），或通过 `--ws-url` 连接远程 WebSocket agent。
**默认情况下**（不带任何 backend 相关参数）按 alkaid0/helper 的规则连接本地 WebSocket agent。命令参数必须逐项传入，不经过 shell 解析：

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

---

## 配置

本地客户端设置保存到：

```bash
# Linux / macOS
~/.config/alcoh/config.json
# Windows
%AppData%\alcoh\config.json
```

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
| `Ctrl+q` | 退出确认 |
| `d` | 删除会话 |

---

## 新手引导

启动时未指定 backend 参数（走默认 alkaid0 WebSocket 连接）、服务端声明 alkaid0 能力、且 `alk.cxykevin.top/config/get` 返回的配置里没有任何模型（`Model.Models` 为空）时，启动即进入**全屏新手引导**：

欢迎 → 选服务商（Deepseek / OpenAI / ...，自动预填提供方 URL）→ 填模型表单（提供方 URL、密钥、模型名、模型 ID、Token 上限、压缩阈值，六项全部必填；密钥掩码显示，提交即写入 `Model.Models.<0>` 并设默认模型 `DefaultModelID=0`）→ 结果页可选中按钮「打开 /server 详细配置」（定位到 `Config/Model/Models`，关闭后回到引导）→ 选**第一个会话的推理强度**（写入本地配置，首个会话激活时自动应用，之后由 `/effort` 管理）→ 基本操作教学 → 完成进主页。

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
```
