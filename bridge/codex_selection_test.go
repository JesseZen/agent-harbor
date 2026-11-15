package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexSelectionUsesOnlyTheSessionsOwnProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const otherID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const thread = "01912345-6789-7abc-8def-0123456789ab"
	s := session{ID: id, CLI: "codex", Mode: "magpie", CodexSessionID: thread, Conversation: &conversationEvidence{Reference: herdrIdentity{Agent: "codex", Kind: "id", Value: thread}}}
	if _, err := readCodexSelection(context.Background(), s); !errors.Is(err, errCodexProfileMissing) {
		t.Fatalf("missing profile: %v", err)
	}
	env := os.Environ()
	profile, err := magpieCodexProfile(id, "group/first", env)
	if err != nil {
		t.Fatal(err)
	}
	other, err := magpieCodexProfile(otherID, "group/other", env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "codex", profile+".config.toml")
	otherPath := filepath.Join(home, "codex", other+".config.toml")
	before, _ := os.ReadFile(otherPath)
	if err := os.WriteFile(path, []byte("# Agent Harbor session "+id+"\nmodel = \"relay/selected\"\nmodel_reasoning_effort = \"high\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	selection, err := readCodexSelection(context.Background(), s)
	if err != nil || selection.ThreadID != thread || selection.Model != "relay/selected" || selection.Effort != "high" || selection.ProfileHash == "" {
		t.Fatalf("wrong selection: %+v, %v", selection, err)
	}
	after, _ := os.ReadFile(otherPath)
	if string(before) != string(after) {
		t.Fatal("another session's profile changed")
	}
	if err := os.WriteFile(path, []byte("model = \"unowned\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCodexSelection(context.Background(), s); !errors.Is(err, errCodexSelectionConflict) {
		t.Fatalf("unowned profile accepted: %v", err)
	}
}

func TestCodexSelectionMustStillMatchAtRecovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			fmt.Fprint(w, `{"name":"magpie"}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"group/saved"},{"id":"relay/new"}]}`)
		}
	}))
	defer server.Close()
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const thread = "01912345-6789-7abc-8def-0123456789ab"
	s := session{ID: id, CLI: "codex", Mode: "magpie", MagpieURL: server.URL, MagpieModel: "group/saved", CodexSessionID: thread, Conversation: &conversationEvidence{Reference: herdrIdentity{Agent: "codex", Kind: "id", Value: thread}}}
	ctx := context.Background()
	fallback := codexSelection{ThreadID: thread, Model: "group/saved", Fallback: true}
	if err := verifyCodexSelection(ctx, s, &fallback); err != nil {
		t.Fatal(err)
	}
	profile, err := magpieCodexProfile(id, "group/saved", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCodexSelection(ctx, s, &fallback); err == nil {
		t.Fatal("an outdated missing-profile review was accepted")
	}
	selection, err := readCodexSelection(ctx, s)
	if err != nil || verifyCodexSelection(ctx, s, &selection) != nil {
		t.Fatalf("valid profile rejected: %+v %v", selection, err)
	}
	path := filepath.Join(home, "codex", profile+".config.toml")
	if err := os.WriteFile(path, []byte("# Agent Harbor session "+id+"\nmodel = \"relay/new\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCodexSelection(ctx, s, &selection); err == nil {
		t.Fatal("a changed profile was accepted without a new review")
	}
}

func TestMagpieRecoveryReviewUsesProfileOrExplicitOriginalChoice(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const thread = "01912345-6789-7abc-8def-0123456789ab"
	row := sessionView{session: session{ID: id, Name: "Fixture", CLI: "codex", Mode: "magpie", Started: true,
		MagpieModel: "group/original", CodexSessionID: thread, Conversation: &conversationEvidence{Reference: herdrIdentity{Agent: "codex", Kind: "id", Value: thread}}},
		Runtime: runtimeObservation{Presence: "historical_exit"}}
	t.Run("profile", func(t *testing.T) {
		service := &fakeApplicationService{Sessions: []sessionView{row}, Selection: &codexSelection{ThreadID: thread, Model: "relay/new", Effort: "high", ProfileHash: "hash"}}
		m := newHarborModel(service)
		m.sessions, m.width, m.height = []sessionView{row}, 100, 40
		m = appKey(m, "enter").(harborModel)
		if !strings.Contains(m.confirmView(), "relay/new") || !strings.Contains(m.confirmView(), "high") {
			t.Fatal("review did not show the session's selected default")
		}
		m = appKey(m, "a").(harborModel)
		_ = appKey(m, "enter")
		if service.LastApproval == nil || service.LastApproval.MagpieSelection == nil || *service.LastApproval.MagpieSelection != *service.Selection {
			t.Fatal("reviewed profile selection was not carried to recovery")
		}
	})
	t.Run("missing profile", func(t *testing.T) {
		service := &fakeApplicationService{Sessions: []sessionView{row}, SelectionErr: errCodexProfileMissing}
		m := newHarborModel(service)
		m.sessions, m.width, m.height = []sessionView{row}, 100, 40
		m = appKey(m, "enter").(harborModel)
		m = appKey(m, "a").(harborModel)
		m = appKey(m, "enter").(harborModel)
		if service.LastApproval != nil {
			t.Fatal("missing profile was silently accepted")
		}
		m = appKey(m, "b").(harborModel)
		_ = appKey(m, "enter")
		if service.LastApproval == nil || service.LastApproval.MagpieSelection == nil || !service.LastApproval.MagpieSelection.Fallback || service.LastApproval.MagpieSelection.Model != "group/original" {
			t.Fatal("explicit original-model choice was not preserved")
		}
	})
	t.Run("conflicting profile", func(t *testing.T) {
		service := &fakeApplicationService{Sessions: []sessionView{row}, SelectionErr: errCodexSelectionConflict}
		m := newHarborModel(service)
		m.sessions, m.width, m.height = []sessionView{row}, 100, 40
		m = appKey(m, "enter").(harborModel)
		m = appKey(m, "a").(harborModel)
		m = appKey(m, "b").(harborModel)
		_ = appKey(m, "enter")
		if service.LastApproval != nil {
			t.Fatal("conflicting profile was accepted")
		}
	})
}
