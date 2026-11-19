package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPrimarySelectionDurableExactRecovery(t *testing.T) {
	const (
		first  = "11111111-1111-7111-8111-111111111111"
		second = "22222222-2222-7222-8222-222222222222"
		third  = "33333333-3333-7333-8333-333333333333"
		other  = "44444444-4444-7444-8444-444444444444"
		id     = "00000000000000000000000000000000"
	)
	for _, reopened := range []bool{false, true} {
		name := "fresh observation"
		if reopened {
			name = "reviewed startup with pending hook"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
			socket := filepath.Join(root, "herdr.sock")
			profile := profileBinding{WorkspaceID: "workspace", ProfileID: "profile", EnvironmentID: "environment", Client: "codex"}
			facts := processFacts{PID: 100, Variables: map[string]string{
				"AH_PROFILE_WORKSPACE": profile.WorkspaceID, "AH_PROFILE_ID": profile.ProfileID,
				"AH_PROFILE_ENVIRONMENT": profile.EnvironmentID, "CODEX_HOME": root,
				"AH_SESSION_RUN_TOKEN": "run", "HERDR_PANE_ID": "w1:p1", "HERDR_SOCKET_PATH": socket,
			}}
			ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: first}
			evidence := conversationEvidence{Reference: ref, ObservedReference: &ref, ObservedAt: time.Now().UTC(), Provenance: "herdr-pane-observation", Status: "observed", RunToken: "run", Pane: "w1:p1", SocketIdentity: "socket-id"}
			s := session{ID: id, Version: 3, CLI: "codex", Profile: &profile, Mode: "magpie", InstanceID: "original-instance", MagpieModel: "original-model", CodexHome: root,
				ProcessGroup: facts.PID, Started: true, RunToken: "run", HerdrPane: "w1:p1", HerdrSocket: socket, HerdrSocketIdentity: "socket-id",
				CodexSessionID: first, Conversation: &evidence, Recovery: "ready"}
			var startup *conversationEvidence
			if reopened {
				evidence.RunToken, evidence.Pane, evidence.SocketIdentity = "old-run", "w1:p2", "old-socket"
				original := evidence
				startup = &original
				evidence.Status, s.Recovery = "missing", "needs_review"
			}
			if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
				t.Fatal(err)
			}
			save := func(report primarySelectionReport) error {
				return savePrimarySelection(id, socket, s.HerdrSocketIdentity, report, facts, startup)
			}
			read := func() session {
				t.Helper()
				got, err := getSession(id)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			switchToB := primarySelectionReport{Source: primarySelectionSource, Reason: "resume", Sequence: 1, PreviousID: first, SelectedID: second, RunToken: "run", Pane: s.HerdrPane, PID: facts.PID}
			for label, change := range map[string]func(*primarySelectionReport){
				"different run":     func(r *primarySelectionReport) { r.RunToken = "old-run" },
				"different pane":    func(r *primarySelectionReport) { r.Pane = "w1:p4" },
				"different pid":     func(r *primarySelectionReport) { r.PID++ },
				"unverified source": func(r *primarySelectionReport) { r.Source = "herdr:codex" },
				"startup":           func(r *primarySelectionReport) { r.Reason = "startup" },
				"gap":               func(r *primarySelectionReport) { r.Sequence++ },
				"wrong previous":    func(r *primarySelectionReport) { r.PreviousID = third },
			} {
				invalid := switchToB
				change(&invalid)
				if err := save(invalid); err == nil || read().CodexSessionID != first {
					t.Fatalf("%s changed the original target: %v", label, err)
				}
			}
			facts.Variables["AH_PROFILE_ID"] = "wrong-profile"
			if err := save(switchToB); err == nil {
				t.Fatal("different Profile accepted")
			}
			facts.Variables["AH_PROFILE_ID"] = profile.ProfileID
			if err := save(switchToB); err != nil {
				t.Fatal(err)
			}
			accepted := read()
			if err := save(switchToB); err != nil || !sameSession(accepted, read()) {
				t.Fatal("exact duplicate is not idempotent", err)
			}
			if accepted.CodexSessionID != second || accepted.Recovery != "ready" || accepted.Conversation.PrimarySelection.Original.Reference != ref || queryReferenceCheck(accepted) != nil {
				t.Fatal("B was not durably recoverable", accepted)
			}
			if observeAfterPrimarySelection(&accepted, nil) || observeAfterPrimarySelection(&accepted, &ref) || observeAfterPrimarySelection(&accepted, &accepted.Conversation.Reference) {
				t.Fatal("passive stale/current/empty observation overrode accepted B")
			}
			switchToC := switchToB
			switchToC.Sequence, switchToC.PreviousID, switchToC.SelectedID = 2, second, third
			if err := save(switchToC); err != nil {
				t.Fatal(err)
			}
			if err := save(switchToB); err == nil {
				t.Fatal("out-of-order selection accepted")
			}
			accepted = read()
			if accepted.CodexSessionID != third || accepted.Conversation.PrimarySelection.Original.Reference != ref || !reflect.DeepEqual(accepted.Profile, &profile) || accepted.Mode != s.Mode || accepted.InstanceID != s.InstanceID || accepted.MagpieModel != s.MagpieModel {
				t.Fatal("sequence lost selection, original evidence or connection intent", accepted)
			}
			divergent := ref
			divergent.Value = other
			if !observeAfterPrimarySelection(&accepted, &divergent) || accepted.Recovery != "needs_review" || accepted.ReportedCodexID != other || accepted.CodexSessionID != third {
				t.Fatal("divergent later passive evidence did not retain the saved target for review", accepted)
			}

			// Start another verified recovery before its SessionStart fires.
			accepted = read()
			oldSelection := accepted.Conversation.PrimarySelection
			verifiedStartup := *accepted.Conversation
			accepted.RunToken, accepted.Recovery = "new-run", "needs_review"
			accepted.Conversation.Status = "missing"
			facts.Variables["AH_SESSION_RUN_TOKEN"] = accepted.RunToken
			if err := withSessions(func([]session) ([]session, error) { return []session{accepted}, nil }); err != nil {
				t.Fatal(err)
			}
			if err := save(switchToC); err == nil {
				t.Fatal("previous run could still write a selection")
			}
			switchToD := switchToC
			switchToD.RunToken, switchToD.Sequence, switchToD.PreviousID, switchToD.SelectedID = accepted.RunToken, 1, third, other
			if err := savePrimarySelection(id, socket, s.HerdrSocketIdentity, switchToD, facts, &verifiedStartup); err != nil {
				t.Fatal(err)
			}
			accepted = read()
			latest := accepted.Conversation.PrimarySelection
			if latest.Original.Reference.Value != third || !reflect.DeepEqual(latest.PreviousRun, oldSelection) || latest.PreviousRun.Original.Reference != ref {
				t.Fatal("new run dropped earlier selection evidence", latest)
			}
			if oldReport := verifiedStartup.Reference; observeAfterPrimarySelection(&accepted, &oldReport) || accepted.Recovery != "ready" {
				t.Fatal("same-run prior reference interfered with the selection")
			}
			if !observeAfterPrimarySelection(&accepted, &ref) || accepted.Recovery != "needs_review" || accepted.ReportedCodexID != first || accepted.CodexSessionID != other {
				t.Fatal("reference from an unrelated prior run was suppressed", accepted)
			}
		})
	}
}
