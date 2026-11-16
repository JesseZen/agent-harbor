package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Codex 0.169.0 writes names to this append-only metadata index. Read only
// the bound home and exact observed ID, never conversation messages or a screen.
func codexConversationName(s session) string {
	if s.CLI != "codex" || s.Conversation == nil || s.Conversation.Status != "observed" ||
		s.Conversation.Reference.Kind != "id" || s.Conversation.Reference.Agent != "codex" ||
		!validCodexID(s.Conversation.Reference.Value) {
		return ""
	}
	e, err := sessionClientEnvironment(s)
	if err != nil {
		return ""
	}
	home := environmentValue(e.Environment, "CODEX_HOME")
	if home == "" {
		home = filepath.Join(environmentValue(e.Environment, "HOME"), ".codex")
	}
	if !filepath.IsAbs(home) || s.CodexHome == "" || filepath.Clean(home) != filepath.Clean(s.CodexHome) {
		return ""
	}
	f, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	// Bound the work on each observation; older names outside the tail can wait
	// for a later Codex update or an explicit manual rename.
	const limit = int64(4 << 20)
	start := max(int64(0), info.Size()-limit)
	r := bufio.NewReader(io.NewSectionReader(f, start, info.Size()-start))
	if start > 0 {
		_, _ = r.ReadString('\n')
	}
	name := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil { // An unfinished append is not a published title.
			break
		}
		var entry struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.ID == s.Conversation.Reference.Value {
			name = strings.TrimSpace(entry.Name)
		}
	}
	if validateSessionName(name) != nil {
		return ""
	}
	return name
}

// Runs from the existing identity observer. Persisting the one-shot policy
// makes restart/resume preserve both automatic and later manual choices.
func captureAutomaticSessionName(id, token string) {
	s, err := getSession(id)
	if err != nil || !s.AutoNamePending || s.RunToken != token || s.Conversation == nil ||
		s.Conversation.RunToken != token || s.Conversation.Pane != s.HerdrPane ||
		s.Conversation.SocketIdentity != s.HerdrSocketIdentity {
		return
	}
	name := codexConversationName(s)
	if name == "" {
		return
	}
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
				} `json:"pane"`
			}
			if agentRPC(s.HerdrSocket, "pane.get", map[string]any{"pane_id": s.HerdrPane}, &pane) != nil ||
				pane.Pane.ID != s.HerdrPane || pane.Pane.Tab != s.HerdrTab || pane.Pane.Reference == nil || *pane.Pane.Reference != s.Conversation.Reference {
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
