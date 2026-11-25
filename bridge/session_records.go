package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Record actions share the launch lock so removal cannot race a new launch.
func changeSessionRecord(id string, change func(session) (*session, error)) error {
	if !validID(id) {
		return errors.New("invalid bridge session ID")
	}
	lock, err := lockSession(id)
	if err != nil {
		return err
	}
	defer lock.Close()
	return withSessions(func(all []session) ([]session, error) {
		for i, s := range all {
			if s.ID != id {
				continue
			}
			next, err := change(s)
			if err != nil {
				return nil, err
			}
			if next == nil {
				return append(all[:i], all[i+1:]...), nil
			}
			all[i] = *next
			return all, nil
		}
		return nil, errors.New("session not found")
	})
}

func validateSessionName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return errors.New("session name must contain 1–80 characters without control characters")
	}
	return nil
}

func renameSession(id, name string) error {
	name = strings.TrimSpace(name)
	if err := validateSessionName(name); err != nil {
		return err
	}
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID != id {
				continue
			}
			all[i].Name = name
			all[i].AutoNamePending = false
			all[i].AutoNameSync = false
			all[i].HerdrAgentName = agentName(name, id)
			return all, nil
		}
		return nil, errors.New("session not found")
	})
}

func renameSessionReviewed(expected session, name string) error {
	if err := validateSessionName(name); err != nil {
		return err
	}
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID == expected.ID {
				if !sameSession(all[i], expected) {
					return nil, errors.New("session changed; review again")
				}
				all[i].Name = strings.TrimSpace(name)
				all[i].AutoNamePending = false
				all[i].AutoNameSync = false
				all[i].HerdrAgentName = agentName(name, expected.ID)
				return all, nil
			}
		}
		return nil, errors.New("session not found")
	})
}

func removeSession(expected session) error {
	return changeSessionRecord(expected.ID, func(s session) (*session, error) {
		if !sameSession(s, expected) {
			return nil, errors.New("session changed; review its details again")
		}
		if processGroupActive(s.ProcessGroup) {
			return nil, errors.New("session is still running; stop it before removing its record")
		}
		return nil, nil
	})
}

func archiveBlockReason(v sessionView) string {
	if v.External {
		return "external sessions cannot be archived"
	}
	if v.Archived {
		return "session is already archived"
	}
	if v.DiscoveryWarning != "" {
		return v.DiscoveryWarning
	}
	if v.Running || v.Runtime.Presence == "observed" || processGroupActive(v.ProcessGroup) || sessionLaunchPending(v.session) {
		return "Cannot archive active, Idle, or Done sessions."
	}
	switch v.Runtime.Presence {
	case "not_started", "historical_exit", "confirmed_exit":
		return ""
	default:
		return "Cannot archive: session exit is not confirmed."
	}
}

func (harborService) SetPinned(expected sessionView, pinned bool) error {
	if expected.External {
		return errors.New("external sessions cannot be pinned")
	}
	if !validID(expected.ID) {
		return errors.New("invalid bridge session ID")
	}
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID == expected.ID {
				all[i].Pinned = pinned
				return all, nil
			}
		}
		return nil, errors.New("session not found")
	})
}

// Archive re-verifies live runtime state under the session lock without
// stopping processes, touching client history, or altering conversation/route bindings.
func (harborService) Archive(expected sessionView) error {
	if expected.External {
		return errors.New("external sessions cannot be archived")
	}
	if !validID(expected.ID) {
		return errors.New("invalid bridge session ID")
	}
	lock, err := lockSession(expected.ID)
	if err != nil {
		return err
	}
	defer lock.Close()
	current, _, err := currentRuntime(expected.ID)
	if err != nil {
		return err
	}
	if !sameSession(current.session, expected.session) {
		return errors.New("session changed; refresh and try again")
	}
	if reason := archiveBlockReason(current); reason != "" {
		return errors.New(reason)
	}
	now := time.Now().UTC()
	return withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID != expected.ID {
				continue
			}
			if !sameSession(all[i], expected.session) {
				return nil, errors.New("session changed; refresh and try again")
			}
			if processGroupActive(all[i].ProcessGroup) {
				return nil, errors.New("session is still running; stop it before archiving")
			}
			all[i].Archived = true
			all[i].ArchivedAt = &now
			return all, nil
		}
		return nil, errors.New("session not found")
	})
}

func (harborService) Unarchive(expected sessionView) error {
	if expected.External {
		return errors.New("external sessions cannot be unarchived")
	}
	return changeSessionRecord(expected.ID, func(s session) (*session, error) {
		if !sameSession(s, expected.session) {
			return nil, errors.New("session changed; refresh and try again")
		}
		if !s.Archived {
			return nil, errors.New("session is not archived")
		}
		s.Archived = false
		s.ArchivedAt = nil
		return &s, nil
	})
}

func recordCommand(args []string) error {
	if args[0] == "rename" {
		if len(args) != 3 {
			return errors.New("usage: rename SESSION_ID NAME")
		}
		return appService.Rename(args[1], args[2])
	}
	if len(args) != 2 {
		return errors.New("usage: remove SESSION_ID")
	}
	view, err := appService.Get(args[1])
	s := view.session
	if err != nil {
		return err
	}
	fmt.Printf("Remove %q from Harbor? Codex history is kept. Type yes: ", s.Name)
	var reply string
	if _, err := fmt.Scanln(&reply); err != nil || reply != "yes" {
		return errors.New("removal cancelled")
	}
	return appService.Remove(sessionApproval{Session: s, Observation: &view.Runtime, Confirmed: true})
}

// Herdr requires unique ASCII agent handles of at most 32 characters.
func agentName(name, id string) string {
	var out strings.Builder
	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			out.WriteRune(c)
		} else if out.Len() > 0 && !strings.HasSuffix(out.String(), "-") {
			out.WriteByte('-')
		}
		if out.Len() >= 23 {
			break
		}
	}
	base := strings.Trim(out.String(), "-")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "session"
	}
	if len(id) >= 8 {
		return base + "-" + id[:8]
	}
	return base
}
