package model

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/cxykevin/alcoh/internal/i18n"
)

// AtEntry 是输入框 @ 文件引用的一个候选项：会话工作目录内的文件或目录。
type AtEntry struct {
	Name  string
	IsDir bool
}

// AtPanelRows 是 @ 补全选择框最多展示的行数。
const AtPanelRows = 8

// atToken 解析光标所在位置的 @ 文件引用 token。
//
// token 从行内最近一个 '@' 开始（'@' 必须在行首或空白之后，避免匹配邮件
// 地址等文本），延伸到光标处且不能跨越空白行。dir 是 token 中最后一个 '/'
// 之前的部分（相对会话工作目录，空串表示工作目录本身），prefix 是之后输入的
// 过滤前缀：例如 "@src/ma" → dir="src"、prefix="ma"。
func (m *AppModel) atToken() (row, start int, dir, prefix string, ok bool) {
	if m.Input == nil || m.Input.CY < 0 || m.Input.CY >= len(m.Input.Lines) {
		return 0, 0, "", "", false
	}
	row = m.Input.CY
	line := m.Input.Lines[row]
	cx := m.Input.CX
	if cx < 0 || cx > len(line) {
		return 0, 0, "", "", false
	}
	for i := cx - 1; i >= 0; i-- {
		r := line[i]
		if r == ' ' || r == '\t' {
			return 0, 0, "", "", false
		}
		if r != '@' {
			continue
		}
		if i > 0 && line[i-1] != ' ' && line[i-1] != '\t' {
			return 0, 0, "", "", false
		}
		token := string(line[i+1 : cx])
		if slash := strings.LastIndex(token, "/"); slash >= 0 {
			dir = token[:slash]
			prefix = token[slash+1:]
		} else {
			prefix = token
		}
		return row, i, dir, prefix, true
	}
	return 0, 0, "", "", false
}

// UpdateAtState 根据输入框内容刷新 @ 补全状态。
//
// 光标不在 @ token 内（无 token、token 已被空白结束、光标移出）时关闭选择框；
// 目录变化时清空旧条目并标记加载中，由 app 层经 fs/read 异步拉取条目。服务端
// 未声明 alkaid0 扩展能力时 @ 只是普通字符，不展开选择框。
func (m *AppModel) UpdateAtState() {
	_, _, dir, prefix, ok := m.atToken()
	if !ok || !m.SupportsAlkaid0() {
		m.CloseAt()
		return
	}
	m.AtOpen = true
	// 待加载目录用 AtLoadedDir 判断（空串同时是根目录与未加载的零值，
	// 必须配合 AtLoaded 区分），否则从根目录切到子目录后加载标记会立刻失效。
	if !m.AtLoaded || m.AtLoadedDir != dir {
		m.AtLoading = true
	}
	if dir != m.AtDir {
		m.AtEntries = nil
		m.AtSelected = 0
		m.AtDir = dir
	}
	m.AtPrefix = prefix
	m.clampAtSelected()
}

// CloseAt 关闭 @ 补全选择框并丢弃已加载的目录快照。
func (m *AppModel) CloseAt() {
	m.AtOpen = false
	m.AtSelected = 0
	m.AtDir = ""
	m.AtPrefix = ""
	m.AtEntries = nil
	m.AtLoadedDir = ""
	m.AtLoaded = false
	m.AtLoading = false
	m.AtError = ""
	m.ClearAtPreview()
}

// AtFilteredEntries 返回按当前前缀过滤后的候选项（前缀匹配，不区分大小写）。
func (m *AppModel) AtFilteredEntries() []AtEntry {
	if m.AtPrefix == "" {
		return m.AtEntries
	}
	query := strings.ToLower(m.AtPrefix)
	out := make([]AtEntry, 0, len(m.AtEntries))
	for _, entry := range m.AtEntries {
		if strings.HasPrefix(strings.ToLower(entry.Name), query) {
			out = append(out, entry)
		}
	}
	return out
}

// clampAtSelected 把选中索引限制在过滤后的列表范围内。
func (m *AppModel) clampAtSelected() {
	n := len(m.AtFilteredEntries())
	if n == 0 {
		m.AtSelected = 0
		return
	}
	if m.AtSelected >= n {
		m.AtSelected = n - 1
	}
	if m.AtSelected < 0 {
		m.AtSelected = 0
	}
}

// AtSelectedEntry 返回当前选中的候选项。
func (m *AppModel) AtSelectedEntry() (AtEntry, bool) {
	entries := m.AtFilteredEntries()
	if m.AtSelected < 0 || m.AtSelected >= len(entries) {
		return AtEntry{}, false
	}
	return entries[m.AtSelected], true
}

