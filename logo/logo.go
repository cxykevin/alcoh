// Package logo 嵌入终端展示用的 ascii logo 源文件（logo/logo.ansi），
// 供界面在配置编辑器根页面等位置绘制。
package logo

import _ "embed"

//go:embed logo.ansi
var Ansi string
