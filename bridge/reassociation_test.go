package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyReassociationExplicitReview(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.Started = true
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.ConversationIDSource = "manual"
	s.RunToken = "old-owner"
	data, _ := json.Marshal([]session{s})
	os.WriteFile(filepath.Join(b.root, "bridge/sessions.json"), data, 0600)
	ref := `{"source":"herdr:codex","agent":"codex","kind":"id","value":"11111111-1111-4111-8111-111111111111"}`
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "reassociate", s.ID, ref, "Reviewed original native conversation and connection")
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader("yes\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	got := b.sessions()[0]
	if got.Conversation == nil || got.Conversation.Provenance != "reviewed-compatibility" || got.Conversation.Status != "reviewed" {
		t.Fatalf("missing honest review: %+v", got.Conversation)
	}
	if got.CodexSessionID != s.CodexSessionID || got.ConversationIDSource != s.ConversationIDSource || got.RunToken != "" {
		t.Fatal("legacy evidence lost or old owner retained")
	}
	out, err := b.command("show", s.ID)
	if err != nil || !strings.Contains(out, "reviewed-compatibility") {
		t.Fatal(out, err)
	}
}

func TestReassociationServiceRejectsStaleConflictAndRecordsRejection(t *testing.T) {
	b := newTestBridge(t)
	for _, e := range b.env {
		if k, v, ok := strings.Cut(e, "="); ok {
			t.Setenv(k, v)
		}
	}
	h := harborService{}
	s, err := h.Create(sessionDraft{Directory: b.project, Connection: "local"})
	if err != nil {
		t.Fatal(err)
	}
	ref := herdrIdentity{"herdr:codex", "codex", "id", "11111111-1111-4111-8111-111111111111"}
	if err := h.Rename(s.ID, "changed"); err != nil {
		t.Fatal(err)
	}
	if err := h.Reassociate(sessionApproval{Session: s, Confirmed: true}, ref, "review"); err == nil {
		t.Fatal("stale approval accepted")
	}
	v, _ := h.Get(s.ID)
	if err := h.Reassociate(sessionApproval{Session: v.session}, ref, "rejected unknown identity"); err != nil {
		t.Fatal(err)
	}
	v, _ = h.Get(s.ID)
	if v.Conversation != nil || v.Recovery != "needs_review" || len(v.Reassociations) != 1 || v.Reassociations[0].Accepted {
		t.Fatal("rejection lost")
	}
	if !strings.Contains(v.Reason, "reassociate") {
		t.Fatalf("missing action: %s", v.Reason)
	}
	if err := h.Reassociate(sessionApproval{Session: v.session, Confirmed: true}, ref, "reviewed original"); err != nil {
		t.Fatal(err)
	}
	v, _ = h.Get(s.ID)
	different := ref
	different.Value = "22222222-2222-4222-8222-222222222222"
	if err := h.Reassociate(sessionApproval{Session: v.session, Confirmed: true}, different, "switch"); err == nil {
		t.Fatal("conflict accepted")
	}
	after, _ := h.Get(s.ID)
	if !sameSession(after.session, v.session) {
		t.Fatal("conflict changed evidence")
	}
}

// A legacy ID is conversation intent even when older records omitted Started.
func TestLegacyConversationCannotStartFresh(t *testing.T) {
	b := newTestBridge(t)
	for _, e := range b.env {
		if k, v, ok := strings.Cut(e, "="); ok {
			t.Setenv(k, v)
		}
	}
	h := harborService{}
	s, err := h.Create(sessionDraft{Directory: b.project, Connection: "local"})
	if err != nil {
		t.Fatal(err)
	}
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	if err := withSessions(func(all []session) ([]session, error) { all[0] = s; return all, nil }); err != nil {
		t.Fatal(err)
	}
	err = h.Start(sessionApproval{Session: s, Confirmed: true})
	if err == nil || !strings.Contains(err.Error(), "reassociate") {
		t.Fatalf("legacy intent not gated: %v", err)
	}
}
