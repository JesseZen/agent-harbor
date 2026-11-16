package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProfileDefaultCreationAndSelectionDoesNotMoveSessions(t *testing.T) {
	h, _ := profileFixture(t)
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "missing-client"))
	a, err := h.EnsureProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != profileIsolated || a.Name != "Default" {
		t.Fatal(a)
	}
	b := mustProfile(t, h, "B")
	ab, _ := bindingOf(a, "codex")
	bb, _ := bindingOf(b, "codex")
	record, err := h.Create(sessionDraft{Profile: &ab, Client: "codex", Connection: "local", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SelectProfile(bb); err != nil {
		t.Fatal(err)
	}
	selected, err := h.EnsureProfiles()
	if err != nil || selected.ID != b.ID {
		t.Fatal(selected, err)
	}
	saved, err := getSession(record.ID)
	if err != nil || saved.Profile == nil || *saved.Profile != ab {
		t.Fatal("navigation moved session", saved, err)
	}
	scope, err := h.ProfileSessions(profileClientRequest{Binding: bb})
	if err != nil || len(scope) != 0 {
		t.Fatal(scope, err)
	}
}

func TestProfileUpgradeRequiresOriginalEnvironmentReview(t *testing.T) {
	h, _ := profileFixture(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("CODEX_SQLITE_HOME", home)
	legacy := session{Version: 2, ID: "0123456789abcdef0123456789abcdef", CLI: "codex", CodexHome: home, Mode: "local", WorkingDirectory: t.TempDir()}
	withSessions(func([]session) ([]session, error) { return []session{legacy}, nil })
	existing, err := h.EnsureProfiles()
	if err != nil || existing.Kind != profileExisting {
		t.Fatal(existing, err)
	}
	isolated := mustProfile(t, h, "Fresh")
	fresh, _ := bindingOf(isolated, "codex")
	h.SelectProfile(fresh)
	if _, err := sessionClientEnvironment(legacy); err == nil {
		t.Fatal("unbound record inherited visible Profile")
	}
	b, _ := bindingOf(existing, "codex")
	if err := h.ReviewSessionProfile(sessionApproval{Session: legacy}, b); err == nil {
		t.Fatal("unconfirmed binding accepted")
	}
	if err := h.ReviewSessionProfile(sessionApproval{Session: legacy, Confirmed: true}, fresh); !errors.Is(err, ErrProfileIdentity) {
		t.Fatal(err)
	}
	if err := h.ReviewSessionProfile(sessionApproval{Session: legacy, Confirmed: true}, b); err != nil {
		t.Fatal(err)
	}
	saved, _ := getSession(legacy.ID)
	e, err := sessionClientEnvironment(saved)
	if err != nil || e.Home != home || saved.Mode != legacy.Mode || saved.CodexSessionID != legacy.CodexSessionID {
		t.Fatal(saved, e, err)
	}
	if err := h.ReviewSessionProfile(sessionApproval{Session: saved, Confirmed: true}, fresh); err == nil {
		t.Fatal("moved an existing conversation")
	}
}

func TestProfileAccountMutationScopeChangesOnlyA(t *testing.T) {
	h, _ := profileFixture(t)
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	ab, bb := bindingFor(a), bindingFor(b)
	for _, binding := range []profileBinding{ab, bb} {
		if err := h.WithProfileClient(context.Background(), profileClientRequest{binding}, profileAccountMutation, func(scope profileClientScope) error {
			return os.WriteFile(filepath.Join(scope.Environment.Home, "synthetic-account"), []byte("before"), 0600)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.WithProfileClient(context.Background(), profileClientRequest{ab}, profileAccountMutation, func(scope profileClientScope) error {
		return os.WriteFile(filepath.Join(scope.Environment.Home, "synthetic-account"), []byte("after"), 0600)
	}); err != nil {
		t.Fatal(err)
	}
	ae, _ := h.ResolveProfileClient(profileClientRequest{ab})
	be, _ := h.ResolveProfileClient(profileClientRequest{bb})
	ad, _ := os.ReadFile(filepath.Join(ae.Home, "synthetic-account"))
	bd, _ := os.ReadFile(filepath.Join(be.Home, "synthetic-account"))
	if string(ad) != "after" || string(bd) != "before" {
		t.Fatal("mutation crossed Profiles")
	}
	// Unsupported credential backend cannot be used by account/login tooling.
	os.WriteFile(filepath.Join(ae.Home, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\"\n"), 0600)
	called := false
	if err := h.WithProfileClient(context.Background(), profileClientRequest{ab}, profileAccountMutation, func(profileClientScope) error { called = true; return nil }); err == nil || called {
		t.Fatal("unsafe backend admitted")
	}
}

func TestProfileUnboundSessionCannotLaunchEvenBeforeRegistryInitialization(t *testing.T) {
	h, _ := profileFixture(t)
	legacy := session{ID: "0123456789abcdef0123456789abcdef", CLI: "codex", Mode: "local", WorkingDirectory: t.TempDir()}
	if err := launchReviewed(legacy, newConversation, ""); err == nil {
		t.Fatal("unbound legacy session launched")
	}
	if err := h.Open(sessionApproval{Session: legacy, Confirmed: true}); err == nil {
		t.Fatal("unbound legacy pane opened")
	}
}

func TestProfileEnvironmentUsesCapturedDefaultAndIgnoresOtherMissingHome(t *testing.T) {
	h, _ := profileFixture(t)
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	ab, bb := bindingFor(a), bindingFor(b)
	if err := withMagpieRegistry(func(r *magpieRegistry) error { r.Default = "workspace-shared"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := withProfiles(false, func(r *profileRegistry) (bool, error) {
		for i := range r.Profiles {
			if r.Profiles[i].ID == b.ID {
				r.Profiles[i].DefaultInstanceID = "b-only"
			}
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.SelectProfile(bb); err != nil {
		t.Fatal(err)
	}
	// A is captured by another popup; B's subsequent choice must not leak into A.
	e, err := h.ProfileEnvironment(ab)
	if err != nil || e.DefaultInstance != "workspace-shared" || e.Profile == nil || *e.Profile != ab {
		t.Fatalf("captured environment: %+v %v", e, err)
	}
	if err := os.Rename(b.Clients[0].Home, b.Clients[0].Home+"-moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ProfileEnvironment(ab); err != nil {
		t.Fatal("unrelated missing selected Profile blocked A", err)
	}
}
