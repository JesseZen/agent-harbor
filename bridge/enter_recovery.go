package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

var errRecoveryStale = errors.New("recovery changed; recheck and confirm again")

const idleRecoverySource = "herdr.enter_idle_shell"

type recoveryCandidate struct {
	Label, Title string
	Reference    herdrIdentity
	Seen         time.Time
	Selection    *codexSelection
	SelectionErr error
}

type recoveryApproval struct {
	Target      herdrIdentity
	Shell       previousPane
	Fingerprint string
}

func (harborService) RecoveryMetadata(s session) (map[string]string, string, error) {
	fingerprint, err := recoveryConnectionFingerprint(s)
	if err == nil && s.CLI == "codex" {
		var environment profileClientEnvironment
		environment, err = sessionClientEnvironment(s)
		if err == nil {
			err = checkEnterRecoveryIntegration(s, environment, environment.Environment)
		}
	}
	return codexNameIndex(s), fingerprint, err
}

func checkEnterRecoveryIntegration(s session, environment profileClientEnvironment, env []string) error {
	if s.CLI != "codex" {
		return nil
	}
	herdr := os.Getenv("HERDR_BIN_PATH")
	if herdr == "" || s.Profile == nil || environment.Binding != *s.Profile || environment.Home == "" || environmentValue(env, "CODEX_HOME") != environment.Home {
		return errors.New("cannot check Codex recovery hooks in the recorded Profile")
	}
	enabled, err := codexHooksEnabled(environment.Home)
	if err != nil {
		return err
	}
	if enabled != nil && !*enabled {
		return errors.New("Codex recovery hooks are disabled. Enable them in the recorded Profile, then recheck.")
	}
	current, err := codexIntegrationStatus(herdr, s.WorkingDirectory, env)
	if err != nil {
		return err
	}
	if !current || enabled == nil {
		return errors.New("Prepare Herdr's Codex recovery hooks in the recorded Profile, then recheck.")
	}
	return nil
}

