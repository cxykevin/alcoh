package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleConfig() json.RawMessage {
	return json.RawMessage(`{
		"Version": 1,
		"Model": {
			"ProviderKey": "sk-or-abc123",
			"DefaultModelID": 1,
			"Models": {
				"1": {"ModelName": "Kimi", "TokenLimit": 8192},
				"2": {"ModelName": "Deepseek", "TokenLimit": 128000}
			}
		},
		"Server": {"host": "127.0.0.1", "port": 7433, "key": "alk-secret", "path": "/acp"},
		"Flags": {"Enable": true, "Ratio": 0.7}
	}`)
}

// rowKeys 返回当前页面各行的 Key 列表（便于断言布局）。
func rowKeys(ed *ConfigEditor) []string {
	children := ed.CurrentChildren()
	out := make([]string, len(children))
	for i, n := range children {
		out[i] = n.Key
	}
	return out
}

func (n *ConfigNode) findChild(key string) *ConfigNode {
	for _, c := range n.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}

// kindOf 返回节点的 ConfigKind（nil 返回 -1），便于测试断言输出。
func kindOf(n *ConfigNode) int {
	if n == nil {
		return -1
	}
	return int(n.Kind)
}

func TestConfigEditorBuild(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	// 根页面：直接子对象与标量按自然顺序（数字优先，其余字典序）。
	want := []string{"Flags", "Model", "Server", "Version"}
	got := rowKeys(ed)
	if len(got) != len(want) {
		t.Fatalf("root rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("root row[%d] key = %q, want %q (rows=%v)", i, got[i], want[i], got)
		}
	}
	if ed.Selected != 0 {
		t.Fatalf("initial selected = %d, want 0", ed.Selected)
	}
	if ed.SelectedNode().Key != "Flags" {
		t.Fatalf("initial selected node = %v, want Flags", ed.SelectedNode())
	}
	// 嵌套对象不展开为行，仅在进入后可见。
	models := ed.Root.findChild("Model").findChild("Models")
	if len(models.Children) != 2 {
		t.Fatalf("Models children = %d, want 2", len(models.Children))
	}
}

func TestConfigEditorDisplayKey(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	// 翻译命中：结构体字段显示中文名。
	cases := []struct {
		key  string
		want string
	}{
		{"Model", "模型"},
		{"Version", "配置版本"},
	}
	for _, c := range cases {
		if n := ed.Root.findChild(c.key); n == nil || n.DisplayKey() != c.want {
			t.Errorf("DisplayKey(%s) = %q, want %q", c.key, n.DisplayKey(), c.want)
		}
	}
	// Model 子字段嵌套。
	for _, c := range []struct {
		key  string
		want string
	}{
		{"DefaultModelID", "默认模型 ID"},
		{"ProviderKey", "提供方密钥"},
		{"Models", "模型集合"},
	} {
		if n := ed.Root.findChild("Model").findChild(c.key); n == nil || n.DisplayKey() != c.want {
			t.Errorf("DisplayKey(%s) = %q, want %q", c.key, n.DisplayKey(), c.want)
		}
	}
	// Server 子字段嵌套。
	if n := ed.Root.findChild("Server").findChild("host"); n == nil || n.DisplayKey() != "主机" {
		t.Errorf("DisplayKey(host) = %q, want 主机", n.DisplayKey())
	}
	if n := ed.Root.findChild("Server").findChild("key"); n == nil || n.DisplayKey() != "访问密钥" {
		t.Errorf("DisplayKey(key) = %q, want 访问密钥", n.DisplayKey())
	}
	// ignoreSignals 为小写键，同样命中翻译。
	ed3 := NewConfigEditor(json.RawMessage(`{"ignoreSignals":["SIGINT"],"Version":1}`))
	if n := ed3.Root.findChild("ignoreSignals"); n == nil || n.DisplayKey() != "忽略系统 Signal" {
		t.Errorf("DisplayKey(ignoreSignals) = %q, want 忽略系统 Signal", n.DisplayKey())
	}
	// 未收录的键显示原始字段名（如 map 自定义键、数字模型 ID）。
	if n := ed.Root.findChild("Model").findChild("Models").findChild("1"); n.DisplayKey() != "1" {
		t.Errorf("DisplayKey(1) = %q, want raw 1", n.DisplayKey())
	}
	// 数组元素显示位置 "[i]"。
	ed2 := NewConfigEditor(json.RawMessage(`{"Args":["-a"]}`))
	args := ed2.Root.findChild("Args")
	ed2.Enter() // 进入 Args
	if n := ed2.CurrentChildren()[0]; n.DisplayKey() != "[0]" {
		t.Errorf("array elem DisplayKey = %q, want [0]", n.DisplayKey())
	}
	_ = args
}

// nodeAtPath 返回从 root 沿 path 逐层前进到的节点；任一段缺失时返回 nil。
func nodeAtPath(root *ConfigNode, path ...string) *ConfigNode {
	n := root
	for _, seg := range path {
		if n = n.findChild(seg); n == nil {
			return nil
		}
	}
	return n
}

// TestModelRefPreview 验证"选择模型"的键（SummaryModel / TitleModel / AgentModel /
// DefaultModelID / EmbeddingModelID / SearchSummaryModel）解析出行尾灰字提示的
// 模型名称：数字或字符串引用按 Models 的键匹配、未命中再按模型条目的 ModelID
// 匹配；ModelName 为空时退回 ModelID；引用不到模型或模型无名时不提示。
func TestModelRefPreview(t *testing.T) {
	cfg := `{
		"Model": {
			"DefaultModelID": 1,
			"Models": {
				"1": {"ModelName": "Kimi", "ModelID": "moonshot-v1-8k"},
				"2": {"ModelName": "", "ModelID": "deepseek-chat"},
				"3": {"ModelName": "", "ModelID": ""}
			}
		},
		"Agent": {
			"SummaryModel": 2,
			"TitleModel": 9,
			"MaxCallCount": 50,
			"Agents": {"main": {"AgentModel": 1}}
		},
		"Context": {"EmbeddingModelID": 3, "SearchSummaryModel": "moonshot-v1-8k"}
	}`
	ed := NewConfigEditor(json.RawMessage(cfg))
	cases := []struct {
		path []string
		want string
		ok   bool
	}{
		{[]string{"Model", "DefaultModelID"}, "Kimi", true},        // 数字键命中，显示 ModelName
		{[]string{"Agent", "SummaryModel"}, "deepseek-chat", true}, // ModelName 为空 → 退回 ModelID
		{[]string{"Agent", "TitleModel"}, "", false},               // 引用不到模型（没有键 9）
		{[]string{"Agent", "Agents", "main", "AgentModel"}, "Kimi", true},
		{[]string{"Context", "EmbeddingModelID"}, "", false},      // 模型既没有 ModelName 也没有 ModelID
		{[]string{"Context", "SearchSummaryModel"}, "Kimi", true}, // 字符串引用按 ModelID 匹配
		{[]string{"Agent", "MaxCallCount"}, "", false},            // 非选择模型的键
		{[]string{"Model", "Models"}, "", false},                  // 集合（对象）本身不提示
	}
	for _, c := range cases {
		n := nodeAtPath(ed.Root, c.path...)
		if n == nil {
			t.Fatalf("node %v not found", c.path)
		}
		got, ok := ed.ModelRefPreview(n)
		if got != c.want || ok != c.ok {
			t.Errorf("ModelRefPreview(%v) = (%q, %v), want (%q, %v)", c.path, got, ok, c.want, c.ok)
		}
	}
	// 配置里没有 Models 集合时，引用键一律不提示。
	ed2 := NewConfigEditor(json.RawMessage(`{"Agent":{"SummaryModel":1}}`))
	if got, ok := ed2.ModelRefPreview(nodeAtPath(ed2.Root, "Agent", "SummaryModel")); ok || got != "" {
		t.Errorf("without Models: ModelRefPreview = (%q, %v), want no hint", got, ok)
	}
	// nil 节点安全。
	if got, ok := ed.ModelRefPreview(nil); ok || got != "" {
		t.Errorf("ModelRefPreview(nil) = (%q, %v), want no hint", got, ok)
	}
}

