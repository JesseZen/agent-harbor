package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func profileFixture(t *testing.T) (harborService, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	t.Setenv(originalEnvKey, "")
	// No managed snapshot in this isolated test process.
	os.Unsetenv(originalEnvKey)
	if err := ensureHome(root); err != nil {
		t.Fatal(err)
	}
	return harborService{}, root
}
func bindingFor(p profile) profileBinding {
	return profileBinding{WorkspaceID: p.WorkspaceID, ProfileID: p.ID, Client: "codex", EnvironmentID: p.Clients[0].ID}
}
func mustProfile(t *testing.T, h harborService, name string) profile {
	t.Helper()
	p, err := h.CreateProfile(profileCreateRequest{Name: name, Kind: profileIsolated})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileContractABAndReadOnly(t *testing.T) {
	h, root := profileFixture(t)
	t.Setenv("OPENAI_API_KEY", "synthetic-unrelated")
	t.Setenv("CODEX_HOME", "/ambient/forbidden")
	t.Setenv("CODEX_SQLITE_HOME", "/ambient/database")
	t.Setenv("CODEX_CONFIG", "ambient")
	t.Setenv("PROFILE_TOOLING_TEST", "preserved")
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	before, _ := os.ReadFile(filepath.Join(root, "profiles.json"))
	ae, err := h.ResolveProfileClient(profileClientRequest{bindingFor(a)})
	if err != nil {
		t.Fatal(err)
	}
	be, err := h.ResolveProfileClient(profileClientRequest{bindingFor(b)})
	if err != nil {
		t.Fatal(err)
	}
	if ae.Home == be.Home || ae.SQLite == be.SQLite || ae.Runtime == be.Runtime {
		t.Fatal("A/B roots shared")
	}
	for _, e := range []profileClientEnvironment{ae, be} {
		if environmentValue(e.Environment, "CODEX_HOME") != e.Home || environmentValue(e.Environment, "CODEX_SQLITE_HOME") != e.SQLite {
			t.Fatal("ambient home won")
		}
		if environmentValue(e.Environment, "OPENAI_API_KEY") != "" || environmentValue(e.Environment, "CODEX_CONFIG") != "" {
			t.Fatal("ambient credential/config retained")
		}
		if environmentValue(e.Environment, "HOME") != os.Getenv("HOME") || environmentValue(e.Environment, "PROFILE_TOOLING_TEST") != "preserved" {
			t.Fatal("tooling lost")
		}
		if !e.Executable {
			t.Fatal("supported environment unavailable")
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "profiles.json"))
	if string(before) != string(after) {
		t.Fatal("read mutated registry")
	}
	if err := h.RenameProfile(bindingFor(a), "Renamed"); err != nil {
		t.Fatal(err)
	}
	renamed, err := h.ResolveProfileClient(profileClientRequest{bindingFor(a)})
	if err != nil || renamed.Home != ae.Home || renamed.ProfileName != "Renamed" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	if err := h.ValidateProfileResult(bindingFor(a), bindingFor(b)); !errors.Is(err, ErrProfileIdentity) {
		t.Fatal(err)
	}
	wrong := bindingFor(a)
	wrong.WorkspaceID = b.ID
	if _, err := h.ResolveProfileClient(profileClientRequest{wrong}); !errors.Is(err, ErrProfileIdentity) {
		t.Fatal(err)
	}
}

func TestProfileContractExistingAndLegacy(t *testing.T) {
	h, _ := profileFixture(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte("synthetic"), 0600)
	p, err := h.CreateProfile(profileCreateRequest{Name: "Existing environment", Kind: profileExisting, ExistingHome: dir, ExistingSQLite: dir})
	if err != nil {
		t.Fatal(err)
	}
	req := profileClientRequest{bindingFor(p)}
	env, err := h.ResolveProfileClient(req)
	if err != nil || env.Home != dir || len(env.Arguments) != 0 {
		t.Fatalf("existing: %+v %v", env, err)
	}
	a := mustProfile(t, h, "A")
	records := []session{{Version: 2, ID: "old", CLI: "codex", CodexHome: dir}, {Version: 2, ID: "bound", CLI: "codex", Profile: &req.Binding}, {Version: 2, ID: "other", CLI: "codex", Profile: ptrProfileBinding(bindingFor(a))}}
	if err := withSessions(func([]session) ([]session, error) { return records, nil }); err != nil {
		t.Fatal(err)
	}
	scoped, err := h.ProfileSessions(req)
	if err != nil || len(scoped) != 1 || scoped[0].ID != "bound" {
		t.Fatalf("scope: %+v %v", scoped, err)
	}
	var roundTrip []session
	withSessions(func(all []session) ([]session, error) { roundTrip = all; return nil, nil })
	if !reflect.DeepEqual(records, roundTrip) {
		t.Fatal("legacy records rebound or rewritten")
	}
	if _, err := h.CreateProfile(profileCreateRequest{Name: "Alias", Kind: profileExisting, ExistingHome: dir, ExistingSQLite: dir}); !errors.Is(err, ErrProfileAlias) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "auth.json"))
	if string(data) != "synthetic" {
		t.Fatal("credential modified")
	}
}
func ptrProfileBinding(b profileBinding) *profileBinding { return &b }

