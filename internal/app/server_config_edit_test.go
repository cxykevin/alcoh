package app

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cxykevin/alcoh/internal/demo"
	"github.com/cxykevin/alcoh/internal/input"
	"github.com/cxykevin/alcoh/internal/model"
)

// TestServerConfigEditorAddThenEditInt 回归测试：新增模型并等写回与整配置重载
// 完成（Saving 解除）后，编辑数字字段 CompressSize。两个 set 串行完成，
// 每次写回都触发全量重载，最终服务端值与界面显示均为编辑后的新值——
// 验证"改动即保存、等待服务端确认后全量重载"的链路不丢 int 编辑。
func TestServerConfigEditorAddThenEditInt(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开编辑器
	waitAtomic(t, &b.gets, 1, "config/get calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// 导航到 Models 集合页 (新增) 行并新增模型。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // Model
	ft.sendKey(input.SimpleKey(input.KeyDown))  // Models
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 集合页
	ft.sendKey(input.SimpleKey(input.KeyDown))  // 键 2
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (新增)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 新增（Current=新模型子页）

	// 新增后进入 Saving 阻塞：先等写回发出（Saving 已置位），再等写回完成、
	// 重载结束、重定向到新模型页，之后才允许继续编辑。
	waitAtomic(t, &b.sets, 1, "config/set calls after add")
	waitNotSaving(t, a)
	if got := a.snapshot().ServerCurKey; got != "3" {
		t.Fatalf("current after add = %q, want model 3", got)
	}

	// 编辑新模型第一行 CompressSize（int 字段）。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 编辑 CompressSize
	time.Sleep(30 * time.Millisecond)
	ft.sendKey(input.RuneKey('u', input.ModCtrl)) // 清空预填 128000
	for _, r := range "99999" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 提交（Saving 阻塞开始）

	// 第二个 set（CompressSize 编辑）串行完成，其后全量重载读到新值。
	waitAtomic(t, &b.sets, 2, "config/set calls")
	// 重载完成（Saving 解除）且仍在模型 3 子页。
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.ServerCfg && !s.ServerSaving && s.ServerCurKey == "3"
	})

	// 锁内读取最终值断言（编辑写回 + 全量重载后以服务端为准，显示编辑后的值）。
	a.modelMu.RLock()
	var compNum float64
	compOK := false
	if ed := a.model.ServerCfg; ed != nil {
		if cur := ed.Current(); cur != nil && cur.Key == "3" {
			if n := nodeChild(cur, "CompressSize"); n != nil && n.Kind == model.ConfigNumber {
				compNum = n.Num
				compOK = true
			}
		}
	}
	a.modelMu.RUnlock()
	if !compOK || compNum != 99999 {
		t.Fatalf("CompressSize after edit = %v (ok=%v), want 99999", compNum, compOK)
	}

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// TestServerConfigEditorModelPick 验证"选择模型"的键（DefaultModelID）按 Enter
// 打开模型选择框而不是文本输入：候选项直接来自配置的 Model.Models，↑↓ 选择后
// Enter 把该模型的键写回配置（patch 与手工输入等价），Esc 则只关闭不写回。
func TestServerConfigEditorModelPick(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开编辑器
	waitAtomic(t, &b.gets, 1, "config/get calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// Model(行0) → DefaultModelID(行0)：当前值 1，配置里有模型 1/2 → 打开选择框。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 进入 Model 子页
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // DefaultModelID → 选择框
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerPicking })
	if got := a.snapshot(); got.ServerPickSel != 0 {
		t.Fatalf("initial pick row = %d, want 0 (current model 1)", got.ServerPickSel)
	}

	// 先验证 Esc：关闭选择框且不写回。
	ft.sendKey(input.SimpleKey(input.KeyEsc))
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.ServerPicking })
	if n := atomic.LoadInt64(&b.sets); n != 0 {
		t.Fatalf("config/set calls after Esc = %d, want 0", n)
	}

	// 重新打开，↓ 选中模型 2 后 Enter → 写回 {"Model":{"DefaultModelID":2}}。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 再次打开选择框
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerPicking })
	ft.sendKey(input.SimpleKey(input.KeyDown))  // 模型 2
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 写回

	waitAtomic(t, &b.sets, 1, "config/set calls after pick")
	b.mu.Lock()
	patches := append([]json.RawMessage(nil), b.patches...)
	b.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("set patches = %d, want 1", len(patches))
	}
	var patch map[string]any
	if err := json.Unmarshal(patches[0], &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	modelObj, ok := patch["Model"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model = %v, want object", patch["Model"])
	}
	if v, ok := modelObj["DefaultModelID"].(float64); !ok || v != 2 {
		t.Errorf("patch Model.DefaultModelID = %v, want 2", modelObj["DefaultModelID"])
	}

	// 写回后整配置重载，值以服务端为准；选择框已关闭。
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg && !s.ServerSaving && !s.ServerPicking })
	if got := readDefaultModelID(a); got != 2 {
		t.Fatalf("DefaultModelID after reload = %v, want 2", got)
	}

	// e：选择框内退回手工输入（写入不在 Model.Models 里的引用值时使用）：
	// 预填当前值 2，Ctrl+U 清空后输入 1，Enter 写回 {"Model":{"DefaultModelID":1}}。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 重新打开选择框
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerPicking })
	ft.sendKey(input.RuneKey('e', input.ModNone)) // 关闭选择框 → 值编辑
	waitSnapshot(t, a, func(s modelSnapshot) bool { return !s.ServerPicking })
	time.Sleep(30 * time.Millisecond)
	ft.sendKey(input.RuneKey('u', input.ModCtrl))
	ft.sendKey(input.RuneKey('1', input.ModNone))
	ft.sendKey(input.SimpleKey(input.KeyEnter))
	waitAtomic(t, &b.sets, 2, "config/set calls after manual input")
	b.mu.Lock()
	manual := append(json.RawMessage(nil), b.patches[1]...)
	b.mu.Unlock()
	if string(manual) != `{"Model":{"DefaultModelID":1}}` {
		t.Fatalf("manual input patch = %s, want {\"Model\":{\"DefaultModelID\":1}}", manual)
	}
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg && !s.ServerSaving })
	if got := readDefaultModelID(a); got != 1 {
		t.Fatalf("DefaultModelID after manual input = %v, want 1", got)
	}

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// readDefaultModelID 在读锁内读取服务端配置树中 Model.DefaultModelID 的数值
// （不存在或非数字时返回 -1）。
func readDefaultModelID(a *App) float64 {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	if ed := a.model.ServerCfg; ed != nil && ed.Root != nil {
		if n := nodeChild(nodeChild(ed.Root, "Model"), "DefaultModelID"); n != nil && n.Kind == model.ConfigNumber {
			return n.Num
		}
	}
	return -1
}