// TestConfigEditorModelPick 验证"选择模型"键的模型选择框：候选项直接来自配置的
// Model.Models（顺序与 Models 集合页一致，隐藏模型与所有类型都列出），打开时
// 选中当前引用值，↑↓ 带环绕，Esc 只关闭不写回，Enter 写回选中模型的键且值的
// 类型与字段原有类型一致（数字字段写数字、字符串字段写字符串、null 按键推断）。
func TestConfigEditorModelPick(t *testing.T) {
	cfg := `{
		"Model": {"DefaultModelID": 2, "ProviderKey": "sk-test", "Models": {
			"1": {"ModelName": "Kimi", "ModelID": "kimi-k2", "Hide": true},
			"2": {"ModelName": "Deepseek", "ModelID": "deepseek-v3"},
			"3": {"ModelName": "Embed", "ModelID": "bge-m3", "Type": "embedding"},
			"4": {"ModelName": "", "ModelID": "bge-reranker", "Type": "rerank"},
			"5": {}
		}},
		"Agent": {"SummaryModel": "deepseek-chat", "TitleModel": null,
			"Agents": {"main": {"AgentModel": 1}}}
	}`
	ed := NewConfigEditor(json.RawMessage(cfg))

	// 候选列表：全部模型（含 Hide 与 embedding/rerank），ModelName 为空退回
	// ModelID，再为空退回键。
	opts := ed.ModelPickOptions()
	if len(opts) != 5 {
		t.Fatalf("pick options = %d, want 5 (%v)", len(opts), opts)
	}
	keys := []string{"1", "2", "3", "4", "5"}
	for i, want := range keys {
		if opts[i].Key != want {
			t.Errorf("option[%d].Key = %q, want %q", i, opts[i].Key, want)
		}
	}
	if !opts[0].Hide || opts[0].Name != "Kimi" || opts[0].ID != "kimi-k2" {
		t.Errorf("option[0] = %+v, want 隐藏的 Kimi/kimi-k2", opts[0])
	}
	if opts[2].Type != "embedding" || opts[3].Type != "rerank" {
		t.Errorf("types = %q/%q, want embedding/rerank", opts[2].Type, opts[3].Type)
	}
	if opts[3].Name != "bge-reranker" {
		t.Errorf("option[3].Name = %q, want ModelID fallback bge-reranker", opts[3].Name)
	}
	if opts[4].Name != "5" {
		t.Errorf("option[4].Name = %q, want key fallback 5", opts[4].Name)
	}

	// 数字引用：打开时选中当前值 2。
	ed.focusNode([]string{"Model", "DefaultModelID"})
	if !ed.BeginModelPick() {
		t.Fatal("BeginModelPick should open for Model.DefaultModelID")
	}
	if !ed.PickingModel || ed.PickSelected != 1 {
		t.Fatalf("pick state = (%v, %d), want (true, 1)", ed.PickingModel, ed.PickSelected)
	}
	// ↑↓ 带环绕。
	ed.PickMove(-1)
	if ed.PickSelected != 0 {
		t.Errorf("PickMove(-1) from 1 = %d, want 0", ed.PickSelected)
	}
	ed.PickMove(-1)
	if ed.PickSelected != len(opts)-1 {
		t.Errorf("PickMove wrap = %d, want %d", ed.PickSelected, len(opts)-1)
	}

	// Esc：关闭选择框、不改动节点，关闭后再提交也不产生 patch。
	ed.CancelModelPick()
	n := nodeAtPath(ed.Root, "Model", "DefaultModelID")
	if ed.PickingModel || n == nil || n.Kind != ConfigNumber || n.Num != 2 {
		t.Fatalf("after cancel: picking=%v node=%+v, want unchanged 2", ed.PickingModel, n)
	}
	if patch, ok := ed.CommitModelPick(); ok || patch != nil {
		t.Fatalf("CommitModelPick after cancel = (%s, %v), want (nil, false)", patch, ok)
	}

	// 数字字段：重新打开（当前值 2 落在索引 1），移到键 3 → 写回数字 3。
	if !ed.BeginModelPick() {
		t.Fatal("BeginModelPick should reopen")
	}
	ed.PickMove(1) // 索引 1 → 2（键 "3"）
	patch, ok := ed.CommitModelPick()
	if !ok || string(patch) != `{"Model":{"DefaultModelID":3}}` {
		t.Fatalf("numeric pick patch = %s (ok=%v), want {\"Model\":{\"DefaultModelID\":3}}", patch, ok)
	}
	if n.Kind != ConfigNumber || n.Num != 3 || ed.PickingModel {
		t.Errorf("node after numeric pick = kind %v num %v picking %v", n.Kind, n.Num, ed.PickingModel)
	}
	// 选择框已关闭：再次提交失败。
	if _, ok := ed.CommitModelPick(); ok {
		t.Error("CommitModelPick twice should fail")
	}

	// 字符串引用值按 ModelID 命中（deepseek-chat 无对应模型，停在首行）。
	ed.focusNode([]string{"Agent", "SummaryModel"})
	if !ed.BeginModelPick() || ed.PickSelected != 0 {
		t.Fatalf("SummaryModel pick = (%v, %d), want (true, 0)", ed.PickingModel, ed.PickSelected)
	}
	patch, ok = ed.CommitModelPick()
	if !ok || string(patch) != `{"Agent":{"SummaryModel":"1"}}` {
		t.Fatalf("string pick patch = %s (ok=%v), want {\"Agent\":{\"SummaryModel\":\"1\"}}", patch, ok)
	}
	sn := nodeAtPath(ed.Root, "Agent", "SummaryModel")
	if sn.Kind != ConfigString || sn.Str != "1" {
		t.Errorf("string node after pick = kind %v str %q, want string 1", sn.Kind, sn.Str)
	}

	// null（未赋值）：按键的形状推断为数字；子代理的 AgentModel 同理。
	ed.focusNode([]string{"Agent", "TitleModel"})
	if !ed.BeginModelPick() {
		t.Fatal("BeginModelPick should open for unset (null) TitleModel")
	}
	ed.PickMove(3) // 键 4
	patch, ok = ed.CommitModelPick()
	if !ok || string(patch) != `{"Agent":{"TitleModel":4}}` {
		t.Fatalf("null pick patch = %s (ok=%v), want {\"Agent\":{\"TitleModel\":4}}", patch, ok)
	}
	tn := nodeAtPath(ed.Root, "Agent", "TitleModel")
	if tn.Kind != ConfigNumber || tn.Num != 4 {
		t.Errorf("null node after pick = kind %v num %v, want number 4", tn.Kind, tn.Num)
	}
	// 子代理模型：AgentModel = 1 → 当前选中首行。
	ed.focusNode([]string{"Agent", "Agents", "main", "AgentModel"})
	if !ed.BeginModelPick() || ed.PickSelected != 0 {
		t.Fatalf("AgentModel pick = (%v, %d), want (true, 0)", ed.PickingModel, ed.PickSelected)
	}

	// 非"选择模型"的键、以及配置里没有模型时都不能打开选择框。
	ed.CancelModelPick()
	ed.focusNode([]string{"Model", "ProviderKey"})
	pk := ed.SelectedNode()
	if pk == nil {
		t.Fatalf("ProviderKey not found, current page key = %q", ed.Current().Key)
	}
	if ed.CanPickModel(pk) {
		t.Errorf("ProviderKey (selected %q) should not be pickable", pk.Key)
	}
	empty := NewConfigEditor(json.RawMessage(`{"Model":{"Models":{}},"Agent":{"SummaryModel":1}}`))
	empty.focusNode([]string{"Agent", "SummaryModel"})
	if empty.CanPickModel(empty.SelectedNode()) || empty.BeginModelPick() {
		t.Error("no models in config: pick should be unavailable")
	}
}

