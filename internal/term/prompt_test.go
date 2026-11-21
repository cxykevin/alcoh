package term

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestReadSecretLine 验证按行读取：去掉 CRLF、保留其余字符、EOF 返回已读内容。
func TestReadSecretLine(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantEOF bool
	}{
		{in: "secret\n", want: "secret"},
		{in: "secret\r\n", want: "secret"},
		{in: "a b\tc\n", want: "a b\tc"},
		{in: "前\n", want: "前"},
		{in: "\n", want: ""},
		// 没有换行就到 EOF：返回已读内容 + io.EOF（调用方按"读到内容"处理）。
		{in: "rest", want: "rest", wantEOF: true},
		{in: "", want: "", wantEOF: true},
	}
	for _, tc := range cases {
		got, err := readSecretLine(strings.NewReader(tc.in))
		if tc.wantEOF {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("readSecretLine(%q) error = %v, want EOF", tc.in, err)
			}
		} else if err != nil {
			t.Fatalf("readSecretLine(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("readSecretLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
