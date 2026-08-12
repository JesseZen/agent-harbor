package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newGlobalCommandGateTestHome(t *testing.T) *Home {
	t.Helper()
	home := NewHome()
	home.initialLoading = false
	t.Cleanup(home.cancel)
	return home
}

func TestCanOpenGlobalCommandMenuOnlyInNormalBrowsing(t *testing.T) {
	home := newGlobalCommandGateTestHome(t)
	if !home.CanOpenGlobalCommandMenu() {
		t.Fatal("normal Home browsing rejected global command menu")
	}
}

func TestColonReachesInsertModeWhenGlobalCommandMenuIsBlocked(t *testing.T) {
	home, _, capture := armHomeWithOneSession(t)
	updated, _ := home.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	home = updated.(*Home)
	if home.CanOpenGlobalCommandMenu() {
		t.Fatal("insert mode allowed global command menu")
	}

	updated, _ = home.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_ = updated.(*Home)
	if len(capture.calls) != 1 || capture.calls[0].text != ":" {
		t.Fatalf("insert sink calls = %#v, want one colon", capture.calls)
	}
}

func TestColonReachesAgentDeckSearch(t *testing.T) {
	home := newGlobalCommandGateTestHome(t)
	home.search.Show()
	if home.CanOpenGlobalCommandMenu() {
		t.Fatal("search allowed global command menu")
	}

	updated, _ := home.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	home = updated.(*Home)
	if got := home.search.input.Value(); got != ":" {
		t.Fatalf("search input = %q, want colon", got)
	}
}

func TestColonReachesAgentDeckDialogInput(t *testing.T) {
	home := newGlobalCommandGateTestHome(t)
	home.newDialog.Show()
	if home.CanOpenGlobalCommandMenu() {
		t.Fatal("dialog allowed global command menu")
	}

	updated, _ := home.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	home = updated.(*Home)
	if got := home.newDialog.nameInput.Value(); got != ":" {
		t.Fatalf("dialog input = %q, want colon", got)
	}
}
