package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func activeDuplicateConversation(all []session, self session, ref herdrIdentity) bool {
	for _, other := range all {
		if other.ID == self.ID || other.CLI != self.CLI {
			continue
		}
		if self.Profile != nil && other.Profile != nil && *self.Profile != *other.Profile {
			continue
		}
		if !processGroupActive(other.ProcessGroup) && !sessionLaunchPending(other) {
			continue
		}
		if other.Conversation != nil && other.Conversation.Reference == ref {
			return true
		}
		if self.CLI == "codex" && ref.Kind == "id" && other.CodexSessionID != "" && other.CodexSessionID == ref.Value {
			return true
		}
	}
	return false
}

// Installation and hook enablement are not identity evidence. Only a fresh
// pane report in this run can establish the Conversation reference. When
// resuming a previously verified Conversation, retain that verified reference
// if the client does not emit a new hook report and no conflict or transport
// failure occurs. Within a verified foreground run, a clean in-window switch
// to another valid Conversation automatically updates the recovery target while
// preserving transition history, unless another active session owns it.
func candidateIdentityCapture(id, token, client string, startup *conversationEvidence) (func(), func() error) {
	herdr, pane, socket := os.Getenv("HERDR_BIN_PATH"), os.Getenv("HERDR_PANE_ID"), os.Getenv("HERDR_SOCKET_PATH")
	socketID := socketIdentity(socket)
	warning := "Conversation identity not reported; inspect this Profile's client integration and hook trust."

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
				before, _ := json.Marshal(*s)
				commit := func() ([]session, error) {
					after, _ := json.Marshal(*s)
					if bytes.Equal(before, after) {
						return nil, nil
					}
					return all, nil
				}
				if s.Conversation != nil && s.Conversation.PrimarySelection != nil && s.Conversation.PrimarySelection.RunToken == token {
					if !observeAfterPrimarySelection(s, ref) {
						return nil, nil
					}
					return commit()
				}
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
						s.IdentityWarning = ""
						return commit()
					}
					// Codex 0.169+ does not re-emit SessionStart when launched via
					// resume. Preserve a verified startup reference while the pane
					// stays quiet and healthy; transport loss or conflicts still gate.
					if startup != nil && !blocked && !finalReadUnresolved && (state == "pending" || state == "missing") && s.Conversation != nil && s.Conversation.Reference == startup.Reference && s.ReportedCodexID == "" {
						s.Conversation.Status = startup.Status
						s.Recovery = "ready"
						s.IdentityWarning = ""
						return commit()
					}
					if finalReadUnresolved && state == "missing" {
						state = "final_observation_unavailable"
					}
					s.IdentityWarning += " Observation: " + state + "; review recovery."
					s.Recovery = "needs_review"
					if s.Conversation != nil {
						s.Conversation.Status = state
					}
					return commit()
				}
				expected := s.Conversation
				sameObservation := expected != nil && expected.Status == "observed" && expected.Reference == *ref && expected.RunToken == token && expected.Pane == pane && expected.SocketIdentity == socketID
				differs := (expected != nil && expected.Reference != *ref) || (s.CodexSessionID != "" && (ref.Kind != "id" || ref.Value != s.CodexSessionID))
				duplicateActive := activeDuplicateConversation(all, *s, *ref)
				validFollowRef := s.CLI != "codex" || (ref.Kind == "id" && validCodexID(ref.Value))
				legacyConflict := (expected != nil && expected.Status == "conflict") || s.ReportedCodexID != ""
				canFollow := differs && !baselineUnknown && !blocked && !legacyConflict && validFollowRef && !duplicateActive
				status := "observed"
				if baselineUnknown || blocked {
					status = "initial_unverified"
				} else if differs && !canFollow {
					status = "conflict"
					blocked = true
				}
				now := time.Now().UTC()
				if expected == nil {
					s.Version = 3
					s.Conversation = &conversationEvidence{Reference: *ref, Provenance: "herdr-pane-observation", ObservedAt: now, Pane: pane, SocketIdentity: socketID, RunToken: token, Status: status}
				} else if canFollow {
					prev := expected.Reference
					if prev.Value == "" && s.CodexSessionID != "" {
						prev = herdrIdentity{Source: "herdr:codex", Agent: s.CLI, Kind: "id", Value: s.CodexSessionID}
					}
					s.Version = 3
					s.Conversation.History = appendConversationHistory(s.Conversation.History, conversationTransition{
						From:     prev,
						To:       *ref,
						At:       now,
						Source:   "herdr-hook",
						RunToken: token,
					})
					s.Conversation.Reference = *ref
					s.Conversation.Provenance = "herdr-pane-observation"
					s.Conversation.Status = "observed"
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
						s.Conversation.ObservedAt = now
					}
					s.Conversation.RunToken = token
					s.Conversation.Pane = pane
					s.Conversation.SocketIdentity = socketID
					if s.CLI == "codex" && ref.Kind == "id" {
						s.CodexSessionID = ref.Value
					}
					s.ConversationIDSource = "herdr-hook"
					s.Recovery = "ready"
					s.IdentityWarning = ""
					if canFollow {
						s.ReportedCodexID = ""
						s.CandidateIDs = nil
						s.Candidates = nil
						s.LastActivityAt = &now
						if startup != nil {
							copy := *s.Conversation
							startup = &copy
						}
					}
				} else {
					s.Recovery = "needs_review"
					if duplicateActive {
						s.IdentityWarning = "Conversation is already active in another session; inspect details and review recovery."
					} else {
						s.IdentityWarning = "Pane identity is initial/unverified or changed; inspect show and use reassociate with the saved reference to explicitly review the original conversation and connection."
					}
					if s.CLI == "codex" && ref.Kind == "id" && ref.Value != s.CodexSessionID {
						s.ReportedCodexID = ref.Value
					}
				}
				return commit()
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
			// An exhausted periodic read can race with client exit. Only a
			// fresh identity report can resolve it; an empty final read cannot.
			finalReadUnresolved = true
			state = "observation_unavailable"
		}
		if err := update(ref, state); err != nil {
			captureErr = err
		}
		if !finishing && err == nil {
			captureAutomaticSessionName(id, token)
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
