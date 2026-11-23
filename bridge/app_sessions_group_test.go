package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestStatusGrouperPartitionsAndHidesEmptyGroups(t *testing.T) {
	views := []sessionView{
		{session: session{ID: "stop-1", Name: "Stopped 1"}, Runtime: runtimeObservation{Presence: "historical_exit"}, Status: "Stopped"},
		{session: session{ID: "ext-1", Name: "agy"}, External: true, Running: true, Runtime: runtimeObservation{Presence: "observed", Activity: "idle"}, Status: "Idle"},
		{session: session{ID: "act-1", Name: "Active 1"}, Running: true, Runtime: runtimeObservation{Presence: "observed", Activity: "working"}, Status: "Working"},
		{session: session{ID: "stop-2", Name: "Stopped 2"}, Runtime: runtimeObservation{Presence: "not_started"}, Status: "Not started"},
	}
	groups := (statusGrouper{}).Group(views)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].ID != "active" || groups[0].Meta != "1" || groups[0].Sessions[0].ID != "act-1" {
		t.Fatalf("unexpected active group: %+v", groups[0])
	}
	if groups[1].ID != "stopped" || groups[1].Meta != "2" || groups[1].Sessions[0].ID != "stop-1" || groups[1].Sessions[1].ID != "stop-2" {
		t.Fatalf("unexpected stopped group: %+v", groups[1])
	}
	if groups[2].ID != "external" || groups[2].Meta != "1" || groups[2].Sessions[0].ID != "ext-1" {
		t.Fatalf("unexpected external group: %+v", groups[2])
	}

	// Empty groups must be omitted.
	onlyActive := (statusGrouper{}).Group([]sessionView{views[2]})
	if len(onlyActive) != 1 || onlyActive[0].ID != "active" {
		t.Fatalf("expected only active group, got %+v", onlyActive)
	}
}

func TestStatusRoleAvoidsYellowForNormalLiveStates(t *testing.T) {
	cases := map[string]string{
		"Working":     "ok",
		"Running":     "ok",
		"Idle":        "label",
		"Done":        "label",
		"Starting":    "title",
		"Blocked":     "warn",
		"Stopped":     "muted",
		"Not started": "muted",
		"External":    "muted",
		"Unknown":     "muted",
		"Failed":      "bad",
	}
	for word, want := range cases {
		if got := statusRole(word); got != want {
			t.Errorf("statusRole(%q) = %q, want %q", word, got, want)
		}
	}
}

