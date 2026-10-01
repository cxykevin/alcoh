package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// alkaid0TestSession 创建声明 alkaid0 私有协议 v0.4 的会话。
func alkaid0TestSession(id string) *model.SessionState {
	s := model.NewSession(id, "session")
	s.SetAlkaid0V04(true)
	return s
}

// applyAlkaid0ToolCall 应用一次 alkaid0 工具调用：content 与服务端推送同形
// （首个文本块是全参数预览 + alk.cxykevin.top/calling_info 参数块）。
func applyAlkaid0ToolCall(t *testing.T, s *model.SessionState, id, name string, args map[string]any) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	preview := "Path: preview\n"
	done := acp.ToolCompleted
	title := "[Call " + name + "]" + id
	s.ApplyToolCall(&acp.ToolCallUpdateEvent{
		SessionID: s.ID, ToolCallID: id, Status: &done, Title: &title,
		ContentSet: true,
		Content: []acp.ToolCallContent{
			{Type: "content", Content: &acp.ContentBlock{Type: "text", Text: &preview}},
			{Type: acp.ToolCallingInfoType, Name: name, Args: raw},
		},
	})
}

// drawText 把会话画进给定尺寸的缓冲区并按行返回文本。
func drawText(t *testing.T, s *model.SessionState, w, h int) string {
	t.Helper()
	ml := &MessageList{Theme: renderer.DefaultTheme()}
	b := renderer.NewBuffer(w, h)
	ml.Draw(renderer.NewCanvas(b), renderer.NewRect(0, 0, w, h), s)
	return strings.Join(bufferText(b), "\n")
}

// TestAlkaid0ToolCallTitleEdit 验证 edit 标题为 Edit(path)，正文只展开
// 标题未消费的其余参数。
func TestAlkaid0ToolCallTitleEdit(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "edit", map[string]any{
		"path": "src/main.go", "target": "append", "text": "hello",
	})
	got := drawText(t, s, 80, 20)
	if !strings.Contains(got, "Edit(src/main.go)") {
		t.Fatalf("title should be Edit(path):\n%s", got)
	}
	if strings.Contains(got, "[Call edit]") {
		t.Fatalf("server title should be replaced:\n%s", got)
	}
	if !strings.Contains(got, "Target: append") || !strings.Contains(got, "Text: hello") {
		t.Fatalf("body should show remaining args:\n%s", got)
	}
	if strings.Contains(got, "Path: src/main.go") || strings.Contains(got, "Path: preview") {
		t.Fatalf("body should not repeat title args:\n%s", got)
	}
}

// TestAlkaid0ToolCallTitleRun 验证 run 标题为 Run {type}({reason})，
// 后台任务在 type 后加 *；正文只展开 command/sandbox 等其余参数。
func TestAlkaid0ToolCallTitleRun(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "run", map[string]any{
		"type": "shell", "reason": "serve the app", "command": "npm run dev",
		"background": true, "sandbox": false,
	})
	got := drawText(t, s, 100, 20)
	if !strings.Contains(got, "Run shell*(serve the app)") {
		t.Fatalf("background run title should carry *:\n%s", got)
	}
	if !strings.Contains(got, "Command: npm run dev") || !strings.Contains(got, "Sandbox: false") {
		t.Fatalf("body should show remaining run args:\n%s", got)
	}
	if strings.Contains(got, "Reason:") || strings.Contains(got, "Type:") || strings.Contains(got, "Background:") {
		t.Fatalf("body should not repeat title args:\n%s", got)
	}

	// 前台 sleep：标题用 reason，数字 command 按 JSON 原样进正文，不带 *。
	s2 := alkaid0TestSession("s2")
	applyAlkaid0ToolCall(t, s2, "c2", "run", map[string]any{
		"type": "sleep", "reason": "wait", "command": 5,
	})
	got2 := drawText(t, s2, 100, 20)
	if !strings.Contains(got2, "Run sleep(wait)") {
		t.Fatalf("foreground run title mismatch:\n%s", got2)
	}
	if !strings.Contains(got2, "Command: 5") {
		t.Fatalf("body should show command:\n%s", got2)
	}
}

// TestAlkaid0ToolCallTitleSearch 验证 search 标题为 Search({query})，
// online=true 时显示 Search online({query})。
func TestAlkaid0ToolCallTitleSearch(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "search", map[string]any{
		"query": "golang", "online": true, "max_results": 10,
	})
	got := drawText(t, s, 100, 20)
	if !strings.Contains(got, "Search online(golang)") {
		t.Fatalf("online search title mismatch:\n%s", got)
	}
	if !strings.Contains(got, "Max_results: 10") {
		t.Fatalf("body should show remaining search args:\n%s", got)
	}
	if strings.Contains(got, "Query:") || strings.Contains(got, "Online:") {
		t.Fatalf("body should not repeat title args:\n%s", got)
	}

	s2 := alkaid0TestSession("s2")
	applyAlkaid0ToolCall(t, s2, "c2", "search", map[string]any{
		"query": "golang", "online": false, "recursive": false,
	})
	got2 := drawText(t, s2, 100, 20)
	if !strings.Contains(got2, "Search(golang)") || strings.Contains(got2, "Search online") {
		t.Fatalf("local search title mismatch:\n%s", got2)
	}
	if !strings.Contains(got2, "Recursive: false") {
		t.Fatalf("body should show remaining search args:\n%s", got2)
	}
}

