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

func recoveryConflictFixture(s session) session {
	s.Version, s.Started, s.Recovery = 3, true, "needs_review"
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.ReportedCodexID = "22222222-2222-4222-8222-222222222222"
	s.Conversation = &conversationEvidence{
		Reference:         herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: s.CodexSessionID},
		ObservedReference: &herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: s.ReportedCodexID},
		Status:            "conflict",
	}
	s.Candidates = []candidateEvidence{{ID: s.ReportedCodexID, LastSeen: time.Now().UTC()}}
	return s
}

func TestEnterRecoveryConflictKeyboardAndRetry(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, key := range []string{"up", "down"} {
		t.Run(key, func(t *testing.T) {
			row := savedReviewFixture()
			row.session = recoveryConflictFixture(row.session)
			row.Name = "Unverified session title"
			row.CodexConnection = "caller"
			f := &fakeApplicationService{Sessions: []sessionView{row}}
			m := newHarborModel(f)
			m.width, m.height, m.sessions = 44, 12, f.Sessions
			m = appKey(m, "enter").(harborModel)
			view := m.View()
			for _, text := range []string{"Originally saved", "Last reported", "↑↓/jk select"} {
				if !strings.Contains(view, text) {
					t.Fatalf("missing %q at narrow width: %s", text, view)
				}
			}
			if strings.Contains(view, row.CodexSessionID) || strings.Contains(view, row.ReportedCodexID) || strings.Contains(view, row.Name) || strings.Contains(view, "\x1b[") || m.recoveryCursor != -1 {
				t.Fatal("unexpected initial recovery choice or view", view)
			}
			m = appKey(m, "enter").(harborModel)
			m = appKey(m, key).(harborModel)
			m = appKey(m, "esc").(harborModel)
			if f.LastApproval != nil || m.screen != "sessions" || m.searchSelectedID() != row.ID {
				t.Fatal("cancel saved a choice or lost the original selection")
			}
			m = appKey(m, "enter").(harborModel)
			m = appKey(m, key).(harborModel)
			f.Err = errors.New("fixture pane startup failed")
			m = appKey(m, "enter").(harborModel)
			want := row.CodexSessionID
			if key == "down" {
				want = row.ReportedCodexID
			}
			if f.LastApproval == nil || f.LastApproval.Recovery.Target.Value != want || !m.recoveryRetry || !strings.Contains(m.View(), "Retry recovery") {
				t.Fatal("retry lost explicit choice", m.View(), f.LastApproval)
			}
			m = appKey(m, "up").(harborModel)
			m = appKey(m, "down").(harborModel)
			_ = appKey(m, "enter")
			if f.LastApproval.Recovery.Target.Value != want {
				t.Fatal("retry changed the recovery target")
			}
		})
	}
}

func TestEnterRecoveryCandidateClickSelectsThenConfirms(t *testing.T) {
	row := savedReviewFixture()
	row.session = recoveryConflictFixture(row.session)
	f := &fakeApplicationService{Sessions: []sessionView{row}}
	m := newHarborModel(f)
	m.sessions = f.Sessions
	m = appKey(m, "enter").(harborModel)
	m = reviewClick(t, m, "recovery-candidate:1")
	if m.recoveryCursor != 1 || f.LastApproval != nil {
		t.Fatal("click did not select the reported conversation")
	}
	_ = reviewClick(t, m, "recovery-candidate:1")
	if f.LastApproval == nil || f.LastApproval.Recovery.Target.Value != row.ReportedCodexID {
		t.Fatal("clicking the current row did not confirm the reported conversation")
	}
}

func TestEnterRecoverySavesConflictChoiceAndRejectsStaleApproval(t *testing.T) {
	b := newTestBridge(t)
	for _, pair := range b.env {
		if key, value, ok := strings.Cut(pair, "="); ok {
			t.Setenv(key, value)
		}
	}
	h := harborService{}
	base, err := h.Create(sessionDraft{Directory: b.project, Connection: "local"})
	if err != nil {
		t.Fatal(err)
	}
	socket := queryTestSocket(t, b)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	herdr := filepath.Join(b.root, "bin", "failed-query")
	if err := os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\nprintf bad-response\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_HERDR_BIN", herdr)
	base.HerdrSocket, base.HerdrSocketIdentity = socket, socketIdentity(socket)
	now := time.Now().UTC()
	base.RunToken, base.LastStartedAt, base.LastExitAt = "original-run", &now, &now
	for _, index := range []int{0, 1} {
		s := recoveryConflictFixture(base)
		if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
			t.Fatal(err)
		}
		v, err := h.Get(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := recoveryCandidates(s)
		if err != nil || len(candidates) != 2 {
			t.Fatal(candidates, err)
		}
		target := candidates[index].Reference
		fingerprint, err := recoveryConnectionFingerprint(v.session)
		if err != nil {
			t.Fatal(err)
		}
		a := sessionApproval{Session: v.session, Observation: &v.Runtime, Confirmed: true, DevelopmentQuery: true, QueryUncertaintyAccepted: true, Recovery: &recoveryApproval{Target: target, Fingerprint: fingerprint}}
		projectConfig := filepath.Join(s.WorkingDirectory, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(projectConfig), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(projectConfig, []byte(`openai_base_url = "http://127.0.0.1:51000/v1"`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := h.Open(a); !errors.Is(err, errRecoveryStale) {
			t.Fatal("connection change accepted", err)
		}
		unchanged, _ := getSession(s.ID)
		if !sameSession(unchanged, v.session) {
			t.Fatal("stale approval wrote a choice", unchanged)
		}
		if err := os.Remove(projectConfig); err != nil {
			t.Fatal(err)
		}
		var retry *recoveryOpenError
		if err := h.Open(a); !errors.As(err, &retry) || !strings.Contains(err.Error(), "incompatible Herdr query") {
			t.Fatal("failed query lost its recovery approval", err)
		}
		saved, _ := getSession(s.ID)
		if !sameSession(saved, retry.approval.Session) || saved.CodexSessionID != target.Value || !sameProfileBinding(saved.Profile, s.Profile) || saved.Mode != s.Mode || saved.RunToken != s.RunToken {
			t.Fatal("confirmed choice, Profile, connection or old-run evidence changed", saved)
		}
		var original session
		if len(saved.Reassociations) != 1 || json.Unmarshal(saved.Reassociations[0].Original, &original) != nil || !sameSession(original, s) {
			t.Fatal("original conflict evidence was lost", saved.Reassociations)
		}
		if err := h.Open(retry.approval); !errors.As(err, &retry) || retry.approval.Recovery.Target != target {
			t.Fatal("retry did not retain the selected reference", err)
		}
	}
}

func TestEnterRecoveryRejectsOccupiedTargetRegardlessOfEvidenceSource(t *testing.T) {
	s := recoveryConflictFixture(session{CLI: "codex"})
	ref := *s.Conversation.ObservedReference
	selected := recoveryTarget(s, ref)
	ref.Source = "another source"
	if err := recoveryOccupied(selected, []sessionView{{Runtime: runtimeObservation{Presence: "observed", Reference: &ref}}}); !errors.Is(err, errConversationObserved) {
		t.Fatal("occupied target was accepted", err)
	}
	s.Conversation.ObservedReference, s.ReportedCodexID = nil, ""
	if candidates, err := recoveryCandidates(s); err == nil || len(candidates) != 0 {
		t.Fatal("incomplete report became a recovery option", candidates, err)
	}
}
