package app

import (
	"context"
	"errors"
	"time"

	"github.com/cxykevin/alcoh/internal/clipboard"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
)

// clipboardTimeout 是读取系统剪贴板的超时：剪贴板命令异常时不要让 UI 卡住。
const clipboardTimeout = 3 * time.Second

// clipboardRead 是读取系统剪贴板的间接层，测试可替换。
var clipboardRead = clipboard.Read

// pasteClipboard 处理会话输入框的 Ctrl+V：剪贴板有图片时插入 [Image]
// 占位符（发送时转成 ACP image 块），否则插入文本。
func (a *App) pasteClipboard() {
	m := a.model
	if m.Input == nil || m.View != model.ViewSession || m.Focus != model.FocusInput {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()
	content, err := clipboardRead(ctx)
	if err != nil {
		if errors.Is(err, clipboard.ErrEmpty) {
			return
		}
		m.ShowError(i18n.T("读取剪贴板失败: %s", err.Error()))
		return
	}
	if content.Image != nil {
		if m.Input.InsertImage(content.Image.MIME, content.Image.Data) {
			m.UpdateSlashState()
			m.ShowInfo(i18n.T("已粘贴图片"))
		}
		return
	}
	if content.Text != "" {
		m.Input.InsertText(content.Text)
		m.UpdateSlashState()
	}
}
