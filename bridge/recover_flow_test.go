package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoverSingleDecisionAndRetryFocus(t *testing.T) {
	row := savedReviewFixture()
	row.CodexConnection = "caller"
	f := &fakeApplicationService{Sessions: []sessionView{row}}
	m := newHarborModel(f)
	m.sessions = []sessionView{row}
	m = appKey(m, "enter").(harborModel)
	if m.screen != "confirm" || m.uiFocus != "review:enter" {
		t.Fatalf("recovery did not focus Resume: %s %s", m.screen, m.uiFocus)
	}
	for _, a := range m.reviewActions() {
		if a.ID == "review:a" {
			t.Fatal("recovery still needs a separate acceptance action")
		}
	}
	m.reviewOpenedAt = time.Time{}
	f.Err = errors.New("synthetic recover failure")
	m = appKey(m, "enter").(harborModel)
	if f.LastApproval == nil || !f.LastApproval.QueryUncertaintyAccepted || !f.LastApproval.SavedReferenceAccepted {
		t.Fatal("Resume did not submit the displayed conversation as one decision")
	}
	if m.screen != "confirm" || m.uiFocus != "review:enter" || !strings.Contains(m.notice, "synthetic recover failure") {
		t.Fatalf("retry focus jumped away: %s %s %s", m.screen, m.uiFocus, m.notice)
	}
	m = appKey(m, "i").(harborModel)
	m = appKey(m, "esc").(harborModel)
	if m.uiFocus == "review:esc" {
		t.Fatal("returning from details focused Cancel")
	}
}

func TestRecoverArrowsScrollWithoutSelectingFooterActions(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen, m.action, m.uiFocus = "confirm", "query-resume", "review:enter"
	m.reviewed = savedReviewFixture().session
	m = appKey(m, "down").(harborModel)
	if m.screen != "confirm" || m.uiFocus != "review:enter" {
		t.Fatalf("Down changed the direct-action context: %s %s", m.screen, m.uiFocus)
	}
}

func TestRecoverSavedReferenceApprovalSurvivesFailedLaunch(t *testing.T) {
	b := newTestBridge(t)
	for _, pair := range b.env {
		if k, v, ok := strings.Cut(pair, "="); ok {
			t.Setenv(k, v)
		}
	}
	h := harborService{}
	s, err := h.Create(sessionDraft{Directory: b.project, Connection: "local"})
	if err != nil {
		t.Fatal(err)
	}
	socket := queryTestSocket(t, b)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	herdr := filepath.Join(b.root, "bin", "query-herdr")
	os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\necho incompatible-query\n"), 0700)
	t.Setenv("AGENT_HARBOR_HERDR_BIN", herdr)
	now := time.Now().UTC()
	s.Version, s.Started, s.RunToken, s.LastStartedAt, s.LastExitAt = 3, true, "original-run", &now, &now
	s.HerdrSocket, s.HerdrSocketIdentity = socket, socketIdentity(socket)
	s.Conversation = savedReviewFixture().Conversation
	s.Conversation.Status = "observation_unavailable"
	s.Conversation.ObservedAt, s.Conversation.Pane, s.Conversation.SocketIdentity, s.Conversation.RunToken = now, "old-pane", "old-socket", "old-run"
	s.CodexSessionID = s.Conversation.Reference.Value
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	v, err := h.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := sessionApproval{Session: v.session, Observation: &v.Runtime, Confirmed: true, DevelopmentQuery: true, QueryUncertaintyAccepted: true}
	if err := h.Open(a); err == nil || !strings.Contains(err.Error(), "observation needs review") {
		t.Fatal("missing explicit reference approval accepted", err)
	}
	unchanged, _ := getSession(s.ID)
	if !sameSession(unchanged, s) {
		t.Fatal("unapproved reference changed")
	}
	a.SavedReferenceAccepted = true
	err = h.Open(a)
	var retry *recoveryOpenError
	if !errors.As(err, &retry) || !strings.Contains(err.Error(), "incompatible Herdr query") {
		t.Fatal("saved reference did not reach query with a retryable approval", err)
	}
	updated, _ := getSession(s.ID)
	if len(updated.Reassociations) != 1 || !sameSession(updated, retry.approval.Session) || queryReferenceCheck(updated) != nil {
		t.Fatal("reviewed approval lost", updated.Recovery)
	}
	if updated.Conversation.Reference != s.Conversation.Reference || updated.Mode != s.Mode || !sameProfileBinding(updated.Profile, s.Profile) {
		t.Fatal("review changed conversation or connection")
	}
	if err := h.Open(retry.approval); err == nil || !strings.Contains(err.Error(), "incompatible Herdr query") {
		t.Fatal("retry hit stale approval instead of the original failure", err)
	}
	again, _ := getSession(s.ID)
	if len(again.Reassociations) != 1 {
		t.Fatal("retry asked for another review")
	}
	// A failed launch must not approve a later record edit implicitly.
	changed := again
	changed.Name = "Edited after confirmation"
	if err := withSessions(func([]session) ([]session, error) { return []session{changed}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := h.Open(retry.approval); err == nil || !strings.Contains(err.Error(), "session changed") {
		t.Fatal("retry approved a changed record", err)
	}
	// A real conflict must still stop without writing an acceptance.
	conflict := s
	copyEvidence := *s.Conversation
	copyEvidence.Status = "conflict"
	conflict.Conversation = &copyEvidence
	if err := withSessions(func([]session) ([]session, error) { return []session{conflict}, nil }); err != nil {
		t.Fatal(err)
	}
	v, _ = h.Get(s.ID)
	a.Session, a.Observation = v.session, &v.Runtime
	if err := h.Open(a); err == nil {
		t.Fatal("conflicting reference accepted")
	}
	got, _ := getSession(s.ID)
	oldJSON, _ := json.Marshal(conflict)
	newJSON, _ := json.Marshal(got)
	if string(oldJSON) != string(newJSON) {
		t.Fatal("conflict record changed")
	}
}
