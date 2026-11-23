package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCandidateShortSessionUsesOfficialFinalReport(t *testing.T) {
	b := newTestBridge(t)
	// Initialize ownership before queryTestSocket creates the managed directory.
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(h, []byte(`#!/bin/sh
if test "$1 $2" = "integration status"; then echo "codex: current (v10)"; exit 0; fi
case "$*" in
 "integration status") echo "Codex: current (test)";;
 *) if test -f "$CAPTURE"; then
 if ! test -f "$HOME/retried"; then touch "$HOME/retried"; exit 1; fi
 echo '{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}'
 else echo '{"result":{"pane":{}}}'; fi;;
esac
`), 0700)
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"), 0700)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+h)
	out, err := b.command("create", "--dir", b.project)
	if err != nil {
		t.Fatal(out, err)
	}
	got := b.sessions()[0]
	if got.Conversation == nil || got.Conversation.Reference.Value != "01912345-6789-7abc-8def-0123456789ab" || got.Recovery != "ready" {
		t.Fatalf("final upstream reference not recoverable: %+v", got)
	}
	args, _ := os.ReadFile(filepath.Join(b.root, "capture"))
	if strings.Contains(string(args), "notify=") {
		t.Fatal("candidate injected notify")
	}
	if _, err := os.Stat(filepath.Join(b.root, "retried")); err != nil {
		t.Fatal("transient failure fixture not exercised", err)
	}
}

func TestCandidateInitialIdentityNeedsExplicitChoice(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(h, []byte(`#!/bin/sh
if test "$1 $2" = "integration status"; then echo "codex: current (v10)"; exit 0; fi
echo '{"result":{"pane":{"agent_session":{"source":"official:opaque-source/v2","agent":"codex","kind":"path","value":"/opaque/conversation with spaces"}}}}'
`), 0700)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+h)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	got := b.sessions()[0]
	if got.Conversation == nil || got.Conversation.Reference.Source != "official:opaque-source/v2" || got.Conversation.Reference.Kind != "path" || got.Conversation.Reference.Value != "/opaque/conversation with spaces" || got.Conversation.Status != "initial_unverified" || got.Recovery == "ready" {
		t.Fatalf("initial report silently trusted or lost: %+v", got)
	}
}
func TestCandidateMissingContextIsVisibleWithoutNotify(t *testing.T) {
	b := newTestBridge(t)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1")
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	got := b.sessions()[0]
	if got.Recovery == "ready" || !strings.Contains(got.IdentityWarning, "missing managed pane/socket context") {
		t.Fatalf("missing context hidden: %+v", got)
	}
	args, _ := os.ReadFile(filepath.Join(b.root, "capture"))
	if strings.Contains(string(args), "notify=") {
		t.Fatal("missing context fell back to notify")
	}
}

