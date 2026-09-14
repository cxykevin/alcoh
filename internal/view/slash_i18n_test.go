package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/i18n"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
	"github.com/cxykevin/alcoh/internal/widget"
)

// TestSlashPanelLocalizesServerCommandDescription 验证命令面板中服务端命令的
// 描述：服务端声明 alkaid0 v0.4 私有能力时渲染客户端译文，未声明该能力的
// 第三方服务端原样渲染服务端英文原文。
func TestSlashPanelLocalizesServerCommandDescription(t *testing.T) {
	old := i18n.Current()
	i18n.SetLang(i18n.Zh)
	t.Cleanup(func() { i18n.SetLang(old) })

	m := model.New()
	m.ActivateSession("s1", "会话")
	m.Active.Commands = []acp.AvailableCommand{{Name: "compress", Description: "Compress the history"}}
	m.Input = widget.NewInputBuffer()
	for _, r := range "/comp" {
		m.Input.InsertRune(r)
	}
	m.UpdateSlashState()

	draw := func() string {
		const w, h = 90, 24
		b := renderer.NewBuffer(w, h)
		NewAppView(renderer.DefaultTheme()).Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), m)
		return strings.Join(bufferText(b), "\n")
	}

	if got := draw(); !strings.Contains(got, "Compress the history") {
		t.Fatalf("server description should stay as-is without alkaid0 capability:\n%s", got)
	}
	m.SetAgentInfo(acp.AgentInfo{Name: "alkaid0", Version: "test"}, acp.AgentCapabilities{
		Raw: json.RawMessage(`{"alk.cxykevin.top/alkaid0/v0.4":{}}`)})
	got := draw()
	if !strings.Contains(got, "压缩历史记录") {
		t.Fatalf("localized server description missing from slash panel:\n%s", got)
	}
	if strings.Contains(got, "Compress the history") {
		t.Fatalf("server text should be replaced by the client translation:\n%s", got)
	}
}
