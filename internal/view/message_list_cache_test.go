package view

import (
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// TestMessageListCacheReflectsUpdates 验证复用同一个 MessageList（正文渲染缓存
// 跨帧保留，见 AppView.bodyMessageList）时，内容变化仍然会重绘：流式追加、
// 思考展开/折叠都必须出现在下一帧，不能因为命中缓存而停留在旧画面。
func TestMessageListCacheReflectsUpdates(t *testing.T) {
	theme := renderer.DefaultTheme()
	s := model.NewSession("s1", "会话")
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1",
		Message: acp.Message{MessageID: "a1", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("第一条助手消息")}}},
	})
	s.ApplyMessage(&acp.MessageUpdateEvent{
		SessionID: "s1", IsThought: true,
		Message: acp.Message{MessageID: "t1", ContentSet: true,
			Content: []acp.ContentBlock{{Type: "text", Text: new("思考正文内容")}}},
	})

	ml := &MessageList{Theme: theme}
	draw := func(w, h int) string {
		b := renderer.NewBuffer(w, h)
		ml.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), s)
		return strings.Join(bufferText(b), "\n")
	}

	if got := draw(60, 12); !strings.Contains(got, "第一条助手消息") {
		t.Fatalf("first frame missing assistant text:\n%s", got)
	}
	// 流式追加：缓存必须失效并重绘。
	s.AppendChunk(&acp.MessageChunkEvent{SessionID: "s1", MessageID: "a1", Text: "（追加片段）"})
	if got := draw(60, 12); !strings.Contains(got, "（追加片段）") {
		t.Fatalf("cached frame missed appended chunk:\n%s", got)
	}
	// 思考完成后默认折叠：正文不显示。
	if got := draw(60, 12); strings.Contains(got, "思考正文内容") {
		t.Fatalf("collapsed thought should not render body:\n%s", got)
	}
	// 展开思考：正文必须出现（缓存需按展开态失效）。
	s.ToggleMessage("t1")
	if got := draw(60, 12); !strings.Contains(got, "思考正文内容") {
		t.Fatalf("expanded thought body missing (stale cache):\n%s", got)
	}
}
