package app

import (
	"os"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
)

// promptText 把 ACP prompt 内容块折叠成测试断言用的文本；图片块按
// [Image] 占位符还原。
func promptText(blocks []acp.ContentBlock) string {
	var sb strings.Builder
	for _, blk := range blocks {
		if blk.Text != nil {
			sb.WriteString(*blk.Text)
			continue
		}
		if blk.Type == "image" {
			sb.WriteString(acp.ImagePlaceholder)
		}
	}
	return sb.String()
}

// TestMain 把测试进程的界面语言固定为中文。
//
// 服务端配置编辑器的展示顺序按"翻译后的显示名"排序（见 model.configNodeLess），
// 而配置页用例的按键导航是按中文布局硬编码的（根页 [Server, Model, Version]、
// Model 子页 [Models, ProviderKey, DefaultModelID] 等）。界面语言取自运行环境的
// locale（i18n.Detect：配置 → ALCOH_LANG → LC_ALL/LC_MESSAGES/LANG），而 saveSettings
// 每次保存本地配置都会重新检测一次：CI 的 macOS runner 上 locale 是英文，
// 于是语言被切成 en、根页顺序变成 [Model, Server, Version]，↓ 落到 Server 上，
// 随后的 Enter 写回就成了 {"Server":{"key":""}}（用例报 patch Model = <nil>）。
//
// 这里把所有 locale 环境变量都设成中文（Detect 只有在环境值解析为中文以外时
// 才提前返回，所以每个都要覆盖）。需要英文的用例自己 SetLang/再还原即可。
func TestMain(m *testing.M) {
	for _, env := range []string{"ALCOH_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		os.Setenv(env, "zh_CN.UTF-8")
	}
	i18n.SetLang(i18n.Zh)
	os.Exit(m.Run())
}
