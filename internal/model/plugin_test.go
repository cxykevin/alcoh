package model

import (
	"strings"
	"testing"
)

// TestPluginCommandsInSlashList 验证插件命令进入命令面板并与本地/agent 命令去重。
func TestPluginCommandsInSlashList(t *testing.T) {
	m := New()
	m.SetPluginCommands([]string{"/hello", "/clear"})
	got := m.SlashCommands()
	if !containsString(got, "/hello") {
		t.Fatalf("SlashCommands = %v, want include /hello", got)
	}
	// 与本地命令重名时只出现一次。
	count := 0
	for _, c := range got {
		if c == "/clear" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("/clear appears %d times, want 1: %v", count, got)
	}
}

// TestPluginCommandInfo 验证插件命令描述进入补全说明。
func TestPluginCommandInfo(t *testing.T) {
	m := New()
	m.SetPluginCommands([]string{"/hello"})
	m.SetPluginCommandInfo(map[string]SlashCommandInfo{
		"/hello": {Name: "/hello", Description: "打个招呼", ArgsHint: "[name]"},
	})
	found := false
	for _, info := range m.slashCommandInfos() {
		if info.Name == "/hello" {
			found = true
			if info.Description != "打个招呼" || info.ArgsHint != "[name]" {
				t.Fatalf("plugin command info = %+v", info)
			}
		}
	}
	if !found {
		t.Fatal("plugin command description missing from slashCommandInfos")
	}
}

// TestPluginStatus 验证状态栏插件文本的设置/清除与排序。
func TestPluginStatus(t *testing.T) {
	m := New()
	m.SetPluginStatus("zz", "on")
	m.SetPluginStatus("aa", "ok")
	lines := m.PluginStatusLines()
	if len(lines) != 2 || lines[0] != "aa: ok" || lines[1] != "zz: on" {
		t.Fatalf("PluginStatusLines = %v, want sorted [aa: ok, zz: on]", lines)
	}
	m.SetPluginStatus("aa", "")
	lines = m.PluginStatusLines()
	if len(lines) != 1 || lines[0] != "zz: on" {
		t.Fatalf("after clear PluginStatusLines = %v", lines)
	}
	if strings.ContainsAny(strings.Join(lines, ""), "\n") {
		t.Fatal("status lines must be single-line")
	}
}
