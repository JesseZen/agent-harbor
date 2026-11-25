package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
)

// session_organization.json stores Pin and Archive state independently of
// sessions.json so already-running older `_pane` processes that rewrite
// sessions.json cannot strip Pin or Archive flags during online popup upgrades.
type sessionOrgState struct {
	Pinned     bool       `json:"pinned,omitempty"`
	Archived   bool       `json:"archived,omitempty"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

type sessionOrgFile struct {
	Version int                        `json:"version"`
	Entries map[string]sessionOrgState `json:"entries"`
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

// Lock ordering: always acquire sessions.lock BEFORE session_organization.lock.
func withSessionOrg(root string, update func(map[string]sessionOrgState) bool) (sessionOrgFile, error) {
	org := sessionOrgFile{Version: 1, Entries: map[string]sessionOrgState{}}
	lock, err := os.OpenFile(filepath.Join(root, "session_organization.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return org, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return org, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	path := filepath.Join(root, "session_organization.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return org, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &org); err != nil {
			return sessionOrgFile{}, errors.New("invalid session organization state: " + err.Error())
		}
		if org.Entries == nil {
			org.Entries = map[string]sessionOrgState{}
		}
		if org.Version == 0 {
			org.Version = 1
		}
	}
	if update != nil && update(org.Entries) {
		org.Version = 1
		encoded, err := json.MarshalIndent(org, "", "  ")
		if err != nil {
			return org, err
		}
		if err := writeStateFile(path, append(encoded, '\n')); err != nil {
			return org, err
		}
	}
	return org, nil
}

func readSessionOrg(root string) (sessionOrgFile, error) {
	return withSessionOrg(root, nil)
}

func setSessionOrgState(root, id string, state sessionOrgState) error {
	_, err := withSessionOrg(root, func(entries map[string]sessionOrgState) bool {
		if !state.Pinned && !state.Archived {
			if _, exists := entries[id]; !exists {
				return false
			}
			delete(entries, id)
			return true
		}
		state.ArchivedAt = cloneTimePtr(state.ArchivedAt)
		entries[id] = state
		return true
	})
	return err
}

func pruneRemovedSessionOrg(root string, before, after []session) error {
	kept := make(map[string]bool, len(after))
	for _, s := range after {
		kept[s.ID] = true
	}
	var removed []string
	for _, s := range before {
		if !kept[s.ID] {
			removed = append(removed, s.ID)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	_, err := withSessionOrg(root, func(entries map[string]sessionOrgState) bool {
		changed := false
		for _, id := range removed {
			if _, ok := entries[id]; ok {
				delete(entries, id)
				changed = true
			}
		}
		return changed
	})
	return err
}

func applySessionOrg(sessions []session, org sessionOrgFile) {
	if len(org.Entries) == 0 {
		return
	}
	for i := range sessions {
		if entry, ok := org.Entries[sessions[i].ID]; ok {
			sessions[i].Pinned = entry.Pinned
			sessions[i].Archived = entry.Archived
			sessions[i].ArchivedAt = cloneTimePtr(entry.ArchivedAt)
		}
	}
}

var knownSessionJSONKeys = func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(session{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			keys[name] = true
		}
	}
	return keys
}()

func extractUnknownSessionFields(data []byte, sessions []session) map[string]map[string]json.RawMessage {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(data, &rawItems); err != nil || len(rawItems) != len(sessions) {
		return nil
	}
	var unknown map[string]map[string]json.RawMessage
	for i, raw := range rawItems {
		id := sessions[i].ID
		if id == "" {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		for k, v := range obj {
			if knownSessionJSONKeys[k] {
				continue
			}
			if unknown == nil {
				unknown = map[string]map[string]json.RawMessage{}
			}
			if unknown[id] == nil {
				unknown[id] = map[string]json.RawMessage{}
			}
			unknown[id][k] = v
		}
	}
	return unknown
}

func marshalSessionsPreservingUnknown(sessions []session, unknown map[string]map[string]json.RawMessage) ([]byte, error) {
	if len(unknown) == 0 {
		return json.MarshalIndent(sessions, "", "  ")
	}
	merged := make([]json.RawMessage, len(sessions))
	for i, s := range sessions {
		raw, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		extras := unknown[s.ID]
		if len(extras) == 0 {
			merged[i] = raw
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
		for k, v := range extras {
			if _, exists := obj[k]; !exists {
				obj[k] = v
			}
		}
		reencoded, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		merged[i] = reencoded
	}
	return json.MarshalIndent(merged, "", "  ")
}
