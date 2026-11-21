// Package connspec 解析 alcoh 的第一个位置参数（连接目标）。
//
// 支持的写法：
//
//	/home/aaaaa                 本地目录（绝对路径）
//	./abc  ../x  ~/x            本地目录（相对进程当前目录 / 家目录）
//	C:\Users\x  \\nas\share     本地目录（Windows 路径）
//	123.45.6.78                 远端主机，端口默认 7433，无路径
//	123.45.6.78:1234            远端主机 + 端口
//	{key}@example.com           用户名位置是明文密钥（无路径）
//	example.com:7433/home/bin   host:port 之后的整段是远端文件系统路径
//	key@host:port/path          密钥 + 主机 + 远端路径
//	ws://host:7433/path         显式 scheme（wss 表示 TLS）
//
// 远端路径一律进入 Spec.Path（会话 cwd / 对方文件系统路径），与 WebSocket
// 端点路径无关——端点路径默认是固定的 /acp，由命令行 --url 覆盖（见 cmd/alcoh）。
package connspec

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// DefaultPort 是远端连接的默认 WebSocket 端口（与 wscfg 默认值一致）。
const DefaultPort uint16 = 7433

// Kind 是位置参数的类别。
type Kind int

const (
	// KindNone 表示没有位置参数：沿用默认本地连接。
	KindNone Kind = iota
	// KindLocal 表示参数是一个本地目录：仍连默认后端，只改 cwd。
	KindLocal
	// KindRemote 表示参数描述一个远端 alkaid0 服务。
	KindRemote
)

// Spec 是解析后的连接目标。
type Spec struct {
	Kind Kind
	// Dir 是 KindLocal 的本地目录（原文，可能带 ~ 或相对路径，由调用方展开/取绝对）。
	Dir string
	// Host/Port/Scheme 是 KindRemote 的连接地址；Scheme 为 ws 或 wss。
	Host   string
	Port   uint16
	Scheme string
	// Path 是 KindRemote 的远端文件系统路径（会话 cwd），空表示未指定
	//（调用方应在启动后打开目录选择器）。
	Path string
	// Key 是连接串里内联的明文密钥（"{key}@host" 的用户名位置）。
	Key string
}

// Parse 解析位置参数；空参数返回 KindNone。
func Parse(arg string) (Spec, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return Spec{Kind: KindNone}, nil
	}
	if isLocalPath(arg) {
		return Spec{Kind: KindLocal, Dir: arg}, nil
	}
	return parseRemote(arg)
}

