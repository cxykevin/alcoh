package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// childByKey 返回节点下指定 Key 的子节点（大小写不敏感：新增条目用
// "Name"/"Command" 等大写键，从 config.json 加载的条目用小写键）。
func childByKey(n *ConfigNode, key string) *ConfigNode {
	for _, c := range n.Children {
		if strings.EqualFold(c.Key, key) {
			return c
		}
	}
	return nil
}

// TestPluginsEditorAddDeleteDeferred 验证 /plugins 编辑器的 plugins 数组支持
// 新增（AddPluginsItem，延迟写回）与删除（DeleteItem）。新条目标记为 pending：
// 编辑其任一字段前不参与整体数组 patch，编辑后才随整体数组写回。
func TestPluginsEditorAddDeleteDeferred(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":[{"name":"a","command":"/bin/a"}]}`))
	ed.isLocalConfig = true
	// 聚焦到 plugins 数组页。
	ed.Focus([]string{"plugins"})
	if !ed.CanAdd() {
		t.Fatal("plugins array should allow add")
	}
	if ed.AddRowIndex() < 0 {
		t.Fatal("plugins array should show add row")
	}
	ed.Selected = ed.AddRowIndex()
	if !ed.OnAddRow() {
		t.Fatal("selected row should be the add row")
	}
	if !ed.AddPluginsItem() {
		t.Fatal("AddPluginsItem failed")
	}
	// 自动进入新条目子页；条目尚未写回（pending）。
	cur := ed.Current()
	if cur == nil || cur.Key != "1" || cur.Kind != ConfigObject {
		t.Fatalf("after add should be inside new item, got %+v", cur)
	}
	if !cur.Pending {
		t.Fatal("new item should be pending (not yet written back)")
	}
	// 编辑新条目字段：清除 pending，patch 为整体数组（含两条）。
	if !ed.Back() {
		t.Fatal("Back should return to array page")
	}
	patch := ed.patchForNode(childByKey(ed.CurrentChildren()[1], "command"), nil)
	if ed.CurrentChildren()[1].Pending {
		t.Fatal("editing a field should clear pending on that item")
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(patch, &wrapper); err != nil {
		t.Fatalf("patch should be object wrapper: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(wrapper["plugins"], &arr); err != nil {
		t.Fatalf("patch plugins should be whole-array replace: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("patch array = %v, want 2 items", arr)
	}
	// 删除刚新增的条目：回到数组页，剩余 1 项。
	ed.Focus([]string{"plugins"})
	ed.Selected = len(ed.CurrentChildren()) - 1
	ed.Enter()
	if !ed.CanDelete() {
		t.Fatal("plugins item page should allow delete")
	}
	del := ed.DelRowIndex()
	ed.Selected = del
	if !ed.OnDeleteRow() {
		t.Fatal("selected row should be the delete row")
	}
	dpatch, ok := ed.DeleteItem()
	if !ok {
		t.Fatal("DeleteItem failed")
	}
	var dwrapper map[string]json.RawMessage
	if err := json.Unmarshal(dpatch, &dwrapper); err != nil {
		t.Fatalf("delete patch should be object wrapper: %v", err)
	}
	var darr []map[string]any
	if err := json.Unmarshal(dwrapper["plugins"], &darr); err != nil {
		t.Fatalf("delete patch plugins should be whole-array replace: %v", err)
	}
	if len(darr) != 1 || darr[0]["name"] != "a" {
		t.Fatalf("delete patch = %v, want only original item", darr)
	}
}

// TestPluginsPendingNotPersisted 验证 pending 的空条目不参与整体数组 patch：
// 新增后未编辑的条目不会因编辑其它条目的字段而被持久化（避免凭空生成空配置）。
func TestPluginsPendingNotPersisted(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":[{"command":"/bin/a"}]}`))
	ed.isLocalConfig = true
	ed.Focus([]string{"plugins"})
	ed.Selected = ed.AddRowIndex()
	if !ed.AddPluginsItem() {
		t.Fatal("AddPluginsItem failed")
	}
	// 编辑已存在条目 [0] 的 command：patch 应只含 [0]，过滤 pending 的 [1]。
	items := func() []*ConfigNode {
		ed.Back()
		return ed.CurrentChildren()
	}()
	if len(items) != 2 {
		t.Fatalf("array children = %d, want 2", len(items))
	}
	patch := ed.patchForNode(childByKey(items[0], "command"), nil)
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(patch, &wrapper); err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(wrapper["plugins"], &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["command"] != "/bin/a" {
		t.Fatalf("patch array = %v, want only existing item", arr)
	}
	// 编辑新条目 [1] 的 command：pending 清除，patch 含两条。
	patch2 := ed.patchForNode(childByKey(items[1], "command"), nil)
	var wrapper2 map[string]json.RawMessage
	if err := json.Unmarshal(patch2, &wrapper2); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wrapper2["plugins"], &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 2 {
		t.Fatalf("patch array = %v, want both items", arr)
	}
}

// TestOpenPlugins 验证 /plugins 打开与关闭的模态状态。
func TestOpenPlugins(t *testing.T) {
	m := New()
	m.OpenPlugins()
	if m.Modal != ModalPlugins {
		t.Fatalf("modal = %v, want ModalPlugins", m.Modal)
	}
	m.SetPluginsConfig(json.RawMessage(`{"version":1}`))
	if m.PluginsCfg == nil {
		t.Fatal("PluginsCfg should be set")
	}
	m.ClosePlugins()
	if m.Modal != NoModal || m.PluginsCfg != nil {
		t.Fatal("ClosePlugins should reset modal and editor")
	}
}

// TestPluginsCommandInPanel 验证 /plugins 出现在本地命令面板。
func TestPluginsCommandInPanel(t *testing.T) {
	m := New()
	found := false
	for _, c := range m.SlashCommands() {
		if c == "/plugins" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SlashCommands = %v, want include /plugins", m.SlashCommands())
	}
}

// TestNoPhantomPluginsOnOpen 验证打开时不再凭空生成 plugins 段：配置缺失时
// 聚焦停留在根页（Current 为根），根页提供「(新增)」入口；配置已有 plugins
// 数组时才聚焦进入该页。
func TestNoPhantomPluginsOnOpen(t *testing.T) {
	// 缺失：停留在根页，根页可新增（CanAdd），但不自动补空数组。
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"colorMode":"auto"}`))
	ed.isLocalConfig = true
	ed.Focus([]string{"plugins"})
	if cur := ed.Current(); cur == nil || cur.Parent != nil {
		t.Fatalf("without plugins section should stay at root, current = %+v", cur)
	}
	if ed.HasPluginsArray() {
		t.Fatal("plugins array should not be fabricated on open")
	}
	if !ed.CanAdd() {
		t.Fatal("local-config root should allow add when no plugins section")
	}
	// 已存在：聚焦进入 plugins 数组页。
	ed2 := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":[{"command":"/bin/a"}]}`))
	ed2.isLocalConfig = true
	ed2.Focus([]string{"plugins"})
	if cur := ed2.Current(); cur == nil || cur.Key != "plugins" || cur.Kind != ConfigArray {
		t.Fatalf("with plugins section should focus plugins page, got %+v", cur)
	}
	// plugins 数组页可继续新增条目。
	if !ed2.CanAdd() {
		t.Fatal("plugins page should allow add")
	}
	// 回到根页：已有 plugins 数组，根页不再提供新增行。
	ed2.Focus(nil)
	if ed2.CanAdd() {
		t.Fatal("root should not allow add when plugins array exists")
	}
	// null：非数组视为缺失，聚焦不进入 null 子页，根页可新增以替换修复。
	ed3 := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":null}`))
	ed3.isLocalConfig = true
	ed3.Focus([]string{"plugins"})
	if cur := ed3.Current(); cur == nil || cur.Parent != nil {
		t.Fatalf("plugins:null should stay at root, current = %+v", cur)
	}
	if !ed3.CanAdd() {
		t.Fatal("root should allow add to replace null plugins")
	}
	// 服务端配置编辑器根页不提供新增。
	ed4 := NewConfigEditor(json.RawMessage(`{"Config":{"Version":1}}`))
	if ed4.CanAdd() {
		t.Fatal("server config editor root should not allow add")
	}
}

// TestPluginsEditNestedArrayPatch 验证编辑插件条目内 args 等嵌套数组元素时，
// patch 仍为整个 plugins 数组（而非按 args 路径的局部数组段 patch——后者会被
// mergeJSON 误判为 map 导致 applyLocalConfigPatch 类型不匹配保存失败）。
func TestPluginsEditNestedArrayPatch(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":[{"command":"/bin/a","args":["--x"]}]}`))
	ed.isLocalConfig = true
	ed.Focus([]string{"plugins"})
	item := ed.CurrentChildren()[0]
	argsNode := childByKey(item, "args")
	if argsNode == nil || len(argsNode.Children) == 0 {
		t.Fatal("expected plugin item with args[0]")
	}
	patch := ed.patchForNode(argsNode.Children[0], nil)
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(patch, &wrapper); err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(wrapper["plugins"], &arr); err != nil {
		t.Fatalf("patch plugins should be whole-array replace, got %s", patch)
	}
	if len(arr) != 1 {
		t.Fatalf("patch array = %v, want 1 item", arr)
	}
}

// TestPluginsPendingExcludedFromDelete 验证删除已有插件条目时，patch 过滤尚未
// 写回的 pending 空条目（避免删除操作把空配置一并持久化）。
func TestPluginsPendingExcludedFromDelete(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":[{"command":"/bin/a"},{"command":"/bin/b"}]}`))
	ed.isLocalConfig = true
	ed.Focus([]string{"plugins"})
	// 新增 pending 条目 [2]。
	ed.Selected = ed.AddRowIndex()
	if !ed.AddPluginsItem() {
		t.Fatal("AddPluginsItem failed")
	}
	// 返回数组页并进入条目 [0]，删除之。
	ed.Back()
	ed.Selected = 0
	ed.Enter()
	ed.Selected = ed.DelRowIndex()
	if !ed.OnDeleteRow() {
		t.Fatal("selected row should be the delete row")
	}
	dpatch, ok := ed.DeleteItem()
	if !ok {
		t.Fatal("DeleteItem failed")
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(dpatch, &wrapper); err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(wrapper["plugins"], &arr); err != nil {
		t.Fatal(err)
	}
	// 删除 [0] 后剩余 "command":"/bin/b"；pending 的 [2] 被过滤。
	if len(arr) != 1 || arr[0]["command"] != "/bin/b" {
		t.Fatalf("delete patch array = %v, want only existing /bin/b item", arr)
	}
}

// TestAddPluginsArrayFromRoot 验证本地配置根页「(新增)」直接新建 plugins 段并
// 追加首条目（pending 延迟写回），编辑条目字段后随整体数组写回。
func TestAddPluginsArrayFromRoot(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"colorMode":"auto"}`))
	ed.isLocalConfig = true
	ed.Selected = ed.AddRowIndex()
	if !ed.OnAddRow() {
		t.Fatal("root page should show add row")
	}
	if !ed.AddPluginsArray() {
		t.Fatal("AddPluginsArray failed")
	}
	// 根下出现 plugins 数组，聚焦进入新条目子页（pending）。
	if !ed.HasPluginsArray() {
		t.Fatal("plugins array should exist after AddPluginsArray")
	}
	cur := ed.Current()
	if cur == nil || cur.Key != "0" || cur.Kind != ConfigObject || !cur.Pending {
		t.Fatalf("after add current = %+v, want pending plugin item 0", cur)
	}
	// 排序保持：colorMode < plugins < version。
	var order []string
	for _, c := range ed.Root.Children {
		order = append(order, c.Key)
	}
	if order[0] != "colorMode" || order[1] != "plugins" || order[2] != "version" {
		t.Fatalf("root child order = %v", order)
	}
	// 编辑该条目 command：整体写回含该条目。
	patch := ed.patchForNode(childByKey(cur, "command"), nil)
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(patch, &wrapper); err != nil {
		t.Fatalf("patch should be object wrapper: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(wrapper["plugins"], &arr); err != nil {
		t.Fatalf("patch plugins should be whole-array replace: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("patch array = %v, want 1 item", arr)
	}

	// plugins:null 时同样可经根页新增替换修复。
	ed3 := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":null}`))
	ed3.isLocalConfig = true
	if !ed3.AddPluginsArray() {
		t.Fatal("AddPluginsArray should replace null plugins")
	}
	if cur := ed3.Current(); cur == nil || cur.Key != "0" {
		t.Fatalf("after add on null current = %+v, want item 0", cur)
	}
}
