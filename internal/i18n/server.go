package i18n

// 服务端（alkaid0）私有文本的客户端翻译。
//
// alkaid0 下发的命令描述等文本只有英文原文（服务端不做 i18n）。服务端在
// initialize 中声明 alk.cxykevin.top/alkaid0/v0.4 私有能力后，客户端按当前
// 界面语言自行翻译这些文本：中文界面查下面的对照表（英文原文 → 中文），英文
// 界面直接用服务端原文，因此服务端更新文案时英文界面自动跟随；中文界面未
// 收录的条目同样回退服务端原文（宁可显示英文，也不猜译）。
//
// 对照表的键必须与 alkaid0 "server/actions/commands.go" 中 commandMaps 各项的
// Description 逐字一致（含标点、空格与大小写）；服务端改动文案后，此处未同步
// 的条目会回退英文原文，等待同步。每条上方的注释标明它属于哪条命令，便于核对。
var serverZh = map[string]string{
	// /background
	"Set background mode on/off — keep session alive after all clients disconnect": "开启/关闭后台模式——所有客户端断开后保持会话存活",
	// /compress
	"Compress the history": "压缩历史记录",
	// /feedback
	"Submit feedback to the feedback server": "向反馈服务器提交反馈",
	// /help
	"Show this help message": "显示本帮助信息",
	// /index
	"Build codebase index (extract LSP symbols → submit embedding tasks). Subcommands: clean (clear db), status (show progress), cancel (stop running index), lsp-reset (reset LSP fail counters)": "构建代码库索引（提取 LSP 符号 → 提交嵌入任务）。子命令：clean（清空数据库）、status（查看进度）、cancel（停止正在运行的索引）、lsp-reset（重置 LSP 失败计数）",
	// /init
	"Analyze the codebase and generate an AGENTS.md guidance file": "分析代码库并生成 AGENTS.md 指导文件",
	// /mask
	"Manage custom mask values — add <value> masks a value outbound and restores it in the response, del <value> stops masking it": "管理自定义掩码值——add <value> 在发送时掩码该值并在响应中还原，del <value> 停止掩码",
	// /reload
	"Reload config file from disk": "从磁盘重新加载配置文件",
	// /s
	"Send a configured phrase — /s <short> expands the phrase to its full text and sends it to the model; /s with no args lists all configured phrases": "发送预置短语——/s <short> 把短语展开为完整文本后发给模型；不带参数则列出全部已配置短语",
	// /title
	"Set the conversation title, or reset it (no args) to fall back to the AI-generated title": "设置会话标题；不带参数则重置，回退到 AI 生成的标题",
	// /usage
	"Show global token usage statistics, or reset them": "查看全局 token 用量统计，或重置统计",
	// /version
	"Show Alkaid0 version information": "显示 Alkaid0 版本信息",
}

// TServer 返回服务端下发文本（如命令描述）在当前界面语言下的展示文本。
// 中文界面查对照表，其它语言与未收录文本一律返回服务端原文。
func TServer(s string) string {
	if Current() != Zh {
		return s
	}
	if zh, ok := serverZh[s]; ok {
		return zh
	}
	return s
}
