package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// This order is shared by status, confirmation and the final launch check.
func checkSession(s session, kind launchKind, override string) error {
	return checkSessionBase(s, kind, override, true)
}

func checkSessionBase(s session, kind launchKind, override string, checkModel bool) error {
	if err := validateProfileLaunch(s); err != nil {
		return err
	}
	if err := neutralReferenceCheck(s); err != nil {
		return err
	}
	if processGroupActive(s.ProcessGroup) {
		return errors.New("a previous session process is still running or closing")
	}
	if kind == resumeConversation {
		var duplicate bool
		if err := withSessions(func(all []session) ([]session, error) {
			for _, other := range all {
				if other.ID != s.ID && s.Conversation != nil && other.Conversation != nil && s.Conversation.Reference == other.Conversation.Reference && (processGroupActive(other.ProcessGroup) || sessionLaunchPending(other)) {
					duplicate = true
				}
				if other.ID != s.ID && s.CodexSessionID != "" && other.CodexSessionID == s.CodexSessionID && (processGroupActive(other.ProcessGroup) || sessionLaunchPending(other)) {
					duplicate = true
				}
			}
			return nil, nil
		}); err != nil {
			return err
		}
		if duplicate {
			return errors.New("this Codex conversation is still running in another session")
		}

	}
	resolved, err := checkDirectory(s.WorkingDirectory)
	if err != nil {
		return err
	}
	if resolved != s.WorkingDirectory {
		return errors.New("working directory target changed; verify the original directory before recovery")
	}
	if len(s.CandidateIDs) > 0 || s.ReportedCodexID != "" && s.ReportedCodexID != s.CodexSessionID {
		return errors.New("conversation ID ambiguous; use verify to explicitly choose a conversation")
	}
	if kind == resumeConversation {
		if err := queryReferenceCheck(s); err != nil {
			return err
		}
	}

	if kind == resumeConversation && s.CodexHome != "" {
		environment, err := sessionClientEnvironment(s)
		env := environment.Environment
		if err != nil {
			return err
		}
		home := environmentValue(env, "CODEX_HOME")
		if home == "" {
			home = filepath.Join(environmentValue(env, "HOME"), ".codex")
		}
		absolute, err := filepath.Abs(home)
		if err != nil {
			return err
		}
		if absolute != s.CodexHome {
			return errors.New("Codex home changed; explicitly verify the original conversation and configuration before recovery")
		}

	}
	if s.Mode == "magpie" {
		if s.CLI == "codex" && s.CodexConnection != "" && s.CodexConnection != "api" && !joinedCodex(s) {
			return errors.New("unsupported Codex connection mode; review this session")
		}
		if s.InstanceSnapshot != nil {
			probe := probeGateway(s.MagpieURL)
			if probe.State == gatewayOffline {
				return errors.New("gateway offline; start the recorded Magpie instance explicitly")
			}
			health := s.InstanceSnapshot.health(probe)
			if s.InstanceSnapshot.ConfigDir == "" && kind == resumeConversation {
				health.State = instanceUnknown
			}
			if health.needsAttention() && override == "" {
				return fmt.Errorf("Magpie instance %s; verify its configuration and use --override-instance REASON explicitly", health.State)
			}
		} else if kind == resumeConversation && override == "" {
			return errors.New("Magpie instance identity unknown; use --override-instance REASON after reviewing the gateway")
		}
		if joinedCodex(s) {
			if err := requireCallerOnlyGateway(s.MagpieURL); err != nil {
				return err
			}
		}
		if checkModel && !(joinedCodex(s) && (kind == resumeConversation || !strings.Contains(s.MagpieModel, "/"))) {
			if err := checkMagpieModel(s.MagpieURL, s.MagpieModel); err != nil {
				return err
			}
		}
	}
	return nil
}

// Keep the reviewed Harbor record intact for its compare-and-swap checks,
// while checking availability of the model Codex actually last selected.
func checkSessionSelection(s session, kind launchKind, override string, selection *codexSelection) error {
	if kind == resumeConversation && s.Mode == "magpie" && s.CLI == "codex" && !joinedCodex(s) {
		if selection == nil {
			return errors.New("review the last Codex model before recovery")
		}
		s.MagpieModel = selection.Model
	}
	return checkSession(s, kind, override)
}
