package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if !s.ShowSteps || !s.ShowTrace || !s.ShowContext {
		t.Fatalf("defaults should be all true: %+v", s)
	}
}

func TestLoadSettingsMissingFile(t *testing.T) {
	s, err := LoadSettings(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.ShowContext {
		t.Fatal("missing file should keep show_context true")
	}
}

func TestLoadSettingsExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.toml")
	if err := os.WriteFile(path, []byte("show_context = false\nshow_steps = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ShowContext || s.ShowSteps {
		t.Fatalf("expected explicit false: %+v", s)
	}
}

func TestLoadSettingsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.toml")
	if err := os.WriteFile(path, []byte("show_steps = [[[\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSettings(path)
	if err == nil {
		t.Fatal("expected error for malformed toml")
	}
}