// waitNotSaving 轮询直到配置编辑器不在保存中（写回与全量重载已完成）。
// 经锁内快照读取，避免与事件循环并发读写。
func waitNotSaving(t *testing.T, a *App) {
	t.Helper()
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg && !s.ServerSaving })
}

// ---- 小辅助（供本文件 UI 驱动测试用）----

// nodeChild 返回节点的直接子节点。
func nodeChild(n *model.ConfigNode, key string) *model.ConfigNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}

// TestServerConfigEditorAddHooksItem 回归测试：Hooks 的事件数组页（命令列表）
// 提供「(新增)」行——Enter 追加一条空命令并写回整个数组（服务端对数组字段整体
// 替换）；整配置重载后仍停在该数组页并选中新行（元素是字符串、没有子页面，
// 用户按 Enter 即可编辑）。
func TestServerConfigEditorAddHooksItem(t *testing.T) {
	ft := newFakeTerm()
	b := &alkaid0Backend{Backend: demo.New(true)}
	// 预置含 Hooks 的服务端配置（alkaid0 v0.6.3 起 config/get 的结构）。
	b.cfg = map[string]any{}
	seed := `{"Version":1,"Hooks":{"OnCronjobWakeup":[],"OnTaskFinish":["echo done"],"OnUserMessage":[]}}`
	if err := json.Unmarshal([]byte(seed), &b.cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	a := New(ft, b)
	done := runApp(t, a)

	time.Sleep(100 * time.Millisecond)
	for _, r := range "/server" {
		ft.sendKey(input.RuneKey(r, input.ModNone))
	}
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 打开编辑器
	waitAtomic(t, &b.gets, 1, "config/get calls")
	waitSnapshot(t, a, func(s modelSnapshot) bool { return s.ServerCfg })

	// 根页 [Hooks, Version] → Hooks 对象页 [OnCronjobWakeup, OnTaskFinish,
	// OnUserMessage] → OnTaskFinish 数组页 [0, (新增)]。
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // Hooks
	ft.sendKey(input.SimpleKey(input.KeyDown))  // OnTaskFinish
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 数组页
	ft.sendKey(input.SimpleKey(input.KeyDown))  // (新增)
	ft.sendKey(input.SimpleKey(input.KeyEnter)) // 新增一条命令

	waitAtomic(t, &b.sets, 1, "config/set calls after add")
	waitNotSaving(t, a)

	b.mu.Lock()
	patches := append([]json.RawMessage(nil), b.patches...)
	b.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("set patches = %d, want 1", len(patches))
	}
	var patch struct {
		Hooks struct {
			OnTaskFinish []string `json:"OnTaskFinish"`
		} `json:"Hooks"`
	}
	if err := json.Unmarshal(patches[0], &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	got := patch.Hooks.OnTaskFinish
	if len(got) != 2 || got[0] != "echo done" || got[1] != "" {
		t.Fatalf("patch OnTaskFinish = %q, want [\"echo done\" \"\"]", got)
	}

	// 重载后仍停在 OnTaskFinish 数组页（选中新行由导航状态恢复）。
	waitSnapshot(t, a, func(s modelSnapshot) bool {
		return s.ServerCfg && !s.ServerSaving && s.ServerCurKey == "OnTaskFinish"
	})
	a.modelMu.RLock()
	ed := a.model.ServerCfg
	var rows int
	var newValue string
	if ed != nil {
		if cur := ed.Current(); cur != nil {
			rows = len(cur.Children)
			if rows > 1 {
				newValue = cur.Children[rows-1].Str
			}
		}
	}
	a.modelMu.RUnlock()
	if rows != 2 {
		t.Fatalf("OnTaskFinish rows = %d, want 2 (echo done + new empty)", rows)
	}
	if newValue != "" {
		t.Errorf("new row value = %q, want empty", newValue)
	}

	ft.sendKey(input.SimpleKey(input.KeyEsc))
	ft.sendKey(input.RuneKey('q', input.ModCtrl))
	time.Sleep(50 * time.Millisecond)
	ft.sendKey(input.RuneKey('y', input.ModNone))
	waitRun(t, done)
}

// TestRuleEditorKeyEditsAndSaves 验证规则字段的全屏 IDE 按键：Enter 打开、
// Enter 换行、Tab 缩进、Ctrl+S 提交写回；Esc 取消不改动节点值。
func TestRuleEditorKeyEditsAndSaves(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	ed := model.NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name == \"read\""}}`))
	ed.Focus([]string{"Agent"})
	a.model.ServerCfg = ed
	refresh := func() {}

	// Enter 打开全屏 IDE（而不是单行输入框）。
	if !a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh) {
		t.Fatal("Enter on AutoApprove should be handled")
	}
	if !ed.FullscreenEdit || !ed.Editing {
		t.Fatalf("fullscreen=%v editing=%v, want both true", ed.FullscreenEdit, ed.Editing)
	}

	// 光标移到末尾，Enter 换行，Tab 缩进后再输入注释。
	a.configEditorKey(input.SimpleKey(input.KeyPageDown), ed, refresh)
	a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh)
	a.configEditorKey(input.SimpleKey(input.KeyTab), ed, refresh)
	for _, r := range "// note" {
		a.configEditorKey(input.RuneKey(r, input.ModNone), ed, refresh)
	}
	want := "ToolCall.Name == \"read\"\n    // note"
	if got := ed.EditInput.Text(); got != want {
		t.Fatalf("editor text = %q, want %q", got, want)
	}

	// Ctrl+S 提交：退出 IDE 并把新表达式写回节点。
	a.configEditorKey(input.RuneKey('s', input.ModCtrl), ed, refresh)
	if ed.FullscreenEdit || ed.Editing {
		t.Fatal("Ctrl+S should commit and leave the rule IDE")
	}
	if n := ed.SelectedNode(); n == nil || n.Str != want {
		t.Fatalf("node value = %v, want %q", n, want)
	}

	// 重新打开后 Esc 取消：节点值保持刚才提交的内容。
	a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh)
	a.configEditorKey(input.RuneKey('x', input.ModNone), ed, refresh)
	a.configEditorKey(input.SimpleKey(input.KeyEsc), ed, refresh)
	if ed.FullscreenEdit || ed.Editing {
		t.Fatal("Esc should cancel the rule IDE")
	}
	if n := ed.SelectedNode(); n == nil || n.Str != want {
		t.Fatalf("node value after Esc = %v, want unchanged %q", n, want)
	}
}

// TestRuleEditorDiagnosticFollowsInput 验证全屏 IDE 每次按键后重新编译：
// 合法表达式没有诊断，输入未知函数后 EditDiag 指向出错行列。
func TestRuleEditorDiagnosticFollowsInput(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	ed := model.NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name == \"read\""}}`))
	ed.Focus([]string{"Agent"})
	a.model.ServerCfg = ed
	refresh := func() {}

	if !a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh) {
		t.Fatal("Enter on AutoApprove should open the rule IDE")
	}
	if ed.EditDiag != nil {
		t.Fatalf("initial diag = %+v, want nil for a valid rule", ed.EditDiag)
	}

	// 光标移到末尾，追加未知函数调用。
	a.configEditorKey(input.SimpleKey(input.KeyPageDown), ed, refresh)
	for _, r := range " && nope()" {
		a.configEditorKey(input.RuneKey(r, input.ModNone), ed, refresh)
	}
	d := ed.EditDiag
	if d == nil || d.Line != 1 || !strings.Contains(d.Message, "nope") {
		t.Fatalf("diag after typing = %+v, want unknown name nope on line 1", d)
	}
}

