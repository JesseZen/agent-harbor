package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const primarySelectionOptIn = "AGENT_HARBOR_EXPERIMENTAL_CODEX_PRIMARY_SELECTION"
const primarySelectionSocket = "CODEX_TUI_PRIMARY_SELECTION_SOCKET"

const primarySelectionSource = "herdr:codex-primary-selection-v1"

type primarySelectionReport struct {
	Source     string `json:"source"`
	Reason     string `json:"reason"`
	Sequence   uint64 `json:"sequence"`
	PreviousID string `json:"previous_id"`
	SelectedID string `json:"selected_id"`
	RunToken   string `json:"run_token"`
	Pane       string `json:"pane_id"`
	PID        int    `json:"pid"`
}

type primarySelectionEvidence struct {
	primarySelectionReport
	Original *conversationEvidence `json:"original"`
	// PriorReferences is scoped to this run. PreviousRun preserves the audit history.
	PriorReferences []string                  `json:"prior_references,omitempty"`
	PreviousRun     *primarySelectionEvidence `json:"previous_run,omitempty"`
}

func observePrimarySelection(s session, plan runPlan, kind launchKind, cmd *exec.Cmd) (func(), error) {
	stop := func() {}
	cmd.Env = replaceEnvironment(cmd.Env, map[string]*string{primarySelectionSocket: nil})
	if os.Getenv(primarySelectionOptIn) != "1" || s.CLI != "codex" || s.Profile == nil || s.RecoverySetupSkipped || os.Getenv("HERDR_PLUGIN_ID") != "agent-harbor.bridge" {
		return stop, nil
	}
	root, err := bridgeHome()
	if err != nil {
		return stop, err
	}
	pane, herdrSocket := os.Getenv("HERDR_PANE_ID"), os.Getenv("HERDR_SOCKET_PATH")
	if pane == "" || !managedSocket(root, herdrSocket) {
		return stop, errors.New("Codex selection observer requires the owned Herdr pane")
	}
	socketID := socketIdentity(herdrSocket)
	config, err := managedConfigContext(root)
	if err != nil {
		return stop, err
	}
	executable, err := filepath.EvalSymlinks(cmd.Path)
	if err != nil {
		return stop, err
	}
	herdr, err := filepath.EvalSymlinks(os.Getenv("HERDR_BIN_PATH"))
	if err != nil {
		return stop, err
	}
	address := filepath.Join(config, ".as-"+plan.Token[:24])
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		return stop, fmt.Errorf("start Codex selection observer: %w", err)
	}
	if err := os.Chmod(address, 0600); err != nil {
		listener.Close()
		return stop, err
	}
	cmd.Env = replaceEnvironment(cmd.Env, map[string]*string{primarySelectionSocket: &herdrSocket})
	var startup *conversationEvidence
	if kind == resumeConversation && s.Recovery == "ready" && s.ReportedCodexID == "" && s.Conversation != nil && s.Conversation.Reference.Value == s.CodexSessionID {
		startup = s.Conversation
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			report := primarySelectionReport{}
			decoder := json.NewDecoder(io.LimitReader(conn, 4096))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&report); err == nil && report.Pane == pane && report.RunToken == plan.Token && socketIdentity(herdrSocket) == socketID && selectionPeerMatches(conn, herdr) {
				facts, err := readProcessFacts(report.PID)
				if err == nil && facts.Executable == executable && savePrimarySelection(s.ID, herdrSocket, socketID, report, facts, startup) == nil {
					_, _ = io.WriteString(conn, "accepted\n")
				}
			}
			_ = conn.Close()
		}
	}()
	return func() {
		_ = listener.Close()
		<-done
	}, nil
}

func selectionPeerMatches(conn *net.UnixConn, executable string) bool {
	peer, err := conn.SyscallConn()
	if err != nil {
		return false
	}
	var pid int
	if err := peer.Control(func(fd uintptr) { pid = selectionPeerPID(int(fd)) }); err != nil || pid <= 1 {
		return false
	}
	facts, err := readProcessFacts(pid)
	return err == nil && facts.Executable == executable
}

func observeAfterPrimarySelection(s *session, ref *herdrIdentity) bool {
	if ref == nil || *ref == s.Conversation.Reference {
		return false
	}
	valid := ref.Source == "herdr:codex" && ref.Agent == "codex" && ref.Kind == "id" && validCodexID(ref.Value)
	if valid && slices.Contains(s.Conversation.PrimarySelection.PriorReferences, ref.Value) {
		return false
	}
	if s.Conversation.Status == "conflict" && s.Conversation.ObservedReference != nil && *s.Conversation.ObservedReference == *ref {
		return false
	}
	s.Conversation.Status, s.Conversation.ObservedReference = "conflict", ref
	s.Recovery = "needs_review"
	s.IdentityWarning = "Codex reported a different conversation without a confirmed primary selection. Review the saved target before recovery."
	if valid {
		s.ReportedCodexID = ref.Value
	}
	return true
}

