package app

import (
	"encoding/base64"
	"testing"

	"github.com/cxykevin/alcoh/internal/acp"
	"github.com/cxykevin/alcoh/internal/widget"
)

func decodeImageData(t *testing.T, blk acp.ContentBlock) []byte {
	t.Helper()
	if blk.Type != "image" || blk.Data == nil || blk.MimeType == nil {
		t.Fatalf("block = %#v, want image with data and mimeType", blk)
	}
	data, err := base64.StdEncoding.DecodeString(*blk.Data)
	if err != nil {
		t.Fatalf("image data is not valid base64: %v", err)
	}
	return data
}

func TestBuildPromptBlocksInterleavesTextAndImages(t *testing.T) {
	images := []widget.Image{{MIME: "image/png", Data: []byte("png")}}
	blocks := buildPromptBlocks("看图 "+widget.ImagePlaceholder+" 描述", images)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %#v, want 3 blocks", blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text == nil || *blocks[0].Text != "看图 " {
		t.Fatalf("first block = %#v, want text 看图 ", blocks[0])
	}
	if got := decodeImageData(t, blocks[1]); string(got) != "png" {
		t.Fatalf("image data = %q, want png", got)
	}
	if *blocks[1].MimeType != "image/png" {
		t.Fatalf("mime = %q, want image/png", *blocks[1].MimeType)
	}
	if blocks[2].Type != "text" || blocks[2].Text == nil || *blocks[2].Text != " 描述" {
		t.Fatalf("last block = %#v, want text  描述", blocks[2])
	}
}

func TestBuildPromptBlocksDeletedPlaceholderCancelsThatImage(t *testing.T) {
	// 输入框里原本有 A、B 两个占位符，用户删掉了第一个：Images 只返回 B。
	blocks := buildPromptBlocks("第二张 "+widget.ImagePlaceholder, []widget.Image{
		{MIME: "image/jpeg", Data: []byte("B")},
	})
	var image acp.ContentBlock
	for _, blk := range blocks {
		if blk.Type == "image" {
			image = blk
		}
	}
	if image.Type != "image" {
		t.Fatalf("blocks = %#v, want one image block", blocks)
	}
	if got := decodeImageData(t, image); string(got) != "B" {
		t.Fatalf("image data = %q, want B", got)
	}
}

func TestBuildPromptBlocksWithoutImagesKeepsPlaceholderText(t *testing.T) {
	blocks := buildPromptBlocks("手动输入 "+widget.ImagePlaceholder, nil)
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatalf("blocks = %#v, want a single text block", blocks)
	}
	if got := *blocks[0].Text; got != "手动输入 "+widget.ImagePlaceholder {
		t.Fatalf("text = %q", got)
	}
}

func TestBuildPromptBlocksImageOnly(t *testing.T) {
	blocks := buildPromptBlocks(widget.ImagePlaceholder, []widget.Image{{MIME: "image/png", Data: []byte("x")}})
	if len(blocks) != 1 || blocks[0].Type != "image" {
		t.Fatalf("blocks = %#v, want a single image block", blocks)
	}
}

func TestSamePromptImages(t *testing.T) {
	a := []widget.Image{{MIME: "image/png", Data: []byte("x")}}
	if !samePromptImages(a, []widget.Image{{MIME: "image/png", Data: []byte("x")}}) {
		t.Fatal("equal image lists should compare equal")
	}
	if samePromptImages(a, nil) {
		t.Fatal("non-empty vs empty should differ")
	}
	if samePromptImages(a, []widget.Image{{MIME: "image/jpeg", Data: []byte("x")}}) {
		t.Fatal("different mime should differ")
	}
}