func TestConfigEditorNavigate(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	// 根页 Crumb。
	if got := ed.Crumb(); len(got) != 1 || got[0] != "Config" {
		t.Fatalf("root crumb = %v, want [Config]", got)
	}
	// 选中 Model（根页 index 1）并进入。
	ed.Move(1)
	if n := ed.SelectedNode(); n.Key != "Model" {
		t.Fatalf("selected = %v, want Model", n)
	}
	if !ed.Enter() {
		t.Fatal("Enter should succeed on object")
	}
	if got := ed.Crumb(); len(got) != 2 || got[0] != "Config" || got[1] != "模型" {
		t.Fatalf("crumb = %v, want [Config 模型]", got)
	}
	want := []string{"DefaultModelID", "Models", "ProviderKey"}
	got := rowKeys(ed)
	if len(got) != len(want) {
		t.Fatalf("model rows = %v, want %v", got, want)
	}
	if ed.Selected != 0 || ed.SelectedNode().Key != "DefaultModelID" {
		t.Fatalf("entered selected = %d (%v), want 0 DefaultModelID", ed.Selected, ed.SelectedNode())
	}
	// 进入标量行不动作。
	if ed.Enter() {
		t.Fatal("Enter on scalar should not navigate")
	}
	// Back 恢复根页与进入前的选中行（Model, index 1）。
	if !ed.Back() {
		t.Fatal("Back should succeed")
	}
	if got := ed.Crumb(); len(got) != 1 || got[0] != "Config" {
		t.Fatalf("crumb after back = %v, want [Config]", got)
	}
	if ed.Selected != 1 || ed.SelectedNode().Key != "Model" {
		t.Fatalf("selected after back = %d (%v), want 1 Model", ed.Selected, ed.SelectedNode())
	}
	if ed.Back() {
		t.Fatal("Back at root should fail")
	}
}

