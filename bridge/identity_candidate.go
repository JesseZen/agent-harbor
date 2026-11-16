package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func candidateRunPlan(s session, env []string) (runPlan, error) {
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return runPlan{}, err
	}
	home := environmentValue(env, "CODEX_HOME")
	if home == "" {
		home = filepath.Join(environmentValue(env, "HOME"), ".codex")
	}
	home, err := filepath.Abs(home)
	if s.CLI != "codex" {
		home = ""
	}
	return runPlan{Profile: s.Profile, SessionID: s.ID, Token: hex.EncodeToString(token), Home: home}, err
}

// Installation status is diagnostic only: it cannot prove feature enablement,
// trust approval or operation in the client's effective configuration context.
func candidateIdentityCapture(id, token, client string) (func(), func() error) {
	herdr, pane, socket := os.Getenv("HERDR_BIN_PATH"), os.Getenv("HERDR_PANE_ID"), os.Getenv("HERDR_SOCKET_PATH")
	socketID := socketIdentity(socket)
	warning := "Official identity integration is unproven; inspect Herdr integration status in this CLI configuration, enable/trust its hooks, then explicitly review recovery."
	if herdr != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		out, err := exec.CommandContext(ctx, herdr, "integration", "status").Output()
		cancel()
		if err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID"))+": current") {
			warning = "Official integration installed; hook enablement and trust remain unproven until a fresh pane identity report."
		}
	}
	validContext := herdr != "" && pane != "" && socketID != "" && os.Getenv("HERDR_PLUGIN_ID") == "agent-harbor.bridge" && (os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == client || os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "session")
	if !validContext {
		warning = "Official identity observation unavailable: missing managed pane/socket context; reopen in managed Herdr and review recovery."
	}
	var initial *herdrIdentity
	var initialErr error
	if validContext {
		initial, initialErr = paneIdentity(context.Background(), herdr, pane)
	}
	// Initial nonempty identity may be stale or merely agent.start launch arguments.
	// Keep it visible but do not convert it into verified observation.
	baselineUnknown := initialErr != nil || initial != nil
	blocked := initial != nil
	observedThisRun, finishing := false, false
	finalReadUnresolved := false
	update := func(ref *herdrIdentity, state string) error {
		return withSessions(func(all []session) ([]session, error) {
			for i := range all {
				s := &all[i]
				if s.ID != id {
					continue
				}
				if s.RunToken != token {
					return nil, nil
				} // a newer run or explicit choice owns the record
				s.IdentityWarning = warning
				currentSocket := socketIdentity(socket)
				recordChanged := s.ProcessGroup != 0 && (s.HerdrPane != pane || s.HerdrSocket != socket)
				if !validContext || currentSocket != socketID || recordChanged {
					state = "association_lost"
					// Missing transport invalidates observation until a fresh same-run
					// report. A different instance or association requires explicit review.
					finalReadUnresolved = true
					if !validContext || currentSocket != "" && currentSocket != socketID || recordChanged {
						blocked = true
					}
					ref = nil
				}
				if ref != nil && (ref.Source == "" || ref.Agent != s.CLI || ref.Kind == "" || ref.Value == "") {
					state = "invalid_report"
					ref = nil
				}
				if ref == nil {
					// Herdr clears agent_session when a client exits. A final
					// empty report does not erase a fresh same-run acceptance;
					// socket loss, read failures and conflicts still gate recovery.
					if finishing && observedThisRun && !blocked && !finalReadUnresolved && state == "missing" && s.Conversation != nil {
						s.Conversation.Status = "observed"
						s.Recovery = "ready"
						s.IdentityWarning = "Client exited; retained the fresh official identity observed during this run. Final pane report is empty; connection, login and writer review remain required."
						return all, nil
					}
					if finalReadUnresolved && state == "missing" {
						state = "final_observation_unavailable"
					}
					s.IdentityWarning += " Observation: " + state + "; review recovery."
					s.Recovery = "needs_review"
					if s.Conversation != nil {
						s.Conversation.Status = state
					}
					return all, nil
				}
				expected := s.Conversation
				sameObservation := expected != nil && expected.Status == "observed" && expected.Reference == *ref && expected.RunToken == token && expected.Pane == pane && expected.SocketIdentity == socketID
				status := "observed"
				if baselineUnknown || blocked {
					status = "initial_unverified"
				}
				if expected != nil && expected.Reference != *ref {
					status = "conflict"
					blocked = true
				}
				if s.CodexSessionID != "" && (ref.Kind != "id" || ref.Value != s.CodexSessionID) {
					status = "conflict"
					blocked = true
				}
				if expected == nil {
					s.Version = 3
					s.Conversation = &conversationEvidence{Reference: *ref, Provenance: "herdr-pane-observation", ObservedAt: time.Now().UTC(), Pane: pane, SocketIdentity: socketID, RunToken: token, Status: status}
				} else {
					s.Conversation.Status = status
				}
				s.Conversation.ObservedReference = ref
				if status == "observed" {
					observedThisRun = true
					finalReadUnresolved = false
					// Repeated reports of the same accepted run/context are not new
					// evidence. Preserve approvals until meaningful evidence changes.
					if !sameObservation {
						s.Conversation.ObservedAt = time.Now().UTC()
					}
					s.Conversation.RunToken = token
					s.Conversation.Pane = pane
					s.Conversation.SocketIdentity = socketID
					if s.CLI == "codex" && ref.Kind == "id" {
						s.CodexSessionID = ref.Value
					}
					s.ConversationIDSource = "herdr-hook"
					s.Recovery = "ready"
					s.IdentityWarning = "Official pane identity observed; recovery still requires connection, login and writer review."
				} else {
					s.Recovery = "needs_review"
					s.IdentityWarning = "Pane identity is initial/unverified or changed; inspect show and use reassociate with the saved reference to explicitly review the original conversation and connection."
					if s.CLI == "codex" && ref.Kind == "id" && ref.Value != s.CodexSessionID {
						s.ReportedCodexID = ref.Value
					}
				}
				return all, nil
			}
			return nil, nil
		})
	}
	var captureErr error
	active := false
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	sample := func() {
		var ref *herdrIdentity
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			ref, err = paneIdentity(context.Background(), herdr, pane)
			if err == nil {
				break
			}
		}
		if err == nil && ref == nil && !blocked {
			baselineUnknown = false
		}
		if errors.Is(err, errPaneAssociation) {
			blocked = true
		}
		state := "missing"
		if err != nil {
			if finishing {
				finalReadUnresolved = true
			}
			state = "observation_unavailable"
		}
		if err := update(ref, state); err != nil {
			captureErr = err
		}
	}
	activate := func() {
		active = true
		captureErr = update(initial, "pending")
		if !validContext {
			close(done)
			return
		}
		go func() {
			defer close(done)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sample()
				}
			}
		}()
	}
	return activate, func() error {
		if !active {
			cancel()
			return nil
		}
		if !validContext {
			cancel()
			return captureErr
		}
		cancel()
		<-done
		finishing = true
		// A bounded final window catches short sessions and slightly late hook reports.
		for attempt := 0; attempt < 3; attempt++ {
			sample()
			if attempt < 2 {
				time.Sleep(150 * time.Millisecond)
			}
		}
		return captureErr
	}
}
