package main

import (
	"strings"
	"testing"
)

func TestRecoverMissingReferenceStopsBeforeReview(t *testing.T) {
	row := sessionView{session: session{ID: "missing", Name: "Finished", CLI: "codex", Mode: "local", Started: true, RunToken: "run"}, Runtime: runtimeObservation{Presence: "historical_exit"}}
	f := &fakeApplicationService{Sessions: []sessionView{row}}
	m := newHarborModel(f)
	m.sessions = []sessionView{row}
	m = appKey(m, "enter").(harborModel)
	if m.screen != "sessions" || !strings.Contains(m.notice, "Conversation identity was not captured") {
		t.Fatalf("missing identity entered recovery review: screen=%s notice=%s", m.screen, m.notice)
	}
	if strings.Contains(strings.Join(f.Calls, "|"), "open ") {
		t.Fatal("missing identity launched")
	}
	if got := deriveSession(row.session).Reason; strings.Contains(got, "Legacy") {
		t.Fatal("new record mislabeled legacy", got)
	}
}

func TestRecoverNormalIdentityNoticeNotRepeatedInList(t *testing.T) {
	m := searchFixture()
	m.width, m.height = 160, 45
	for i := range m.sessions {
		m.sessions[i].Reason = ""
		m.sessions[i].Recovery = "ready"
		m.sessions[i].IdentityWarning = "Official pane identity observed; recovery still requires connection, login and writer review."
	}
	if strings.Contains(m.sessionsLayout().view(), "Official pane identity") {
		t.Fatal("normal identity reminder rendered as a warning in sessions list")
	}
}
