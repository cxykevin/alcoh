// Package clipboard 读取系统剪贴板（Ctrl+V）：优先取图片，其次取文本。
//
// 图片以原始字节 + MIME 返回，由 app 层编码成 ACP image 内容块；这里不关心
// 协议与 UI。桌面平台通过系统命令读取（Linux: wl-paste/xclip/xsel，
// macOS: pngpaste/osascript/pbpaste，Windows: PowerShell）。
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Image 是剪贴板中的一张图片。
type Image struct {
	MIME string
	Data []byte
}

// Content 是剪贴板内容：有图片时优先返回图片，否则返回文本。
type Content struct {
	Text  string
	Image *Image
}

var (
	// ErrEmpty 表示剪贴板为空（或只包含不支持的内容）。
	ErrEmpty = errors.New("clipboard is empty")
	// ErrUnavailable 表示当前平台/环境没有可用的剪贴板读取方式。
	ErrUnavailable = errors.New("clipboard is unavailable")
)

// Read 读取系统剪贴板。图片存在时忽略同一剪贴板中的文本。
func Read(ctx context.Context) (Content, error) {
	switch runtime.GOOS {
	case "linux":
		return readLinux(ctx)
	case "darwin":
		return readDarwin(ctx)
	case "windows":
		return readWindows(ctx)
	default:
		return Content{}, ErrUnavailable
	}
}

// ---- Linux ----

// imageMIMEPriority 是支持的图片类型优先级。只读取栅格图，避免把 SVG
// 等无法被模型直接解析的类型当图片发送。
var imageMIMEPriority = []string{
	"image/png",
	"image/jpeg",
	"image/webp",
	"image/gif",
	"image/bmp",
	"image/tiff",
}

