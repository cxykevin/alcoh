package connspec

import "testing"

// TestParse 覆盖启动位置参数的全部写法。
func TestParse(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want Spec
	}{
		{name: "空参数", arg: "", want: Spec{Kind: KindNone}},
		{name: "空白参数", arg: "   ", want: Spec{Kind: KindNone}},
		{name: "绝对本地目录", arg: "/home/aaaaa", want: Spec{Kind: KindLocal, Dir: "/home/aaaaa"}},
		{name: "点相对目录", arg: "./abc", want: Spec{Kind: KindLocal, Dir: "./abc"}},
		{name: "父相对目录", arg: "../abc", want: Spec{Kind: KindLocal, Dir: "../abc"}},
		{name: "家目录", arg: "~/work", want: Spec{Kind: KindLocal, Dir: "~/work"}},
		{name: "Windows 反斜杠", arg: `C:\Users\x`, want: Spec{Kind: KindLocal, Dir: `C:\Users\x`}},
		{name: "Windows 正斜杠", arg: "C:/Users/x", want: Spec{Kind: KindLocal, Dir: "C:/Users/x"}},
		{name: "Windows 相对盘符", arg: "C:foo", want: Spec{Kind: KindLocal, Dir: "C:foo"}},
		{name: "UNC 路径", arg: `\\nas\share`, want: Spec{Kind: KindLocal, Dir: `\\nas\share`}},
		{
			name: "纯 IP",
			arg:  "123.45.6.78",
			want: Spec{Kind: KindRemote, Host: "123.45.6.78", Port: 7433, Scheme: "ws"},
		},
		{
			name: "IP 带端口",
			arg:  "123.45.6.78:1234",
			want: Spec{Kind: KindRemote, Host: "123.45.6.78", Port: 1234, Scheme: "ws"},
		},
		{
			name: "域名",
			arg:  "example.com",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 7433, Scheme: "ws"},
		},
		{
			name: "裸名字按主机处理",
			arg:  "abc",
			want: Spec{Kind: KindRemote, Host: "abc", Port: 7433, Scheme: "ws"},
		},
		{
			name: "内联密钥",
			arg:  "{key}@example.com",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 7433, Scheme: "ws", Key: "{key}"},
		},
		{
			name: "主机端口与远端路径",
			arg:  "example.com:7433/home/bin",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 7433, Scheme: "ws", Path: "/home/bin"},
		},
		{
			name: "密钥主机端口路径",
			arg:  "k3y@example.com:1234/srv/app",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 1234, Scheme: "ws", Path: "/srv/app", Key: "k3y"},
		},
		{
			name: "显式 wss",
			arg:  "wss://example.com/acp",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 7433, Scheme: "wss", Path: "/acp"},
		},
		{
			name: "带密钥与路径的 wss",
			arg:  "wss://key@example.com:443/home/a",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 443, Scheme: "wss", Path: "/home/a", Key: "key"},
		},
		{
			name: "IPv6 带端口",
			arg:  "[::1]:7433",
			want: Spec{Kind: KindRemote, Host: "::1", Port: 7433, Scheme: "ws"},
		},
		{
			name: "IPv6 默认端口",
			arg:  "[fd00::2]",
			want: Spec{Kind: KindRemote, Host: "fd00::2", Port: 7433, Scheme: "ws"},
		},
		{
			name: "根路径",
			arg:  "example.com:7433/",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 7433, Scheme: "ws", Path: "/"},
		},
		{
			name: "路径中的 @ 不当作密钥",
			arg:  "example.com/home/user@host",
			want: Spec{Kind: KindRemote, Host: "example.com", Port: 7433, Scheme: "ws", Path: "/home/user@host"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.arg)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.arg, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.arg, got, tc.want)
			}
		})
	}
}

// TestParseErrors 覆盖非法连接串的明确报错。
func TestParseErrors(t *testing.T) {
	for _, arg := range []string{
		"::1",                // IPv6 未加方括号
		"example.com:abc",    // 端口非数字
		"example.com:0",      // 端口越界
		"example.com:65536",  // 端口越界
		"@example.com",       // 缺主机
		"http://example.com", // 不支持的协议
		"[::1",               // 缺 ']'
	} {
		if spec, err := Parse(arg); err == nil {
			t.Errorf("Parse(%q) = %+v, want error", arg, spec)
		}
	}
}

// TestHostPort 验证 IPv6 自动加方括号。
func TestHostPort(t *testing.T) {
	cases := map[string]string{
		"example.com:7433": "example.com:7433",
		"[::1]:7433":       "[::1]:7433",
		"[fd00::2]:1234":   "[fd00::2]:1234",
	}
	for in, want := range cases {
		spec, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", in, err)
		}
		if got := spec.HostPort(); got != want {
			t.Errorf("Parse(%q).HostPort() = %q, want %q", in, got, want)
		}
	}
}

// TestWSURL 验证 WS 地址只由端点路径与密钥组成：位置参数里的远端路径不进入
// URL（它是会话 cwd）。
func TestWSURL(t *testing.T) {
	spec, err := Parse("example.com:7433/home/bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := spec.WSURL("/acp", "k")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://example.com:7433/acp?key=k"; got != want {
		t.Fatalf("WSURL = %q, want %q", got, want)
	}
	// 端点为空回落 /acp；缺少前导斜杠时补齐；无密钥不带 query。
	got, err = spec.WSURL("", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://example.com:7433/acp"; got != want {
		t.Fatalf("WSURL(default endpoint) = %q, want %q", got, want)
	}
	got, err = spec.WSURL("alcohol/acp", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://example.com:7433/alcohol/acp"; got != want {
		t.Fatalf("WSURL(slashless endpoint) = %q, want %q", got, want)
	}

	wss, err := Parse("wss://example.com:443/srv")
	if err != nil {
		t.Fatal(err)
	}
	got, err = wss.WSURL("", "k2")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wss://example.com:443/acp?key=k2"; got != want {
		t.Fatalf("WSURL(wss) = %q, want %q", got, want)
	}

	if _, err := Parse("/local/dir"); err != nil {
		t.Fatal(err)
	}
	local, _ := Parse("/local/dir")
	if _, err := local.WSURL("", ""); err == nil {
		t.Fatal("local spec should not build a WS URL")
	}
}

// TestConnID 验证连接标识不含密钥、scheme/host 小写，用于 conn.keys 与 dir.history。
func TestConnID(t *testing.T) {
	spec, err := Parse("k3y@Example.COM:1234/srv")
	if err != nil {
		t.Fatal(err)
	}
	got, err := spec.ConnID("/acp")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://example.com:1234/acp"; got != want {
		t.Fatalf("ConnID = %q, want %q", got, want)
	}
}

// TestMaskURL 验证展示地址把 key 掩码成 ***，且不转义。
func TestMaskURL(t *testing.T) {
	cases := map[string]string{
		"ws://h:7433/acp?key=secret":     "ws://h:7433/acp?key=***",
		"ws://h:7433/acp":                "ws://h:7433/acp",
		"ws://h:7433/acp?a=1&key=secret": "ws://h:7433/acp?a=1&key=***",
		"not a url":                      "not a url",
	}
	for in, want := range cases {
		if got := MaskURL(in); got != want {
			t.Errorf("MaskURL(%q) = %q, want %q", in, got, want)
		}
	}
}
