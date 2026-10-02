package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := configPath(); got != "prom-dash.yaml" {
		t.Errorf("without user config: got %q, want prom-dash.yaml", got)
	}
	want := filepath.Join(home, ".config", "prom-dash", "prom-dash.yaml")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := configPath(); got != want {
		t.Errorf("with user config: got %q, want %q", got, want)
	}
}
