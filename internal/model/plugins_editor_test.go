package model

import (
	"encoding/json"
	"testing"
)

// TestPluginsEditorAddDelete 验证 /plugins 编辑器的 plugins 数组支持
// 新增（AddPluginsItem）与删除（DeleteItem），且 patch 为整体数组替换。
func TestPluginsEditorAddDelete(t *testing.T) {
	ed := NewConfigEditor(json.RawMessage(`{"version":1,"plugins":[{"name":"a","command":"/bin/a"}]}`))
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
	patch, ok := ed.AddPluginsItem()
	if !ok {
		t.Fatal("AddPluginsItem failed")
	}
	// 自动进入新条目子页，首行 Name。
	cur := ed.Current()
	if cur == nil || cur.Key != "1" || cur.Kind != ConfigObject {
		t.Fatalf("after add should be inside new item, got %+v", cur)
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(patch, &wrapper); err != nil {
		t.Fatalf("patch should be object wrapper: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(wrapper["plugins"], &arr); err != nil {
		t.Fatalf("patch plugins should be whole-array replace: %v", err)
	}
	if len(arr) != 2 || arr[1]["Command"] != "" || arr[1]["Disabled"] != false {
		t.Fatalf("patch array = %v, want 2 items with empty plugin", arr)
	}
	// 删除刚新增的条目：回到数组页，剩余 1 项。
	back := ed.Back()
	if !back {
		t.Fatal("Back should return to array page")
	}
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