func TestCandidateChangedIdentityAndLostSocketRequireReview(t *testing.T) {
	for _, scenario := range []string{"lost", "reconnect", "replacement", "reconnect_empty"} {
		t.Run(scenario, func(t *testing.T) {
			b := newTestBridge(t)
			if out, err := b.command("sessions"); err != nil {
				t.Fatal(out, err)
			}
			h := filepath.Join(b.root, "bin", "herdr")
			report := filepath.Join(b.root, "report")
			os.WriteFile(h, []byte("#!/bin/sh\nif test \"$1 $2\" = \"integration status\"; then echo \"codex: current (v10)\"; exit 0; fi\nif test -f \"$HOME/report\"; then cat \"$HOME/report\"; else echo '{\"result\":{\"pane\":{}}}'; fi\n"), 0700)
			writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf running > \"$CAPTURE\"\nread reply || true\n"), 0700)
			socket := queryTestSocket(t, b)
			b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+socket, "HERDR_BIN_PATH="+h)
			cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
			cmd.Env = b.env
			in, _ := cmd.StdinPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { in.Close(); cmd.Wait() }()
			wait := func(predicate func(session) bool) session {
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					all := b.sessions()
					if len(all) == 1 && predicate(all[0]) {
						return all[0]
					}
					time.Sleep(30 * time.Millisecond)
				}
				t.Fatal("observation transition not visible")
				return session{}
			}
			wait(func(s session) bool { return s.ProcessGroup != 0 })
			first := `{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}`
			os.WriteFile(report, []byte(first), 0600)
			accepted := wait(func(s session) bool { return s.Conversation != nil && s.Conversation.Status == "observed" })
			if scenario == "lost" {
				os.Remove(socket)
			} else {
				if err := os.Rename(socket, socket+".original"); err != nil {
					t.Fatal(err)
				}
			}
			got := wait(func(s session) bool { return s.Recovery == "needs_review" })
			if got.Conversation.Reference != accepted.Conversation.Reference {
				t.Fatal("expected identity overwritten")
			}

			if scenario == "reconnect" {
				if err := os.Rename(socket+".original", socket); err != nil {
					t.Fatal(err)
				}
				got = wait(func(s session) bool { return s.Conversation.Status == "observed" && s.Recovery == "ready" })
				if got.Conversation.Reference != accepted.Conversation.Reference || got.RunToken != accepted.RunToken {
					t.Fatal("reconnect changed original intent")
				}
			}

			if scenario == "reconnect_empty" {
				os.WriteFile(report, []byte(`{"result":{"pane":{}}}`), 0600)
				if err := os.Rename(socket+".original", socket); err != nil {
					t.Fatal(err)
				}
				in.Close()
				_ = cmd.Wait() // EOF may be a nonzero client exit; identity must still stay gated.
				got = b.sessions()[0]
				if got.Recovery != "needs_review" || got.Conversation.Status == "observed" {
					t.Fatal("empty final report reused identity across outage", got)
				}
			}
			if scenario == "replacement" {
				replacement, err := net.Listen("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				defer replacement.Close()
				in.Close()
				_ = cmd.Wait() // final samples inspect the different nonempty socket instance
				got = b.sessions()[0]
				if got.Conversation.Status == "observed" || got.Recovery != "needs_review" || got.Conversation.Reference != accepted.Conversation.Reference {
					t.Fatal("replacement accepted old identity")
				}
			}
		})
	}
}

func TestCandidateVerifiedInWindowSwitchFollowsAndRecordsHistory(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	report := filepath.Join(b.root, "report")
	os.WriteFile(h, []byte("#!/bin/sh\nif test \"$1 $2\" = \"integration status\"; then echo \"codex: current (v10)\"; exit 0; fi\nif test -f \"$HOME/report\"; then cat \"$HOME/report\"; else echo '{\"result\":{\"pane\":{}}}'; fi\n"), 0700)
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf running > \"$CAPTURE\"\nread reply || true\n"), 0700)
	socket := queryTestSocket(t, b)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+socket, "HERDR_BIN_PATH="+h)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
	cmd.Env = b.env
	in, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); cmd.Wait() }()
	wait := func(predicate func(session) bool) session {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			all := b.sessions()
			if len(all) == 1 && predicate(all[0]) {
				return all[0]
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatal("expected state not reached")
		return session{}
	}
	wait(func(s session) bool { return s.ProcessGroup != 0 })
	const (
		idA = "01912345-6789-7abc-8def-0123456789aa"
		idB = "01912345-6789-7abc-8def-0123456789bb"
		idC = "01912345-6789-7abc-8def-0123456789cc"
	)
	writeReport := func(id string) {
		os.WriteFile(report, []byte(`{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"`+id+`"}}}}`), 0600)
	}
	writeReport(idA)
	wait(func(s session) bool { return s.CodexSessionID == idA && s.Recovery == "ready" })

	writeReport(idB)
	gotB := wait(func(s session) bool { return s.CodexSessionID == idB && s.Recovery == "ready" })
	if gotB.Conversation == nil || gotB.Conversation.Status != "observed" || gotB.IdentityWarning != "" || gotB.ReportedCodexID != "" {
		t.Fatalf("switch A->B did not stay clean ready: %+v", gotB)
	}
	if len(gotB.Conversation.History) != 1 || gotB.Conversation.History[0].From.Value != idA || gotB.Conversation.History[0].To.Value != idB {
		t.Fatalf("switch A->B history missing: %+v", gotB.Conversation.History)
	}

	writeReport(idC)
	gotC := wait(func(s session) bool { return s.CodexSessionID == idC && s.Recovery == "ready" })
	if len(gotC.Conversation.History) != 2 || gotC.Conversation.History[1].From.Value != idB || gotC.Conversation.History[1].To.Value != idC {
		t.Fatalf("switch B->C history missing: %+v", gotC.Conversation.History)
	}

	// Simulate Herdr clearing agent_session on client exit.
	os.WriteFile(report, []byte(`{"result":{"pane":{}}}`), 0600)
	in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("client exit failed: %v", err)
	}
	final := b.sessions()[0]
	if final.CodexSessionID != idC || final.Recovery != "ready" || final.Conversation == nil || final.Conversation.Status != "observed" || final.IdentityWarning != "" {
		t.Fatalf("final exited state lost latest conversation C: %+v", final)
	}
}