func TestConfigEditorCrumbNested(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	// Model → Models → 数字键对象。
	ed.Move(1) // Model
	ed.Enter()
	ed.Move(1) // Models
	ed.Enter()
	want := []string{"Config", "模型", "模型集合"}
	if got := ed.Crumb(); !equalStrings(got, want) {
		t.Fatalf("crumb = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestConfigSchemaHidden 验证 $schema 元数据字段不进入配置树（隐藏，不可编辑）。
func TestConfigSchemaHidden(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"$schema":"https://example/schema","Version":1}`))
	if ed.Root.findChild("$schema") != nil {
		t.Error("$schema should be hidden from the config tree")
	}
	got := rowKeys(ed)
	if len(got) != 1 || got[0] != "Version" {
		t.Errorf("root rows = %v, want [Version]", got)
	}
}

// TestConfigKeyLabelsAllApply 遍历翻译表，确保每个已收录字段名都能命中
// 翻译（防止新增字段时漏配 DisplayKey 路径）。
func TestConfigKeyLabelsAllApply(t *testing.T) {
	for key, label := range configKeyLabels {
		raw := json.RawMessage(`{"` + key + `": 1}`)
		ed := NewConfigEditor(raw)
		n := ed.Root.findChild(key)
		if n == nil {
			t.Errorf("configKeyLabels key %q not found in tree", key)
			continue
		}
		if got := n.DisplayKey(); got != label {
			t.Errorf("DisplayKey(%q) = %q, want %q", key, got, label)
		}
	}
}

func TestConfigEditorSensitiveMasking(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	key := ed.Root.findChild("Server").findChild("key")
	if !key.Sensitive {
		t.Fatal("Server.key should be sensitive")
	}
	if key.ValueText() != "alk-***" {
		t.Errorf("masked key = %q, want %q", key.ValueText(), "alk-***")
	}
	providerKey := ed.Root.findChild("Model").findChild("ProviderKey")
	if !providerKey.Sensitive {
		t.Fatal("Model.ProviderKey should be sensitive")
	}
	if providerKey.ValueText() != "sk-o***" {
		t.Errorf("masked ProviderKey = %q, want %q", providerKey.ValueText(), "sk-o***")
	}
	// 短值全掩码。
	if got := maskSecret("abc"); got != "***" {
		t.Errorf("maskSecret(abc) = %q, want ***", got)
	}
}

func TestConfigEditorMoveWrap(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	// 根页 4 行：Flags/Model/Server/Version。在首行按上移动到末尾。
	ed.Move(-1)
	if n := ed.SelectedNode(); n == nil || n.Key != "Version" {
		t.Fatalf("wrap selected = %v, want Version", n)
	}
	// 页面为空时 Move 不崩溃。
	ed2 := NewConfigEditor(json.RawMessage(`{}`))
	ed2.Move(1)
	if ed2.Selected != 0 {
		t.Fatalf("empty page selected = %d, want 0", ed2.Selected)
	}
}

func TestConfigEditorCommitNumber(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	ed.Move(1) // Model
	ed.Enter()
	// 子页 DefaultModelID 是 index 0。
	ed.BeginEdit()
	if !ed.Editing {
		t.Fatal("BeginEdit should enter editing")
	}
	ed.EditInput = inputFromString("7")
	patch, ok, errMsg := ed.CommitEdit()
	if !ok {
		t.Fatalf("CommitEdit failed: %s", errMsg)
	}
	if ed.Editing {
		t.Fatal("CommitEdit should exit editing")
	}
	if string(patch) != `{"Model":{"DefaultModelID":7}}` {
		t.Errorf("patch = %s, want {\"Model\":{\"DefaultModelID\":7}}", patch)
	}
}

func TestConfigEditorCommitString(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	ed.Move(2) // Server
	ed.Enter()
	// 子页 host 是 index 0。
	ed.BeginEdit()
	ed.EditInput = inputFromString("0.0.0.0")
	patch, ok, _ := ed.CommitEdit()
	if !ok {
		t.Fatal("CommitEdit failed")
	}
	if string(patch) != `{"Server":{"host":"0.0.0.0"}}` {
		t.Errorf("patch = %s, want {\"Server\":{\"host\":\"0.0.0.0\"}}", patch)
	}
	if ed.SelectedNode().Str != "0.0.0.0" {
		t.Errorf("host.Str = %q, want 0.0.0.0", ed.SelectedNode().Str)
	}
}

func TestConfigEditorCommitInvalidNumber(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	ed.Move(1) // Model
	ed.Enter()
	ed.BeginEdit()
	ed.EditInput = inputFromString("abc")
	if _, ok, _ := ed.CommitEdit(); ok {
		t.Fatal("CommitEdit should reject invalid number")
	}
	if !ed.Editing {
		t.Fatal("editing should stay active after failed commit")
	}
}

func TestConfigEditorToggleBool(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	// Flags 是根页 index 0，子页 Enable 是 index 0。
	ed.Enter()
	if !ed.SelectedNode().Bool {
		t.Fatal("precondition: Enable should start true")
	}
	patch := ed.ToggleBool()
	if ed.SelectedNode().Bool {
		t.Fatal("Enable should be toggled to false")
	}
	if string(patch) != `{"Flags":{"Enable":false}}` {
		t.Errorf("patch = %s, want {\"Flags\":{\"Enable\":false}}", patch)
	}
}

func TestConfigEditorCanAdd(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	if ed.CanAdd() {
		t.Error("root should not allow add")
	}
	// Model.Models 允许新增。
	ed.Move(1) // Model
	ed.Enter()
	ed.Move(1) // Models
	ed.Enter() // 进入 Models 集合页
	if !ed.CanAdd() || !ed.IsModels() {
		t.Error("Models should allow add and be models collection")
	}
	// 其他对象（Server）不允许新增。
	ed.Back()  // Models
	ed.Back()  // Model
	ed.Back()  // root
	ed.Move(2) // Server
	ed.Enter()
	if ed.CanAdd() {
		t.Error("Server should not allow add")
	}
	// Agent.Agents 允许新增（非数字键）。
	ed2 := NewConfigEditor(json.RawMessage(`{"Agent":{"Agents":{"main":{}}}}`))
	ed2.Enter() // Agent
	ed2.Enter() // Agents
	if !ed2.CanAdd() || ed2.IsModels() {
		t.Error("Agents should allow add and not be models collection")
	}
}

func TestConfigEditorAddModelsItem(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	ed.Move(1) // Model
	ed.Enter()
	ed.Move(1) // Models
	ed.Enter() // 进入 Models 集合页：页面 [1, 2, (新增)]
	ed.Move(2) // 移到末尾「(新增)」行
	if !ed.OnAddRow() {
		t.Fatalf("selected = %d, want on add row", ed.Selected)
	}
	patch, ok := ed.AddModelsItem()
	if !ok {
		t.Fatal("AddModelsItem should succeed")
	}
	models := ed.Root.findChild("Model").findChild("Models")
	if len(models.Children) != 3 {
		t.Fatalf("Models children = %d, want 3", len(models.Children))
	}
	// 新键为下一个数字 3，且自动进入新模型子页（Current 是新模型节点，
	// 选中首行 ModelName，而非停留在 Models 集合页）。
	if ed.Current() == models || ed.Current() == nil || ed.Current().Key != "3" {
		t.Fatalf("current page = %v, want new model page", ed.Current())
	}
	if ed.SelectedNode() == nil || ed.SelectedNode() != ed.Current().Children[0] {
		t.Fatalf("selected = %v, want first row of new model page", ed.SelectedNode())
	}
	// 数字字段（-1/8192/128000 等）应为 ConfigNumber 而非 ConfigNull：
	// buildConfigNode 需识别 map[string]any 中的 Go int（新增项直接构造，
	// 非 json.Unmarshal 的 float64），否则新建内容显示 null。
	cur := ed.Current()
	for _, k := range []string{"ModelTopP", "ModelTopK", "ModelTemperature", "TokenLimit", "CompressSize"} {
		n := cur.findChild(k)
		if n == nil || n.Kind != ConfigNumber {
			t.Errorf("%s: missing or not ConfigNumber (kind=%v)", k, kindOf(n))
		}
	}
	if psc := cur.findChild("ProviderSpecificConfig"); psc != nil {
		if n := psc.findChild("Dimension"); n == nil || n.Kind != ConfigNumber {
			t.Errorf("ProviderSpecificConfig.Dimension: missing or not ConfigNumber (kind=%v)", kindOf(n))
		}
	}

	// 新模型应加载完整配置项（README 默认值），而非仅 ModelName。
	var pm map[string]any
	if err := json.Unmarshal(patch, &pm); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	mm := pm["Model"].(map[string]any)
	ms := mm["Models"].(map[string]any)
	m3, ok := ms["3"].(map[string]any)
	if !ok {
		t.Fatalf("patch Model.Models.3 = %v, want object", ms["3"])
	}
	for _, k := range []string{"ModelName", "ModelID", "ModelDescription", "TokenLimit", "ProviderKey", "EnableThinking", "EnableToolCalling", "CompressSize"} {
		if _, ok := m3[k]; !ok {
			t.Errorf("patch Model.Models.3 missing %s", k)
		}
	}
	psc, ok := m3["ProviderSpecificConfig"].(map[string]any)
	if !ok {
		t.Fatal("patch Model.Models.3.ProviderSpecificConfig should be object")
	}
	for _, k := range []string{"EnableDeepseekThinking", "EnableReasoningEffort", "EnableTopP", "Dimension", "ToolPromptEnhance"} {
		if _, ok := psc[k]; !ok {
			t.Errorf("patch ProviderSpecificConfig missing %s", k)
		}
	}
}

func TestConfigEditorAddAgentsKey(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"Agents":{"main":{}}}}`))
	ed.Enter() // Agent
	ed.Enter() // Agents：页面 [main, (新增)]
	ed.Move(1) // 移到「(新增)」行
	if !ed.OnAddRow() {
		t.Fatal("should be on add row")
	}
	if !ed.BeginAddKey() {
		t.Fatal("BeginAddKey should succeed on Agents")
	}
	ed.AddInput = inputFromString("frontend")
	patch, ok, errMsg := ed.ConfirmAddKey()
	if !ok {
		t.Fatalf("ConfirmAddKey failed: %s", errMsg)
	}
	agents := ed.Root.findChild("Agent").findChild("Agents")
	if len(agents.Children) != 2 {
		t.Fatalf("Agents children = %d, want 2", len(agents.Children))
	}
	// 自动进入新代理子页（Current 是 frontend 节点，选中首行 AgentName）。
	if ed.Current() == agents || ed.Current() == nil || ed.Current().Key != "frontend" {
		t.Fatalf("current page = %v, want new agent page", ed.Current())
	}
	if ed.SelectedNode() == nil || ed.SelectedNode() != ed.Current().Children[0] {
		t.Fatalf("selected = %v, want first row of new agent page", ed.SelectedNode())
	}
	// 数字字段（AgentModel、Color 的 RGB）应为 ConfigNumber 而非 ConfigNull。
	cur := ed.Current()
	if n := cur.findChild("AgentModel"); n == nil || n.Kind != ConfigNumber || n.Num != 0 {
		t.Errorf("AgentModel: want ConfigNumber 0, got kind=%v", kindOf(n))
	}
	if col := cur.findChild("Color"); col != nil {
		for _, k := range []string{"Red", "Green", "Blue"} {
			if n := col.findChild(k); n == nil || n.Kind != ConfigNumber || n.Num != 128 {
				t.Errorf("Color.%s: want ConfigNumber 128, got kind=%v", k, kindOf(n))
			}
		}
	}
	// 新代理应加载完整配置项（README 默认值），而非仅 AgentName。
	var pa map[string]any
	if err := json.Unmarshal(patch, &pa); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	ag := pa["Agent"].(map[string]any)
	ags := ag["Agents"].(map[string]any)
	fe, ok := ags["frontend"].(map[string]any)
	if !ok {
		t.Fatalf("patch Agent.Agents.frontend = %v, want object", ags["frontend"])
	}
	for _, k := range []string{"AgentName", "AgentDescription", "AgentShortDescription", "AgentPrompt", "AgentModel", "AutoApprove", "AutoReject", "DisableSandbox"} {
		if _, ok := fe[k]; !ok {
			t.Errorf("patch Agent.Agents.frontend missing %s", k)
		}
	}
	// Color 子对象完整。
	col, ok := fe["Color"].(map[string]any)
	if !ok {
		t.Fatal("patch Agent.Agents.frontend.Color should be object")
	}
	for _, k := range []string{"Red", "Green", "Blue"} {
		if _, ok := col[k]; !ok {
			t.Errorf("patch Color missing %s", k)
		}
	}
}

