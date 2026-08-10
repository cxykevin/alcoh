// Package i18n 提供轻量国际化支持：语言检测与按当前语言翻译用户可见文本。
//
// 翻译以中文原文为键（ID）：代码中直接写中文（zh 即原文），en 目录提供英文
// 翻译，未收录的键回退中文原文。翻译发生在渲染/展示时（T），因此 /settings
// 中切换语言后所有界面文本即时更新。
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Lang 是支持的语言标识。
type Lang string

const (
	// Zh 简体中文（默认，翻译原文）。
	Zh Lang = "zh"
	// En 英文。
	En Lang = "en"
)

var (
	mu   sync.RWMutex
	lang = Zh
)

// SetLang 设置当前语言，影响之后所有 T 调用。
func SetLang(l Lang) {
	if l != Zh && l != En {
		l = Zh
	}
	mu.Lock()
	lang = l
	mu.Unlock()
}

// Current 返回当前语言。
func Current() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// Parse 把任意语言标识归一化为支持的 Lang；无法识别时返回 Zh。
// 兼容 "zh-CN"/"zh_cn"/"en-US"/"en_US" 等形式（取首段小写）。
func Parse(s string) Lang {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_"); i >= 0 {
		s = s[:i]
	}
	switch s {
	case "zh", "cn", "chs", "zh-hans":
		return Zh
	case "en", "us", "gb", "eng":
		return En
	default:
		// 常见 LANG 值回退中文（默认语言）。
		return Zh
	}
}

// Detect 确定启动语言，优先级从高到低：
//  1. configured 非空（本地配置 /settings 写入的 language）
//  2. 环境变量 ALCOH_LANG
//  3. 系统语言环境 LANG / LC_ALL / LC_MESSAGES
//
// 全部不可用时回退中文。
func Detect(configured string) Lang {
	if configured != "" {
		return Parse(configured)
	}
	for _, env := range []string{"ALCOH_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(env); v != "" {
			if l := Parse(v); l != Zh {
				return l
			}
		}
	}
	return Zh
}

// T 按当前语言翻译 id（id 为中文原文）。en 未收录时回退 id 本身。
// args 按 fmt.Sprintf 规则填入翻译文本中的占位符（动词与原文一致）。
func T(id string, args ...any) string {
	return TFor(Current(), id, args...)
}

// getenv 与 os.Getenv 的间接层，便于测试注入。
var getenv = os.Getenv

// TFor 按指定语言翻译；en 未收录时回退中文原文。
func TFor(l Lang, id string, args ...any) string {
	out := id
	if l == En {
		if tr, ok := en[id]; ok {
			out = tr
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(out, args...)
	}
	return out
}
