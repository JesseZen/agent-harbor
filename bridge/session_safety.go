package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
)

type launchKind string

const (
	newConversation    launchKind = "new"
	resumeConversation launchKind = "resume"
)

func lockSession(id string) (*os.File, error) {
	root, err := bridgeHome()
	if err != nil {
		return nil, err
	}
	if err := ensureHome(root); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, "running-"+id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("session is still running")
	}
	return f, nil
}

func processGroupActive(group int) bool {
	if group == 0 {
		return false
	}
	if group < 2 {
		return true
	}
	err := syscall.Kill(-group, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func sameSession(a, b session) bool {
	return reflect.DeepEqual(a, b)
}

func beginRun(expected session, kind launchKind, plan runPlan) error {
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID != expected.ID {
				continue
			}
			s := &all[i]
			if !sameSession(*s, expected) {
				return nil, errors.New("session changed; review its details again")
			}
			if processGroupActive(s.ProcessGroup) {
				return nil, errors.New("a previous session process is still running or closing")
			}
			if kind == newConversation && s.Started {
				return nil, errors.New("session was already started; use explicit recovery")
			}
			if kind == resumeConversation && s.Recovery != "ready" {
				return nil, errors.New("session identity needs verification before recovery")
			}
			for _, other := range all {
				if kind == resumeConversation && other.ID != s.ID && ((s.CodexSessionID != "" && other.CodexSessionID == s.CodexSessionID) || (s.Conversation != nil && other.Conversation != nil && s.Conversation.Reference == other.Conversation.Reference)) && (processGroupActive(other.ProcessGroup) || sessionLaunchPending(other)) {
					return nil, errors.New("this Codex conversation is still running in another session")
				}
			}
			s.Started, s.ProcessGroup = true, 0
			now := time.Now().UTC()
			s.LastStartedAt = &now
			s.RunToken, s.CodexHome = plan.Token, plan.Home
			if s.CodexSessionID != "" {
				s.Recovery = "needs_review"
			}
			return all, nil
		}
		return nil, errors.New("session not found")
	})
}

// Only called when exec.Start proves that no Codex process was created.
func undoFailedStart(previous session, plan runPlan) error {
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID == previous.ID {
				expected := previous
				expected.Started, expected.ProcessGroup = true, 0
				expected.LastStartedAt = all[i].LastStartedAt
				expected.RunToken, expected.CodexHome = plan.Token, plan.Home
				if expected.CodexSessionID != "" {
					expected.Recovery = "needs_review"
				}
				if !sameSession(all[i], expected) {
					return nil, errors.New("session changed during failed start; review it manually")
				}
				all[i] = previous
				return all, nil
			}
		}
		return nil, errors.New("session not found")
	})
}

func verifySession(expected session, codexID, dir string) error {
	if !validCodexID(codexID) {
		return errors.New("Codex session ID must be a UUID")
	}
	env, err := userEnvironment()
	if err != nil {
		return err
	}
	codexHome := environmentValue(env, "CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(environmentValue(env, "HOME"), ".codex")
	}
	codexHome, err = filepath.Abs(codexHome)
	if err != nil {
		return err
	}
	lock, err := lockSession(expected.ID)
	if err != nil {
		return err
	}
	defer lock.Close()
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID != expected.ID {
				continue
			}
			s := &all[i]
			if !sameSession(*s, expected) {
				return nil, errors.New("session changed; review its details again")
			}
			if processGroupActive(s.ProcessGroup) {
				return nil, errors.New("a previous session process is still running or closing")
			}

			if s.Conversation != nil {
				ref := s.Conversation.Reference
				if s.CLI != "codex" || ref.Agent != s.CLI || ref.Kind != "id" || ref.Value != codexID {
					return nil, errors.New("verify cannot replace the saved conversation; inspect show and use reassociate with the original reference, or verify the saved Codex ID")
				}
				if err := neutralReferenceCheck(*s); err != nil {
					return nil, err
				}
				original, err := reviewBaseline(*s)
				if err != nil {
					return nil, err
				}
				s.Reassociations = append(s.Reassociations, reassociationDecision{Original: original, At: time.Now().UTC(), Reference: ref, Reason: "Explicit verify of saved conversation and directory", Accepted: true, Previous: s.Conversation})
				s.Conversation = &conversationEvidence{Reference: ref, Provenance: "reviewed-compatibility", Status: "reviewed"}
				s.IdentityWarning = ""
			}
			s.WorkingDirectory, s.CodexSessionID = dir, codexID
			if s.Conversation == nil {
				s.ConversationIDSource = "manual"
			}
			s.CandidateIDs = nil
			s.Candidates = nil
			s.RunToken = ""
			s.CodexHome = codexHome
			s.ReportedCodexID = ""
			s.Recovery, s.ProcessGroup = "ready", 0
			return all, nil
		}
		return nil, errors.New("session not found")
	})
}

func sessionLaunchPending(s session) bool {
	if !s.Started || s.ProcessGroup != 0 || s.LastStartedAt == nil || s.LastExitAt != nil && !s.LastStartedAt.After(*s.LastExitAt) {
		return false
	}
	root, err := bridgeHome()
	if err != nil {
		return false
	}
	f, err := os.OpenFile(filepath.Join(root, "running-"+s.ID+".lock"), os.O_RDWR, 0600)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}