func TestConfigEditorAddAgentsKeyEmpty(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"Agents":{"main":{}}}}`))
	ed.Enter() // Agent
	ed.Enter() // Agents
	ed.Move(1) // (新增)
	ed.BeginAddKey()
	ed.AddInput = inputFromString("")
	if _, ok, _ := ed.ConfirmAddKey(); ok {
		t.Fatal("ConfirmAddKey should reject empty key")
	}
	agents := ed.Root.findChild("Agent").findChild("Agents")
	if len(agents.Children) != 1 {
		t.Fatalf("Agents children should stay 1 after empty key")
	}
}

func TestConfigEditorCaptureRestoreState(t *testing.T) {
	raw := json.RawMessage(`{"Model":{"Models":{"1":{"ModelName":"Kimi","TokenLimit":8192}}}}`)
	ed := NewConfigEditor(raw)
	// 导航到模型 1 的 TokenLimit 并进入编辑，输入新值。
	ed.focusNode([]string{"Model", "Models", "1", "TokenLimit"})
	ed.BeginEdit()
	ed.EditInput = inputFromString("10000")

	s := ed.CaptureState()
	// 整配置重载重建后恢复：应保留导航、重新进入 TokenLimit 编辑并保留文本。
	ed2 := NewConfigEditor(raw)
	ed2.RestoreState(s)
	if !ed2.Editing {
		t.Fatal("restore should re-enter editing")
	}
	if ed2.EditNode == nil || ed2.EditNode.Key != "TokenLimit" {
		t.Fatalf("edit node = %v, want TokenLimit", ed2.EditNode)
	}
	if got := ed2.EditInput.Text(); got != "10000" {
		t.Fatalf("edit text = %q, want 10000", got)
	}
	// 恢复后提交应生成正确的数字 patch。
	patch, ok, errMsg := ed2.CommitEdit()
	if !ok {
		t.Fatalf("commit after restore failed: %s", errMsg)
	}
	var pm map[string]any
	if err := json.Unmarshal(patch, &pm); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	tl := pm["Model"].(map[string]any)["Models"].(map[string]any)["1"].(map[string]any)["TokenLimit"]
	if tl != 10000.0 {
		t.Fatalf("patch TokenLimit = %v, want 10000", tl)
	}

	// 未编辑时捕获/恢复保留导航位置与选中行（重载后不得回退到行首）。
	ed3 := NewConfigEditor(raw)
	ed3.Focus([]string{"Model", "Models"})
	ed3.Move(1) // 选中行 1（Models 页含「(新增)」行，共 2 行）
	ed4 := NewConfigEditor(raw)
	ed4.RestoreState(ed3.CaptureState())
	if cur := ed4.Current(); cur == nil || cur.Key != "Models" {
		t.Fatalf("restored current = %v, want Models page", cur)
	}
	if ed4.Selected != 1 {
		t.Fatalf("restored selected = %d, want 1", ed4.Selected)
	}
}

func TestConfigEditorFocus(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())

	// 空路径回到根页。
	ed.Focus(nil)
	if cur := ed.Current(); cur != ed.Root {
		t.Fatalf("focus nil current = %v, want root", cur)
	}

	// Focus 到集合页：Model → Models。
	ed.Focus([]string{"Model", "Models"})
	if cur := ed.Current(); cur == nil || cur.Key != "Models" {
		t.Fatalf("focus Models current = %v, want Models page", cur)
	}
	if len(ed.Stack) != 3 {
		t.Fatalf("focus Models stack depth = %d, want 3", len(ed.Stack))
	}

	// Focus 到模型项子页：Model → Models → 1。
	ed.Focus([]string{"Model", "Models", "1"})
	if cur := ed.Current(); cur == nil || cur.Key != "1" {
		t.Fatalf("focus model 1 current = %v, want model 1 page", cur)
	}
	if len(ed.Stack) != 4 {
		t.Fatalf("focus model 1 stack depth = %d, want 4", len(ed.Stack))
	}
	if ed.Selected != 0 {
		t.Fatalf("focus model 1 selected = %d, want 0", ed.Selected)
	}

	// Focus 到不存在的键停在最近可达页面（Models 集合页），不崩溃也不回根。
	ed.Focus([]string{"Model", "Models", "999"})
	if cur := ed.Current(); cur == nil || cur.Key != "Models" {
		t.Fatalf("focus missing key current = %v, want Models page", cur)
	}

	// Focus 到不存在的顶层字段停在根页。
	ed.Focus([]string{"NoSuchField"})
	if cur := ed.Current(); cur != ed.Root {
		t.Fatalf("focus missing top current = %v, want root", cur)
	}
}

// TestConfigEditorDeleteModelItem 验证「(删除该项)」行在模型项（Model.Models.*）
// 的子页内，删除当前模型项并返回 Models 集合页（对象键置零写回）。
func TestConfigEditorDeleteModelItem(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	models := ed.Root.findChild("Model").findChild("Models")
	ed.Move(1) // Model
	ed.Enter()
	ed.Move(1) // Models
	ed.Enter()
	// Models 集合页本身不提供删除行（只有项子页可删）。
	if ed.CanDelete() || ed.DelRowIndex() != -1 {
		t.Fatalf("Models collection should not expose delete row: del=%d", ed.DelRowIndex())
	}
	ed.Enter() // 进入模型 1 子页：字段与复制、重命名、删除操作行
	if !ed.CanDelete() {
		t.Fatal("model item page should allow delete")
	}
	if ed.DelRowIndex() != len(ed.CurrentChildren())+2 {
		t.Fatalf("model item del row = %d, want %d", ed.DelRowIndex(), len(ed.CurrentChildren())+2)
	}
	ed.Move(ed.DelRowIndex()) // 跳到操作行 (删除该项)
	if !ed.OnDeleteRow() {
		t.Fatalf("selected = %d, want on delete row", ed.Selected)
	}
	patch, ok := ed.DeleteItem()
	if !ok {
		t.Fatal("DeleteItem should succeed on model item")
	}
	if string(patch) != `{"Model":{"Models":{"1":null}}}` {
		t.Errorf("patch = %s, want {\"Model\":{\"Models\":{\"1\":null}}}", patch)
	}
	if len(models.Children) != 1 {
		t.Fatalf("Models children = %d, want 1", len(models.Children))
	}
	// 删除后返回 Models 集合页，选中相邻项（键 2）。
	if ed.Current() != models || ed.SelectedNode().Key != "2" {
		t.Fatalf("after delete: current=%v selected=%v, want Models page key 2", ed.Current(), ed.SelectedNode())
	}
}

// TestConfigEditorDeleteAgentItem 验证删除子代理项（含空对象项：只有 (删除该项)
// 一行），删除后返回 Agents 集合页并选中相邻项。
func TestConfigEditorDeleteAgentItem(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Agent":{"Agents":{"main":{"AgentName":"Main","DisableSandbox":true},"frontend":{}}}}`))
	agents := ed.Root.findChild("Agent").findChild("Agents")
	ed.Enter() // Agent
	ed.Enter() // Agents：页面 [frontend, main]（字典序）
	if ed.CanDelete() {
		t.Fatal("Agents collection should not allow delete")
	}
	ed.Enter() // 进入 frontend 子页（空对象，仅 (删除该项) 一行）
	if !ed.CanDelete() {
		t.Fatal("agent item page should allow delete")
	}
	if ed.DelRowIndex() != 2 {
		t.Fatalf("frontend page: del=%d, want delete row after copy and rename", ed.DelRowIndex())
	}
	ed.Move(ed.DelRowIndex())
	if !ed.OnDeleteRow() {
		t.Fatalf("selected = %d, want on delete row", ed.Selected)
	}
	patch, ok := ed.DeleteItem()
	if !ok {
		t.Fatal("DeleteItem should succeed on agent item")
	}
	if string(patch) != `{"Agent":{"Agents":{"frontend":null}}}` {
		t.Errorf("patch = %s, want {\"Agent\":{\"Agents\":{\"frontend\":null}}}", patch)
	}
	if len(agents.Children) != 1 {
		t.Fatalf("Agents children = %d, want 1", len(agents.Children))
	}
	// 删除后返回 Agents 集合页，选中相邻项（main）。
	if ed.Current() != agents || ed.SelectedNode().Key != "main" {
		t.Fatalf("after delete: current=%v selected=%v, want Agents page main", ed.Current(), ed.SelectedNode())
	}
}