// TestRuleEditorCompletionKeys 验证补全下拉框的按键交互：输入触发、↑↓ 选择、
// Enter/Tab 接受、Esc 只关闭下拉框不退出编辑器。
func TestRuleEditorCompletionKeys(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	ed := model.NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":""}}`))
	ed.Focus([]string{"Agent"})
	a.model.ServerCfg = ed
	refresh := func() {}
	a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh)

	for _, r := range "has" {
		a.configEditorKey(input.RuneKey(r, input.ModNone), ed, refresh)
	}
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 1 || ed.RuleCompleteItems[0].Insert != "hasParam" {
		t.Fatalf("items = %+v (open=%v), want hasParam", ed.RuleCompleteItems, ed.RuleCompleteOpen)
	}
	a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh)
	if ed.RuleCompleteOpen || ed.EditInput.Text() != "hasParam" {
		t.Fatalf("after Enter: text=%q open=%v", ed.EditInput.Text(), ed.RuleCompleteOpen)
	}

	// 清空后输入 ToolCall.，↓ 选中 ID，Tab 接受。
	a.configEditorKey(input.RuneKey('u', input.ModCtrl), ed, refresh)
	for _, r := range "ToolCall." {
		a.configEditorKey(input.RuneKey(r, input.ModNone), ed, refresh)
	}
	if !ed.RuleCompleteOpen || len(ed.RuleCompleteItems) != 3 {
		t.Fatalf("member items = %+v, want 3", ed.RuleCompleteItems)
	}
	a.configEditorKey(input.SimpleKey(input.KeyDown), ed, refresh)
	a.configEditorKey(input.SimpleKey(input.KeyTab), ed, refresh)
	if got := ed.EditInput.Text(); got != "ToolCall.ID" {
		t.Fatalf("after Tab = %q, want ToolCall.ID", got)
	}

	// 再输入 Ag 触发变量补全；Esc 只关闭下拉框。
	for _, r := range " && Ag" {
		a.configEditorKey(input.RuneKey(r, input.ModNone), ed, refresh)
	}
	if !ed.RuleCompleteOpen {
		t.Fatal("popup should open for Ag")
	}
	a.configEditorKey(input.SimpleKey(input.KeyEsc), ed, refresh)
	if ed.RuleCompleteOpen || !ed.FullscreenEdit {
		t.Fatalf("Esc should only close the popup: open=%v fullscreen=%v", ed.RuleCompleteOpen, ed.FullscreenEdit)
	}
}