func readLinux(ctx context.Context) (Content, error) {
	var errs []error
	// Wayland 会话优先 wl-paste；X11 兼容层下 xclip 更可靠。
	if os.Getenv("WAYLAND_DISPLAY") != "" && commandExists("wl-paste") {
		if c, err := readWayland(ctx); err == nil {
			return c, nil
		} else {
			errs = append(errs, err)
		}
	}
	if commandExists("xclip") {
		if c, err := readXclip(ctx); err == nil {
			return c, nil
		} else {
			errs = append(errs, err)
		}
	}
	if commandExists("wl-paste") {
		if c, err := readWayland(ctx); err == nil {
			return c, nil
		} else {
			errs = append(errs, err)
		}
	}
	if commandExists("xsel") {
		if out, err := run(ctx, "xsel", "--clipboard", "--output"); err == nil {
			if c := textContent(out); c.Text != "" {
				return c, nil
			}
		} else {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return Content{}, errors.Join(errs...)
	}
	return Content{}, ErrUnavailable
}

func readWayland(ctx context.Context) (Content, error) {
	types, err := run(ctx, "wl-paste", "--list-types")
	if err != nil {
		return Content{}, err
	}
	if mime := pickImageMIME(string(types)); mime != "" {
		if data, err := run(ctx, "wl-paste", "--no-newline", "--type", mime); err == nil && len(data) > 0 {
			return Content{Image: &Image{MIME: mime, Data: data}}, nil
		}
	}
	out, err := run(ctx, "wl-paste", "--no-newline")
	if err != nil {
		// 老版本 wl-clipboard 没有 --no-newline。
		out, err = run(ctx, "wl-paste")
	}
	if err != nil {
		return Content{}, err
	}
	c := textContent(out)
	if c.Text == "" {
		return Content{}, ErrEmpty
	}
	return c, nil
}

func readXclip(ctx context.Context) (Content, error) {
	if targets, err := run(ctx, "xclip", "-selection", "clipboard", "-o", "-t", "TARGETS"); err == nil {
		if mime := pickImageMIME(string(targets)); mime != "" {
			if data, err := run(ctx, "xclip", "-selection", "clipboard", "-o", "-t", mime); err == nil && len(data) > 0 {
				return Content{Image: &Image{MIME: mime, Data: data}}, nil
			}
		}
	}
	out, err := run(ctx, "xclip", "-selection", "clipboard", "-o")
	if err != nil {
		return Content{}, err
	}
	c := textContent(out)
	if c.Text == "" {
		return Content{}, ErrEmpty
	}
	return c, nil
}

// pickImageMIME 从 TARGETS/--list-types 输出中挑选最合适的图片 MIME。
func pickImageMIME(list string) string {
	have := make(map[string]bool)
	for _, line := range strings.Split(list, "\n") {
		t := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(t, "image/") {
			have[t] = true
		}
	}
	for _, mime := range imageMIMEPriority {
		if have[mime] {
			return mime
		}
	}
	return ""
}

// ---- macOS ----

func readDarwin(ctx context.Context) (Content, error) {
	if commandExists("pngpaste") {
		if data, err := run(ctx, "pngpaste", "-"); err == nil && len(data) > 0 {
			return Content{Image: &Image{MIME: "image/png", Data: data}}, nil
		}
	}
	if data, err := readDarwinImageOSA(ctx); err == nil && len(data) > 0 {
		return Content{Image: &Image{MIME: "image/png", Data: data}}, nil
	}
	out, err := run(ctx, "pbpaste")
	if err != nil {
		return Content{}, err
	}
	c := textContent(out)
	if c.Text == "" {
		return Content{}, ErrEmpty
	}
	return c, nil
}

// readDarwinImageOSA 用 AppleScript 把剪贴板里的 PNG 写到临时文件。
// 未安装 pngpaste 时的兜底路径。
func readDarwinImageOSA(ctx context.Context) ([]byte, error) {
	tmp, err := os.CreateTemp("", "alcoh-clip-*.png")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(path) }()

	script := fmt.Sprintf(`set p to POSIX file %s
try
	set f to open for access p with write permission
	set eof of f to 0
	write (the clipboard as «class PNGf») to f
	close access f
on error errMsg
	try
		close access p
	end try
	error errMsg
end try`, strconv.Quote(path))
	if _, err := run(ctx, "osascript", "-e", script); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// ---- Windows ----

func readWindows(ctx context.Context) (Content, error) {
	if data, err := readWindowsImage(ctx); err == nil && len(data) > 0 {
		return Content{Image: &Image{MIME: "image/png", Data: data}}, nil
	}
	out, err := runPowerShell(ctx, "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); [Console]::Out.Write((Get-Clipboard -Raw))")
	if err != nil {
		return Content{}, err
	}
	c := textContent(out)
	if c.Text == "" {
		return Content{}, ErrEmpty
	}
	return c, nil
}

// readWindowsImage 用 PowerShell 把剪贴板位图保存为 PNG 后读回。
func readWindowsImage(ctx context.Context) ([]byte, error) {
	tmp, err := os.CreateTemp("", "alcoh-clip-*.png")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(path) }()

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$img = [System.Windows.Forms.Clipboard]::GetImage()
if ($null -ne $img) {
	$img.Save(%s, [System.Drawing.Imaging.ImageFormat]::Png)
	Write-Output 'ok'
}`, psQuote(path))
	out, err := runPowerShellSTA(ctx, script)
	if err != nil || !strings.Contains(string(out), "ok") {
		return nil, err
	}
	return os.ReadFile(path)
}

// psQuote 把路径转成 PowerShell 单引号字符串字面量。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func runPowerShell(ctx context.Context, script string) ([]byte, error) {
	return runFirst(ctx, [][]string{
		{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script},
		{"pwsh", "-NoProfile", "-NonInteractive", "-Command", script},
	})
}

func runPowerShellSTA(ctx context.Context, script string) ([]byte, error) {
	return runFirst(ctx, [][]string{
		{"powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", script},
		{"pwsh", "-NoProfile", "-NonInteractive", "-STA", "-Command", script},
	})
}

func runFirst(ctx context.Context, commands [][]string) ([]byte, error) {
	var lastErr error
	for _, args := range commands {
		if !commandExists(args[0]) {
			continue
		}
		out, err := run(ctx, args[0], args[1:]...)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrUnavailable
	}
	return nil, lastErr
}

// ---- 公共工具 ----

func textContent(data []byte) Content {
	text := strings.TrimRight(string(data), "\x00")
	if text == "" {
		return Content{}
	}
	return Content{Text: text}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