// TestConfigEditorAddLanguageServerKey 验证在 Context.LSP.LanguageServers 集合页
// （与 Agent.Agents 同构的名称键 map）新增键（文件扩展名如 ".py"），加载默认配置
// （Command 空串 + Args 空数组），自动进入子页并生成仅含新键的 patch。
func TestConfigEditorAddLanguageServerKey(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Context":{"LSP":{"Enabled":false,"LanguageServers":{"go":{}},"IdleTimeout":600}}}`))
	ed.Focus([]string{"Context", "LSP", "LanguageServers"}) // 集合页：[go, (新增)]
	if !ed.CanAdd() {
		t.Fatal("LanguageServers should allow add")
	}
	if ed.CanDelete() {
		t.Fatal("LanguageServers collection should not allow delete")
	}
	ed.Move(1) // 移到「(新增)」行
	if !ed.OnAddRow() {
		t.Fatal("should be on add row")
	}
	if !ed.BeginAddKey() {
		t.Fatal("BeginAddKey should succeed on LanguageServers")
	}
	ed.AddInput = inputFromString(".py")
	patch, ok, errMsg := ed.ConfirmAddKey()
	if !ok {
		t.Fatalf("ConfirmAddKey failed: %s", errMsg)
	}
	ls := ed.Root.findChild("Context").findChild("LSP").findChild("LanguageServers")
	if len(ls.Children) != 2 {
		t.Fatalf("LanguageServers children = %d, want 2", len(ls.Children))
	}
	// 自动进入新语言服务器子页（Current 是 ".py" 节点，选中首行 Command）。
	if ed.Current() == ls || ed.Current() == nil || ed.Current().Key != ".py" {
		t.Fatalf("current page = %v, want new language server page", ed.Current())
	}
	if ed.SelectedNode() == nil || ed.SelectedNode() != ed.Current().Children[0] {
		t.Fatalf("selected = %v, want first row of new page", ed.SelectedNode())
	}
	cur := ed.Current()
	if n := cur.findChild("Command"); n == nil || n.Kind != ConfigString || n.Str != "" {
		t.Errorf("Command: want ConfigString empty, got kind=%v", kindOf(n))
	}
	if n := cur.findChild("Args"); n == nil || n.Kind != ConfigArray {
		t.Errorf("Args: want ConfigArray, got kind=%v", kindOf(n))
	}
	// patch 应仅含新键（完整 Command+Args 对象）。
	var pa map[string]any
	if err := json.Unmarshal(patch, &pa); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	lsObj := pa["Context"].(map[string]any)["LSP"].(map[string]any)["LanguageServers"].(map[string]any)
	py, ok := lsObj[".py"].(map[string]any)
	if !ok {
		t.Fatalf("patch LanguageServers[.py] = %v, want object", lsObj[".py"])
	}
	for _, k := range []string{"Command", "Args"} {
		if _, ok := py[k]; !ok {
			t.Errorf("patch LanguageServers[.py] missing %s", k)
		}
	}
}

// TestConfigEditorDeleteLanguageServerItem 验证删除语言服务器项，返回
// LanguageServers 集合页并选中相邻项，patch 为 null 键。
func TestConfigEditorDeleteLanguageServerItem(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Context":{"LSP":{"LanguageServers":{"go":{"Command":"gopls"},"py":{"Command":"pyright"}}}}}`))
	ls := ed.Root.findChild("Context").findChild("LSP").findChild("LanguageServers")
	ed.Focus([]string{"Context", "LSP", "LanguageServers", "go"}) // 进入 go 子页
	if !ed.CanDelete() {
		t.Fatal("language server item page should allow delete")
	}
	patch, ok := ed.DeleteItem()
	if !ok {
		t.Fatal("DeleteItem should succeed on language server item")
	}
	if string(patch) != `{"Context":{"LSP":{"LanguageServers":{"go":null}}}}` {
		t.Errorf("patch = %s, want {\"Context\":{\"LSP\":{\"LanguageServers\":{\"go\":null}}}}", patch)
	}
	if len(ls.Children) != 1 {
		t.Fatalf("LanguageServers children = %d, want 1", len(ls.Children))
	}
	// 删除后返回 LanguageServers 集合页，选中相邻项（py）。
	if ed.Current() != ls || ed.SelectedNode().Key != "py" {
		t.Fatalf("after delete: current=%v selected=%v, want LanguageServers page py", ed.Current(), ed.SelectedNode())
	}
}

