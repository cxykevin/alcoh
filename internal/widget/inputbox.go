package widget

import (
	"strings"

	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/renderer"
)

// EditOp 是一次可撤销的编辑操作。
type EditOp struct {
	apply   func(b *InputBuffer)
	unapply func(b *InputBuffer)
}

// InputBuffer 是多行文本缓冲，管理光标、撤销/重做、历史。
// 行以 rune 切片存储；CX 为字符偏移（跨宽字符两步移动）。
type InputBuffer struct {
	Lines  [][]rune
	CX     int
	CY     int
	Scroll int

	History []string
	HistPos int // -1 = 编辑态；>=0 = 正在浏览历史
	draft   string

	UndoStack []EditOp
	RedoStack []EditOp

	killText string // kill ring（Ctrl+Y 粘贴）
}

// NewInputBuffer 创建空输入缓冲。
func NewInputBuffer() *InputBuffer {
	return &InputBuffer{
		Lines:   [][]rune{{}},
		HistPos: -1,
	}
}

// Text 返回整个缓冲的文本（按行拼接）。
func (b *InputBuffer) Text() string {
	return JoinLines(b.Lines)
}

// JoinLines 把行 rune 切片拼接为字符串。
func JoinLines(lines [][]rune) string {
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(line))
	}
	return sb.String()
}

// SplitLines 把字符串拆为行 rune 切片。
func SplitLines(s string) [][]rune {
	if s == "" {
		return [][]rune{{}}
	}
	parts := strings.Split(s, "\n")
	lines := make([][]rune, len(parts))
	for i, p := range parts {
		lines[i] = []rune(p)
	}
	return lines
}

func (b *InputBuffer) curLine() []rune { return b.Lines[b.CY] }

func (b *InputBuffer) setCurLine(l []rune) { b.Lines[b.CY] = l }

func (b *InputBuffer) pushUndo(op EditOp) {
	b.UndoStack = append(b.UndoStack, op)
	b.RedoStack = nil
	if len(b.UndoStack) > 200 {
		b.UndoStack = b.UndoStack[len(b.UndoStack)-200:]
	}
}

func (b *InputBuffer) snapshot() (lines [][]rune, cx, cy int) {
	cp := make([][]rune, len(b.Lines))
	for i, l := range b.Lines {
		cp[i] = append([]rune(nil), l...)
	}
	return cp, b.CX, b.CY
}

// edit 执行一次可撤销编辑：apply 修改，undo 时完整恢复快照，redo 重放 apply。
func (b *InputBuffer) edit(apply func(*InputBuffer)) {
	oldLines, oldCX, oldCY := b.snapshot()
	b.pushUndo(EditOp{
		apply: apply,
		unapply: func(x *InputBuffer) {
			x.Lines = oldLines
			x.CX, x.CY = oldCX, oldCY
		},
	})
	apply(b)
	b.clampCursor()
}

func (b *InputBuffer) clampCursor() {
	if b.CY < 0 {
		b.CY = 0
	}
	if b.CY >= len(b.Lines) {
		b.CY = len(b.Lines) - 1
	}
	max := len(b.Lines[b.CY])
	if b.CX > max {
		b.CX = max
	}
	if b.CX < 0 {
		b.CX = 0
	}
}

// InsertRune 在光标处插入字符。
func (b *InputBuffer) InsertRune(r rune) {
	if r == '\n' {
		b.InsertNewline()
		return
	}
	b.edit(func(x *InputBuffer) {
		line := x.curLine()
		cx := x.CX
		line = append(line[:cx], append([]rune{r}, line[cx:]...)...)
		x.setCurLine(line)
		x.CX++
	})
}

