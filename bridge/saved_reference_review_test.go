package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type savedReviewCall struct {
	approval sessionApproval
	ref      herdrIdentity
	reason   string
}

type savedReviewService struct {
	*fakeApplicationService
	reviews []savedReviewCall
}

func (f *savedReviewService) Reassociate(a sessionApproval, ref herdrIdentity, reason string) error {
	f.reviews = append(f.reviews, savedReviewCall{a, ref, reason})
	return f.Err
}

func savedReviewFixture() sessionView {
	ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "11111111-1111-4111-8111-111111111111"}
	return sessionView{
		session: session{ID: "fixture", Version: 3, Name: "API", CLI: "codex", Mode: "magpie", Started: true,
			WorkingDirectory: "/fixture/project", CodexSessionID: ref.Value, InstanceID: "managed",
			MagpieURL: "http://127.0.0.1:3435", MagpieModel: "relay-a/test-model",
			Conversation: &conversationEvidence{Reference: ref, Provenance: "herdr-pane-observation", Status: "missing"}},
		Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown", Instance: "workspace"},
	}
}

func TestSavedReferenceReviewRequiresExplicitChoice(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		row := savedReviewFixture()
		f := &savedReviewService{fakeApplicationService: &fakeApplicationService{Sessions: []sessionView{row}}}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = model.Update(m.Init()())
		model = appKey(model, "v")
		if got := model.(harborModel); got.screen != "confirm" || got.action != "review-reference" {
			t.Fatalf("%v did not open reference review: %s", size, model.View())
		}
		visible := ""
		for i := 0; i < 80; i++ {
			visible += model.View()
			model = appKey(model, "down")
		}
		for _, want := range []string{"herdr:codex", "codex", "11111111-1111-4111-8111-111111111111", "relay-a/test-model", "missing"} {
			if !strings.Contains(visible, want) {
				t.Fatalf("%v hid %q from the review", size, want)
			}
		}
		model = appKey(model, "enter")
		if len(f.reviews) != 0 {
			t.Fatal("saved identity without explicit acceptance")
		}
		model = appKey(model, "esc")
		if len(f.reviews) != 0 {
			t.Fatal("saved identity after cancellation")
		}
		model = appKey(model, "v")
		model = appKey(model, "a")
		model = appKey(model, "enter")
		if len(f.reviews) != 1 || !f.reviews[0].approval.Confirmed || f.reviews[0].approval.Observation == nil || f.reviews[0].ref != row.Conversation.Reference || strings.TrimSpace(f.reviews[0].reason) == "" {
			t.Fatalf("incorrect reviewed identity: %+v", f.reviews)
		}
		if strings.Contains(strings.Join(f.Calls, "|"), "open ") {
			t.Fatal("reference review launched a conversation")
		}
	}
}

func TestSavedReferenceReviewDoesNotGuessConflictingOrLiveIdentity(t *testing.T) {
	tests := map[string]func(*sessionView){
		"missing reference": func(v *sessionView) { v.Conversation = nil },
		"conflict":          func(v *sessionView) { v.Conversation.Status = "conflict" },
		"invalid report":    func(v *sessionView) { v.Conversation.Status = "invalid_report" },
		"different report": func(v *sessionView) {
			other := v.Conversation.Reference
			other.Value = "another"
			v.Conversation.ObservedReference = &other
		},
		"ambiguous ID": func(v *sessionView) { v.CandidateIDs = []string{"another"} },
		"live target":  func(v *sessionView) { v.Runtime.Presence = "observed"; v.Running = true },
		"external":     func(v *sessionView) { v.External = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			row := savedReviewFixture()
			mutate(&row)
			f := &savedReviewService{fakeApplicationService: &fakeApplicationService{Sessions: []sessionView{row}}}
			m := newHarborModel(f)
			var model tea.Model = m
			model, _ = model.Update(m.Init()())
			model = appKey(model, "v")
			if model.(harborModel).screen == "confirm" || len(f.reviews) != 0 {
				t.Fatal("ambiguous or live identity was offered as a safe saved-reference review")
			}
		})
	}
}

func TestReassociateRejectsChangedRuntimeObservation(t *testing.T) {
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
	ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "11111111-1111-4111-8111-111111111111"}
	now := time.Now().UTC()
	s.Version, s.Started, s.CodexSessionID, s.LastExitAt = 3, true, ref.Value, &now
	s.Conversation = &conversationEvidence{Reference: ref, Provenance: "herdr-pane-observation", Status: "missing"}
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	v, err := h.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed := v.Runtime
	changed.Presence = "observed"
	if err := h.Reassociate(sessionApproval{Session: v.session, Observation: &changed, Confirmed: true}, ref, "reviewed saved reference"); err == nil {
		t.Fatal("accepted review against a changed runtime")
	}
	after, err := h.Get(s.ID)
	if err != nil || !sameSession(after.session, v.session) {
		t.Fatal("rejected review changed the record", err)
	}
	if err := h.Reassociate(sessionApproval{Session: v.session, Observation: &v.Runtime, Confirmed: true}, ref, "reviewed saved reference"); err != nil {
		t.Fatal(err)
	}
}

func TestReassociateRejectsLaunchInProgress(t *testing.T) {
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
	ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "11111111-1111-4111-8111-111111111111"}
	now := time.Now().UTC()
	s.Version, s.Started, s.CodexSessionID, s.LastStartedAt = 3, true, ref.Value, &now
	s.Conversation = &conversationEvidence{Reference: ref, Provenance: "herdr-pane-observation", Status: "missing"}
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	lock, err := lockSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := h.Reassociate(sessionApproval{Session: s, Confirmed: true}, ref, "reviewed saved reference"); err == nil {
		t.Fatal("accepted review while launch held the session lock")
	}
	after, err := getSession(s.ID)
	if err != nil || !sameSession(after, s) {
		t.Fatal("rejected review changed the record", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Reassociate(sessionApproval{Session: s, Confirmed: true}, ref, "reviewed saved reference"); err != nil {
		t.Fatal(err)
	}
}