// TestConfigEditorAddPhrasesItem 验证在 Context.Phrase.Phrases 数组页（list）新增
// 短语元素：直接追加 {Short,Text,Desc} 空默认值，自动进入新元素子页，返回的 patch
// 为整个数组（数组字段整体替换写回）。
func TestConfigEditorAddPhrasesItem(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Context":{"Phrase":{"Enable":true,"Phrases":[{"Short":"intro","Text":"hi","Desc":"d"}]}}}`))
	ed.Focus([]string{"Context", "Phrase", "Phrases"}) // 数组页：[0, (新增)]
	if !ed.CanAdd() {
		t.Fatal("Phrases array should allow add")
	}
	ed.Move(1) // 移到「(新增)」行
	if !ed.OnAddRow() {
		t.Fatal("should be on add row")
	}
	patch, ok := ed.AddPhrasesItem()
	if !ok {
		t.Fatal("AddPhrasesItem should succeed on Phrases")
	}
	phrases := ed.Root.findChild("Context").findChild("Phrase").findChild("Phrases")
	if len(phrases.Children) != 2 {
		t.Fatalf("Phrases children = %d, want 2", len(phrases.Children))
	}
	// 自动进入新元素子页（Current 是索引 1 节点，选中首行 Short）。
	if ed.Current() == phrases || ed.Current() == nil || ed.Current().Key != "1" {
		t.Fatalf("current page = %v, want new phrase element page", ed.Current())
	}
	if ed.SelectedNode() == nil || ed.SelectedNode() != ed.Current().Children[0] {
		t.Fatalf("selected = %v, want first row of new page", ed.SelectedNode())
	}
	cur := ed.Current()
	for _, k := range []string{"Short", "Text", "Desc"} {
		if n := cur.findChild(k); n == nil || n.Kind != ConfigString || n.Str != "" {
			t.Errorf("%s: want ConfigString empty, got kind=%v", k, kindOf(n))
		}
	}
	// patch 为整个数组（含新元素与原有元素）。
	var pa map[string]any
	if err := json.Unmarshal(patch, &pa); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	arr := pa["Context"].(map[string]any)["Phrase"].(map[string]any)["Phrases"].([]any)
	if len(arr) != 2 {
		t.Fatalf("patch Phrases len = %d, want 2", len(arr))
	}
	elem, ok := arr[1].(map[string]any)
	if !ok {
		t.Fatalf("patch Phrases[1] = %v, want object", arr[1])
	}
	for _, k := range []string{"Short", "Text", "Desc"} {
		if _, ok := elem[k]; !ok {
			t.Errorf("patch Phrases[1] missing %s", k)
		}
	}
}

// TestConfigEditorDeletePhrasesItem 验证删除短语数组元素：返回 Phrases 数组页，
// patch 为不含被删元素的整个数组。
func TestConfigEditorDeletePhrasesItem(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Context":{"Phrase":{"Enable":true,"Phrases":[{"Short":"intro","Text":"hi","Desc":"d"},{"Short":"plan","Text":"p","Desc":"pd"}]}}}`))
	phrases := ed.Root.findChild("Context").findChild("Phrase").findChild("Phrases")
	ed.Focus([]string{"Context", "Phrase", "Phrases", "0"}) // 进入元素 0 子页
	if !ed.CanDelete() {
		t.Fatal("phrase element page should allow delete")
	}
	patch, ok := ed.DeleteItem()
	if !ok {
		t.Fatal("DeleteItem should succeed on phrase element")
	}
	var pa map[string]any
	if err := json.Unmarshal(patch, &pa); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	arr := pa["Context"].(map[string]any)["Phrase"].(map[string]any)["Phrases"].([]any)
	if len(arr) != 1 {
		t.Fatalf("patch Phrases len = %d, want 1", len(arr))
	}
	if len(phrases.Children) != 1 {
		t.Fatalf("Phrases children = %d, want 1", len(phrases.Children))
	}
	// 删除后返回 Phrases 数组页，选中相邻元素（现索引 0，即原来的 plan）。
	if ed.Current() != phrases || ed.SelectedNode() == nil || ed.SelectedNode().Key != "0" {
		t.Fatalf("after delete: current=%v selected=%v, want Phrases page element 0", ed.Current(), ed.SelectedNode())
	}
	if el := ed.SelectedNode().findChild("Short"); el == nil || el.Str != "plan" {
		t.Fatalf("after delete adjacent element Short = %v, want plan", el)
	}
}

// TestConfigEditorEditPhrasesItemField 编辑短语数组元素的字段时，patch 必须是
// 整个数组的整体替换（服务端数组字段整体赋值，局部路径 patch 类型不匹配会保存
// 失败），未修改的兄弟元素与同元素其他字段保留。
func TestConfigEditorEditPhrasesItemField(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Context":{"Phrase":{"Enable":true,"Phrases":[{"Short":"intro","Text":"hi","Desc":"d"},{"Short":"plan","Text":"p","Desc":"pd"}]}}}`))
	ed.focusNode([]string{"Context", "Phrase", "Phrases", "0", "Text"}) // 选中 Text 行
	ed.BeginEdit()
	ed.EditInput = inputFromString("你好")
	patch, ok, errMsg := ed.CommitEdit()
	if !ok {
		t.Fatalf("CommitEdit failed: %s", errMsg)
	}
	var pa map[string]any
	if err := json.Unmarshal(patch, &pa); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	arr := pa["Context"].(map[string]any)["Phrase"].(map[string]any)["Phrases"].([]any)
	if len(arr) != 2 {
		t.Fatalf("patch Phrases len = %d, want 2", len(arr))
	}
	e0, _ := arr[0].(map[string]any)
	if e0["Text"] != "你好" {
		t.Errorf("patch Phrases[0].Text = %v, want 你好", e0["Text"])
	}
	if e0["Short"] != "intro" {
		t.Errorf("patch Phrases[0].Short = %v, want intro (sibling field preserved)", e0["Short"])
	}
	e1, _ := arr[1].(map[string]any)
	if e1["Short"] != "plan" || e1["Text"] != "p" {
		t.Errorf("patch Phrases[1] = %v, want unchanged sibling element", e1)
	}
}