func recoveryConnectionFingerprint(s session) (string, error) {
	if s.Profile == nil {
		return "", ErrProfileIdentity
	}
	p, home, err := resolveProfileRecord(*s.Profile)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if err := json.NewEncoder(hash).Encode([]any{s.Profile, p.Kind, home}); err != nil {
		return "", err
	}
	if s.CLI == "codex" {
		e, err := sessionClientEnvironment(s)
		if err != nil {
			return "", err
		}
		codexHome := environmentValue(e.Environment, "CODEX_HOME")
		if codexHome == "" {
			codexHome = filepath.Join(environmentValue(e.Environment, "HOME"), ".codex")
		}
		paths := []string{filepath.Join(codexHome, "config.toml"), filepath.Join(codexHome, "auth.json"), "/etc/codex/config.toml", "/etc/codex/requirements.toml"}
		for dir := filepath.Clean(s.WorkingDirectory); filepath.IsAbs(dir); dir = filepath.Dir(dir) {
			paths = append(paths, filepath.Join(dir, ".codex", "config.toml"))
			if dir == filepath.Dir(dir) {
				break
			}
		}
		for _, path := range paths {
			if err := hashRecoveryFile(hash, path); err != nil {
				return "", err
			}
		}
		for _, name := range []string{"CODEX_CONFIG", "OPENAI_BASE_URL", "OPENAI_API_KEY"} {
			digest := sha256.Sum256([]byte(environmentValue(e.Environment, name)))
			hash.Write(digest[:])
		}
	}
	if s.Mode == "magpie" {
		r, err := loadMagpieRegistry()
		if err != nil {
			return "", err
		}
		i, _ := r.find(s.InstanceID)
		if err := json.NewEncoder(hash).Encode([]any{i.ID, i.Address, i.ConfigDir, i.Kind, i.Version, i.Executable}); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func hashRecoveryFile(digests hash.Hash, path string) error {
	const limit = 2 << 20
	digests.Write([]byte(path + "\x00"))
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		digests.Write([]byte("missing\x00"))
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return errors.New("a scoped Codex configuration or login file cannot be checked")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("a scoped Codex configuration or login file cannot be read")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) > limit {
		return errors.New("a scoped Codex configuration or login file cannot be read")
	}
	digest := sha256.Sum256(data)
	digests.Write(digest[:])
	return nil
}

func checkRecoveryFingerprint(s session, expected string) error {
	current, err := recoveryConnectionFingerprint(s)
	if err != nil || current != expected {
		return fmt.Errorf("%w: Profile or connection settings changed or are unavailable", errRecoveryStale)
	}
	return nil
}

func completeRecoveryReference(client string, ref herdrIdentity) bool {
	return ref.Agent == client && selectionTextValid(ref.Source, 512) && selectionTextValid(ref.Kind, 512) && selectionTextValid(ref.Value, 4096)
}

func sameConversationTarget(a, b herdrIdentity) bool {
	return a.Agent == b.Agent && a.Kind == b.Kind && a.Value == b.Value
}

func recoveryConflict(s session) bool {
	c := s.Conversation
	return len(s.CandidateIDs) > 0 || len(s.Candidates) > 0 || s.ReportedCodexID != "" && s.ReportedCodexID != s.CodexSessionID || c != nil && (c.Status == "conflict" || c.ObservedReference != nil && !sameConversationTarget(c.Reference, *c.ObservedReference) || neutralReferenceCheck(s) != nil)
}

func recoveryCandidates(s session) ([]recoveryCandidate, error) {
	if !recoveryConflict(s) {
		if s.Conversation != nil && completeRecoveryReference(s.CLI, s.Conversation.Reference) {
			ref := s.Conversation.Reference
			if queryReferenceCheck(s) == nil {
				return []recoveryCandidate{{Label: "Saved conversation", Reference: ref}}, nil
			}
			if _, err := savedReferenceForReview(s); err == nil {
				return []recoveryCandidate{{Label: "Saved conversation", Reference: ref}}, nil
			}
			return nil, errors.New("saved conversation identity needs review; check Details")
		}
		return nil, errors.New(missingConversationRecovery)
	}
	var saved, reported herdrIdentity
	if c := s.Conversation; c != nil {
		saved = c.Reference
		if c.ObservedReference != nil && !sameConversationTarget(saved, *c.ObservedReference) {
			reported = *c.ObservedReference
		}
	}
	if s.CLI == "codex" {
		if saved == (herdrIdentity{}) && validCodexID(s.CodexSessionID) {
			saved = herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: s.CodexSessionID}
		}
		if reported == (herdrIdentity{}) && validCodexID(s.ReportedCodexID) {
			reported = herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: s.ReportedCodexID}
		}
		if s.CodexSessionID != "" && s.CodexSessionID != saved.Value || s.ReportedCodexID != "" && s.ReportedCodexID != reported.Value {
			return nil, errors.New("saved and reported evidence disagree; check Details before associating a conversation")
		}
	}
	if !completeRecoveryReference(s.CLI, saved) || !completeRecoveryReference(s.CLI, reported) || sameConversationTarget(saved, reported) {
		return nil, errors.New("original or last reported conversation is incomplete; check Details")
	}
	return []recoveryCandidate{{Label: "Originally saved", Reference: saved}, {Label: "Last reported", Reference: reported}}, nil
}

func recoveryTarget(s session, ref herdrIdentity) session {
	if queryReferenceCheck(s) == nil && !recoveryConflict(s) && s.Conversation.Reference == ref {
		return s
	}
	s.Version = 3
	s.Conversation = &conversationEvidence{Reference: ref, Provenance: "reviewed-compatibility", Status: "reviewed"}
	if s.CLI == "codex" {
		s.CodexSessionID = ref.Value
		s.ConversationIDSource = "manual"
	}
	s.Recovery = "ready"
	s.ReportedCodexID, s.IdentityWarning = "", ""
	s.CandidateIDs, s.Candidates = nil, nil
	return s
}

