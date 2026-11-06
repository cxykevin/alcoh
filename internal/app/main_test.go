package app

import (
	"os"
	"testing"

	"github.com/cxykevin/alcoh/internal/i18n"
)

// TestMain 把测试进程的界面语言固定为中文。
//
// 服务端配置编辑器的展示顺序按"翻译后的显示名"排序（见 model.configNodeLess），
// 而配置页用例的按键导航是按中文布局硬编码的（根页 [Server, Model, Version] 等）。
// 不固定语言时，结果取决于运行环境的 locale：本机默认中文没问题，但 CI 的
// macOS runner 上语言检测为英文，顺序变成 [Model, Server, Version]，↓ 会落到
// Server 上，后续 Enter 写回的就成了其它字段。
func TestMain(m *testing.M) {
	i18n.SetLang(i18n.Zh)
	os.Exit(m.Run())
}
