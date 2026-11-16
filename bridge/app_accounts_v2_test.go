package main

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestAccountsV2ControlsAndResults(t *testing.T) {
	t.Setenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS", "1")
	for _, size := range [][2]int{{160, 45}, {100, 30}, {60, 20}} {
		m := newHarborModel(&fakeApplicationService{})
		m.screen = "accounts"
		m.width = size[0]
		m.height = size[1]
		p, ok := m.accountsPage()
		if !ok || !strings.Contains(p.view(), "Unknown") {
			t.Fatal("missing evidence state")
		}
		for _, a := range m.accountActions() {
			found := false
			for _, r := range p.Hits {
				if r.ID == a.ID {
					found = true
					if p.target(r.X, r.Y) != a.ID {
						t.Fatal("wrong hit")
					}
				}
			}
			if !found {
				t.Fatalf("unreachable %s at %v", a.ID, size)
			}
		}
		m.screen = "accounts-busy"
		for _, key := range []string{"s", "l", "q", "?", "esc"} {
			n, c, h := m.accountsPageInput(uiKey(key))
			if !h || c != nil || n.(harborModel).screen != "accounts-busy" {
				t.Fatalf("busy leaked %s", key)
			}
		}
		m.scroll = 900
		n, _, handled := m.accountsPageInput(appAccountDone{result: strings.Repeat("Synthetic session · Unknown: identity unconfirmed\n", 60), err: errors.New("Synthetic action failed")})
		m = n.(harborModel)
		if !handled || m.scroll != 0 || m.uiFocus != "account:back" || !strings.Contains(m.accountResult, "Synthetic action failed") {
			t.Fatal("completion discarded evidence")
		}
		n, _, _ = m.accountsPageInput(uiKey("pgdown"))
		m = n.(harborModel)
		p, _ = m.accountsPage()
		if !strings.Contains(p.view(), "Unknown") || strings.Contains(p.view(), "Select saved login") {
			t.Fatal("wrong result context")
		}
		found := false
		for _, r := range p.Hits {
			if r.ID == "account:back" {
				found = true
				n, _, _ = m.accountsPageInput(tea.MouseMsg{X: r.X, Y: r.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				m = n.(harborModel)
				break
			}
		}
		if !found || m.screen != "accounts" || m.scroll != 0 {
			t.Fatal("back unreachable")
		}
	}
}
func TestAccountsV2HiddenAndStale(t *testing.T) {
	t.Setenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS", "")
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "sessions"
	if !strings.Contains(m.View(), "Accounts") {
		t.Fatal("default account entry missing")
	}
	if _, ok := m.accountsPage(); ok {
		t.Fatal("account page visible")
	}
	n, _, handled := m.accountsPageInput(appAccountDone{result: "stale"})
	if !handled || n.(harborModel).screen != "sessions" {
		t.Fatal("stale result navigated")
	}
}

// The legacy seam remains source compatible but cannot mutate ambient login.
func TestAccountsRequireProfile(t *testing.T) {
	result, err := (harborService{}).CodexAccountAction("switch", nil, nil, nil)
	if err == nil || result != "" {
		t.Fatal("ambient account action accepted")
	}
}