// isLocalPath 判断参数是否应视为本地目录：绝对路径、显式相对路径、家目录
// 前缀、Windows 盘符/UNC/反斜杠路径。裸名字（如 "abc"）按主机名处理。
func isLocalPath(arg string) bool {
	switch {
	case strings.HasPrefix(arg, "/"),
		strings.HasPrefix(arg, "./"),
		strings.HasPrefix(arg, "../"),
		strings.HasPrefix(arg, "~"),
		strings.HasPrefix(arg, `\`),
		strings.Contains(arg, `\`):
		return true
	}
	// Windows 盘符：C: / C:\ / C:/... 以及相对盘符路径 C:foo。
	if len(arg) >= 2 && arg[1] == ':' && isDriveLetter(arg[0]) {
		return true
	}
	return false
}

func isDriveLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// parseRemote 解析 "[scheme://][key@]host[:port][/path]"。
func parseRemote(arg string) (Spec, error) {
	spec := Spec{Kind: KindRemote, Port: DefaultPort, Scheme: "ws"}
	rest := arg
	if i := strings.Index(rest, "://"); i > 0 {
		scheme := strings.ToLower(rest[:i])
		if scheme != "ws" && scheme != "wss" {
			return Spec{}, fmt.Errorf("不支持的协议 %q（只支持 ws:// 与 wss://）", rest[:i])
		}
		spec.Scheme = scheme
		rest = rest[i+3:]
	}
	if rest == "" {
		return Spec{}, errors.New("连接目标为空")
	}

	// 只在 authority 段（第一个 "/" 之前）里找密钥分隔符 '@'：远端路径本身
	// 可能包含 '@'（如 /home/user@host）。
	authEnd := len(rest)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authEnd = i
	}
	authority := rest[:authEnd]
	if i := strings.LastIndexByte(authority, '@'); i >= 0 {
		if i == 0 {
			return Spec{}, errors.New("密钥为空")
		}
		spec.Key = authority[:i]
		authority = authority[i+1:]
	}
	if authority == "" {
		return Spec{}, errors.New("缺少主机名")
	}
	if authEnd < len(rest) {
		spec.Path = rest[authEnd:]
	}

	host, port, err := splitHostPort(authority)
	if err != nil {
		return Spec{}, err
	}
	spec.Host = host
	spec.Port = port
	return spec, nil
}

// splitHostPort 拆分 "host[:port]"，支持 "[::1]:7433" 形式的 IPv6。
func splitHostPort(authority string) (string, uint16, error) {
	host := authority
	portStr := ""
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return "", 0, fmt.Errorf("IPv6 地址缺少 ']': %s", authority)
		}
		host = authority[1:end]
		if tail := authority[end+1:]; tail != "" {
			if !strings.HasPrefix(tail, ":") {
				return "", 0, fmt.Errorf("主机地址格式错误: %s", authority)
			}
			portStr = tail[1:]
		}
	} else if i := strings.LastIndexByte(authority, ':'); i >= 0 {
		if strings.Count(authority, ":") > 1 {
			return "", 0, fmt.Errorf("IPv6 地址需要用方括号包裹: %s", authority)
		}
		host, portStr = authority[:i], authority[i+1:]
	}
	if host == "" {
		return "", 0, errors.New("缺少主机名")
	}
	port := DefaultPort
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil || p < 1 || p > 65535 {
			return "", 0, fmt.Errorf("端口无效: %q", portStr)
		}
		port = uint16(p)
	}
	return host, port, nil
}

// HostPort 返回 "host:port"（IPv6 自动加方括号）。
func (s Spec) HostPort() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(int(s.Port)))
}

// HasPath 报告是否指定了远端路径。
func (s Spec) HasPath() bool { return s.Kind == KindRemote && s.Path != "" }

// DefaultEndpoint 是 alkaid0 的固定 WebSocket 端点路径。
const DefaultEndpoint = "/acp"

// endpointPath 归一化端点路径：空值回落到 /acp，缺少前导 "/" 时补上。
func endpointPath(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return DefaultEndpoint
	}
	if !strings.HasPrefix(endpoint, "/") {
		return "/" + endpoint
	}
	return endpoint
}

// WSURL 用本连接目标构造 WebSocket 地址：WS 端点路径来自 endpoint（默认
// /acp），密钥并入 query。远端路径（s.Path）只作为会话 cwd，不进入 URL。
func (s Spec) WSURL(endpoint, key string) (string, error) {
	if s.Kind != KindRemote {
		return "", errors.New("not a remote connection spec")
	}
	if s.Host == "" {
		return "", errors.New("host is empty")
	}
	scheme := s.Scheme
	if scheme == "" {
		scheme = "ws"
	}
	u := url.URL{Scheme: scheme, Host: s.HostPort(), Path: endpointPath(endpoint)}
	if key != "" {
		q := url.Values{}
		q.Set("key", key)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// ConnID 返回不含密钥的规范化连接标识：用于 conn.keys 记忆密钥与 dir.history
// 分桶。去掉 query / userinfo / fragment，scheme 与 host 小写。
func (s Spec) ConnID(endpoint string) (string, error) {
	if s.Kind != KindRemote {
		return "", errors.New("not a remote connection spec")
	}
	if s.Host == "" {
		return "", errors.New("host is empty")
	}
	scheme := strings.ToLower(s.Scheme)
	if scheme == "" {
		scheme = "ws"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   strings.ToLower(s.HostPort()),
		Path:   endpointPath(endpoint),
	}
	return u.String(), nil
}

// MaskURL 返回可安全展示的连接地址：认证密钥（key query）以 *** 替代。
func MaskURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	if q := u.Query(); q.Has("key") {
		q.Del("key")
		parts := make([]string, 0, 2)
		if rest := q.Encode(); rest != "" {
			parts = append(parts, rest)
		}
		// 手写 ***，避免被 url.Values.Encode 转义成 %2A%2A%2A。
		parts = append(parts, "key=***")
		u.RawQuery = strings.Join(parts, "&")
	}
	return u.String()
}
