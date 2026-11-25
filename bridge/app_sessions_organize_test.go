package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func settleCmd(t *testing.T, m harborModel, cmd tea.Cmd) harborModel {
	t.Helper()
	var model tea.Model = m
	for steps := 0; cmd != nil && steps < 16; steps++ {
		next := settleApp(model, cmd)
		model = next
		cmd = nil
	}
	return model.(harborModel)
}

func sendSessionsKey(t *testing.T, m harborModel, k string) harborModel {
	t.Helper()
	n, cmd := m.Update(uiKey(k))
	return settleCmd(t, n.(harborModel), cmd)
}

func makeOrganizeSession(id, name string, presence, activity string, opts ...func(*sessionView)) sessionView {
	running := presence == "observed"
	status := unobservedStatus(runtimeObservation{Presence: presence})
	if running {
		status = runtimeLabel(runtimeObservation{Presence: presence, Activity: activity})
	}
	v := sessionView{
		session: session{
			ID:               id,
			Version:          3,
			Name:             name,
			WorkingDirectory: "/work/harbor",
			CLI:              "codex",
			Mode:             "local",
			Recovery:         "ready",
			Started:          presence != "not_started",
			CreatedAt:        time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
		},
		Running: running,
		Runtime: runtimeObservation{Presence: presence, Activity: activity},
		Status:  status,
	}
	if v.Started {
		v.Conversation = &conversationEvidence{
			Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "11111111-1111-7111-8111-111111111111"},
			Status:    "observed",
		}
	}
	for _, opt := range opts {
		opt(&v)
	}
	return v
}

