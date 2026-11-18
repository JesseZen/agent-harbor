package main

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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
		if !ok || !strings.Contains(p.view(), "Select a Profile") {
			t.Fatal("missing evidence state")
		}
		for _, a := range []string{"account:i", "account:s", "account:?"} {
			found := false
			for _, r := range p.Hits {
				if r.ID == a {
					found = true
					if p.target(r.X, r.Y) != a {
						t.Fatal("wrong hit")
					}
				}
			}
			if !found {
				t.Fatalf("unreachable %s at %v", a, size)
			}
		}
		if size[0] == 60 {
			if !strings.HasSuffix(strings.TrimSpace(ansi.Strip(p.Lines[len(p.Lines)-1])), "...") {
				t.Fatal("missing overflow marker")
			}
			next, _ := m.Update(uiKey("?"))
			help := next.(harborModel)
			if help.screen != "help" || !strings.Contains(help.helpView(), "Not signed in") {
				t.Fatal("hidden action missing from help")
			}
			next, _ = m.Update(uiKey("o"))
			if next.(harborModel).screen != "accounts-result" {
				t.Fatal("hidden action lost its shortcut")
			}
		}
		m.screen = "accounts-busy"
		for _, key := range []string{"s", accountLoginKey, "q", "?", "esc"} {
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
