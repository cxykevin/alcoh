package view

import "testing"

func TestFormatSessionTime(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2026-08-08T14:30:00+08:00", "08-08 14:30"},
		{"2026-08-08T09:05:09Z", "08-08 09:05"},
		{"not-a-time", "not-a-time"}, // 解析失败原样返回
		{"", ""},
	}
	for _, c := range cases {
		if got := formatSessionTime(c.in); got != c.want {
			t.Errorf("formatSessionTime(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
