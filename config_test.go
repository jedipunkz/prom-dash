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

func TestParseSpan(t *testing.T) {
	for in, want := range map[string]float64{"300s": 300, "12h": 43200, "1h30m": 5400, "30d": 2592000, "1.5d": 129600} {
		if got, err := parseSpan(in); err != nil || got != want {
			t.Errorf("parseSpan(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "abc", "0h", "-1d", "12"} {
		if _, err := parseSpan(in); err == nil {
			t.Errorf("parseSpan(%q): want error", in)
		}
	}
}

func TestFormatSpan(t *testing.T) {
	for in, want := range map[float64]string{300: "5m", 90: "90s", 43200: "12h", 86400: "1d", 180 * 86400: "180d", 5400: "90m"} {
		if got := formatSpan(in); got != want {
			t.Errorf("formatSpan(%v) = %q, want %q", in, got, want)
		}
	}
}
