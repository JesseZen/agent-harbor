package preferences_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/preferences"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))

	if err := preferences.Save(preferences.Prefs{Locale: "zh-CN", Theme: "nord"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := preferences.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Locale != "zh-CN" || got.Theme != "nord" {
		t.Fatalf("got %+v", got)
	}
	path := filepath.Join(root, "config", "agent-harbor", "tui-preferences.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "missing"))

	got, err := preferences.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Locale != "" || got.Theme != "" {
		t.Fatalf("expected empty defaults, got %+v", got)
	}
}
