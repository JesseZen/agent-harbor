package main

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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
	if strings.Contains(m.View(), "Accounts") {
		t.Fatal("experimental entry visible")
	}
	if _, ok := m.accountsPage(); ok {
		t.Fatal("account page visible")
	}
	n, _, handled := m.accountsPageInput(appAccountDone{result: "stale"})
	if !handled || n.(harborModel).screen != "sessions" {
		t.Fatal("stale result navigated")
	}
}

// Exercise the actual action, session store, process ownership and RPC boundary.
// The selector and all identities are synthetic; no native auth files are read.
func TestAccountsV2ServicePartialVerification(t *testing.T) {
	root := visibilityTestRoot(t)
	t.Setenv(originalEnvKey, "")
	// userEnvironment distinguishes an absent snapshot from an empty managed one.
	os.Unsetenv(originalEnvKey)
	selector := filepath.Join(root, "synthetic-selector")
	if err := os.WriteFile(selector, []byte("#!/bin/sh\ncase \"$1\" in\n switch) printf selected > \"$HOME/selected\";;\n list) printf '%s\\n' '{\"active_account_key\":\"user::account-b\"}';;\n *) exit 1;;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_CODEX_AUTH_BIN", selector)
	child := exec.Command("sleep", "30")
	child.Env = append(os.Environ(), "AH_SESSION_RUN_TOKEN=synthetic-account-v2")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	var records []session
	var observed []<-chan []string
	for i, tc := range []struct {
		name, active      string
		rejected, unknown bool
	}{
		{"Verified session", "account-b", false, false},
		{"Mismatch session", "account-a", false, false},
		{"Rejected session", "account-b", true, false},
		{"Unknown session", "account-b", false, true},
	} {
		socket, seen := accountFixtureServer(t, tc.active, tc.rejected)
		s := session{Version: 2, ID: fmt.Sprintf("synthetic-%d", i), Name: tc.name, CLI: "codex", CodexHome: os.Getenv("CODEX_HOME"), ProcessGroup: child.Process.Pid, WorkingDirectory: root, RunToken: "synthetic-account-v2", CodexServerPID: child.Process.Pid, CodexServerSocket: socket, CodexServerSocketID: codexSocketIdentity(socket)}
		if tc.unknown {
			s.RunToken = "wrong-synthetic-token"
		} else {
			observed = append(observed, seen)
		}
		records = append(records, s)
	}
	if err := withSessions(func([]session) ([]session, error) { return records, nil }); err != nil {
		t.Fatal(err)
	}
	result, err := (harborService{}).CodexAccountAction("switch", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "selected")); err != nil {
		t.Fatal("selector not called", err)
	}
	for _, want := range []string{
		"confirmed in 1 running Harbor session(s)",
		"Verified session · Verified: selected identity confirmed. Official quota: 5 hours: 42% used",
		"Mismatch session · Failed: identity unconfirmed. Codex did not confirm the selected account",
		"Rejected session · Failed: identity unconfirmed. Codex app/list was not accepted",
		"Unknown session · Unknown: identity unconfirmed. the running Codex server could not be verified",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing %q in %s", want, result)
		}
	}
	// Each inspectable session was checked for active turns, then refreshed.
	for _, seen := range observed {
		for range 2 {
			select {
			case <-seen:
			case <-time.After(2 * time.Second):
				t.Fatal("missing account RPC connection")
			}
		}
	}
}
