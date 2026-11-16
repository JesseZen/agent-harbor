package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Enables only controlled child tools on top of the real resolver and lock.
// This does not establish #19 native-client runtime readiness.
type controlledAccountService struct{ harborService }

func (h controlledAccountService) WithProfileClient(ctx context.Context, req profileClientRequest, op profileClientOperation, fn func(profileClientScope) error) error {
	return h.harborService.WithProfileClient(ctx, req, profileInspect, func(scope profileClientScope) error {
		scope.Environment.Executable = true
		return fn(scope)
	})
}
func (h controlledAccountService) ProfileCodexAccounts(ctx context.Context, req codexAccountRequest, in io.Reader, out, errout io.Writer) (profileAccountResult, error) {
	return profileCodexAccounts(ctx, h, req, in, out, errout)
}

func TestProfileAccountsScopeAndSignedOut(t *testing.T) {
	root := visibilityTestRoot(t)
	os.Unsetenv(originalEnvKey)
	t.Setenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS", "")
	bin := filepath.Join(root, "tools")
	os.Mkdir(bin, 0700)
	codex := filepath.Join(bin, "codex")
	if err := writeNativeClientFixture(codex, []byte(`#!/bin/sh
while [ "$1" = --no-daemon ] || [ "$1" = -c ]; do
 if [ "$1" = -c ]; then shift; fi
 shift
done
case "$1 $2" in
 "login status")
  if [ -f "$CODEX_HOME/auth.json" ]; then echo 'Logged in using ChatGPT'; else echo 'Not logged in'; exit 1; fi;;
 "logout ") unlink "$CODEX_HOME/auth.json";;
 *) exit 1;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	tool := filepath.Join(bin, "selector")
	if err := os.WriteFile(tool, []byte(`#!/bin/sh
case "$1" in
 list)
 printf '%s\n' '{"schema_version":1,"active_account_key":"synthetic::account","accounts":[{"account_key":"synthetic::account","email":"synthetic@example.test","active":true,"access_token_expires_at":4102444800}]}' ;;
 switch)
 [ "$2" = --skip-api ] && [ "$3" = --no-restart-daemon ] || exit 3
 printf '{}' > "$CODEX_HOME/auth.json" ;;
 login) exit 1;;
 *) exit 2;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_CODEX_AUTH_BIN", tool)
	h := controlledAccountService{}
	a, err := h.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.CreateProfile(profileCreateRequest{Name: "B", Kind: profileIsolated})
	if err != nil {
		t.Fatal(err)
	}
	binding := func(p profile) profileBinding {
		return profileBinding{WorkspaceID: p.WorkspaceID, ProfileID: p.ID, Client: "codex", EnvironmentID: p.Clients[0].ID}
	}
	ab, bb := binding(a), binding(b)
	action := func(b profileBinding, action string) (profileAccountResult, error) {
		return h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: b, Action: action}, nil, io.Discard, io.Discard)
	}
	// Production gate is still enforced before any child mutation.
	if _, err := (harborService{}).ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: ab, Action: "switch"}, nil, nil, nil); !errors.Is(err, ErrProfileNotReady) {
		t.Fatal(err)
	}
	for _, bind := range []profileBinding{ab, bb} {
		if r, err := action(bind, "switch"); err != nil || r.State != "selected" {
			t.Fatal(r, err)
		}
	}
	bBefore, err := os.ReadFile(filepath.Join(b.Clients[0].Home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Opening the page does not sign out or select an account.
	if r, err := action(ab, "inspect"); err != nil || r.State != "selected" {
		t.Fatal(r, err)
	}
	if r, err := action(ab, "signed-out"); err != nil || r.State != "signed-out" || len(r.Accounts) != 1 {
		t.Fatal(r, err)
	}
	bAfter, err := os.ReadFile(filepath.Join(b.Clients[0].Home, "auth.json"))
	if err != nil || string(bAfter) != string(bBefore) {
		t.Fatal("B was changed", err)
	}
	if r, err := action(ab, "login"); err == nil || r.State != "signed-out" {
		t.Fatal("failed login claimed success", r, err)
	}
	if err := h.ValidateProfileResult(ab, bb); !errors.Is(err, ErrProfileIdentity) {
		t.Fatal("stale result accepted", err)
	}
	// Unknown live activity blocks mutation even though the tool is usable.
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	s := session{Version: 2, ID: strings.Repeat("a", 32), Name: "A starting", CLI: "codex", Profile: &ab, RunToken: "pending", ProcessGroup: child.Process.Pid}
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := action(ab, "switch"); err == nil {
		t.Fatal("pending session ignored")
	}
	if _, err := action(bb, "switch"); err != nil {
		t.Fatal("A blocked B", err)
	}
}

func TestProfileAccountStaleUIResult(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	a := profileBinding{ProfileID: "A"}
	b := profileBinding{ProfileID: "B"}
	m.accountProfile = &b
	m.screen = "accounts-busy"
	n, _, handled := m.accountsPageInput(appAccountDone{profile: &a, result: "A done"})
	if !handled || n.(harborModel).accountResult != "" || n.(harborModel).screen != "accounts-busy" {
		t.Fatal("A result displayed in B")
	}
}

// Optional native evidence for an actually empty home. This deliberately does
// not bypass the production readiness gate or authorize a real login.
func TestProfileAccountsRealSignedOut(t *testing.T) {
	source := os.Getenv("HARBOR_TEST_CODEX_BIN")
	tool := os.Getenv("HARBOR_TEST_CODEX_AUTH_BIN")
	if source == "" || tool == "" {
		t.Skip("native Codex and pinned codex-auth not supplied")
	}
	root := visibilityTestRoot(t)
	os.Unsetenv(originalEnvKey)
	t.Setenv("AGENT_HARBOR_CODEX_AUTH_BIN", tool)
	t.Setenv("OPENAI_API_KEY", "")
	bin := filepath.Join(root, "native")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bin, "codex"), data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	h := controlledAccountService{}
	p, err := h.CreateProfile(profileCreateRequest{Name: "Signed out native", Kind: profileIsolated})
	if err != nil {
		t.Fatal(err)
	}
	b := profileBinding{WorkspaceID: p.WorkspaceID, ProfileID: p.ID, Client: "codex", EnvironmentID: p.Clients[0].ID}
	result, err := h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: b, Action: "inspect"}, nil, nil, nil)
	if err != nil || result.State != "signed-out" || len(result.Accounts) != 0 {
		t.Fatal(result.State, err)
	}
	if _, err = os.Stat(filepath.Join(p.Clients[0].Home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("native empty-home check created login", err)
	}
}