func TestProfileContractMissingReplacedUnreadableAndAlias(t *testing.T) {
	for _, kind := range []string{"missing", "replacement", "unreadable", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			h, _ := profileFixture(t)
			p := mustProfile(t, h, "A")
			home := p.Clients[0].Home
			switch kind {
			case "missing":
				os.Rename(home, home+"-saved")
			case "replacement":
				os.Rename(home, home+"-saved")
				os.Mkdir(home, 0700)
			case "unreadable":
				os.Chmod(home, 0000)
				defer os.Chmod(home, 0700)
			case "symlink":
				os.Rename(home, home+"-saved")
				os.Symlink(home+"-saved", home)
			}
			_, err := h.ResolveProfileClient(profileClientRequest{bindingFor(p)})
			if !errors.Is(err, ErrProfileHome) && !errors.Is(err, ErrProfileIdentity) {
				t.Fatal(err)
			}
			if kind == "missing" {
				if _, err := os.Lstat(home); !os.IsNotExist(err) {
					t.Fatal("read recreated home")
				}
			}
		})
	}
	h, root := profileFixture(t)
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	data, _ := os.ReadFile(filepath.Join(root, "profiles.json"))
	var r profileRegistry
	json.Unmarshal(data, &r)
	r.Profiles[1].Clients[0].Home = a.Clients[0].Home
	encoded, _ := json.Marshal(r)
	os.WriteFile(filepath.Join(root, "profiles.json"), encoded, 0600)
	if _, err := h.ResolveProfileClient(profileClientRequest{bindingFor(b)}); !errors.Is(err, ErrProfileAlias) {
		t.Fatal(err)
	}
}

func TestProfileContractExecutionScopeAndStaleBinding(t *testing.T) {
	h, _ := profileFixture(t)
	p := mustProfile(t, h, "A")
	req := profileClientRequest{bindingFor(p)}
	for _, op := range []profileClientOperation{profileAccountMutation, profileLaunch, profileInspect} {
		called := false
		err := h.WithProfileClient(context.Background(), req, op, func(scope profileClientScope) error {
			called = true
			if scope.Environment.Binding != req.Binding {
				t.Fatal("scope changed")
			}
			return nil
		})
		if !called || err != nil {
			t.Fatalf("%s: callback=%v err=%v", op, called, err)
		}
	}
	project := t.TempDir()
	record, err := h.Create(sessionDraft{Client: "codex", Connection: "local", Directory: project, Profile: &req.Binding})
	if err != nil {
		t.Fatal(err)
	}
	if record.Profile == nil || *record.Profile != req.Binding {
		t.Fatal("binding not persisted")
	}
	broken := req
	broken.Binding.EnvironmentID = p.ID
	called := false
	if err := h.WithProfileClient(context.Background(), broken, profileLaunch, func(profileClientScope) error { called = true; return nil }); !errors.Is(err, ErrProfileIdentity) || called {
		t.Fatal("stale scope admitted", err)
	}
	os.Rename(p.Clients[0].Home, p.Clients[0].Home+"-saved")
	if _, err := h.Create(sessionDraft{Client: "codex", Connection: "local", Directory: project, Profile: &req.Binding}); !errors.Is(err, ErrProfileHome) {
		t.Fatal(err)
	}
	if err := checkSessionBase(record, newConversation, "", false); !errors.Is(err, ErrProfileHome) {
		t.Fatal(err)
	}
}

func TestProfileContractLockScopeAndCancellation(t *testing.T) {
	h, _ := profileFixture(t)
	a, b := bindingFor(mustProfile(t, h, "A")), bindingFor(mustProfile(t, h, "B"))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- withProfileClientLock(context.Background(), a, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := withProfileClientLock(ctx, a, func() error { t.Error("same profile overlapped"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := withProfileClientLock(ctx, b, func() error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancelled context admitted")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := withProfileClientLock(ctx2, b, func() error { return nil }); err != nil {
		t.Fatal("A blocked B", err)
	}
}