// InsertText 在光标处一次性插入文本，粘贴内容作为单次 undo 操作。
func (b *InputBuffer) InsertText(text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text == "" {
		return
	}
	parts := strings.Split(text, "\n")
	b.edit(func(x *InputBuffer) {
		line := x.curLine()
		cx, cy := x.CX, x.CY
		first := append([]rune(nil), line[:cx]...)
		last := append([]rune(nil), line[cx:]...)
		newLines := make([][]rune, len(parts))
		newLines[0] = append(first, []rune(parts[0])...)
		for i := 1; i < len(parts)-1; i++ {
			newLines[i] = []rune(parts[i])
		}
		newLines[len(parts)-1] = append([]rune(parts[len(parts)-1]), last...)
		x.Lines = append(x.Lines[:cy], append(newLines, x.Lines[cy+1:]...)...)
		x.CY = cy + len(newLines) - 1
		// Keep the cursor before the original suffix, not after it.
		x.CX = len([]rune(parts[len(parts)-1]))
	})
}

// InsertNewline 在光标处换行。
func (b *InputBuffer) InsertNewline() {
	b.edit(func(x *InputBuffer) {
		line := x.curLine()
		cx := x.CX
		rest := append([]rune(nil), line[cx:]...)
		line = line[:cx]
		x.setCurLine(line)
		cy := x.CY
		x.Lines = append(x.Lines[:cy+1], append([][]rune{rest}, x.Lines[cy+1:]...)...)
		x.CX = 0
		x.CY++
	})
}

// Backspace 删除光标前一字符；行首则连接上一行。
func (b *InputBuffer) Backspace() {
	b.edit(func(x *InputBuffer) {
		cx, cy := x.CX, x.CY
		if cx > 0 {
			line := x.curLine()
			x.CX--
			x.Lines[cy] = append(line[:x.CX], line[cx:]...)
			return
		}
		if cy > 0 {
			x.Lines[cy-1] = append(x.Lines[cy-1], x.Lines[cy]...)
			x.Lines = append(x.Lines[:cy], x.Lines[cy+1:]...)
			x.CY--
			x.CX = len(x.Lines[x.CY])
		}
	})
}

// Delete 删除光标处字符；行尾则连接下一行。
func (b *InputBuffer) Delete() {
	b.edit(func(x *InputBuffer) {
		cx, cy := x.CX, x.CY
		line := x.curLine()
		if cx < len(line) {
			x.Lines[cy] = append(line[:cx], line[cx+1:]...)
			return
		}
		if cy < len(x.Lines)-1 {
			x.Lines[cy] = append(x.Lines[cy], x.Lines[cy+1]...)
			x.Lines = append(x.Lines[:cy+1], x.Lines[cy+2:]...)
		}
	})
}

// MoveLeft 光标左移（rune 索引；宽字符一步跨两列）。
func (b *InputBuffer) MoveLeft() {
	if b.CX > 0 {
		b.CX--
	} else if b.CY > 0 {
		b.CY--
		b.CX = len(b.Lines[b.CY])
	}
}

// MoveRight 光标右移（rune 索引）。
func (b *InputBuffer) MoveRight() {
	line := b.curLine()
	if b.CX < len(line) {
		b.CX++
	} else if b.CY < len(b.Lines)-1 {
		b.CY++
		b.CX = 0
	}
}

// MoveUp 光标上移（保持列）。
func (b *InputBuffer) MoveUp() {
	if b.CY > 0 {
		b.CY--
		if b.CX > len(b.Lines[b.CY]) {
			b.CX = len(b.Lines[b.CY])
		}
	}
}

// MoveDown 光标下移（保持列）。
func (b *InputBuffer) MoveDown() {
	if b.CY < len(b.Lines)-1 {
		b.CY++
		if b.CX > len(b.Lines[b.CY]) {
			b.CX = len(b.Lines[b.CY])
		}
	}
}

// MoveLineStart / MoveLineEnd 光标到行首/行尾。
func (b *InputBuffer) MoveLineStart() { b.CX = 0 }

func (b *InputBuffer) MoveLineEnd() { b.CX = len(b.curLine()) }

// MoveBufferStart / MoveBufferEnd 光标到缓冲首/尾。
func (b *InputBuffer) MoveBufferStart() {
	b.CY = 0
	b.CX = 0
}

func (b *InputBuffer) MoveBufferEnd() {
	b.CY = len(b.Lines) - 1
	b.CX = len(b.Lines[b.CY])
}