func TestSessionsGroupedLayoutNoiseReductionAndNavigation(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	p1 := profile{ID: "p1", WorkspaceID: "ws", Name: "Existing environment", Kind: profileExisting}
	p2 := profile{ID: "p2", WorkspaceID: "ws", Name: "Work", Kind: profileIsolated}
	b1 := profileBinding{WorkspaceID: "ws", ProfileID: "p1", Client: "codex", EnvironmentID: "e1"}
	b2 := profileBinding{WorkspaceID: "ws", ProfileID: "p2", Client: "codex", EnvironmentID: "e2"}

	m := newHarborModel(&fakeApplicationService{})
	m.width, m.height = 100, 30
	m.profiles = []profile{p1}
	m.activeProfile = &b1
	m.sessions = []sessionView{
		{
			session: session{
				ID: "s-active", Name: "fix-ui", CLI: "codex", Mode: "local", WorkingDirectory: "/work/agent-harbor",
				Profile: &b1, Started: true, Recovery: "ready",
				Conversation: &conversationEvidence{
					Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "11111111-1111-7111-8111-111111111111"},
					Status:    "observed",
					History: []conversationTransition{{
						From:   herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "00000000-0000-7000-8000-000000000000"},
						To:     herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "11111111-1111-7111-8111-111111111111"},
						At:     time.Now().UTC(),
						Source: "herdr-hook",
					}},
				},
			},
			Running: true,
			Runtime: runtimeObservation{Presence: "observed", Activity: "working", Associated: true},
			Status:  "Working",
		},
		{
			session: session{
				ID: "s-conflict", Name: "legacy-row", CLI: "codex", Mode: "local", WorkingDirectory: "/work/other",
				Profile: &b1, Started: true, Recovery: "needs_review",
				CodexSessionID:  "22222222-2222-7222-8222-222222222222",
				ReportedCodexID: "33333333-3333-7333-8333-333333333333",
				Conversation: &conversationEvidence{
					Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "22222222-2222-7222-8222-222222222222"},
					Status:    "conflict",
				},
			},
			Runtime: runtimeObservation{Presence: "historical_exit"},
			Status:  "Stopped",
		},
		{
			session:  session{ID: "ext:agy", Name: "agy", CLI: "agy", Mode: "unknown", WorkingDirectory: "/work/agent-harbor"},
			External: true,
			Running:  true,
			Runtime:  runtimeObservation{Presence: "observed", Activity: "idle"},
			Status:   "Idle",
		},
	}

	view := cleanUI(m.sessionsLayout().view())
	// Single profile: header and rows hide verbose Profile label.
	if strings.Contains(view, "shared existing environment") || strings.Contains(view, "Profile: Existing environment") {
		t.Fatalf("single-profile mode still shows verbose profile label:\n%s", view)
	}
	// Three groups rendered.
	for _, header := range []string{"Active", "Stopped", "External"} {
		if !strings.Contains(view, header) {
			t.Fatalf("missing group header %q:\n%s", header, view)
		}
	}
	// Compact badge for conflict, calm External tag without yellow warning sentence in list.
	if !strings.Contains(view, "[Conflict]") {
		t.Fatalf("missing [Conflict] badge:\n%s", view)
	}
	if strings.Contains(view, "External · connection unknown") || strings.Contains(view, "Connection and conversation intent unknown") {
		t.Fatalf("external row still contains noisy warning text:\n%s", view)
	}
	if sessionAttention(m.sessions[2]) != "" {
		t.Fatalf("external session should not generate attention warning: %q", sessionAttention(m.sessions[2]))
	}
	// Right preview shows prior transitions summary for selected active session.
	if !strings.Contains(view, "1 prior conversation transitions") {
		t.Fatalf("right preview missing transition summary:\n%s", view)
	}
	// Details includes recorded history transition.
	if details := cleanUI(m.details(m.sessions[0])); !strings.Contains(details, "Prior transitions") || !strings.Contains(details, "herdr-hook") {
		t.Fatalf("details missing transition history:\n%s", details)
	}

	// Keyboard Down crosses group headers seamlessly: s-active -> s-conflict -> ext:agy.
	n := appKey(m, "down").(harborModel)
	if sel, ok := n.selected(); !ok || sel.ID != "s-conflict" {
		t.Fatalf("down did not move across Active->Stopped: %+v", sel)
	}
	n = appKey(n, "down").(harborModel)
	if sel, ok := n.selected(); !ok || sel.ID != "ext:agy" {
		t.Fatalf("down did not move across Stopped->External: %+v", sel)
	}
	// Multi-profile mode surfaces short profile name without verbose suffix.
	m.profiles = []profile{p1, p2}
	m.activeProfile = &b2
	m.sessions[0].Profile = &b2
	multiView := cleanUI(m.sessionsLayout().view())
	if !strings.Contains(multiView, "Harbor · Work") || strings.Contains(multiView, "shared existing environment") {
		t.Fatalf("multi-profile header/row formatting unexpected:\n%s", multiView)
	}

	// Search filter hides groups with no matches.
	m.filter = "agy"
	filteredView := cleanUI(m.sessionsLayout().view())
	if strings.Contains(filteredView, "Active") || strings.Contains(filteredView, "Stopped") || !strings.Contains(filteredView, "External") {
		t.Fatalf("empty groups were not hidden after filtering:\n%s", filteredView)
	}

	// Resume confirmation screen keeps calm muted styling for routine unverified disclosure.
	m.screen = "confirm"
	m.action = "query-resume"
	m.reviewed = m.sessions[0].session
	rawConfirm := m.reviewLayout().view()
	if !strings.Contains(cleanUI(rawConfirm), "Current login and other writers: unverified.") {
		t.Fatalf("missing disclosure line in resume confirmation")
	}
	// Verify muted color (#777D8C -> RGB 119;125;140) rather than warn yellow (#DAB77E -> RGB 218;183;126).
	for _, line := range strings.Split(rawConfirm, "\n") {
		if strings.Contains(cleanUI(line), "Current login and other writers: unverified.") {
			if strings.Contains(line, "218;183;126") {
				t.Fatalf("routine disclosure still painted warn yellow: %q", line)
			}
		}
	}
}