func TestCandidateDuplicateActiveConversationMarksConflict(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	report := filepath.Join(b.root, "report")
	os.WriteFile(h, []byte("#!/bin/sh\nif test \"$1 $2\" = \"integration status\"; then echo \"codex: current (v10)\"; exit 0; fi\nif test -f \"$HOME/report\"; then cat \"$HOME/report\"; else echo '{\"result\":{\"pane\":{}}}'; fi\n"), 0700)
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf running > \"$CAPTURE\"\nread reply || true\n"), 0700)
	socket := queryTestSocket(t, b)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+socket, "HERDR_BIN_PATH="+h)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
	cmd.Env = b.env
	in, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); cmd.Wait() }()
	const (
		idA = "01912345-6789-7abc-8def-0123456789aa"
		idB = "01912345-6789-7abc-8def-0123456789bb"
	)
	waitOne := func(predicate func(session) bool) session {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			for _, s := range b.sessions() {
				if s.ID != "22222222222222222222222222222222" && predicate(s) {
					return s
				}
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatal("expected state not reached")
		return session{}
	}
	waitOne(func(s session) bool { return s.ProcessGroup != 0 })
	os.WriteFile(report, []byte(`{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"`+idA+`"}}}}`), 0600)
	first := waitOne(func(s session) bool { return s.CodexSessionID == idA && s.Recovery == "ready" })

	// Seed a second active session in the same Profile owning idB (using our own live PID).
	other := first
	other.ID = "22222222222222222222222222222222"
	other.Name = "Other active"
	other.ProcessGroup = syscall.Getpgrp()
	other.CodexSessionID = idB
	other.Conversation = &conversationEvidence{
		Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: idB},
		Status:    "observed",
	}
	data, _ := json.Marshal([]session{first, other})
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	// Now session 1 reports idB while session 2 is actively running idB -> must conflict.
	os.WriteFile(report, []byte(`{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"`+idB+`"}}}}`), 0600)
	conflict := waitOne(func(s session) bool { return s.Recovery == "needs_review" })
	if conflict.CodexSessionID != idA || conflict.Conversation.Status != "conflict" || conflict.ReportedCodexID != idB || !strings.Contains(conflict.IdentityWarning, "already active") {
		t.Fatalf("duplicate active writer was not flagged as conflict: %+v", conflict)
	}
}