// TestAlkaid0ToolCallTitleRead 验证 read 标题为 Read(path)；流式预览期
// 服务端把 path 放在 name 键时也要能取到。
func TestAlkaid0ToolCallTitleRead(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "read", map[string]any{
		"path": "src/main.go", "unread": true,
	})
	got := drawText(t, s, 100, 20)
	if !strings.Contains(got, "Read(src/main.go)") {
		t.Fatalf("read title mismatch:\n%s", got)
	}
	if !strings.Contains(got, "Unread: true") {
		t.Fatalf("body should show unread arg:\n%s", got)
	}

	s2 := alkaid0TestSession("s2")
	applyAlkaid0ToolCall(t, s2, "c2", "read", map[string]any{
		"name": "src/other.go",
	})
	if got := drawText(t, s2, 100, 20); !strings.Contains(got, "Read(src/other.go)") {
		t.Fatalf("read title should fall back to name key:\n%s", got)
	}
}

// TestAlkaid0ToolCallTitleOtherTools 验证其它工具按同样规则处理：表里
// 有主参数的工具拼进标题，没有的工具只写工具名，参数全部留给正文。
func TestAlkaid0ToolCallTitleOtherTools(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "fetch", map[string]any{
		"method": "GET", "url": "https://example.com", "timeout": 30,
	})
	got := drawText(t, s, 100, 20)
	if !strings.Contains(got, "Fetch(GET https://example.com)") {
		t.Fatalf("fetch title mismatch:\n%s", got)
	}
	if !strings.Contains(got, "Timeout: 30") {
		t.Fatalf("body should show remaining fetch args:\n%s", got)
	}

	s2 := alkaid0TestSession("s2")
	applyAlkaid0ToolCall(t, s2, "c2", "activate_agent", map[string]any{
		"name": "reviewer", "prompt": "review the diff",
	})
	got2 := drawText(t, s2, 100, 20)
	if !strings.Contains(got2, "Use Agent(reviewer)") {
		t.Fatalf("activate_agent title mismatch:\n%s", got2)
	}
	if !strings.Contains(got2, "Prompt: review the diff") {
		t.Fatalf("body should show activate prompt:\n%s", got2)
	}

	// deactivate_agent 与 activate_agent 同格式。
	s3 := alkaid0TestSession("s3")
	applyAlkaid0ToolCall(t, s3, "c3", "deactivate_agent", map[string]any{
		"name": "reviewer", "prompt": "stop",
	})
	got3 := drawText(t, s3, 100, 20)
	if !strings.Contains(got3, "Deactivate Agent(reviewer)") || !strings.Contains(got3, "Prompt: stop") {
		t.Fatalf("deactivate_agent rendering mismatch:\n%s", got3)
	}

	// 未收录的工具（插件/第三方工具）：只写工具名，参数全部留给正文。
	s4 := alkaid0TestSession("s4")
	applyAlkaid0ToolCall(t, s4, "c4", "plugin_tool", map[string]any{"foo": "bar"})
	got4 := drawText(t, s4, 100, 20)
	if !strings.Contains(got4, "Plugin_tool()") || !strings.Contains(got4, "Foo: bar") {
		t.Fatalf("unknown tool rendering mismatch:\n%s", got4)
	}
}

// TestAlkaid0ToolCallKeepsDiff 验证私有渲染只跳过服务端的参数预览文本块，
// diff 等内容块照旧展示。
func TestAlkaid0ToolCallKeepsDiff(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "edit", map[string]any{
		"path": "src/main.go", "text": "hello",
	})
	diff := "@@ -1 +1 @@\n-old\n+new"
	s.ToolCalls["c1"].Content = append(s.ToolCalls["c1"].Content,
		acp.ToolCallContent{Type: "diff", Text: &diff})
	got := drawText(t, s, 100, 20)
	if !strings.Contains(got, "+new") || !strings.Contains(got, "-old") {
		t.Fatalf("diff block should stay visible:\n%s", got)
	}
	if strings.Contains(got, "Path: preview") {
		t.Fatalf("args preview text block should be skipped:\n%s", got)
	}
}

// TestAlkaid0ToolCallFallbackWithoutCapability 验证服务端未声明 v0.4 时
// 保持服务端标题与完整参数文本块。
func TestAlkaid0ToolCallFallbackWithoutCapability(t *testing.T) {
	s := model.NewSession("s1", "session")
	applyAlkaid0ToolCall(t, s, "c1", "edit", map[string]any{"path": "src/main.go"})
	got := drawText(t, s, 100, 20)
	if !strings.Contains(got, "[Call edit]c1") {
		t.Fatalf("server title should be kept without v0.4:\n%s", got)
	}
	if !strings.Contains(got, "Path: preview") {
		t.Fatalf("server param preview should render without v0.4:\n%s", got)
	}
}

// TestAlkaid0CallingInfoHiddenByDefault 验证 calling_info 参数块属于协议细节：
// 默认不渲染它的标签行（参数已由标题与私有正文消费），「显示协议细节」打开
// 后才作为诊断信息显示。
func TestAlkaid0CallingInfoHiddenByDefault(t *testing.T) {
	s := alkaid0TestSession("s1")
	applyAlkaid0ToolCall(t, s, "c1", "edit", map[string]any{
		"path": "src/main.go", "target": "append", "text": "hello",
	})
	if got := drawText(t, s, 80, 20); strings.Contains(got, acp.ToolCallingInfoType) {
		t.Fatalf("calling_info block should be hidden by default:\n%s", got)
	}
	s.ShowProtocolDetails = true
	if got := drawText(t, s, 80, 20); !strings.Contains(got, acp.ToolCallingInfoType) {
		t.Fatalf("calling_info block should show when protocol details are on:\n%s", got)
	}
}
