package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cxykevin/alcoh/internal/model"
	"github.com/cxykevin/alcoh/internal/renderer"
)

func configTreeSample() json.RawMessage {
	return json.RawMessage(`{
		"Version": 1,
		"Server": {"host": "127.0.0.1", "key": "alk-secret"},
		"Flags": {"Enable": true}
	}`)
}

// configTreeAgentsSample 含 Agent.Agents 集合（允许在末尾新增）。
func configTreeAgentsSample() json.RawMessage {
	return json.RawMessage(`{"Agent":{"Agents":{"main":{}}}}`)
}

// drawConfigTree 渲染配置树并返回行文本。
func drawConfigTree(ed *model.ConfigEditor, w, h int) []string {
	b := renderer.NewBuffer(w, h)
	canv := renderer.NewCanvas(b)
	ct := &ConfigTree{Theme: renderer.DefaultTheme(), Tree: ed}
	ct.Draw(canv, renderer.NewRect(0, 0, w, h))
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = effortRowText(b, y, w)
	}
	return rows
}

func TestConfigTreeDraw(t *testing.T) {
	ed := model.NewConfigEditor(configTreeSample())
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	// 面包屑（根页面）。
	if !strings.Contains(rows[0], "Config") {
		t.Errorf("breadcrumb = %q, want Config", rows[0])
	}
	// 根页行（字典序 Flags / Server / Version）：row1=Flags, row2=Server, row3=Version。
	if !strings.Contains(rows[1], "标志") || !strings.Contains(rows[1], "▸") {
		t.Errorf("Flags row = %q, want 标志 ▸", rows[1])
	}
	if !strings.Contains(rows[2], "服务端") {
		t.Errorf("Server row = %q, want 服务端", rows[2])
	}
	// 标量行：键名（翻译）+ 当前值，不显示类型。
	if !strings.Contains(rows[3], "配置版本 = 1") {
		t.Errorf("Version row = %q, want 配置版本 = 1", rows[3])
	}
	// 不显示数据类型与 keys 数量。
	for _, r := range rows {
		if strings.Contains(r, "(number)") || strings.Contains(r, "(string)") || strings.Contains(r, "keys)") {
			t.Errorf("row %q should not show type or key count", r)
		}
	}
	// 底部操作提示。
	if !strings.Contains(rows[h-1], "Enter 进入/编辑") || !strings.Contains(rows[h-1], "Esc 关闭") {
		t.Errorf("hint row = %q, want Enter 进入/编辑 / Esc 关闭", rows[h-1])
	}
}

func TestConfigTreeDrawEnteredPage(t *testing.T) {
	ed := model.NewConfigEditor(configTreeSample())
	// 进入 Server（根页 index 1）。
	ed.Move(1)
	if !ed.Enter() {
		t.Fatal("Enter should enter Server")
	}
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	// 面包屑显示翻译后的路径。
	if !strings.Contains(rows[0], "Config / 服务端") {
		t.Errorf("breadcrumb = %q, want Config / 服务端", rows[0])
	}
	// 子页行（字典序 host / key / path / port）：row1=host, row2=key。
	if !strings.Contains(rows[1], "主机 = \"127.0.0.1\"") {
		t.Errorf("host row = %q, want 主机 = \"127.0.0.1\"", rows[1])
	}
	// 敏感字段脱敏并标注，不显示原始明文。
	if !strings.Contains(rows[2], "访问密钥 = alk-*** (敏感)") {
		t.Errorf("key row = %q, want 访问密钥 = alk-*** (敏感)", rows[2])
	}
}

func TestConfigTreeDrawEditing(t *testing.T) {
	ed := model.NewConfigEditor(configTreeSample())
	ed.Move(1) // Server
	ed.Enter()
	// host 是子页 index 0，进入编辑模式。
	ed.BeginEdit()
	if !ed.Editing {
		t.Fatal("BeginEdit should enter editing")
	}
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	// 底部显示编辑提示与输入框提示行。
	if !strings.Contains(rows[h-1], "Esc 取消") || !strings.Contains(rows[h-1], "Enter 保存") {
		t.Errorf("edit hint row = %q, want Esc 取消 / Enter 保存", rows[h-1])
	}
	// 输入框行（倒数第二行）包含翻译后的键名 prompt 与预填的当前值。
	if !strings.Contains(rows[h-2], "编辑 主机:") || !strings.Contains(rows[h-2], "127.0.0.1") {
		t.Errorf("edit input row = %q, want 编辑 主机: + 127.0.0.1", rows[h-2])
	}
}

