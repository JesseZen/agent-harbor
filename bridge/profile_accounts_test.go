package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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
 reset)
 [ "$2" = synthetic::account ] && [ "$#" = 2 ] || exit 4
 printf '%s\n' "$2" > "$CODEX_HOME/reset-fixture" ;;
 switch)
 [ "$2" = synthetic::account ] && [ "$3" = --json ] && [ "$4" = --no-restart-daemon ] || exit 3
 printf '{}' > "$CODEX_HOME/auth.json" ;;
 login)
 [ -f "$HOME/allow-fixture-login" ] || exit 1
 printf '{}' > "$CODEX_HOME/auth.json" ;;
 import)
 [ "$2" != "$CODEX_HOME/auth.json" ] && [ -f "$2" ] || exit 5
 printf 'imported' > "$CODEX_HOME/import-fixture" ;;
 *) exit 2;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_CODEX_AUTH_BIN", tool)
	h := harborService{}
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
		return h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: b, Action: action, Selected: "synthetic::account"}, nil, io.Discard, io.Discard)
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
	_, err = h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: ab, Action: "switch", Selected: "synthetic@example.test"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no longer in this Profile") {
		t.Fatalf("switch accepted a non-key selector: %v", err)
	}
	for _, key := range []string{"synthetic@example.test", "synthetic::account"} {
		_, err := h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: ab, Action: "reset", Selected: key}, nil, nil, nil)
		if (err == nil) != (key == "synthetic::account") {
			t.Fatalf("reset with selector %q: %v", key, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(a.Clients[0].Home, "reset-fixture")); err != nil || strings.TrimSpace(string(data)) != "synthetic::account" {
		t.Fatal("reset did not use the captured Profile and exact account key", err)
	}
	if _, err := os.Stat(filepath.Join(b.Clients[0].Home, "reset-fixture")); !os.IsNotExist(err) {
		t.Fatal("reset ran in another Profile", err)
	}
	bAfter, err := os.ReadFile(filepath.Join(b.Clients[0].Home, "auth.json"))
	if err != nil || string(bAfter) != string(bBefore) {
		t.Fatal("B was changed", err)
	}
	if r, err := action(ab, "login"); err == nil || r.login != nil {
		t.Fatal("failed login claimed success", r, err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), "allow-fixture-login"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	staged, err := action(ab, "login")
	if err != nil || staged.login == nil || staged.login.key != "synthetic::account" {
		t.Fatal("authorization did not return a staged login", err)
	}
	defer staged.login.close()
	if _, err := os.Stat(filepath.Join(a.Clients[0].Home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("authorization changed the Profile login", err)
	}
	if _, err := h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: bb, Action: "login", login: staged.login}, nil, nil, nil); !errors.Is(err, ErrProfileIdentity) {
		t.Fatal("staged login accepted by a different Profile", err)
	}
	committed, err := h.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: ab, Action: "login", login: staged.login}, nil, nil, nil)
	if err != nil || committed.Selected != staged.login.key {
		t.Fatal("staged login not selected", err)
	}
	if _, err := os.Stat(filepath.Join(a.Clients[0].Home, "import-fixture")); err != nil {
		t.Fatal("new login not imported into the Profile", err)
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

func TestBlockedProfileAccountSwitchKeepsRoster(t *testing.T) {
	root := visibilityTestRoot(t)
	h := harborService{}
	p := mustProfile(t, h, "Coding")
	binding := bindingFor(p)
	tool := filepath.Join(root, "codex-auth")
	if err := os.WriteFile(tool, []byte(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%s\n' '{"schema_version":1,"accounts":[{"account_key":"backup"}]}'
else
  printf 'ran' > invoked
  exit 1
fi
`), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_CODEX_AUTH_BIN", tool)
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	running := session{Version: 2, ID: strings.Repeat("a", 32), Name: "Running Codex", CLI: "codex", Profile: &binding, ProcessGroup: child.Process.Pid}
	if err := withSessions(func([]session) ([]session, error) { return []session{running}, nil }); err != nil {
		t.Fatal(err)
	}
	m := newHarborModel(h)
	defer m.cancelRefresh()
	m.screen, m.accountProfile = "accounts", &binding
	initial := walkthroughAccounts(binding)
	initial.Accounts[0].Usage = walkthroughUsage(18, 100)
	m.cacheAccountSnapshot(initial)
	m.reconcileAccountRow()
	m.accountRow = "backup"
	next, cmd := m.Update(uiKey("s"))
	m = next.(harborModel)
	if cmd == nil || m.screen != "accounts-busy" {
		t.Fatal("Switch did not start an action")
	}
	next, _ = m.Update(cmd())
	if _, err := os.Stat(filepath.Join(p.Clients[0].Home, "invoked")); !os.IsNotExist(err) {
		t.Fatal("blocked login called codex-auth", err)
	}
	result := next.(harborModel)
	next, _ = result.Update(uiKey("esc"))
	m = next.(harborModel)
	if m.screen != "accounts" || !m.currentAccount("primary") || len(m.accountSnapshot.Accounts) != 2 || !strings.Contains(m.View(), "82%") {
		t.Fatal("blocked login cleared the existing account or quota")
	}
	if !strings.Contains(result.accountResult, running.Name+": Herdr") || strings.Contains(result.accountResult, "Login: unconfirmed") {
		t.Fatalf("blocked result misreported the login state: %s", result.accountResult)
	}
}

func TestAccountActivityUsesVerifiedHerdrRunNotConversationOrAppServer(t *testing.T) {
	root := visibilityTestRoot(t)
	socket := filepath.Join(root, "herdr", "accounts.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	profile := profileBinding{WorkspaceID: "workspace", ProfileID: "profile", EnvironmentID: "environment", Client: "codex"}
	s := session{Name: "Retained window", CLI: "codex", Mode: "magpie", CodexConnection: "caller", Profile: &profile,
		HerdrPane: "pane", HerdrTab: "tab", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket),
		RunToken: "account-test", CodexHome: filepath.Join(root, "codex"), Conversation: &conversationEvidence{Status: "conflict"}}
	s.ProcessGroup = startSessionProcessGroupFixture(t, root, map[string]string{
		"AH_SESSION_RUN_TOKEN": s.RunToken, "CODEX_HOME": s.CodexHome,
		"AH_PROFILE_WORKSPACE": profile.WorkspaceID, "AH_PROFILE_ID": profile.ProfileID, "AH_PROFILE_ENVIRONMENT": profile.EnvironmentID,
	})
	var activity atomic.Value
	activity.Store("working")
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var request struct{ Method string }
			if json.NewDecoder(conn).Decode(&request) == nil {
				var result any
				if request.Method == "agent.list" {
					result = map[string]any{"agents": []herdrAgent{{Pane: s.HerdrPane, Tab: s.HerdrTab, Terminal: "terminal", Workspace: "workspace", Client: s.CLI, Activity: activity.Load().(string)}}}
				} else if request.Method == "pane.process_info" {
					result = map[string]any{"process_info": map[string]any{"pane_id": s.HerdrPane, "foreground_process_group_id": s.ProcessGroup}}
				}
				_ = json.NewEncoder(conn).Encode(map[string]any{"result": result})
			}
			_ = conn.Close()
		}
	}()
	for _, status := range []string{"working", "blocked", "unknown", "idle", "done"} {
		activity.Store(status)
		live, err := accountIdleSessions(context.Background(), []session{s})
		var pending *accountTurnPending
		switch status {
		case "working", "blocked":
			if !errors.As(err, &pending) || pending.name != s.Name {
				t.Fatalf("%s did not defer: %v", status, err)
			}
		case "unknown":
			if err == nil || errors.As(err, &pending) || !strings.Contains(err.Error(), s.Name) {
				t.Fatalf("unknown treated as safe or transient: %v", err)
			}
		default:
			if err != nil || len(live) != 1 {
				t.Fatalf("verified %s window without an App Server blocked login: %v", status, err)
			}
			if got := applyRunningCodexLogin(context.Background(), live[0], profileAccountResult{State: "selected"}); got.State != "not confirmed" {
				t.Fatalf("old window claimed login adoption: %+v", got)
			}
		}
	}
	wrong := s
	wrong.RunToken = "another-run"
	if _, err := accountIdleSessions(context.Background(), []session{wrong}); err == nil {
		t.Fatal("wrong foreground launch token was accepted")
	}
	if _, err := accountIdleSessions(context.Background(), []session{s, s}); err == nil {
		t.Fatal("ambiguous run was accepted")
	}
	apiOnly := s
	apiOnly.CodexConnection = "api"
	activity.Store("working")
	if live, err := accountIdleSessions(context.Background(), []session{apiOnly}); err != nil || len(live) != 0 {
		t.Fatal("API-only window participated in login change", err)
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
