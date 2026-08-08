package renderer

import "testing"

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		name string
		r    rune
		want int
	}{
		{"ascii", 'a', 1},
		{"ascii digit", '1', 1},
		{"cjk", '中', 2},
		{"cjk2", '文', 2},
		{"fullwidth A", 'Ａ', 2},
		{"hiragana", 'あ', 2},
		{"katakana", 'ア', 2},
		{"hangul", '한', 2},
		{"cjk punct", '，', 2},
		{"accent e", 'é', 1},
		{"cyrillic", 'Ж', 1},
		{"emoji", '🚀', 2},
		{"control", '\x03', 0},
	}
	for _, c := range cases {
		if got := runeWidth(c.r); got != c.want {
			t.Errorf("%s: runeWidth(%q) = %d, want %d", c.name, c.r, got, c.want)
		}
	}
}

func TestStringWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"abc", 3},
		{"中文", 4},
		{"你好 world", 4 + 1 + 5},
		{"é", 1},  // e + 组合重音
		{"​", 0},   // 零宽空格
		{"a​b", 2}, // ZWSP 不计宽
		{"👨‍👩", 4}, // ZWJ 家庭 emoji（各 2 列，ZWJ 0 列）
		{"ＡＢ", 4},  // 全角
	}
	for _, c := range cases {
		if got := StringWidth(c.s); got != c.want {
			t.Errorf("StringWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestWrap(t *testing.T) {
	cases := []struct {
		s    string
		w    int
		want []string
	}{
		{"abc", 2, []string{"ab", "c"}},
		{"中文字", 3, []string{"中", "文", "字"}}, // 宽字符不跨行：3 列放不下 4 列的"中文"
		{"hello\nworld", 10, []string{"hello", "world"}},
		{"", 5, []string{""}},
	}
	for _, c := range cases {
		got := Wrap(c.s, c.w)
		if len(got) != len(c.want) {
			t.Errorf("Wrap(%q,%d) = %v, want %v", c.s, c.w, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Wrap(%q,%d)[%d] = %q, want %q", c.s, c.w, i, got[i], c.want[i])
			}
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		w    int
		want string
	}{
		{"hello", 3, "he…"},
		{"hello", 10, "hello"},
		{"中文字", 3, "中…"}, // 3 列放不下 4 列的"中文"，在"中"后截断
		{"abcdef", 5, "abcd…"},
	}
	for _, c := range cases {
		if got := Truncate(c.s, c.w); got != c.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", c.s, c.w, got, c.want)
		}
	}
}