func TestSessionsPinPersistenceOrderingAndTransition(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("AGENT_HARBOR_ASCII", "0")
	root := t.TempDir()
	bridgeDir := filepath.Join(root, "bridge")
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", bridgeDir)
	t.Setenv("HERDR_SOCKET_PATH", "")

	idA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	idB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := withSessions(func([]session) ([]session, error) {
		return []session{
			{ID: idA, Version: 3, Name: "Alpha", WorkingDirectory: "/work/a", CLI: "codex", Mode: "local", Recovery: "ready", CreatedAt: time.Now().UTC()},
			{ID: idB, Version: 3, Name: "Beta", WorkingDirectory: "/work/b", CLI: "codex", Mode: "local", Recovery: "ready", CreatedAt: time.Now().UTC().Add(-time.Minute)},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := harborService{}
	views, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	var beta sessionView
	for _, v := range views {
		if v.ID == idB {
			beta = v
		}
	}
	if err := svc.SetPinned(beta, true); err != nil {
		t.Fatalf("SetPinned(true) failed: %v", err)
	}

	// External cannot be pinned.
	if err := svc.SetPinned(sessionView{session: session{ID: "external:1"}, External: true}, true); err == nil {
		t.Fatal("expected error pinning external session")
	}

	// Verify persistence on disk and ordering at top of its group (no global Pinned group, no duplicate).
	reloaded, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	groups := (statusGrouper{}).Group(reloaded)
	if len(groups) != 1 || groups[0].ID != "stopped" {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	if len(groups[0].Sessions) != 2 || groups[0].Sessions[0].ID != idB || !groups[0].Sessions[0].Pinned || groups[0].Sessions[1].ID != idA {
		t.Fatalf("pinned session not placed first without duplicates: %+v", groups[0].Sessions)
	}

	// Pin follows session from Active -> Stopped.
	activePinned := makeOrganizeSession(idB, "Beta", "observed", "idle", func(v *sessionView) { v.Pinned = true })
	activeOther := makeOrganizeSession(idA, "Alpha", "observed", "working")
	ext := makeOrganizeSession("external:agy", "agy", "observed", "idle", func(v *sessionView) { v.External = true })
	gActive := (statusGrouper{}).Group([]sessionView{activeOther, activePinned, ext})
	if gActive[0].ID != "active" || gActive[0].Sessions[0].ID != idB {
		t.Fatalf("pinned session should be at top of Active: %+v", gActive[0].Sessions)
	}
	stoppedPinned := makeOrganizeSession(idB, "Beta", "historical_exit", "unknown", func(v *sessionView) { v.Pinned = true })
	stoppedOther := makeOrganizeSession("cccccccccccccccccccccccccccccccc", "Gamma", "historical_exit", "unknown")
	gMoved := (statusGrouper{}).Group([]sessionView{activeOther, stoppedOther, stoppedPinned, ext})
	if gMoved[1].ID != "stopped" || gMoved[1].Sessions[0].ID != idB {
		t.Fatalf("pinned session did not follow to top of Stopped: %+v", gMoved[1].Sessions)
	}

	// Normal filter still applies to pinned sessions.
	m := newHarborModel(&fakeApplicationService{Sessions: []sessionView{activeOther, activePinned, ext}})
	m.sessions = []sessionView{activeOther, activePinned, ext}
	m.width, m.height = 100, 30
	if !strings.Contains(cleanUI(m.View()), "📌") {
		t.Fatalf("expected pin icon in rendered view:\n%s", cleanUI(m.View()))
	}
	m.filter = "Alpha"
	vis := m.visible()
	if len(vis) != 1 || vis[0].ID != idA {
		t.Fatalf("filter did not exclude non-matching pinned session: %+v", vis)
	}

	// Unpin persists.
	if err := svc.SetPinned(beta, false); err != nil {
		t.Fatalf("SetPinned(false) failed: %v", err)
	}
	reloaded, err = svc.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range reloaded {
		if v.ID == idB && v.Pinned {
			t.Fatal("expected unpin to persist")
		}
	}
}

func TestSessionsArchiveReverificationPreservationAndRemove(t *testing.T) {
	root := t.TempDir()
	bridgeDir := filepath.Join(root, "bridge")
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", bridgeDir)
	t.Setenv("HERDR_SOCKET_PATH", "")

	// Synthetic client history file that MUST remain untouched on Archive, Unarchive, and Remove.
	clientHistoryDir := filepath.Join(root, "codex-history")
	if err := os.MkdirAll(clientHistoryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	historyFile := filepath.Join(clientHistoryDir, "conversation.jsonl")
	const historyContent = "{\"role\":\"user\",\"content\":\"keep me\"}\n"
	if err := os.WriteFile(historyFile, []byte(historyContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Archive eligibility checks: Idle, Done, Working, Blocked, Not observed, Offline, External are all rejected.
	for _, tc := range []struct {
		name string
		view sessionView
		want bool
	}{
		{"not_started", makeOrganizeSession("1", "Not started", "not_started", "unknown"), true},
		{"historical_exit", makeOrganizeSession("2", "Exited", "historical_exit", "unknown"), true},
		{"confirmed_exit", makeOrganizeSession("3", "Confirmed", "confirmed_exit", "unknown"), true},
		{"observed_idle", makeOrganizeSession("4", "Idle", "observed", "idle"), false},
		{"observed_done", makeOrganizeSession("5", "Done", "observed", "done"), false},
		{"observed_working", makeOrganizeSession("6", "Working", "observed", "working"), false},
		{"not_observed", makeOrganizeSession("7", "Unobserved", "not_observed", "unknown"), false},
		{"offline_discovery", makeOrganizeSession("8", "Offline", "historical_exit", "unknown", func(v *sessionView) { v.DiscoveryWarning = agentDiscoveryWarning }), false},
		{"external", makeOrganizeSession("external:9", "Ext", "observed", "idle", func(v *sessionView) { v.External = true }), false},
	} {
		reason := archiveBlockReason(tc.view)
		if (reason == "") != tc.want {
			t.Errorf("%s: archiveBlockReason=%q, want allowed=%v", tc.name, reason, tc.want)
		}
	}

	// Real harborService persistence & preservation test.
	id := "11111111222222223333333344444444"
	exitedAt := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	binding := profileBinding{WorkspaceID: "ws-1", ProfileID: "prof-1", Client: "codex", EnvironmentID: "env-1"}
	conv := &conversationEvidence{
		Reference:  herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "aaaaaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"},
		Status:     "observed",
		Provenance: "herdr-hook",
	}
	instSnap := &magpieInstance{ID: "lab", Kind: "managed", Address: "http://127.0.0.1:4321"}
	orig := session{
		ID:               id,
		Version:          3,
		Name:             "Carry forward",
		WorkingDirectory: root,
		CLI:              "codex",
		Mode:             "magpie",
		InstanceID:       "lab",
		InstanceSnapshot: instSnap,
		MagpieURL:        "http://127.0.0.1:4321",
		MagpieModel:      "relay/gpt-5",
		Profile:          &binding,
		Conversation:     conv,
		CodexSessionID:   "aaaaaaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee",
		Recovery:         "ready",
		Started:          true,
		Pinned:           true,
		LastExitAt:       &exitedAt,
		CreatedAt:        exitedAt.Add(-time.Hour),
	}
	if err := withSessions(func([]session) ([]session, error) { return []session{orig}, nil }); err != nil {
		t.Fatal(err)
	}

	svc := harborService{}
	view, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate active runner holding the session lock -> Archive must re-verify and refuse.
	lock, err := lockSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Archive(view); err == nil {
		lock.Close()
		t.Fatal("Archive succeeded while session lock was held")
	}
	lock.Close()

	// Archive succeeds when confirmed exited and unlocked.
	if err := svc.Archive(view); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}
	archivedView, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !archivedView.Archived || archivedView.ArchivedAt == nil {
		t.Fatalf("expected archived state persisted: %+v", archivedView.session)
	}
	// Verify conversation, Profile, connection, and Pin were NOT altered.
	if !archivedView.Pinned ||
		archivedView.Conversation == nil || archivedView.Conversation.Reference != conv.Reference ||
		archivedView.Profile == nil || *archivedView.Profile != binding ||
		archivedView.Mode != "magpie" || archivedView.InstanceID != "lab" || archivedView.MagpieModel != "relay/gpt-5" {
		t.Fatalf("Archive modified identity/connection/pin: %+v", archivedView.session)
	}

	// Archived session cannot be started or resumed before explicit Unarchive.
	if err := svc.Open(sessionApproval{Session: archivedView.session, Confirmed: true}); err == nil {
		t.Fatal("Open allowed archived session")
	}
	if err := svc.Resume(sessionApproval{Session: archivedView.session, Confirmed: true}); err == nil {
		t.Fatal("Resume allowed archived session")
	}
	if err := svc.Start(sessionApproval{Session: archivedView.session, Confirmed: true}); err == nil {
		t.Fatal("Start allowed archived session")
	}
	if canEditCodexOptions(archivedView) {
		t.Fatal("canEditCodexOptions allowed archived session")
	}

	// Unarchive restores cleanly without altering Pin, Conversation, Profile, or connection.
	if err := svc.Unarchive(archivedView); err != nil {
		t.Fatalf("Unarchive failed: %v", err)
	}
	restored, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Archived || restored.ArchivedAt != nil || !restored.Pinned ||
		restored.Conversation == nil || restored.Conversation.Reference != conv.Reference ||
		restored.Profile == nil || *restored.Profile != binding ||
		restored.Mode != "magpie" || restored.InstanceID != "lab" || restored.MagpieModel != "relay/gpt-5" {
		t.Fatalf("Unarchive did not preserve original record: %+v", restored.session)
	}

	// Archive again and Remove from Archived -> deletes only Harbor record, keeps client history file.
	if err := svc.Archive(restored); err != nil {
		t.Fatal(err)
	}
	archivedAgain, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(sessionApproval{Session: archivedAgain.session, Observation: &archivedAgain.Runtime, Confirmed: true}); err != nil {
		t.Fatalf("Remove archived record failed: %v", err)
	}
	remaining, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected record removed, got %d", len(remaining))
	}
	data, err := os.ReadFile(historyFile)
	if err != nil || string(data) != historyContent {
		t.Fatalf("client history file was altered or removed: err=%v data=%q", err, string(data))
	}
}

func TestSessionsArchiveViewUIAndExecutionReverification(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("AGENT_HARBOR_ASCII", "0")
	stopped := makeOrganizeSession("s1", "Done work", "historical_exit", "unknown", func(v *sessionView) { v.Pinned = true })
	idle := makeOrganizeSession("s2", "Still idle", "observed", "idle")
	stale := makeOrganizeSession("s3", "Becomes live", "historical_exit", "unknown")

	fake := &fakeApplicationService{Sessions: []sessionView{idle, stopped, stale}}
	m := newHarborModel(fake)
	m.width, m.height = 100, 30
	m.sessions = append([]sessionView(nil), fake.Sessions...)

	// Active Idle session cannot be archived via 'A'.
	m = sendSessionsKey(t, m, "A")
	if !strings.Contains(strings.ToLower(m.notice), "cannot archive") {
		t.Fatalf("expected Idle archive refusal notice, got %q", m.notice)
	}
	if fake.Sessions[0].Archived {
		t.Fatal("Idle session was archived")
	}

	// Move to Stopped pinned session and Archive via 'A'.
	m = sendSessionsKey(t, m, "j")
	if sel, ok := m.selected(); !ok || sel.ID != "s1" {
		t.Fatalf("expected s1 selected, got %+v", sel)
	}
	m = sendSessionsKey(t, m, "A")
	if !fake.Sessions[1].Archived {
		t.Fatal("s1 was not archived")
	}
	if !strings.Contains(cleanUI(m.View()), "Archived (1)") {
		t.Fatalf("footer missing Archived (1):\n%s", cleanUI(m.View()))
	}

	// Simulate s3 becoming observed Idle on the service side right before Archive executes -> re-verification blocks it!
	fake.Sessions[2].Running = true
	fake.Sessions[2].Runtime = runtimeObservation{Presence: "observed", Activity: "idle"}
	// Select s3 in UI (UI still thinks s3 is historical_exit).
	m.focusSessionRowID("s3")
	m = sendSessionsKey(t, m, "A")
	if fake.Sessions[2].Archived {
		t.Fatal("Archive did not re-verify live state on execution")
	}

	// Open Archived view with 'g'.
	m = sendSessionsKey(t, m, "g")
	if !m.archivedView {
		t.Fatal("g did not open Archived view")
	}
	archivedRender := cleanUI(m.View())
	if !strings.Contains(archivedRender, "Archived") || !strings.Contains(archivedRender, "Done work") || !strings.Contains(archivedRender, "📌") {
		t.Fatalf("Archived view missing archived pinned record:\n%s", archivedRender)
	}

	// Enter in Archived view opens Details (never starts or resumes!).
	m = sendSessionsKey(t, m, "enter")
	if m.screen != "detail" {
		t.Fatalf("Enter in Archived view opened %q instead of detail", m.screen)
	}
	if !strings.Contains(cleanUI(m.View()), "Unarchive this session before starting or resuming") {
		t.Fatalf("Archived Details missing unarchive guidance:\n%s", cleanUI(m.View()))
	}
	m = sendSessionsKey(t, m, "esc")
	if m.screen != "sessions" || !m.archivedView {
		t.Fatalf("Esc from Archived Details did not return to Archived view: screen=%s archived=%v", m.screen, m.archivedView)
	}

	// Remove confirmation from Archived view cancels back to Archived view.
	m = sendSessionsKey(t, m, "x")
	if m.screen != "confirm" || m.action != "remove" {
		t.Fatalf("x in Archived view did not open remove confirm: %s %s", m.screen, m.action)
	}
	m = sendSessionsKey(t, m, "esc")
	if m.screen != "sessions" || !m.archivedView {
		t.Fatalf("Esc from remove confirm left Archived view: screen=%s archived=%v", m.screen, m.archivedView)
	}

	// Unarchive via 'u' restores record with Pin preserved and does NOT launch.
	m = sendSessionsKey(t, m, "u")
	if fake.Sessions[1].Archived || !fake.Sessions[1].Pinned {
		t.Fatalf("Unarchive did not clear Archived while keeping Pin: %+v", fake.Sessions[1].session)
	}
	for _, c := range fake.Calls {
		if strings.HasPrefix(c, "open ") || strings.HasPrefix(c, "start ") || strings.HasPrefix(c, "resume ") {
			t.Fatalf("Unarchive triggered launch: %v", fake.Calls)
		}
	}

	// Esc exits Archived view back to normal Sessions.
	m = sendSessionsKey(t, m, "esc")
	if m.archivedView || m.screen != "sessions" {
		t.Fatalf("Esc did not exit Archived view: screen=%s archived=%v", m.screen, m.archivedView)
	}
}

func TestSessionsFoldingRulesAndResurfacing(t *testing.T) {
	views := []sessionView{
		// Active: 1 pinned idle, 1 working, 1 blocked, 1 starting, 1 idle w/ conflict, 8 ordinary idle/done
		makeOrganizeSession("a-pin", "Pinned Idle", "observed", "idle", func(v *sessionView) { v.Pinned = true }),
		makeOrganizeSession("a-work", "Working Now", "observed", "working"),
		makeOrganizeSession("a-block", "Blocked Now", "observed", "blocked"),
		makeOrganizeSession("a-start", "Starting Now", "observed", "idle", func(v *sessionView) { v.Runtime.LaunchPending = true }),
		makeOrganizeSession("a-conf", "Conflict Idle", "observed", "idle", func(v *sessionView) { v.Conversation.Status = "conflict" }),
	}
	for i := 1; i <= 8; i++ {
		act := "idle"
		if i%2 == 0 {
			act = "done"
		}
		views = append(views, makeOrganizeSession(fmt.Sprintf("a-idle-%d", i), fmt.Sprintf("Idle %d", i), "observed", act))
	}
	// Stopped: 1 pinned, 1 not_observed, 1 conflict, 7 exited/not_started
	views = append(views,
		makeOrganizeSession("s-pin", "Pinned Stopped", "historical_exit", "unknown", func(v *sessionView) { v.Pinned = true }),
		makeOrganizeSession("s-unobs", "Not Observed", "not_observed", "unknown"),
		makeOrganizeSession("s-conf", "Conflict Stopped", "historical_exit", "unknown", func(v *sessionView) { v.Conversation.Status = "conflict" }),
	)
	for i := 1; i <= 7; i++ {
		pres := "historical_exit"
		if i == 2 {
			pres = "confirmed_exit"
		} else if i == 3 {
			pres = "not_started"
		}
		views = append(views, makeOrganizeSession(fmt.Sprintf("s-exit-%d", i), fmt.Sprintf("Stopped %d", i), pres, "unknown"))
	}
	// External: 8 sessions (must NOT fold)
	for i := 1; i <= 8; i++ {
		views = append(views, makeOrganizeSession(fmt.Sprintf("external:%d", i), fmt.Sprintf("Ext %d", i), "observed", "idle", func(v *sessionView) { v.External = true }))
	}
	// Archived: 8 sessions (must NOT fold in Archived view)
	for i := 1; i <= 8; i++ {
		views = append(views, makeOrganizeSession(fmt.Sprintf("arch-%d", i), fmt.Sprintf("Archived %d", i), "historical_exit", "unknown", func(v *sessionView) { v.Archived = true }))
	}

	m := newHarborModel(&fakeApplicationService{Sessions: views})
	m.sessions = views
	m.width, m.height = 120, 40

	groups := m.sessionDisplayGroups()
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	// Active: 5 non-collapsible + 5 collapsible + 1 toggle ("Show 3 more") = 11 rows.
	if groups[0].ID != "active" || len(groups[0].Rows) != 11 {
		t.Fatalf("Active collapsed rows = %d, want 11", len(groups[0].Rows))
	}
	lastActive := groups[0].Rows[len(groups[0].Rows)-1]
	if lastActive.Kind != sessionRowToggle || lastActive.Hidden != 3 || lastActive.ToggleLabel() != "Show 3 more" {
		t.Fatalf("unexpected Active toggle row: %+v", lastActive)
	}
	// Stopped: 3 non-collapsible + 5 collapsible + 1 toggle ("Show 2 more") = 9 rows.
	if groups[1].ID != "stopped" || len(groups[1].Rows) != 9 {
		t.Fatalf("Stopped collapsed rows = %d, want 9", len(groups[1].Rows))
	}
	lastStopped := groups[1].Rows[len(groups[1].Rows)-1]
	if lastStopped.Kind != sessionRowToggle || lastStopped.Hidden != 2 || lastStopped.ToggleLabel() != "Show 2 more" {
		t.Fatalf("unexpected Stopped toggle row: %+v", lastStopped)
	}
	// External: 8 rows, NO toggle!
	if groups[2].ID != "external" || len(groups[2].Rows) != 8 || groups[2].HasToggle {
		t.Fatalf("External should never fold: len=%d toggle=%v", len(groups[2].Rows), groups[2].HasToggle)
	}

	// Verify folded Idle #8 re-surfaces immediately when it starts Working!
	updated := append([]sessionView(nil), views...)
	for i := range updated {
		if updated[i].ID == "a-idle-8" {
			updated[i].Runtime.Activity = "working"
			updated[i].Status = "Working"
		}
	}
	n, _ := m.Update(appLoaded{generation: m.listGeneration, sessions: updated})
	m = n.(harborModel)
	foundResurfaced := false
	for _, r := range m.visibleRows() {
		if r.Kind == sessionRowItem && r.View.ID == "a-idle-8" {
			foundResurfaced = true
			break
		}
	}
	if !foundResurfaced {
		t.Fatal("folded Idle session did not resurface when it started Working")
	}

	// Verify folded Idle #7 re-surfaces when it needs attention (Conflict)!
	updated2 := append([]sessionView(nil), updated...)
	for i := range updated2 {
		if updated2[i].ID == "a-idle-7" {
			updated2[i].Conversation.Status = "conflict"
		}
	}
	n, _ = m.Update(appLoaded{generation: m.listGeneration, sessions: updated2})
	m = n.(harborModel)
	foundConflict := false
	for _, r := range m.visibleRows() {
		if r.Kind == sessionRowItem && r.View.ID == "a-idle-7" {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Fatal("folded Idle session did not resurface when it needed attention")
	}

	// Verify Archived view does NOT fold 8 archived sessions.
	m.archivedView = true
	archGroups := m.sessionDisplayGroups()
	if len(archGroups) != 1 || archGroups[0].ID != "archived" || len(archGroups[0].Rows) != 8 || archGroups[0].HasToggle {
		t.Fatalf("Archived view should not fold: %+v", archGroups)
	}
}

func TestSessionsHundredRecordsKeyboardExpandDeepCollapseSearchAndRefresh(t *testing.T) {
	var views []sessionView
	// Active: 2 pinned + 1 working + 25 idle = 28 (20 folded)
	views = append(views,
		makeOrganizeSession("act-pin-1", "Active Pin 1", "observed", "idle", func(v *sessionView) { v.Pinned = true }),
		makeOrganizeSession("act-pin-2", "Active Pin 2", "observed", "done", func(v *sessionView) { v.Pinned = true }),
		makeOrganizeSession("act-work-1", "Active Work 1", "observed", "working"),
	)
	for i := 1; i <= 25; i++ {
		views = append(views, makeOrganizeSession(fmt.Sprintf("act-idle-%03d", i), fmt.Sprintf("Active Idle %03d", i), "observed", "idle"))
	}
	// Stopped: 1 pinned + 1 conflict + 85 exited = 87 (80 folded)
	views = append(views,
		makeOrganizeSession("stp-pin-1", "Stopped Pin 1", "historical_exit", "unknown", func(v *sessionView) { v.Pinned = true }),
		makeOrganizeSession("stp-conf-1", "Stopped Conflict", "historical_exit", "unknown", func(v *sessionView) { v.Conversation.Status = "conflict" }),
	)
	for i := 1; i <= 85; i++ {
		views = append(views, makeOrganizeSession(fmt.Sprintf("stp-exit-%03d", i), fmt.Sprintf("Stopped Exit %03d", i), "historical_exit", "unknown"))
	}
	// External: 3 records
	for i := 1; i <= 3; i++ {
		views = append(views, makeOrganizeSession(fmt.Sprintf("external:%d", i), fmt.Sprintf("External %d", i), "observed", "idle", func(v *sessionView) { v.External = true }))
	}

	fake := &fakeApplicationService{Sessions: views}
	m := newHarborModel(fake)
	m.sessions = views
	m.width, m.height = 120, 34

	// Collapsed total visible rows: Active (3+5+1=9) + Stopped (2+5+1=8) + External (3) = 20 rows (out of 118 records!).
	if got := len(m.visibleRows()); got != 20 {
		t.Fatalf("collapsed visibleRows = %d, want 20", got)
	}

	// Pure keyboard 'j' crosses Active -> Active toggle -> Stopped -> Stopped toggle -> External in 17 steps!
	for i := 0; i < 8; i++ {
		m = sendSessionsKey(t, m, "j")
	}
	row, ok := m.selectedRow()
	if !ok || row.Kind != sessionRowToggle || row.GroupID != "active" || row.ToggleLabel() != "Show 20 more" {
		t.Fatalf("step 8 expected Active toggle, got %+v", row)
	}
	m = sendSessionsKey(t, m, "j")
	if sel, ok := m.selected(); !ok || sel.ID != "stp-pin-1" {
		t.Fatalf("j did not skip folded Active rows into Stopped: %+v", sel)
	}
	for i := 0; i < 7; i++ {
		m = sendSessionsKey(t, m, "j")
	}
	row, ok = m.selectedRow()
	if !ok || row.Kind != sessionRowToggle || row.GroupID != "stopped" || row.ToggleLabel() != "Show 80 more" {
		t.Fatalf("expected Stopped toggle, got %+v", row)
	}
	toggleIndexBefore := m.cursor

	// Enter on Stopped toggle expands all 80 in place and turns toggle into "Show less" at the same index!
	m = sendSessionsKey(t, m, "enter")
	row, ok = m.selectedRow()
	if !ok || row.Kind != sessionRowToggle || row.GroupID != "stopped" || !row.Expanded || row.ToggleLabel() != "Show less" {
		t.Fatalf("Enter did not expand in place: %+v", row)
	}
	if m.cursor != toggleIndexBefore {
		t.Fatalf("toggle moved from index %d to %d", toggleIndexBefore, m.cursor)
	}
	if got := len(m.visibleRows()); got != 100 {
		t.Fatalf("expanded visibleRows = %d, want 100", got)
	}

	// Browse deep (50 rows down into expanded Stopped) and verify footer shows 'z Collapse group'.
	for i := 0; i < 50; i++ {
		m = sendSessionsKey(t, m, "j")
	}
	deepSel, ok := m.selected()
	if !ok || deepSel.ID != "stp-exit-055" {
		t.Fatalf("expected stp-exit-055 deep in expanded Stopped, got %+v", deepSel)
	}
	if !strings.Contains(cleanUI(m.View()), "Collapse group") {
		t.Fatalf("footer missing Collapse group while deep in expanded group:\n%s", cleanUI(m.View()))
	}

	// Background refresh must NOT reset expanded state or move focus away from stp-exit-055!
	n, _ := m.Update(appLoaded{generation: m.listGeneration, sessions: views})
	m = n.(harborModel)
	if !m.expandedGroups["stopped"] {
		t.Fatal("refresh reset expanded group state")
	}
	if afterSel, ok := m.selected(); !ok || afterSel.ID != "stp-exit-055" {
		t.Fatalf("refresh shifted focus away from deep expanded row: %+v", afterSel)
	}

	// Press 'z' deep in expanded group -> collapses Stopped and returns focus directly to Stopped toggle row!
	m = sendSessionsKey(t, m, "z")
	if m.expandedGroups["stopped"] {
		t.Fatal("z did not collapse expanded Stopped group")
	}
	row, ok = m.selectedRow()
	if !ok || row.Kind != sessionRowToggle || row.GroupID != "stopped" || row.Expanded {
		t.Fatalf("z did not return focus to collapsed Stopped action row: %+v", row)
	}

	// Background refresh while focused on toggle row keeps focus on toggle row without selectionMissing.
	n, _ = m.Update(appLoaded{generation: m.listGeneration, sessions: views})
	m = n.(harborModel)
	if m.selectionMissing {
		t.Fatal("refresh while on toggle row set selectionMissing")
	}
	if r, ok := m.selectedRow(); !ok || r.RowID() != "toggle:stopped" {
		t.Fatalf("refresh lost toggle focus: %+v", r)
	}

	// Search finds folded item stp-exit-077, and clearing search keeps stp-exit-077 selected and visible!
	m = keySearch(m, "/")
	m = keySearch(m, "Stopped Exit 077")
	if sel, ok := m.selected(); !ok || sel.ID != "stp-exit-077" {
		t.Fatalf("search did not find folded session stp-exit-077: %+v", sel)
	}
	m = keySearch(m, "enter")
	m = keySearch(m, "esc") // clears filter
	if m.filter != "" {
		t.Fatalf("filter not cleared: %q", m.filter)
	}
	if sel, ok := m.selected(); !ok || sel.ID != "stp-exit-077" {
		t.Fatalf("clearing search auto-folded the selected session: %+v", sel)
	}
	// Moving down from stp-exit-077 lands cleanly on the Stopped toggle row.
	m = sendSessionsKey(t, m, "j")
	if r, ok := m.selectedRow(); !ok || r.RowID() != "toggle:stopped" {
		t.Fatalf("moving off kept-visible row did not land on toggle: %+v", r)
	}
}

func TestSessionsColumnAlignmentBeforeAndAfterExpandWideAndNarrow(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("AGENT_HARBOR_ASCII", "0")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	views := []sessionView{
		makeOrganizeSession("s-pin", "Pinned One", "historical_exit", "unknown", func(v *sessionView) { v.Pinned = true }),
	}
	for i := 1; i <= 8; i++ {
		views = append(views, makeOrganizeSession(fmt.Sprintf("s-%d", i), fmt.Sprintf("Stopped %02d", i), "historical_exit", "unknown"))
	}

	for _, sz := range [][2]int{{120, 34}, {60, 24}, {44, 18}} {
		m := newHarborModel(&fakeApplicationService{Sessions: views})
		m.sessions = views
		m.width, m.height = sz[0], sz[1]

		checkFrame := func(stage string, expanded bool) {
			raw := m.View()
			lines := strings.Split(raw, "\n")
			if len(lines) > m.height {
				t.Fatalf("%v %s: height overflow %d > %d", sz, stage, len(lines), m.height)
			}
			var normalCol, toggleCol, expandedCol, pinStatusCol, normalStatusCol int
			normalCol, toggleCol, expandedCol, pinStatusCol, normalStatusCol = -1, -1, -1, -1, -1
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%v %s: width overflow %d > %d: %q", sz, stage, ansi.StringWidth(line), m.width, cleanUI(line))
				}
				plain := cleanUI(line)
				if idx := strings.Index(plain, "Stopped 05"); idx >= 0 {
					normalCol = ansi.StringWidth(plain[:idx])
					if st := strings.LastIndex(plain, "Stopped"); st > idx {
						normalStatusCol = ansi.StringWidth(plain[:st])
					}
				}
				if idx := strings.Index(plain, "Pinned One"); idx >= 0 {
					if st := strings.LastIndex(plain, "Stopped"); st > idx {
						pinStatusCol = ansi.StringWidth(plain[:st])
					}
				}
				if !expanded {
					if idx := strings.Index(plain, "▸ Show 3 more"); idx >= 0 {
						toggleCol = ansi.StringWidth(plain[:idx]) + ansi.StringWidth("▸ ")
					}
				} else {
					if idx := strings.Index(plain, "▾ Show less"); idx >= 0 {
						toggleCol = ansi.StringWidth(plain[:idx]) + ansi.StringWidth("▾ ")
					}
					if idx := strings.Index(plain, "Stopped 06"); idx >= 0 {
						expandedCol = ansi.StringWidth(plain[:idx])
					}
				}
			}
			if normalCol < 0 && !expanded {
				t.Fatalf("%v %s: missing normal row\n%s", sz, stage, cleanUI(raw))
			}
			if toggleCol < 0 {
				t.Fatalf("%v %s: missing toggle row (normal=%d toggle=%d)\n%s", sz, stage, normalCol, toggleCol, cleanUI(raw))
			}
			if normalCol >= 0 && toggleCol != normalCol {
				t.Fatalf("%v %s: toggle column %d != normal name column %d\n%s", sz, stage, toggleCol, normalCol, cleanUI(raw))
			}
			if expanded && expandedCol >= 0 && expandedCol != toggleCol {
				t.Fatalf("%v %s: expanded row column %d != toggle column %d\n%s", sz, stage, expandedCol, toggleCol, cleanUI(raw))
			}
			if normalStatusCol >= 0 && pinStatusCol >= 0 && pinStatusCol != normalStatusCol {
				t.Fatalf("%v %s: pinned right status column %d != unpinned %d\n%s", sz, stage, pinStatusCol, normalStatusCol, cleanUI(raw))
			}
		}

		// Ensure toggle row is scrolled into view on compact heights too.
		m.focusSessionRowID("toggle:stopped")
		checkFrame("collapsed", false)
		// Expand in place.
		m = sendSessionsKey(t, m, "enter")
		checkFrame("expanded", true)
	}

	// Verify ASCII chrome substitution keeps cell widths intact for new glyphs.
	t.Setenv("AGENT_HARBOR_ASCII", "1")
	for _, glyph := range []string{"📌", "▸", "▾"} {
		if ansi.StringWidth(glyph) != ansi.StringWidth(chromeText(glyph)) {
			t.Fatalf("ASCII width mismatch for %q: %d vs %d (%q)", glyph, ansi.StringWidth(glyph), ansi.StringWidth(chromeText(glyph)), chromeText(glyph))
		}
	}
}

func TestSessionsPinArchiveSurviveOldPaneRewritingSessionsJSON(t *testing.T) {
	root := t.TempDir()
	bridgeDir := filepath.Join(root, "bridge")
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", bridgeDir)
	t.Setenv("HERDR_SOCKET_PATH", "")

	idA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa01"
	idB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb02"
	exited := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if err := withSessions(func([]session) ([]session, error) {
		return []session{
			{ID: idA, Version: 3, Name: "Active A", WorkingDirectory: root, CLI: "codex", Mode: "local", Recovery: "ready", CreatedAt: exited},
			{ID: idB, Version: 3, Name: "Stopped B", WorkingDirectory: root, CLI: "codex", Mode: "local", Recovery: "ready", Started: true, LastExitAt: &exited, CreatedAt: exited},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := harborService{}
	viewA, err := svc.Get(idA)
	if err != nil {
		t.Fatal(err)
	}
	viewB, err := svc.Get(idB)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPinned(viewA, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPinned(viewB, true); err != nil {
		t.Fatal(err)
	}
	viewB, err = svc.Get(idB)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Archive(viewB); err != nil {
		t.Fatal(err)
	}

	// Simulate an already-running older `_pane` process rewriting sessions.json
	// without knowing about pinned / archived / archived_at.
	stripOrgFromSessionsJSON := func() {
		t.Helper()
		sessionsPath := filepath.Join(bridgeDir, "sessions.json")
		raw, err := os.ReadFile(sessionsPath)
		if err != nil {
			t.Fatal(err)
		}
		var objs []map[string]any
		if err := json.Unmarshal(raw, &objs); err != nil {
			t.Fatal(err)
		}
		for _, obj := range objs {
			delete(obj, "pinned")
			delete(obj, "archived")
			delete(obj, "archived_at")
		}
		rewritten, err := json.MarshalIndent(objs, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sessionsPath, append(rewritten, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stripOrgFromSessionsJSON()

	gotA, err := svc.Get(idA)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := svc.Get(idB)
	if err != nil {
		t.Fatal(err)
	}
	if !gotA.Pinned {
		t.Fatalf("Pin on A was wiped when old _pane rewrote sessions.json: %+v", gotA.session)
	}
	if !gotB.Pinned || !gotB.Archived || gotB.ArchivedAt == nil {
		t.Fatalf("Pin/Archive on B was wiped when old _pane rewrote sessions.json: %+v", gotB.session)
	}

	// Unarchive B -> still keeps Pin even if old _pane strips sessions.json again.
	if err := svc.Unarchive(gotB); err != nil {
		t.Fatal(err)
	}
	stripOrgFromSessionsJSON()
	gotB, err = svc.Get(idB)
	if err != nil {
		t.Fatal(err)
	}
	if gotB.Archived || gotB.ArchivedAt != nil || !gotB.Pinned {
		t.Fatalf("Unarchive + old _pane rewrite corrupted B: %+v", gotB.session)
	}

	// Unpin A and remove B -> organization sidecar prunes both entries cleanly.
	if err := svc.SetPinned(gotA, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(sessionApproval{Session: gotB.session, Observation: &gotB.Runtime, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	org, err := readSessionOrg(bridgeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(org.Entries) != 0 {
		t.Fatalf("expected empty session_organization entries after unpin/remove, got %+v", org.Entries)
	}
}

func TestStorePreservesUnknownSessionJSONFields(t *testing.T) {
	root := t.TempDir()
	bridgeDir := filepath.Join(root, "bridge")
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", bridgeDir)

	id := "cccccccccccccccccccccccccccccc03"
	if err := withSessions(func([]session) ([]session, error) {
		return []session{{ID: id, Version: 3, Name: "Original", WorkingDirectory: root, CLI: "codex", Mode: "local", Recovery: "ready"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	// Inject a hypothetical future JSON field directly into sessions.json.
	sessionsPath := filepath.Join(bridgeDir, "sessions.json")
	raw, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	var objs []map[string]any
	if err := json.Unmarshal(raw, &objs); err != nil {
		t.Fatal(err)
	}
	objs[0]["future_meta"] = map[string]any{"tier": "gold", "count": 42}
	encoded, err := json.MarshalIndent(objs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionsPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	// Perform a normal session mutation through withSessions.
	if err := withSessions(func(all []session) ([]session, error) {
		all[0].Name = "Renamed"
		return all, nil
	}); err != nil {
		t.Fatal(err)
	}

	afterRaw, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	var afterObjs []map[string]any
	if err := json.Unmarshal(afterRaw, &afterObjs); err != nil {
		t.Fatal(err)
	}
	meta, ok := afterObjs[0]["future_meta"].(map[string]any)
	if !ok || meta["tier"] != "gold" {
		t.Fatalf("unknown future field was stripped on write: %s", string(afterRaw))
	}
	if afterObjs[0]["name"] != "Renamed" {
		t.Fatalf("known field update was not saved: %s", string(afterRaw))
	}
}

func TestPinArchiveForceRefreshRejectsStaleInFlightAppLoaded(t *testing.T) {
	s1 := makeOrganizeSession("s1", "First", "historical_exit", "unknown")
	s2 := makeOrganizeSession("s2", "Second", "historical_exit", "unknown")
	fake := &fakeApplicationService{Sessions: []sessionView{s1, s2}}
	m := newHarborModel(fake)
	m.sessions = []sessionView{s1, s2}
	m.width, m.height = 120, 34

	// Simulate an in-flight 2s auto-refresh from BEFORE Pin.
	m.refreshing = true
	m.listGeneration = 5
	stalePrePin := []sessionView{s1, s2}

	// Press 'P' to Pin s1 (run serviceTask without running its queued load() yet).
	n, cmd := m.Update(uiKey("P"))
	m = n.(harborModel)
	if cmd == nil {
		t.Fatal("expected serviceTask cmd for Pin")
	}
	doneMsg := cmd() // executes SetPinned + GetContext
	n, _ = m.Update(doneMsg)
	m = n.(harborModel)

	if m.listGeneration <= 5 {
		t.Fatalf("Pin did not advance listGeneration while refresh was in flight: %d", m.listGeneration)
	}
	// Deliver the stale pre-Pin appLoaded (generation 5) -> MUST be ignored!
	n, _ = m.Update(appLoaded{generation: 5, sessions: stalePrePin})
	m = n.(harborModel)
	if !m.sessions[0].Pinned {
		t.Fatal("stale in-flight appLoaded reverted Pin")
	}

	// Same race test for Archive ('A').
	m.refreshing = true
	genBeforeArchive := m.listGeneration
	stalePreArchive := append([]sessionView(nil), m.sessions...)
	n, cmd = m.Update(uiKey("A"))
	m = n.(harborModel)
	doneMsg = cmd()
	n, _ = m.Update(doneMsg)
	m = n.(harborModel)
	if m.listGeneration <= genBeforeArchive {
		t.Fatalf("Archive did not advance listGeneration: %d <= %d", m.listGeneration, genBeforeArchive)
	}
	n, _ = m.Update(appLoaded{generation: genBeforeArchive, sessions: stalePreArchive})
	m = n.(harborModel)
	if !m.sessions[0].Archived {
		t.Fatal("stale in-flight appLoaded reverted Archive")
	}

	// Same race test for Unarchive ('u') in Archived view.
	m.archivedView = true
	m.cursor = 0
	m.refreshing = true
	genBeforeUnarchive := m.listGeneration
	stalePreUnarchive := append([]sessionView(nil), m.sessions...)
	n, cmd = m.Update(uiKey("u"))
	m = n.(harborModel)
	doneMsg = cmd()
	n, _ = m.Update(doneMsg)
	m = n.(harborModel)
	if m.listGeneration <= genBeforeUnarchive {
		t.Fatalf("Unarchive did not advance listGeneration: %d <= %d", m.listGeneration, genBeforeUnarchive)
	}
	n, _ = m.Update(appLoaded{generation: genBeforeUnarchive, sessions: stalePreUnarchive})
	m = n.(harborModel)
	if m.sessions[0].Archived {
		t.Fatal("stale in-flight appLoaded reverted Unarchive")
	}
}

func TestIdentityCandidateSkipsDiskWriteWhenUnchanged(t *testing.T) {
	root := t.TempDir()
	bridgeDir := filepath.Join(root, "bridge")
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", bridgeDir)
	t.Setenv("HERDR_PLUGIN_ID", "agent-harbor.bridge")
	t.Setenv("HERDR_PLUGIN_ENTRYPOINT_ID", "codex")

	sockBase := "/private/tmp"
	if _, err := os.Stat(sockBase); err != nil {
		sockBase = ""
	}
	sockDir, err := os.MkdirTemp(sockBase, "hs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "h.sock")
	// Create a fake unix socket file so socketIdentity returns a stable dev:ino.
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("HERDR_SOCKET_PATH", sockPath)

	convID := "12345678-1234-7123-8123-1234567890ab"
	fakeHerdr := filepath.Join(root, "fake-herdr")
	script := fmt.Sprintf("#!/bin/sh\ncat <<'EOF'\n{\"pane\":{\"agent_session\":{\"source\":\"herdr:codex\",\"agent\":\"codex\",\"kind\":\"id\",\"value\":\"%s\"}}}\nEOF\n", convID)
	if err := os.WriteFile(fakeHerdr, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	id := "dddddddddddddddddddddddddddddd04"
	if err := withSessions(func([]session) ([]session, error) {
		return []session{{
			ID:                  id,
			Version:             3,
			Name:                "Steady",
			WorkingDirectory:    root,
			CLI:                 "codex",
			Mode:                "local",
			Recovery:            "ready",
			Started:             true,
			RunToken:            "tok-steady",
			HerdrPane:           "p1",
			HerdrSocket:         sockPath,
			HerdrSocketIdentity: socketIdentity(sockPath),
		}}, nil
	}); err != nil {
		t.Fatal(err)
	}

	// Start identity capture when initial report is empty (so first 1s tick captures convID,
	// and subsequent 1s ticks observe the exact same convID and must NOT rewrite sessions.json).
	stateFile := filepath.Join(root, "herdr-calls")
	scriptWithFirstEmpty := fmt.Sprintf(`#!/bin/sh
if [ ! -f %q ]; then
  touch %q
  echo '{"pane":{}}'
  exit 0
fi
cat <<'EOF'
{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"%s"}}}
EOF
`, stateFile, stateFile, convID)
	if err := os.WriteFile(fakeHerdr, []byte(scriptWithFirstEmpty), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_PANE_ID", "p1")
	activate, finish := candidateIdentityCapture(id, "tok-steady", "codex", nil)
	activate()
	defer func() { _ = finish() }()

	// Wait for the first 1s tick to observe convID and persist it.
	sessionsPath := filepath.Join(bridgeDir, "sessions.json")
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		cur, err := getSession(id)
		if err == nil && cur.Conversation != nil && cur.Conversation.Reference.Value == convID && cur.Conversation.Status == "observed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	info1, err := os.Stat(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}

	// Wait across >1.5s (at least one more 1s ticker sample) -> sessions.json must NOT be rewritten!
	time.Sleep(1600 * time.Millisecond)
	info2, err := os.Stat(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info1, info2) || !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("identity observer rewrote unchanged sessions.json (mtime %v -> %v)", info1.ModTime(), info2.ModTime())
	}
}
