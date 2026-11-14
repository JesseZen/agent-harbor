package main

import (
	"errors"
	"fmt"
	"path/filepath"
)

// This order is shared by status, confirmation and the final launch check.
func checkSession(s session, kind launchKind, override string) error {
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
		env, err := userEnvironment()
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
		if err := checkMagpieModel(s.MagpieURL, s.MagpieModel); err != nil {
			return err
		}
	}
	return nil
}
