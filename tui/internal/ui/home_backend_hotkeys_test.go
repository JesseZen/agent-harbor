package ui

import (
	"strings"
	"testing"
)

func TestNewHomeWithBackendLoadsDefaultHotkeysForHelpBar(t *testing.T) {
	home := NewHomeWithBackend(&fakeCoreBackend{})
	t.Cleanup(home.cancel)

	for _, action := range []string{
		hotkeyNewSession,
		hotkeyQuickCreate,
		hotkeyCreateGroup,
		hotkeyRename,
		hotkeyDelete,
		hotkeyRestart,
	} {
		want := defaultHotkeyBindings[action]
		if got := home.actionKey(action); got != want {
			t.Fatalf("actionKey(%q) = %q, want %q (backend Home must load defaults for help bar)", action, got, want)
		}
	}

	home.width = 160
	home.height = 40
	bar := home.renderHelpBar()
	for _, needle := range []string{"New", "n", "Group", "g"} {
		if !strings.Contains(bar, needle) {
			t.Fatalf("help bar missing %q with loaded hotkeys:\n%s", needle, bar)
		}
	}
}
