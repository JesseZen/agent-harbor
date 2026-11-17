package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExperimentalAccountEntryAndResult(t *testing.T) {
	t.Setenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS", "1")
	m := newHarborModel(&fakeApplicationService{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(harborModel)
	if m.screen != "accounts" {
		t.Fatal(m.screen)
	}
	detail := m
	detail.screen = "accounts-detail"
	if !strings.Contains(detail.View(), "between replies") || !strings.Contains(detail.View(), "undocumented") {
		t.Fatalf("account entry lacks switching guidance: %s", m.View())
	}
	m.screen = "accounts-busy"
	next, _ = m.Update(appAccountDone{result: "Selected account confirmed in 2 running Harbor session(s)."})
	m = next.(harborModel)
	if m.screen != "accounts-result" || !strings.Contains(m.View(), "2 running Harbor") {
		t.Fatalf("account result is not visible: %s", m.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(harborModel).screen != "accounts" {
		t.Fatal("escape did not return to the account menu")
	}
}
