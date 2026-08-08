package renderer

import (
	"os"
	"strconv"
	"strings"
)

// ColorMode 描述终端对颜色的支持级别。
type ColorMode int

const (
	// ColorModeMono 单色（TERM=dumb 等），忽略所有颜色。
	ColorModeMono ColorMode = iota
	// ColorMode16 仅 16 色。
	ColorMode16
	// ColorMode256 支持 256 色。
	ColorMode256
	// ColorModeTrueColor 支持 24 位真彩色。
	ColorModeTrueColor
)

// DetectColorMode 根据环境变量检测颜色模式。
func DetectColorMode() ColorMode {
	term := os.Getenv("TERM")
	if term == "dumb" {
		return ColorModeMono
	}
	// NO_COLOR 约定：非空即禁用颜色
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return ColorModeMono
	}
	ct := os.Getenv("COLORTERM")
	if ct == "truecolor" || ct == "24bit" || strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return ColorModeTrueColor
	}
	if strings.Contains(term, "256color") || strings.Contains(term, "256") {
		return ColorMode256
	}
	if term != "" {
		return ColorMode16
	}
	// 无 TERM 时保守处理
	return ColorMode16
}

// ansiTo256 将 truecolor 颜色量化到 256 色索引。
func ansiTo256(c Color) int {
	r, g, b := c.Components()
	// 近似 16 色：若接近标准色直接映射
	toQ := func(v uint8) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (int(v) - 35) / 40
	}
	return 16 + 36*toQ(r) + 6*toQ(g) + toQ(b)
}

// ansiTo16 将 truecolor 颜色量化到 16 色近似。
func ansiTo16(c Color) int {
	r, g, b := c.Components()
	// 简单亮度量化
	gray := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	// 判断是否灰阶
	if abs(int(r)-int(g)) < 24 && abs(int(g)-int(b)) < 24 && abs(int(r)-int(b)) < 24 {
		if gray < 128 {
			return 0 // black
		}
		return 7 // white
	}
	// 主色分量
	ri := 0
	if r > 128 {
		ri = 1
	}
	gi := 0
	if g > 128 {
		gi = 1
	}
	bi := 0
	if b > 128 {
		bi = 1
	}
	idx := ri*4 + gi*2 + bi
	// 0..7 基础色；亮色通过 bold 表现
	return idx
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// sgrColor 生成 38/48;...;m 参数序列。
func sgrColor(prefix string, c Color, mode ColorMode) string {
	if c.IsDefault() {
		return ""
	}
	switch mode {
	case ColorModeTrueColor:
		r, g, b := c.Components()
		return prefix + "2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b))
	case ColorMode256:
		return prefix + "5;" + strconv.Itoa(ansiTo256(c))
	default: // ColorMode16 / Mono
		return ""
	}
}

// sgrForStyle 生成完整描述目标样式的 SGR 参数段（不含 CSI 头与结尾 m）。
// 前景/背景默认值显式输出 39/49，保证每次样式切换后终端状态与目标完全一致，
// 避免跨 run 的 SGR 状态污染（旧前景/旧背景残留）。
func sgrForStyle(s Style, mode ColorMode) string {
	if mode == ColorModeMono {
		return ""
	}
	var params []string
	if s.Bold {
		params = append(params, "1")
	}
	if s.Dim {
		params = append(params, "2")
	}
	if s.Italic {
		params = append(params, "3")
	}
	if s.Underline {
		params = append(params, "4")
	}
	if s.Reverse {
		params = append(params, "7")
	}
	if s.Strikethrough {
		params = append(params, "9")
	}
	// 前景：默认 → 39；显式色 → 38;2;...（若当前颜色模式不支持则不输出）
	if s.Fg.IsDefault() {
		params = append(params, "39")
	} else if c := sgrColor("38;", s.Fg, mode); c != "" {
		params = append(params, c)
	}
	// 背景：默认 → 49；显式色 → 48;2;...
	if s.Bg.IsDefault() {
		params = append(params, "49")
	} else if c := sgrColor("48;", s.Bg, mode); c != "" {
		params = append(params, c)
	}
	return strings.Join(params, ";")
}