// TestRuleEditorSaveConfirm 验证规则有编译错误时 Ctrl+S 先进入确认：
// Esc 返回编辑、其它按键取消确认并照常输入、Enter 才带着错误写回。
func TestRuleEditorSaveConfirm(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	ed := model.NewConfigEditor(json.RawMessage(`{"Agent":{"AutoApprove":"ToolCall.Name =="}}`))
	ed.Focus([]string{"Agent"})
	a.model.ServerCfg = ed
	refresh := func() {}

	a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh)
	if ed.EditDiag == nil {
		t.Fatal("invalid rule should have a diagnostic after opening the IDE")
	}
	a.configEditorKey(input.RuneKey('s', input.ModCtrl), ed, refresh)
	if !ed.RuleSaveConfirm || !ed.Editing {
		t.Fatalf("confirm=%v editing=%v, want a save prompt without leaving the editor", ed.RuleSaveConfirm, ed.Editing)
	}

	// Esc：返回编辑，缓冲与诊断保持原样。
	a.configEditorKey(input.SimpleKey(input.KeyEsc), ed, refresh)
	if ed.RuleSaveConfirm || !ed.Editing || ed.EditDiag == nil {
		t.Fatalf("after Esc: confirm=%v editing=%v diag=%v, want still editing", ed.RuleSaveConfirm, ed.Editing, ed.EditDiag)
	}

	// 其它按键取消确认，并作为普通输入处理。
	a.configEditorKey(input.RuneKey('s', input.ModCtrl), ed, refresh)
	a.configEditorKey(input.RuneKey('x', input.ModNone), ed, refresh)
	if ed.RuleSaveConfirm {
		t.Fatal("typing should cancel the save confirmation")
	}
	if !strings.HasPrefix(ed.EditInput.Text(), "x") {
		t.Fatalf("text after cancel = %q, want the typed x at the start", ed.EditInput.Text())
	}

	// Enter：确认保存，退出 IDE 并把当前（有错误的）文本写回节点。
	before := ed.EditInput.Text()
	a.configEditorKey(input.RuneKey('s', input.ModCtrl), ed, refresh)
	if !ed.RuleSaveConfirm {
		t.Fatal("Ctrl+S on an invalid rule should ask for confirmation")
	}
	a.configEditorKey(input.SimpleKey(input.KeyEnter), ed, refresh)
	if ed.FullscreenEdit || ed.Editing {
		t.Fatal("Enter in the confirmation should save and leave the rule IDE")
	}
	if n := ed.SelectedNode(); n == nil || n.Str != before {
		t.Fatalf("saved value = %v, want %q", n, before)
	}
}

// TestConfigEditorKeyDeletesHooksItem 验证 /server 的 Hooks 命令数组页里 d 键
// 走 DeleteSelectedHook：选中命令从树中移除（写回 patch 由模型测试覆盖）。
func TestConfigEditorKeyDeletesHooksItem(t *testing.T) {
	a := New(newFakeTerm(), demo.New(true))
	ed := model.NewConfigEditor(json.RawMessage(`{"Hooks":{"OnUserMessage":["echo a","echo b"]}}`))
	ed.Focus([]string{"Hooks", "OnUserMessage"})
	a.model.ServerCfg = ed

	if !a.configEditorKey(input.RuneKey('d', input.ModNone), ed, func() {}) {
		t.Fatal("d on a Hooks command row should be handled")
	}
	ev := nodeChild(nodeChild(ed.Root, "Hooks"), "OnUserMessage")
	if ev == nil {
		t.Fatal("Hooks.OnUserMessage missing after delete")
	}
	if len(ev.Children) != 1 || ev.Children[0].Str != "echo b" {
		t.Fatalf("OnUserMessage after delete = %v, want [echo b]", ev.Children)
	}
}