func recoveryEndMatches(s session, e *endedEvidence) bool {
	return e != nil && e.Pane == s.HerdrPane && e.RunToken == s.RunToken && reflect.DeepEqual(e.StartedAt, s.LastStartedAt)
}

func sameRecoveryPane(a, b previousPane) bool {
	return a.PaneID != "" && a.PaneID == b.PaneID && a.TabID == b.TabID && a.TerminalID == b.TerminalID && a.SocketIdentity == b.SocketIdentity && a.ShellPID == b.ShellPID
}

func inspectRecoveryEnd(v sessionView, inspect func(session) (previousPane, error)) (previousPane, error) {
	if v.DiscoveryWarning != "" {
		return previousPane{}, errors.New(v.DiscoveryWarning)
	}
	if !v.Started || v.Runtime.Presence == "historical_exit" || v.Runtime.Presence == "confirmed_exit" {
		return previousPane{}, nil
	}
	if v.Runtime.Presence == "observed" || v.Runtime.Instance == "" || v.Runtime.LaunchPending {
		return previousPane{}, errors.New("cannot confirm the previous run ended")
	}
	if v.Ended != nil && v.Ended.Source == idleRecoverySource && v.Ended.ShellPID == 0 && v.Ended.SocketIdentity == v.Runtime.Instance && recoveryEndMatches(v.session, v.Ended) {
		return previousPane{}, nil
	}
	pane, err := inspect(v.session)
	if pane.PaneID != "" && pane.SocketIdentity != v.Runtime.Instance {
		return previousPane{}, errors.New("previous pane belongs to a different workspace")
	}
	return pane, err
}

func recoveryOccupied(target session, views []sessionView) error {
	for _, other := range views {
		if target.Conversation == nil {
			break
		}
		ref := target.Conversation.Reference
		if other.Runtime.Presence == "observed" && other.Runtime.Reference != nil && sameConversationTarget(*other.Runtime.Reference, ref) {
			return errConversationObserved
		}
		if other.ID != target.ID && (processGroupActive(other.ProcessGroup) || sessionLaunchPending(other.session)) && (other.Conversation != nil && sameConversationTarget(other.Conversation.Reference, ref) || ref.Agent == "codex" && ref.Kind == "id" && other.CodexSessionID == ref.Value) {
			return fmt.Errorf("%w: still running in another session", errConversationObserved)
		}
	}
	return nil
}

func checkEnterRecoveryPlan(s session, plan *resumeCommandPlan) error {
	if s.CLI == "codex" && (s.Conversation == nil || len(plan.Argv) != 3 || plan.Argv[1] != "resume" || plan.Argv[2] != s.Conversation.Reference.Value) {
		return errors.New("Herdr did not return an exact command for the confirmed Codex conversation")
	}
	return nil
}