func savePrimarySelection(id, socket, socketID string, report primarySelectionReport, facts processFacts, startup *conversationEvidence) error {
	invalid := errors.New("Codex selection could not be associated with the owned foreground run")
	if report.Source != primarySelectionSource || report.Reason != "resume" || report.Sequence == 0 || report.PID <= 1 || report.PID != facts.PID ||
		!validCodexID(report.PreviousID) || !validCodexID(report.SelectedID) || strings.EqualFold(report.PreviousID, report.SelectedID) ||
		facts.Variables["AH_SESSION_RUN_TOKEN"] != report.RunToken || facts.Variables["HERDR_PANE_ID"] != report.Pane || facts.Variables["HERDR_SOCKET_PATH"] != socket {
		return invalid
	}
	return withSessions(func(all []session) ([]session, error) {
		for index := range all {
			s := &all[index]
			if s.ID != id {
				continue
			}
			if s.CLI != "codex" || s.Profile == nil || !profileProcessMatches(*s, facts) || s.RunToken != report.RunToken || s.HerdrPane != report.Pane ||
				s.HerdrSocket != socket || s.HerdrSocketIdentity != socketID || s.ProcessGroup != report.PID {
				return nil, invalid
			}
			evidence := s.Conversation
			if evidence == nil || evidence.Reference.Agent != "codex" || evidence.Reference.Kind != "id" || s.CodexSessionID != evidence.Reference.Value {
				return nil, errors.New("Codex has no verified saved conversation for this run")
			}
			original := *evidence
			original.PrimarySelection = nil
			sequence := uint64(1)
			var priorReferences []string
			previousRun := evidence.PrimarySelection
			observed := evidence.RunToken == s.RunToken && evidence.Pane == s.HerdrPane && evidence.SocketIdentity == socketID && evidence.Status == "observed" && s.Recovery == "ready" && s.ReportedCodexID == "" && evidence.Provenance == "herdr-pane-observation"
			if prior := evidence.PrimarySelection; prior != nil && prior.RunToken == report.RunToken {
				if prior.primarySelectionReport == report {
					// Retry the sync before acknowledging a retransmission. The
					// previous rename may have succeeded before its directory sync failed.
					return all, nil
				}
				observed = observed || evidence.Status == "conflict" && s.ReportedCodexID == report.SelectedID
				sequence = prior.Sequence + 1
				priorReferences = slices.Clone(prior.PriorReferences)
				previousRun = prior.PreviousRun
				if prior.Original != nil {
					original = *prior.Original
				}
			} else if !observed && startup != nil && startup.Reference == evidence.Reference && s.ReportedCodexID == "" {
				switch evidence.Status {
				case "observed", "reviewed", "pending", "missing", "observation_unavailable":
					observed = true
					original = *startup
					original.PrimarySelection = nil
				}
			}
			if !observed {
				return nil, errors.New("Codex has no verified saved conversation for this run")
			}
			if report.Sequence != sequence || report.PreviousID != s.CodexSessionID {
				return nil, errors.New("Codex selection is stale or does not follow the saved conversation")
			}
			reference := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: report.SelectedID}
			updated := *evidence
			updated.Reference, updated.ObservedReference = reference, &reference
			updated.Status, updated.Provenance = "observed", "herdr-pane-observation"
			updated.RunToken, updated.Pane, updated.SocketIdentity = report.RunToken, report.Pane, socketID
			updated.ObservedAt = time.Now().UTC()
			updated.History = appendConversationHistory(updated.History, conversationTransition{
				From:     herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: report.PreviousID},
				To:       reference,
				At:       updated.ObservedAt,
				Source:   "primary-selection",
				RunToken: report.RunToken,
			})
			if !slices.Contains(priorReferences, report.PreviousID) {
				priorReferences = append(priorReferences, report.PreviousID)
			}
			updated.PrimarySelection = &primarySelectionEvidence{
				primarySelectionReport: report,
				Original:               &original,
				PriorReferences:        priorReferences,
				PreviousRun:            previousRun,
			}
			s.Conversation, s.CodexSessionID = &updated, report.SelectedID
			s.ConversationIDSource, s.Recovery, s.IdentityWarning, s.ReportedCodexID = primarySelectionSource, "ready", "", ""
			return all, nil
		}
		return nil, invalid
	})
}