func TestCandidateStopWaitsForFinalExitBookkeeping(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(h, []byte(`#!/bin/sh
if test "$1 $2" = "integration status"; then echo "codex: current (v10)"; exit 0; fi
case "$*" in
 *"tab list"*) cat "$HOME/tab-info.json";;
 *"pane process-info"*) echo '{"result":{"process_info":{"pane_id":"opaque-pane","shell_pid":321,"foreground_processes":[{"pid":321,"name":"sh"}]}}}';;

 *"session list --json"*) echo '{"sessions":[{"name":"agent-harbor","running":true}]}';;
 *"pane close"*)
   if grep -q '"process_group":' "$AGENT_HARBOR_BRIDGE_HOME/sessions.json"; then exit 1; fi
   touch "$HOME/closed";;
 *"pane get"*) sleep .2; echo '{"result":{"pane":{"pane_id":"opaque-pane","tab_id":"opaque-tab","terminal_id":"fixture-terminal"}}}';;
 *) echo '{}';;
esac
`), 0700)
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf running > \"$CAPTURE\"\nread reply || true\n"), 0700)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_TAB_ID=opaque-tab", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+h, "AGENT_HARBOR_HERDR_BIN="+h)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
	cmd.Env = b.env
	in, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); cmd.Wait() }()
	deadline := time.Now().Add(8 * time.Second)
	var record session
	for time.Now().Before(deadline) {
		all := b.sessions()
		if len(all) == 1 && all[0].ProcessGroup != 0 {
			record = all[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if record.ID == "" {
		t.Fatal("runner never started")
	}
	tabInfo, _ := json.Marshal(map[string]any{"result": map[string]any{"tabs": []map[string]any{{"tab_id": "opaque-tab", "label": record.Name}}}})
	os.WriteFile(filepath.Join(b.root, "tab-info.json"), tabInfo, 0600)
	if out, err := b.command("stop-session", record.ID); err != nil {
		t.Fatal(out, err)
	}
	got := b.sessions()[0]
	if got.LastExitAt == nil || got.ProcessGroup != 0 {
		t.Fatal("stop returned before exit bookkeeping")
	}
	if _, err := os.Stat(filepath.Join(b.root, "closed")); err != nil {
		t.Fatal("owned pane was not closed", err)
	}
}

func TestCandidateFinalReadFailureRecovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures string
		fresh    bool
		ready    bool
	}{
		{"exhausted_then_empty", "3", false, false},
		{"active_exhausted_then_empty", "3", false, false},
		{"transient_within_budget", "2", false, true},
		{"exhausted_then_fresh_identity", "3", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBridge(t)
			if out, err := b.command("sessions"); err != nil {
				t.Fatal(out, err)
			}
			h := filepath.Join(b.root, "bin", "herdr")
			stub := `#!/bin/sh
if test "$1 $2" = "integration status"; then echo "codex: current (v10)"; exit 0; fi
if test -f "$HOME/final"; then
 n=0; test ! -f "$HOME/count" || n=$(cat "$HOME/count")
 n=$((n+1)); echo "$n" > "$HOME/count"
 if test "$n" -le FAILURE_COUNT; then exit 1; fi
 FINAL_RESPONSE; exit 0
fi
if test -f "$CAPTURE"; then
 echo '{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}'
else echo '{"result":{"pane":{}}}'; fi
`
			finalResponse := `echo '{"result":{"pane":{}}}' `
			if tc.fresh {
				finalResponse = `echo '{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}' `
			}
			os.WriteFile(h, []byte(strings.ReplaceAll(strings.ReplaceAll(stub, "FAILURE_COUNT", tc.failures), "FINAL_RESPONSE", finalResponse)), 0700)
			clientScript := "#!/bin/sh\nprintf running > \"$CAPTURE\"\nread reply\ntouch \"$HOME/final\"\n"
			if tc.name == "active_exhausted_then_empty" {
				// Exhaust one periodic observation while the client is still alive.
				// An empty final read must not turn that uncertainty back into ready.
				clientScript += "while ! test -f \"$HOME/count\" || test \"$(cat \"$HOME/count\")\" -lt 3; do sleep .05; done\n"
			}
			writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(clientScript), 0700)
			b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+h)
			cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
			cmd.Env = b.env
			in, _ := cmd.StdinPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(8 * time.Second)
			accepted := false
			for time.Now().Before(deadline) {
				all := b.sessions()
				if len(all) == 1 && all[0].Conversation != nil && all[0].Conversation.Status == "observed" {
					accepted = true
					break
				}
				time.Sleep(30 * time.Millisecond)
			}
			in.Close()
			cmd.Wait()
			if !accepted {
				t.Fatal("no acceptance")
			}
			got := b.sessions()[0]
			if (got.Recovery == "ready") != tc.ready {
				t.Fatalf("final read errors followed by empty report incorrectly retained ready: status=%s warning=%s", got.Conversation.Status, got.IdentityWarning)
			}
			if got.Conversation.Reference.Value != "01912345-6789-7abc-8def-0123456789ab" {
				t.Fatal("accepted reference lost")
			}
			if !tc.ready && (got.Conversation.Status != "final_observation_unavailable" || !strings.Contains(got.IdentityWarning, "final_observation_unavailable")) {
				t.Fatal("unresolved final failure hidden", got.Conversation.Status, got.IdentityWarning)
			}
		})
	}
}

