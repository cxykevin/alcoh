package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadMissingFallsBackToDefaults(t *testing.T) {
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("filesystem unavailable")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != currentVersion || got.ColorMode != "auto" {
		t.Fatalf("defaults = %#v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("filesystem unavailable")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	}
	want := Defaults()
	want.ColorMode = "256"
	want.ToolsExpanded = true
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.ColorMode != want.ColorMode || !got.ToolsExpanded {
		t.Fatalf("loaded = %#v", got)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
}
