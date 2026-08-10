package i18n

import (
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Lang
	}{
		{"", Zh},
		{"zh", Zh},
		{"zh-CN", Zh},
		{"zh_CN", Zh},
		{"en", En},
		{"en-US", En},
		{"en_US.UTF-8", En},
		{"C", Zh},
		{"POSIX", Zh},
		{"ja_JP", Zh}, // 未支持语言回退默认
	}
	for _, c := range cases {
		if got := Parse(c.in); got != c.want {
			t.Errorf("Parse(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDetectPrecedence(t *testing.T) {
	// 配置优先于环境变量。
	t.Setenv("ALCOH_LANG", "en")
	t.Setenv("LANG", "zh_CN")
	if got := Detect("en"); got != En {
		t.Errorf("Detect(config=en) = %q, want en", got)
	}
	if got := Detect(""); got != En {
		t.Errorf("Detect(env ALCOH_LANG=en) = %q, want en", got)
	}
	t.Setenv("ALCOH_LANG", "")
	if got := Detect(""); got != Zh {
		t.Errorf("Detect(LANG=zh_CN) = %q, want zh", got)
	}
	t.Setenv("LANG", "")
	if got := Detect(""); got != Zh {
		t.Errorf("Detect(no env) = %q, want zh (default)", got)
	}
}

func TestTFor(t *testing.T) {
	if got := TFor(Zh, "会话已删除"); got != "会话已删除" {
		t.Errorf("zh T = %q", got)
	}
	if got := TFor(En, "会话已删除"); got != "Session deleted" {
		t.Errorf("en T = %q", got)
	}
	// 未收录的键回退原文。
	if got := TFor(En, "未收录的文本"); got != "未收录的文本" {
		t.Errorf("en fallback = %q", got)
	}
	// 带格式参数。
	if got := TFor(En, "已复制 %d 个字符", 12); got != "Copied 12 characters" {
		t.Errorf("en formatted = %q", got)
	}
	if got := TFor(Zh, "已复制 %d 个字符", 12); got != "已复制 12 个字符" {
		t.Errorf("zh formatted = %q", got)
	}
}

func TestSetLangAffectsT(t *testing.T) {
	SetLang(En)
	defer SetLang(Zh)
	if got := T("帮助"); got != "Help" {
		t.Errorf("T after SetLang(en) = %q, want Help", got)
	}
	SetLang(Zh)
	if got := T("帮助"); got != "帮助" {
		t.Errorf("T after SetLang(zh) = %q, want 帮助", got)
	}
	// 非法值回退 zh。
	SetLang("fr")
	if got := Current(); got != Zh {
		t.Errorf("Current after invalid = %q, want zh", got)
	}
}

// TestEnKeysHaveZhuivalent 验证英文翻译表中每条 key 都是合法字符串（无空键）。
func TestEnKeysNotEmpty(t *testing.T) {
	for k, v := range en {
		if k == "" {
			t.Error("empty translation key")
		}
		if v == "" {
			t.Errorf("empty translation for %q", k)
		}
	}
}
