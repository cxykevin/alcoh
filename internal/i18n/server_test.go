package i18n

import "testing"

// TestTServerTranslatesServerText 验证服务端英文文本在中文界面被替换为客户端
// 译文，英文界面则直接使用服务端原文（服务端文案更新时英文界面自动跟随）。
func TestTServerTranslatesServerText(t *testing.T) {
	defer SetLang(Zh)
	SetLang(Zh)
	if got := TServer("Compress the history"); got != "压缩历史记录" {
		t.Errorf("zh TServer = %q, want 压缩历史记录", got)
	}
	SetLang(En)
	if got := TServer("Compress the history"); got != "Compress the history" {
		t.Errorf("en TServer = %q, want server original text", got)
	}
}

// TestTServerFallsBackToServerText 验证未收录文本（服务端新增或改写文案）与
// 空串一律回退服务端原文，不猜译。
func TestTServerFallsBackToServerText(t *testing.T) {
	defer SetLang(Zh)
	SetLang(Zh)
	for _, s := range []string{"", "A brand new command description"} {
		if got := TServer(s); got != s {
			t.Errorf("TServer(%q) = %q, want fallback to server text", s, got)
		}
	}
}

// TestServerZhTableEntriesUsable 验证对照表没有空条目、且每条都确实是翻译
// （键与值不同，否则中文界面看不到变化）。
func TestServerZhTableEntriesUsable(t *testing.T) {
	if len(serverZh) == 0 {
		t.Fatal("serverZh table is empty")
	}
	for k, v := range serverZh {
		if k == "" || v == "" {
			t.Errorf("empty entry in serverZh: %q -> %q", k, v)
		}
		if k == v {
			t.Errorf("serverZh[%q] equals the server text", k)
		}
	}
}