func (h harborService) approveEnterRecovery(a sessionApproval) (session, error) {
	s := a.Session
	v, all, err := currentRuntime(s.ID)
	if err != nil {
		return s, err
	}
	if v.External || !sameSession(v.session, s) || a.Observation == nil || !sameRuntimeTarget(v.Runtime, *a.Observation) {
		return s, errRecoveryStale
	}
	pane, err := inspectRecoveryEnd(v, h.PreviousPane)
	if err != nil {
		return s, fmt.Errorf("%w: %v", errRecoveryStale, err)
	}
	if pane.PaneID != "" || a.Recovery.Shell.PaneID != "" {
		if !sameRecoveryPane(pane, a.Recovery.Shell) || pane.ShellPID < 2 {
			return s, fmt.Errorf("%w: previous shell changed", errRecoveryStale)
		}
	}
	candidates, err := recoveryCandidates(s)
	if err != nil {
		return s, err
	}
	matched := false
	for _, candidate := range candidates {
		matched = matched || candidate.Reference == a.Recovery.Target
	}
	if !matched {
		return s, errRecoveryStale
	}
	updated := recoveryTarget(s, a.Recovery.Target)
	if err := recoveryOccupied(updated, all); err != nil {
		return s, err
	}
	if err := checkRecoveryFingerprint(s, a.Recovery.Fingerprint); err != nil {
		return s, err
	}
	if err := verifyCodexSelection(context.Background(), updated, a.MagpieSelection); err != nil {
		return s, fmt.Errorf("%w: %v", errRecoveryStale, err)
	}
	if !sameSession(s, updated) {
		original, err := reviewBaseline(s)
		if err != nil {
			return s, err
		}
		updated.Reassociations = append(updated.Reassociations, reassociationDecision{Original: original, At: time.Now().UTC(), Reference: a.Recovery.Target, Reason: "Confirmed recovery target in Sessions", Accepted: true, Previous: s.Conversation})
	}
	if directory, err := checkDirectory(s.WorkingDirectory); err != nil || directory != s.WorkingDirectory {
		return s, fmt.Errorf("%w: working directory is unavailable or changed", errRecoveryStale)
	}
	if s.Mode == "magpie" && s.InstanceSnapshot != nil {
		probe := probeGateway(s.MagpieURL)
		if health := s.InstanceSnapshot.health(probe); health.State == instanceChanged || health.State == instanceNotMagpie {
			return s, fmt.Errorf("%w: Magpie instance %s", errRecoveryStale, health.State)
		}
	}
	if pane.PaneID != "" && (!recoveryEndMatches(s, s.Ended) || s.Ended.Source != idleRecoverySource || s.Ended.SocketIdentity != pane.SocketIdentity || s.Ended.Terminal != pane.TerminalID || s.Ended.ShellPID != pane.ShellPID) {
		updated.Ended = &endedEvidence{Source: idleRecoverySource, At: time.Now().UTC(), StartedAt: s.LastStartedAt, RunToken: s.RunToken, SocketIdentity: pane.SocketIdentity, Pane: pane.PaneID, Terminal: pane.TerminalID, ShellPID: pane.ShellPID}
	} else if v.Runtime.Presence == "confirmed_exit" && !sameSession(s, updated) && recoveryEndMatches(s, s.Ended) {
		ended := *s.Ended
		ended.Source, ended.Reference = idleRecoverySource, nil
		updated.Ended = &ended
	}
	if sameSession(s, updated) {
		return s, nil
	}
	encoded, err := json.MarshalIndent([]session{updated}, "", "  ")
	if err != nil {
		return s, err
	}
	var persisted []session
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return s, err
	}
	updated = persisted[0]
	if err := changeSessionRecord(s.ID, func(current session) (*session, error) {
		if !sameSession(current, s) || processGroupActive(current.ProcessGroup) {
			return nil, errRecoveryStale
		}
		return &updated, nil
	}); err != nil {
		return s, err
	}
	return updated, nil
}

func recoveryHandshakePath() string {
	path := os.Getenv("AGENT_HARBOR_BRIDGE_RECOVERY_RESULT")
	root, err := bridgeHome()
	if err != nil || filepath.Dir(path) != root || !strings.HasPrefix(filepath.Base(path), "recovery-result-") {
		return ""
	}
	return path
}

type recoveryLaunchResult struct {
	Error string
	Stale bool
}

func reportRecoveryLaunch(path string, err error) error {
	result := recoveryLaunchResult{}
	if err != nil {
		result.Error, result.Stale = err.Error(), errors.Is(err, errRecoveryStale)
	}
	data, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		return encodeErr
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		return errors.New("recovery confirmation expired while the pane was opening")
	}
	return writeStateFile(path, data)
}

func awaitRecoveryLaunch(path string) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) > 0 {
			var result recoveryLaunchResult
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			if result.Stale {
				return fmt.Errorf("%w%s", errRecoveryStale, strings.TrimPrefix(result.Error, errRecoveryStale.Error()))
			}
			if result.Error != "" {
				return errors.New(result.Error)
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%w: the client did not report startup", errRecoveryStale)
}