// TestConfigEditorEditArrayElementString 编辑字符串数组元素（如 Args[*]）时，
// patch 同样是整个数组的整体替换。
func TestConfigEditorEditArrayElementString(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"Context":{"LSP":{"LanguageServers":{"go":{"Command":"gopls","Args":["-mode","stdio"]}}}}}`))
	ed.focusNode([]string{"Context", "LSP", "LanguageServers", "go", "Args", "0"})
	ed.BeginEdit()
	ed.EditInput = inputFromString("-verbose")
	patch, ok, errMsg := ed.CommitEdit()
	if !ok {
		t.Fatalf("CommitEdit failed: %s", errMsg)
	}
	var pa map[string]any
	if err := json.Unmarshal(patch, &pa); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	arr := pa["Context"].(map[string]any)["LSP"].(map[string]any)["LanguageServers"].(map[string]any)["go"].(map[string]any)["Args"].([]any)
	if len(arr) != 2 || arr[0] != "-verbose" || arr[1] != "stdio" {
		t.Errorf("Args = %v, want [-verbose stdio]", arr)
	}
}

// TestConfigEditorCanDelete 验证仅模型项/子代理项子页可删除，其他页面不可。
func TestConfigEditorCanDelete(t *testing.T) {
	// 根页面。
	if NewConfigEditor(sampleConfig()).CanDelete() {
		t.Fatal("root should not allow delete")
	}
	// Model 子页（标量字段页）。
	ed := NewConfigEditor(sampleConfig())
	ed.Move(1) // Model
	ed.Enter()
	if ed.CanDelete() {
		t.Fatal("Model page should not allow delete")
	}
	// Models 集合页（可新增，不可删）。
	ed.Move(1)
	ed.Enter()
	if ed.CanDelete() {
		t.Fatal("Models collection should not allow delete")
	}
	// 模型 1 子页（可删）。
	ed.Enter()
	if !ed.CanDelete() {
		t.Fatal("model item page should allow delete")
	}
	// Server 对象页（普通对象，不可删）。
	ed2 := NewConfigEditor(sampleConfig())
	ed2.Move(2) // Server
	ed2.Enter()
	if ed2.CanDelete() {
		t.Fatal("Server page should not allow delete")
	}
	// Args 数组页（数组项不是模型/代理项，不可删）。
	ed3 := NewConfigEditor(json.RawMessage(`{"Args":["-a","-b"]}`))
	ed3.Enter()
	if ed3.CanDelete() || ed3.DelRowIndex() != -1 {
		t.Fatal("Args array page should not allow delete")
	}
	// 模型项的标量字段页（如 ModelName 所在页就是模型项页，可删）；
	// 但模型项内部的对象子字段（如 ProviderSpecificConfig）页不可删。
	ed4 := NewConfigEditor(json.RawMessage(`{"Model":{"Models":{"1":{"ProviderSpecificConfig":{}}}}}`))
	ed4.Move(1) // Model
	ed4.Enter()
	ed4.Move(1) // Models
	ed4.Enter()
	ed4.Enter() // 模型 1 子页：[ProviderSpecificConfig, (删除该项)]
	if !ed4.CanDelete() {
		t.Fatal("model item page should allow delete")
	}
	if ed4.SelectedNode().Key != "ProviderSpecificConfig" {
		t.Fatalf("selected = %v, want ProviderSpecificConfig", ed4.SelectedNode())
	}
	ed4.Enter() // 进入其子页（普通对象，不可删）
	if ed4.CanDelete() || ed4.DelRowIndex() != -1 {
		t.Fatal("model nested object page should not allow delete")
	}
}

// TestConfigEditorDeleteRowWrap 验证「(删除该项)」行是普通可选行，参与环绕。
func TestConfigEditorDeleteRowWrap(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	ed.Move(1) // Model
	ed.Enter()
	ed.Move(1) // Models
	ed.Enter()
	ed.Enter() // 模型 1 子页：[ModelName, TokenLimit, (复制), (重命名键), (删除该项)]
	if ed.RowCount() != len(ed.CurrentChildren())+3 {
		t.Fatalf("model item row count = %d, want %d", ed.RowCount(), len(ed.CurrentChildren())+3)
	}
	ed.Move(ed.DelRowIndex()) // 跳到 (删除该项)
	if !ed.OnDeleteRow() {
		t.Fatal("should be on delete row")
	}
	ed.Move(1) // 环绕回到顶部
	if ed.Selected != 0 {
		t.Fatalf("selected after wrap = %d, want 0", ed.Selected)
	}
}

func TestConfigEditorNullEdit(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"New":null}`))
	if n := ed.SelectedNode(); n.Kind != ConfigNull {
		t.Fatalf("New kind = %v, want null", n.Kind)
	}
	ed.BeginEdit()
	if !ed.Editing {
		t.Fatal("null node should support editing")
	}
	ed.EditInput = inputFromString("value")
	patch, ok, _ := ed.CommitEdit()
	if !ok {
		t.Fatal("CommitEdit should succeed on null node")
	}
	if string(patch) != `{"New":"value"}` {
		t.Errorf("patch = %s, want {\"New\":\"value\"}", patch)
	}
	n := ed.SelectedNode()
	if n.Kind != ConfigString || n.Str != "value" {
		t.Errorf("New node = kind %v str %q, want string value", n.Kind, n.Str)
	}
}

func TestConfigEditorSensitiveEditStartsEmpty(t *testing.T) {
	ed := NewConfigEditor(sampleConfig())
	ed.Move(2) // Server
	ed.Enter()
	ed.Move(1) // key
	if !ed.SelectedNode().Sensitive {
		t.Fatal("selected should be Server.key (sensitive)")
	}
	ed.BeginEdit()
	if !ed.Editing {
		t.Fatal("sensitive node should support editing")
	}
	if text := ed.EditInput.Text(); text != "" {
		t.Errorf("sensitive edit buffer = %q, want empty (value masked)", text)
	}
}

func TestNodePatch(t *testing.T) {
	cases := []struct {
		path []string
		val  any
		want string
	}{
		{[]string{"Version"}, 2, `{"Version":2}`},
		{[]string{"Model", "DefaultModelID"}, 7, `{"Model":{"DefaultModelID":7}}`},
		{[]string{"Model", "Models", "1", "TokenLimit"}, 999, `{"Model":{"Models":{"1":{"TokenLimit":999}}}}`},
		{[]string{"Server", "host"}, "0.0.0.0", `{"Server":{"host":"0.0.0.0"}}`},
		{[]string{"Flags", "Enable"}, true, `{"Flags":{"Enable":true}}`},
	}
	for _, c := range cases {
		got := string(nodePatch(c.path, c.val))
		if got != c.want {
			t.Errorf("nodePatch(%v) = %s, want %s", c.path, got, c.want)
		}
	}
}

func TestConfigEditorSerialization(t *testing.T) {
	// 编辑若干节点后 AsValue 应保持合法 JSON，且 patch 可被服务端 unmarshal 合并。
	ed := NewConfigEditor(sampleConfig())
	ed.Move(1) // Model
	ed.Enter()
	ed.BeginEdit() // DefaultModelID
	ed.EditInput = inputFromString("5")
	patch, ok, _ := ed.CommitEdit()
	if !ok {
		t.Fatal("CommitEdit failed")
	}
	var merged map[string]any
	if err := json.Unmarshal(patch, &merged); err != nil {
		t.Fatalf("patch must be valid JSON: %v", err)
	}
	// 树序列化后仍可重新加载（往返）。
	raw, err := json.Marshal(ed.Root.AsValue())
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	ed2 := NewConfigEditor(raw)
	if len(ed2.CurrentChildren()) == 0 {
		t.Fatal("rebuilt tree should have rows")
	}
	if !strings.Contains(string(raw), `"Version"`) {
		t.Error("serialized tree should contain Version")
	}
}