func TestCandidateRepeatedOfficialObservationPreservesStopApproval(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(h, []byte(`#!/bin/sh
if test "$1 $2" = "integration status"; then echo "codex: current (v10)"; exit 0; fi
case "$*" in
 *"tab list"*) cat "$HOME/tab-info.json";;
 *"pane process-info"*) echo '{"result":{"process_info":{"pane_id":"opaque-pane","shell_pid":321,"foreground_processes":[{"pid":321,"name":"sh"}]}}}';;

 *"session list --json"*) echo '{"sessions":[{"name":"agent-harbor","running":true}]}';;
 *"pane close"*)
   if grep -q '"process_group":' "$AGENT_HARBOR_BRIDGE_HOME/sessions.json"; then exit 1; fi
   touch "$HOME/closed";;
 *"pane get"*) if test -f "$CAPTURE"; then echo '{"result":{"pane":{"pane_id":"opaque-pane","tab_id":"opaque-tab","terminal_id":"fixture-terminal","agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}'; else echo '{"result":{"pane":{"pane_id":"opaque-pane","tab_id":"opaque-tab","terminal_id":"fixture-terminal"}}}'; fi;;
 *) echo '{}';;
esac
`), 0700)
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf running > \"$CAPTURE\"\nread reply || true\n"), 0700)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_TAB_ID=opaque-tab", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+h, "AGENT_HARBOR_HERDR_BIN="+h)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
	cmd.Env = b.env
	in, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); cmd.Wait() }()
	deadline := time.Now().Add(8 * time.Second)
	var record session
	for time.Now().Before(deadline) {
		all := b.sessions()
		if len(all) == 1 && all[0].ProcessGroup != 0 {
			record = all[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if record.ID == "" {
		t.Fatal("runner never started")
	}
	tabInfo, _ := json.Marshal(map[string]any{"result": map[string]any{"tabs": []map[string]any{{"tab_id": "opaque-tab", "label": record.Name}}}})
	os.WriteFile(filepath.Join(b.root, "tab-info.json"), tabInfo, 0600)
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if v := b.sessions()[0]; v.Conversation != nil && v.Conversation.Status == "observed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v := b.sessions()[0]; v.Conversation == nil || v.Conversation.Status != "observed" {
		t.Fatal("official observation absent")
	}
	stop := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "stop-session", record.ID)
	stop.Env = b.env
	approval, _ := stop.StdinPipe()
	output, _ := stop.StdoutPipe()
	var diagnostics strings.Builder
	stop.Stderr = &diagnostics
	if err := stop.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(output).ReadString(':'); err != nil {
		t.Fatal(err)
	}
	// Hold an actual user approval across multiple identical upstream samples.
	time.Sleep(2200 * time.Millisecond)
	approval.Write([]byte("yes\n"))
	approval.Close()
	if err := stop.Wait(); err != nil {
		t.Fatal("stable official observation invalidated stop approval", diagnostics.String(), err)
	}
	got := b.sessions()[0]
	if got.LastExitAt == nil || got.ProcessGroup != 0 {
		t.Fatal("stop returned before exit bookkeeping")
	}
	if _, err := os.Stat(filepath.Join(b.root, "closed")); err != nil {
		t.Fatal("owned pane was not closed", err)
	}
}

func TestCandidateResumePreservesVerifiedIdentityWithoutHookReport(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("sessions"); err != nil {
		t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(h, []byte(`#!/bin/sh
if test "$1 $2" = "integration status"; then echo "codex: current (v10)"; exit 0; fi
case "$*" in
 *"agent resume-command"*) echo '{"version":1,"kind":"codex","argv":["codex","resume","01912345-6789-7abc-8def-0123456789ab"]}' ;;
 *) echo '{"result":{"pane":{}}}' ;;
esac
`), 0700)
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"), 0700)
	socket := queryTestSocket(t, b)
	b.env = append(b.env, "AGENT_HARBOR_IDENTITY_CANDIDATE=1", "AGENT_HARBOR_HERDR_BIN="+h, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=opaque-pane", "HERDR_SOCKET_PATH="+socket, "HERDR_BIN_PATH="+h)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	const refID = "01912345-6789-7abc-8def-0123456789ab"
	s.Version = 3
	s.CodexSessionID = refID
	s.Recovery = "ready"
	s.IdentityWarning = ""
	s.Conversation = &conversationEvidence{
		Reference:      herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: refID},
		Provenance:     "herdr-pane-observation",
		ObservedAt:     time.Now().UTC(),
		Pane:           "previous-pane",
		SocketIdentity: "previous-socket",
		RunToken:       "previous-run",
		Status:         "observed",
	}
	data, _ := json.Marshal([]session{s})
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", s.ID)
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader("accept\nyes\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("resume failed: %v\n%s", err, out)
	}
	got := b.sessions()[0]
	if got.Recovery != "ready" || got.Conversation == nil || got.Conversation.Status != "observed" || got.IdentityWarning != "" {
		t.Fatalf("quiet resume corrupted verified identity: recovery=%q status=%q warning=%q", got.Recovery, got.Conversation.Status, got.IdentityWarning)
	}
}
