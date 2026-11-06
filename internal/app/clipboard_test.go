package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/clipboard"
	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/widget"
)

// blockSession 记录收到的 ACP prompt 内容块，用于验证图片按协议发送。
type blockSession struct {
	acp.Session
	id      string
	mu      sync.Mutex
	prompts [][]acp.ContentBlock
}

func (s *blockSession) ID() string    { return s.id }
func (s *blockSession) Title() string { return "" }

func (s *blockSession) SendPrompt(_ context.Context, prompt []acp.ContentBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, append([]acp.ContentBlock(nil), prompt...))
	return nil
}

func (s *blockSession) sent() [][]acp.ContentBlock {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]acp.ContentBlock(nil), s.prompts...)
}

// withClipboardRead 替换剪贴板读取实现，测试结束后恢复。
func withClipboardRead(t *testing.T, read func(context.Context) (clipboard.Content, error)) {
	t.Helper()
	old := clipboardRead
	clipboardRead = read
	t.Cleanup(func() { clipboardRead = old })
}

// TestCtrlVPastesImagePlaceholder 验证 Ctrl+V 把剪贴板图片插入输入框：
// 显示为 [Image] 占位符，图片附件保存在缓冲里。
func TestCtrlVPastesImagePlaceholder(t *testing.T) {
	withClipboardRead(t, func(context.Context) (clipboard.Content, error) {
		return clipboard.Content{Image: &clipboard.Image{MIME: "image/png", Data: []byte("png")}}, nil
	})
	a := New(newFakeTerm(), demo.New(true))
	a.model.ActivateSession("s1", "")
	a.model.Input = widget.NewInputBuffer()

	a.dispatchKey(input.RuneKey('v', input.ModCtrl))

	if got := a.model.Input.Text(); got != widget.ImagePlaceholder {
		t.Fatalf("input = %q, want %q", got, widget.ImagePlaceholder)
	}
	imgs := a.model.Input.Images()
	if len(imgs) != 1 || imgs[0].MIME != "image/png" {
		t.Fatalf("images = %#v, want one image/png", imgs)
	}
}

// TestCtrlVPastesText 验证剪贴板没有图片时按普通文本粘贴。
func TestCtrlVPastesText(t *testing.T) {
	withClipboardRead(t, func(context.Context) (clipboard.Content, error) {
		return clipboard.Content{Text: "粘贴的文字"}, nil
	})
	a := New(newFakeTerm(), demo.New(true))
	a.model.ActivateSession("s1", "")
	a.model.Input = widget.NewInputBuffer()

	a.dispatchKey(input.RuneKey('v', input.ModCtrl))

	if got := a.model.Input.Text(); got != "粘贴的文字" {
		t.Fatalf("input = %q, want 粘贴的文字", got)
	}
	if got := a.model.Input.Images(); len(got) != 0 {
		t.Fatalf("images = %#v, want none", got)
	}
}

// TestSubmitSendsImageContentBlocks 验证输入框里的 [Image] 占位符在提交时
// 转换成 ACP image 内容块（base64 data + mimeType），而不是普通文本。
func TestSubmitSendsImageContentBlocks(t *testing.T) {
	setConfigDir(t)
	a := New(newFakeTerm(), demo.New(true))
	a.runCtx = context.Background()
	s := &blockSession{id: "s1"}
	a.sess = s
	a.model.ActivateSession("s1", "")
	a.model.Input = widget.NewInputBuffer()
	a.model.Input.InsertText("看图 ")
	if !a.model.Input.InsertImage("image/png", []byte("png-bytes")) {
		t.Fatal("InsertImage failed")
	}

	a.submitPrompt()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(s.sent()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	prompts := s.sent()
	if len(prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(prompts))
	}
	blocks := prompts[0]
	if len(blocks) != 2 {
		t.Fatalf("blocks = %#v, want text + image", blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text == nil || *blocks[0].Text != "看图 " {
		t.Fatalf("first block = %#v, want text 看图 ", blocks[0])
	}
	if blocks[1].Type != "image" || blocks[1].Data == nil || blocks[1].MimeType == nil {
		t.Fatalf("second block = %#v, want image with data/mimeType", blocks[1])
	}
	if *blocks[1].MimeType != "image/png" {
		t.Fatalf("mime = %q, want image/png", *blocks[1].MimeType)
	}
}
