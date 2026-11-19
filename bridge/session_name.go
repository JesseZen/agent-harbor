package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Codex 0.169.0 writes names to this append-only metadata index. Read only
// the bound home, never conversation messages or a screen.
func codexNameIndex(s session) map[string]string {
	if s.CLI != "codex" {
		return nil
	}
	e, err := sessionClientEnvironment(s)
	if err != nil {
		return nil
	}
	home := environmentValue(e.Environment, "CODEX_HOME")
	if home == "" {
		home = filepath.Join(environmentValue(e.Environment, "HOME"), ".codex")
	}
	if !filepath.IsAbs(home) || s.CodexHome == "" || filepath.Clean(home) != filepath.Clean(s.CodexHome) {
		return nil
	}
	f, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	// Bound the work on each observation; older names outside the tail can wait
	// for a later Codex update or an explicit manual rename.
	const limit = int64(4 << 20)
	start := max(int64(0), info.Size()-limit)
	r := bufio.NewReader(io.NewSectionReader(f, start, info.Size()-start))
	if start > 0 {
		_, _ = r.ReadString('\n')
	}
	names := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil { // An unfinished append is not a published title.
			break
		}
		var entry struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && validCodexID(entry.ID) {
			name := strings.TrimSpace(entry.Name)
			if validateSessionName(name) == nil {
				names[entry.ID] = name
			} else {
				delete(names, entry.ID)
			}
		}
	}
	return names
}

// Herdr strips Codex's activity glyph from the OSC title. Match a complete
// index name, optionally followed by Codex's " | project" suffix. Never select
// the newest index row: several Conversations may share this Profile.
func codexTerminalName(names map[string]string, title string) string {
	matched := ""
	for _, name := range names {
		if title == name || strings.HasPrefix(title, name+" | ") {
			if matched != "" && matched != name {
				return ""
			}
			matched = name
		}
	}
	return matched
}

// Runs from the existing identity observer. Persisting the one-shot policy
// makes restart/resume preserve both automatic and later manual choices.
func captureAutomaticSessionName(id, token string) {
	s, err := getSession(id)
	if err != nil || !s.AutoNamePending || s.RunToken != token {
		return
	}
	names := codexNameIndex(s)
	if len(names) == 0 {
		return
	}
	observed := s.Conversation != nil && s.Conversation.Status == "observed" &&
		s.Conversation.RunToken == token && s.Conversation.Pane == s.HerdrPane &&
		s.Conversation.SocketIdentity == s.HerdrSocketIdentity && s.Conversation.Reference.Agent == s.CLI && s.Conversation.Reference.Kind == "id"
	_ = withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID != id {
				continue
			}
			if !sameSession(all[i], s) || s.HerdrSocketIdentity == "" || socketIdentity(s.HerdrSocket) != s.HerdrSocketIdentity {
				return nil, nil
			}
			var pane struct {
				Pane struct {
					ID        string         `json:"pane_id"`
					Tab       string         `json:"tab_id"`
					Label     string         `json:"label"`
					Reference *herdrIdentity `json:"agent_session"`
					Client    string         `json:"agent"`
					Terminal  string         `json:"terminal_id"`
					Workspace string         `json:"workspace_id"`
					Title     string         `json:"terminal_title_stripped"`
				} `json:"pane"`
			}
			if agentRPC(s.HerdrSocket, "pane.get", map[string]any{"pane_id": s.HerdrPane}, &pane) != nil ||
				pane.Pane.ID != s.HerdrPane || pane.Pane.Tab != s.HerdrTab {
				return nil, nil
			}
			if conversationAssociationWarning(s, pane.Pane.Reference) != "" {
				return nil, nil
			}
			name := ""
			if observed {
				if pane.Pane.Reference == nil {
					return nil, nil
				}
				name = names[s.Conversation.Reference.Value]
			} else {
				views := make([]sessionView, len(all))
				for j := range all {
					views[j].session = all[j]
				}
				agent := herdrAgent{Pane: pane.Pane.ID, Tab: pane.Pane.Tab, Client: pane.Pane.Client, Terminal: pane.Pane.Terminal, Workspace: pane.Pane.Workspace, Reference: pane.Pane.Reference}
				if verifiedForegroundRun(context.Background(), s.HerdrSocket, s.HerdrSocketIdentity, agent, views) != i {
					return nil, nil
				}
				name = codexTerminalName(names, pane.Pane.Title)
			}
			if name == "" {
				return nil, nil
			}
			var tab struct {
				Tab struct {
					ID    string `json:"tab_id"`
					Label string `json:"label"`
				} `json:"tab"`
			}
			if agentRPC(s.HerdrSocket, "tab.get", map[string]any{"tab_id": s.HerdrTab}, &tab) != nil || tab.Tab.ID != s.HerdrTab {
				return nil, nil
			}
			for _, label := range []string{pane.Pane.Label, tab.Tab.Label} {
				if label != "" && label != s.Name && label != name {
					// A name set directly in Herdr takes precedence too.
					all[i].AutoNamePending = false
					return all, nil
				}
			}
			var result map[string]any
			if err := agentRPC(s.HerdrSocket, "pane.rename", map[string]any{"pane_id": s.HerdrPane, "label": name}, &result); err != nil {
				return nil, err
			}
			if err := agentRPC(s.HerdrSocket, "tab.rename", map[string]any{"tab_id": s.HerdrTab, "label": name}, &result); err != nil {
				return nil, err
			}
			all[i].Name = name
			all[i].AutoNamePending = false
			return all, nil
		}
		return nil, nil
	})
}

// An upgrade cannot change observers already loaded by running clients. This
// maintenance entry point applies the same checks without restarting a client.
func refreshAutomaticSessionNames() error {
	var records []session
	if err := withSessions(func(all []session) ([]session, error) { records = append(records, all...); return nil, nil }); err != nil {
		return err
	}
	updated := 0
	for _, s := range records {
		if !s.AutoNamePending {
			continue
		}
		captureAutomaticSessionName(s.ID, s.RunToken)
		if next, err := getSession(s.ID); err == nil && next.Name != s.Name && !next.AutoNamePending {
			updated++
		}
	}
	fmt.Printf("Updated %d automatic session names.\n", updated)
	return nil
}