// AtMove 在过滤后的候选项列表中循环移动选中项。
func (m *AppModel) AtMove(delta int) {
	n := len(m.AtFilteredEntries())
	if n == 0 {
		return
	}
	m.AtSelected = (m.AtSelected + delta%n + n) % n
}

// ApplyAtEntries 写入 fs/read 返回的目录条目。只接受当前目录的结果：打字很快
// 时旧目录的请求可能晚到，不能覆盖新目录的列表。err 非空时保留空列表并记录
// 错误文案，供选择框展示。
func (m *AppModel) ApplyAtEntries(dir string, entries []AtEntry, err error) {
	if dir != m.AtDir {
		return
	}
	m.AtLoadedDir = dir
	m.AtLoaded = true
	m.AtLoading = false
	m.AtError = ""
	if err != nil {
		m.AtEntries = nil
		m.AtError = err.Error()
		m.AtSelected = 0
		return
	}
	m.AtEntries = entries
	m.clampAtSelected()
}

// CompleteAt 用当前选中的候选项补全光标处的 @ token。
//
// 文件补全为 "@相对路径" 并追加一个空格，表示选择结束、后续输入回到普通
// prompt 文本；目录补全为 "@相对路径/"，让列表继续列出该目录，用户可继续
// 用 "/" 逐级浏览。返回是否发生补全。
func (m *AppModel) CompleteAt() bool {
	entry, ok := m.AtSelectedEntry()
	if !ok {
		return false
	}
	row, start, _, _, ok := m.atToken()
	if !ok || m.Input == nil {
		return false
	}
	path := m.AtDir
	if path != "" {
		path += "/"
	}
	path += entry.Name
	text := "@" + path
	if entry.IsDir {
		text += "/"
	} else {
		text += " "
	}
	m.Input.ReplaceRangeOnLine(row, start, m.Input.CX, text)
	m.UpdateAtState()
	return true
}

// PreviewMaxBytes 是 @ 引用文件预览读取的字节上限。
const PreviewMaxBytes = 1024

// AtSelectedPath 返回当前选中文件在会话工作目录内的相对路径；选中目录或
// 没有选中项时返回 false（目录不显示预览）。
func (m *AppModel) AtSelectedPath() (string, bool) {
	entry, ok := m.AtSelectedEntry()
	if !ok || entry.IsDir {
		return "", false
	}
	path := m.AtDir
	if path != "" {
		path += "/"
	}
	return path + entry.Name, true
}

// BeginAtPreview 标记开始加载 path 的预览：清空旧内容并置加载中，避免选中
// 切换后短暂显示上一个文件的内容。
func (m *AppModel) BeginAtPreview(path string) {
	m.AtPreviewPath = path
	m.AtPreview = ""
	m.AtPreviewErr = ""
	m.AtPreviewLoading = true
}

// ApplyAtPreview 写入文件预览。只接受当前仍选中的文件：用户快速移动选中项时
// 旧文件的请求可能晚回，不能覆盖新选中项的预览。content 是 fs/read 的前
// 1024 字节；二进制或读取失败时清空内容并记录提示文案。
func (m *AppModel) ApplyAtPreview(path, content string, err error) {
	if cur, ok := m.AtSelectedPath(); !ok || cur != path {
		return
	}
	m.AtPreviewPath = path
	m.AtPreviewLoading = false
	m.AtPreviewErr = ""
	if err != nil {
		m.AtPreview = ""
		m.AtPreviewErr = err.Error()
		return
	}
	text, err := previewText(content)
	if err != nil {
		m.AtPreview = ""
		m.AtPreviewErr = i18n.T("无法预览该文件")
		return
	}
	m.AtPreview = text
}

// ClearAtPreview 清空文件预览状态（选中目录或选择框关闭时调用）。
func (m *AppModel) ClearAtPreview() {
	m.AtPreviewPath = ""
	m.AtPreview = ""
	m.AtPreviewErr = ""
	m.AtPreviewLoading = false
}

// previewText 把 fs/read 返回的前 1024 字节整理成可显示的文本：截掉末尾
// 被字节上限切断的不完整 UTF-8 字符；含 NUL 或中间存在非法 UTF-8 时视为
// 二进制文件，返回错误。
func previewText(content string) (string, error) {
	content = trimIncompleteUTF8(content)
	if strings.ContainsRune(content, '\x00') || !utf8.ValidString(content) {
		return "", errors.New("binary file")
	}
	return content, nil
}

// trimIncompleteUTF8 反复去掉末尾不完整的 UTF-8 字节（按 1024 字节截断时
// 多字节字符可能被切一半）。合法的 U+FFFD（EF BF BD）不会被误删。
func trimIncompleteUTF8(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size != 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