// KillToEnd 删除光标到行尾；行尾处则连接下一行（emacs Ctrl+K）。
func (b *InputBuffer) KillToEnd() {
	b.edit(func(x *InputBuffer) {
		cx, cy := x.CX, x.CY
		line := x.curLine()
		if cx < len(line) {
			x.killText = string(line[cx:])
			x.Lines[cy] = line[:cx]
			return
		}
		if cy < len(x.Lines)-1 {
			x.killText = "\n"
			x.Lines[cy] = append(x.Lines[cy], x.Lines[cy+1]...)
			x.Lines = append(x.Lines[:cy+1], x.Lines[cy+2:]...)
		}
	})
}

// KillToStart 删除光标到行首（Ctrl+U）。
func (b *InputBuffer) KillToStart() {
	b.edit(func(x *InputBuffer) {
		cx, cy := x.CX, x.CY
		if cx > 0 {
			line := x.curLine()
			x.killText = string(line[:cx])
			x.Lines[cy] = append([]rune(nil), line[cx:]...)
			x.CX = 0
		}
	})
}

// DeleteWord 删除光标前一单词（Ctrl+W / Ctrl+Backspace）。
func (b *InputBuffer) DeleteWord() {
	if b.CX == 0 {
		if b.CY > 0 {
			b.Backspace()
		}
		return
	}
	b.edit(func(x *InputBuffer) {
		cx, cy := x.CX, x.CY
		line := x.curLine()
		i := cx - 1
		for i >= 0 && isSpace(line[i]) {
			i--
		}
		for i >= 0 && !isSpace(line[i]) {
			i--
		}
		start := i + 1
		x.killText = string(line[start:cx])
		x.Lines[cy] = append(line[:start], line[cx:]...)
		x.CX = start
	})
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

// Yank 粘贴 kill ring（Ctrl+Y）。
func (b *InputBuffer) Yank() {
	if b.killText == "" {
		return
	}
	for _, r := range b.killText {
		b.InsertRune(r)
	}
}

// Clear 清空整个缓冲。
func (b *InputBuffer) Clear() {
	b.edit(func(x *InputBuffer) {
		x.Lines = [][]rune{{}}
		x.CX, x.CY = 0, 0
	})
}

// Undo 撤销。
func (b *InputBuffer) Undo() {
	if len(b.UndoStack) == 0 {
		return
	}
	op := b.UndoStack[len(b.UndoStack)-1]
	b.UndoStack = b.UndoStack[:len(b.UndoStack)-1]
	op.unapply(b)
	b.RedoStack = append(b.RedoStack, op)
	b.clampCursor()
}

// Redo 重做。
func (b *InputBuffer) Redo() {
	if len(b.RedoStack) == 0 {
		return
	}
	op := b.RedoStack[len(b.RedoStack)-1]
	b.RedoStack = b.RedoStack[:len(b.RedoStack)-1]
	op.apply(b)
	b.UndoStack = append(b.UndoStack, op)
	b.clampCursor()
}

// Submit 提交当前内容到历史并清空，返回文本。
func (b *InputBuffer) Submit() string {
	text := b.Text()
	if text != "" {
		b.History = append(b.History, text)
	}
	b.Lines = [][]rune{{}}
	b.CX, b.CY = 0, 0
	b.HistPos = -1
	b.UndoStack = nil
	b.RedoStack = nil
	return text
}

// HistoryUp 在首行处向上浏览历史。
func (b *InputBuffer) HistoryUp() {
	if len(b.History) == 0 {
		return
	}
	if b.HistPos < 0 {
		b.draft = b.Text()
		b.HistPos = len(b.History) - 1
	} else if b.HistPos > 0 {
		b.HistPos--
	}
	b.loadHistory(b.History[b.HistPos])
}

// HistoryDown 在末行处向下浏览历史。
func (b *InputBuffer) HistoryDown() {
	if b.HistPos < 0 {
		return
	}
	if b.HistPos < len(b.History)-1 {
		b.HistPos++
		b.loadHistory(b.History[b.HistPos])
	} else {
		b.HistPos = -1
		b.Lines = SplitLines(b.draft)
		b.CY = len(b.Lines) - 1
		b.CX = len(b.Lines[b.CY])
	}
}

func (b *InputBuffer) loadHistory(text string) {
	b.Lines = SplitLines(text)
	b.CY = len(b.Lines) - 1
	b.CX = len(b.Lines[b.CY])
}

// ensureCursorVisible 确保光标行在视口内（供绘制时调用）。
func (b *InputBuffer) ensureCursorVisible(viewH int) {
	if viewH <= 0 {
		return
	}
	if b.CY < b.Scroll {
		b.Scroll = b.CY
	}
	if b.CY >= b.Scroll+viewH {
		b.Scroll = b.CY - viewH + 1
	}
	if b.Scroll < 0 {
		b.Scroll = 0
	}
}

// ConsumeTrailingContinuation 将当前逻辑行末尾的反斜杠解释为续行标记。
// 仅光标位于行尾时生效；成功时移除反斜杠并插入换行。
func (b *InputBuffer) ConsumeTrailingContinuation() bool {
	line := b.curLine()
	if b.CX != len(line) || len(line) == 0 || line[len(line)-1] != '\\' {
		return false
	}
	b.edit(func(x *InputBuffer) {
		current := x.curLine()
		x.setCurLine(current[:len(current)-1])
		x.CX--
		line = x.curLine()
		rest := append([]rune(nil), line[x.CX:]...)
		x.setCurLine(line[:x.CX])
		cy := x.CY
		x.Lines = append(x.Lines[:cy+1], append([][]rune{rest}, x.Lines[cy+1:]...)...)
		x.CX = 0
		x.CY++
	})
	return true
}

// VisualHeight 报告按显示宽度自动换行后的视觉行数，范围由调用方 clamp。
// promptW 只占第一视觉行，后续行从输入区左边开始。
func (b *InputBuffer) VisualHeight(width, promptW int) int {
	if width < 1 {
		return 1
	}
	height := 0
	for lineIndex, line := range b.Lines {
		available := width
		if lineIndex == 0 {
			available -= promptW
		}
		if available < 1 {
			available = 1
		}
		rows := 1
		used := 0
		for _, r := range line {
			rw := renderer.RuneWidth(r)
			if rw == 0 {
				continue
			}
			if used > 0 && used+rw > available {
				rows++
				used = 0
				available = width
				if available < 1 {
					available = 1
				}
			}
			used += rw
		}
		height += rows
	}
	if height < 1 {
		return 1
	}
	return height
}

// ReplaceFirstToken 用 replacement 替换首行首个空白之前的 token。
// 若 keepSuffix 为 true，则保留原 token 后面的内容（含参数）；否则用 replacement + " " 覆盖 token 及其分隔符。
// 光标停在 replacement 结尾之后（若有参数则位于空格之后）。整个操作作为单次 undo 记录。
func (b *InputBuffer) ReplaceFirstToken(replacement string) {
	if len(b.Lines) == 0 {
		return
	}
	line := b.Lines[0]
	end := len(line)
	for i, r := range line {
		if r == ' ' || r == '\t' {
			end = i
			break
		}
	}
	rest := append([]rune(nil), line[end:]...)
	repl := []rune(replacement)
	b.edit(func(x *InputBuffer) {
		newFirst := append([]rune(nil), repl...)
		if len(rest) == 0 {
			newFirst = append(newFirst, ' ')
		} else {
			newFirst = append(newFirst, rest...)
		}
		x.Lines[0] = newFirst
		x.CY = 0
		x.CX = len(repl)
		if len(rest) == 0 {
			x.CX++
		}
	})
}

// InputBox 是可绘制的多行输入框组件。
type InputBox struct {
	Buf        *InputBuffer
	Prompt     string
	Style      renderer.Style
	Cursor     renderer.Style
	Focused    bool
	GhostText  string
	GhostStyle renderer.Style
}

// OnKey 处理输入框按键。返回 true 表示已消费；Enter/Tab/Esc 返回 false 交由上层。
func (ib *InputBox) OnKey(ke input.KeyEvent) bool {
	b := ib.Buf
	switch {
	case ke.Type == input.KeyPaste:
		b.InsertText(ke.Text)
	case ke.Type == input.KeyRune && !ke.IsCtrl() && !ke.IsAlt():
		b.InsertRune(ke.Rune)
	case ke.Type == input.KeyEnter && !ke.IsShift():
		if b.ConsumeTrailingContinuation() {
			return true
		}
		return false // Enter 提交由上层处理
	case ke.Type == input.KeyEnter && ke.IsShift():
		b.InsertNewline()
	case ke.Type == input.KeyTab:
		return false
	case ke.Type == input.KeyBackspace && !ke.IsCtrl():
		b.Backspace()
	case ke.Type == input.KeyDelete:
		b.Delete()
	case ke.Type == input.KeyLeft && !ke.IsAlt():
		b.MoveLeft()
	case ke.Type == input.KeyRight && !ke.IsAlt():
		b.MoveRight()
	case ke.Type == input.KeyUp:
		if b.CY == 0 {
			b.HistoryUp()
		} else {
			b.MoveUp()
		}
	case ke.Type == input.KeyDown:
		if b.CY == len(b.Lines)-1 {
			b.HistoryDown()
		} else {
			b.MoveDown()
		}
	case ke.Type == input.KeyHome:
		b.MoveLineStart()
	case ke.Type == input.KeyEnd:
		b.MoveLineEnd()
	case ke.Type == input.KeyPageUp:
		b.MoveBufferStart()
	case ke.Type == input.KeyPageDown:
		b.MoveBufferEnd()
	case ke.IsCtrl() && ke.Rune == 'a':
		b.MoveLineStart()
	case ke.IsCtrl() && ke.Rune == 'e':
		b.MoveLineEnd()
	case ke.IsCtrl() && ke.Rune == 'b':
		b.MoveLeft()
	case ke.IsCtrl() && ke.Rune == 'f':
		b.MoveRight()
	case ke.IsCtrl() && ke.Rune == 'p':
		if b.CY == 0 {
			b.HistoryUp()
		} else {
			b.MoveUp()
		}
	case ke.IsCtrl() && ke.Rune == 'n':
		if b.CY == len(b.Lines)-1 {
			b.HistoryDown()
		} else {
			b.MoveDown()
		}
	case ke.IsCtrl() && ke.Rune == 'k':
		b.KillToEnd()
	case ke.IsCtrl() && ke.Rune == 'u':
		b.KillToStart()
	case ke.IsCtrl() && ke.Rune == 'w':
		b.DeleteWord()
	case ke.IsCtrl() && ke.Rune == 'y':
		b.Yank()
	case ke.IsCtrl() && ke.Rune == 'd':
		b.Delete()
	case ke.IsCtrl() && (ke.Rune == '/' || ke.Rune == '_'):
		b.Undo()
	case ke.IsAlt() && ke.Type == input.KeyLeft:
		line := b.curLine()
		for b.CX > 0 && isSpace(line[b.CX-1]) {
			b.MoveLeft()
		}
		for b.CX > 0 && !isSpace(b.curLine()[b.CX-1]) {
			b.MoveLeft()
		}
	case ke.IsAlt() && ke.Type == input.KeyRight:
		line := b.curLine()
		for b.CX < len(line) && isSpace(line[b.CX]) {
			b.MoveRight()
		}
		for b.CX < len(b.curLine()) && !isSpace(b.curLine()[b.CX]) {
			b.MoveRight()
		}
	default:
		return false
	}
	return true
}

// Draw 绘制输入框到 rect。prompt 只在首行显示；光标用强调色块绘制。
func (ib *InputBox) Draw(c *renderer.Canvas, r renderer.Rect) {
	b := ib.Buf
	viewH := r.H
	if viewH <= 0 {
		return
	}
	prompt := ib.Prompt
	if prompt == "" {
		prompt = "> "
	}
	promptW := renderer.StringWidth(prompt)

	// 先绘制显式行与视觉软换行，避免 CJK 宽字符在行末被拆开。
	visualRows := make([]visualInputRow, 0)
	for lineIndex, line := range b.Lines {
		row := visualInputRow{}
		available := r.W
		if lineIndex == 0 {
			available -= promptW
		}
		if available < 1 {
			available = 1
		}
		used := 0
		for runeIndex, rr := range line {
			rw := renderer.RuneWidth(rr)
			if rw == 0 {
				continue
			}
			if used > 0 && used+rw > available {
				visualRows = append(visualRows, row)
				row = visualInputRow{start: runeIndex}
				used = 0
				available = r.W
				if available < 1 {
					available = 1
				}
			}
			row.runes = append(row.runes, rr)
			used += rw
		}
		visualRows = append(visualRows, row)
	}
	if len(visualRows) == 0 {
		visualRows = append(visualRows, visualInputRow{})
	}
	start := 0
	if ib.Focused {
		cursorRow := visualRowForCursor(b, promptW, r.W)
		if cursorRow >= viewH {
			start = cursorRow - viewH + 1
		}
	}
	if start > len(visualRows)-1 {
		start = len(visualRows) - 1
	}
	for i := 0; i < viewH && start+i < len(visualRows); i++ {
		vr := visualRows[start+i]
		x := r.X
		if start+i == 0 {
			c.PutText(x, r.Y+i, prompt, ib.Style)
			x += promptW
		}
		c.PutText(x, r.Y+i, string(vr.runes), ib.Style)
	}

	if ib.Focused && ib.GhostText != "" && b.CY == 0 && b.CX == len(b.Lines[0]) {
		cx, cy, _ := visualCursorPosition(b, promptW, r.W)
		cy -= start
		if cy >= 0 && cy < viewH {
			remaining := r.W - cx
			if remaining > 0 {
				c.PutText(r.X+cx, r.Y+cy, renderer.Truncate(ib.GhostText, remaining), ib.GhostStyle)
			}
		}
	}

	if ib.Focused {
		cx, cy, cursorRune := visualCursorPosition(b, promptW, r.W)
		cy -= start
		if cy >= 0 && cy < viewH {
			line := b.Lines[b.CY]
			st := ib.Cursor
			if cursorRune < len(line) {
				c.Put(r.X+cx, r.Y+cy, renderer.CellRune(line[cursorRune], st))
			} else {
				c.Put(r.X+cx, r.Y+cy, renderer.CellRune(' ', st))
			}
		}
	}
}

type visualInputRow struct {
	start int
	runes []rune
}

func visualCursorPosition(b *InputBuffer, promptW, width int) (x, y, runeIndex int) {
	if width < 1 {
		width = 1
	}
	for lineIndex, line := range b.Lines {
		available := width
		if lineIndex == 0 {
			available -= promptW
		}
		if available < 1 {
			available = 1
		}
		used := 0
		for i, rr := range line {
			rw := renderer.RuneWidth(rr)
			if rw == 0 {
				if lineIndex == b.CY && i == b.CX {
					return cursorX(used, y, promptW, width), y, i
				}
				continue
			}
			// Match Draw's soft-wrap decision before resolving the cursor at i.
			if used > 0 && used+rw > available {
				y++
				used = 0
				available = width
			}
			if lineIndex == b.CY && i == b.CX {
				return cursorX(used, y, promptW, width), y, i
			}
			used += rw
		}
		if lineIndex == b.CY {
			return cursorX(used, y, promptW, width), y, len(line)
		}
		y++
	}
	return cursorX(0, y, promptW, width), y, 0
}

func cursorX(used, row, promptW, width int) int {
	if row == 0 {
		if promptW >= width {
			return width - 1
		}
		return used + promptW
	}
	return used
}

func visualRowForCursor(b *InputBuffer, promptW, width int) int {
	_, y, _ := visualCursorPosition(b, promptW, width)
	return y
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runeSliceWidth(rs []rune) int {
	w := 0
	for _, r := range rs {
		w += renderer.RuneWidth(r)
	}
	return w
}