func TestConfigTreeDrawModelPreview(t *testing.T) {
	// Model.Models 集合页：每个模型键行尾以灰色显示 ModelName 预览；
	// ModelName 为空或缺失的键不显示预览。
	ed := model.NewConfigEditor(json.RawMessage(`{"Model":{"Models":{
		"0":{"ModelName":"GPT-4o"},
		"1":{"ModelName":""}
	}}}`))
	ed.Enter() // Model
	ed.Enter() // Models：页面 [0, 1]（数字键按数值排序）
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	if !strings.Contains(rows[1], "GPT-4o") {
		t.Errorf("row1 = %q, want GPT-4o ModelName preview", rows[1])
	}
	if strings.Contains(rows[2], "GPT-4o") {
		t.Errorf("row2 = %q, should not show preview (ModelName empty)", rows[2])
	}
}

func TestConfigTreeDrawAddRow(t *testing.T) {
	ed := model.NewConfigEditor(configTreeAgentsSample())
	// Agent → Agents：页面 [main, (新增)]。
	ed.Enter()
	ed.Enter()
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	if !strings.Contains(rows[1], "main") {
		t.Errorf("row1 = %q, want main", rows[1])
	}
	if !strings.Contains(rows[2], "(新增)") {
		t.Errorf("row2 = %q, want (新增) add row", rows[2])
	}
	// 移到「(新增)」行并高亮。
	ed.Move(1)
	if !ed.OnAddRow() {
		t.Fatal("Move should land on the add row")
	}
	rows = drawConfigTree(ed, w, h)
	if !strings.Contains(rows[2], "❯") || !strings.Contains(rows[2], "(新增)") {
		t.Errorf("highlighted add row = %q, want ❯ (新增)", rows[2])
	}
}

func TestConfigTreeDrawDeleteRow(t *testing.T) {
	// 代理项子页（Agent.Agents.main）：普通字段行之后是「(删除该项)」行。
	ed := model.NewConfigEditor(json.RawMessage(`{"Agent":{"Agents":{"main":{"AgentName":"Main","DisableSandbox":true}}}}`))
	ed.Enter() // Agent
	ed.Enter() // Agents：页面 [main, (新增)]，集合页本身不提供删除行
	ed.Enter() // main 子页：[AgentName, DisableSandbox, (删除该项)]
	if !ed.CanDelete() {
		t.Fatal("agent item page should allow delete")
	}
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	if !strings.Contains(rows[1], "代理名称 = \"Main\"") {
		t.Errorf("row1 = %q, want AgentName row", rows[1])
	}
	if !strings.Contains(rows[3], "(删除该项)") {
		t.Errorf("row3 = %q, want (删除该项) delete row", rows[3])
	}
	// 移到「(删除该项)」行并高亮。
	ed.Move(2)
	if !ed.OnDeleteRow() {
		t.Fatal("Move should land on delete row")
	}
	rows = drawConfigTree(ed, w, h)
	if !strings.Contains(rows[3], "❯") || !strings.Contains(rows[3], "(删除该项)") {
		t.Errorf("highlighted delete row = %q, want ❯ (删除该项)", rows[3])
	}
}

func TestConfigTreeDrawAddingKey(t *testing.T) {
	ed := model.NewConfigEditor(configTreeAgentsSample())
	// Agent → Agents → 「(新增)」行 → 新增键输入。
	ed.Enter()
	ed.Enter()
	ed.Move(1)
	if !ed.OnAddRow() || !ed.BeginAddKey() {
		t.Fatal("BeginAddKey should succeed on Agents add row")
	}
	const w, h = 60, 12
	rows := drawConfigTree(ed, w, h)

	if !strings.Contains(rows[h-2], "新键名:") {
		t.Errorf("add-key input row = %q, want 新键名:", rows[h-2])
	}
	if !strings.Contains(rows[h-1], "Enter 添加") {
		t.Errorf("add-key hint row = %q, want Enter 添加", rows[h-1])
	}
}
