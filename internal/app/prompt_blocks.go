package app

import (
	"bytes"
	"encoding/base64"
	"strings"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/widget"
)

// buildPromptBlocks 把输入框文本与图片附件转换成 ACP prompt 内容块。
//
// 文本里的每个 [Image] 占位符按出现顺序消费一个图片附件；占位符被删除后
// 对应图片不再出现在结果里，即取消发送。多余（没有对应附件）的占位符
// 保留为普通文本，便于用户手动输入方括号内容。
func buildPromptBlocks(text string, images []widget.Image) []acp.ContentBlock {
	var blocks []acp.ContentBlock
	appendText := func(s string) {
		if s == "" {
			return
		}
		blocks = append(blocks, acp.TextBlock(s))
	}
	for {
		idx := strings.Index(text, widget.ImagePlaceholder)
		if idx < 0 || len(images) == 0 {
			break
		}
		appendText(text[:idx])
		img := images[0]
		images = images[1:]
		data := base64.StdEncoding.EncodeToString(img.Data)
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, acp.ContentBlock{Type: "image", Data: &data, MimeType: &mime})
		text = text[idx+len(widget.ImagePlaceholder):]
	}
	appendText(text)
	return blocks
}

// samePromptImages 判断两组图片附件是否一致，用于"运行中提交先停止再发送"
// 期间确认用户没有改动输入框里的图片。
func samePromptImages(a, b []widget.Image) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].MIME != b[i].MIME || !bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}
