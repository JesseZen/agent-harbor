package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAutomaticSessionNameUsesObservedConversationOnce(t *testing.T) {
	for _, scenario := range []string{"automatic", "manual-harbor", "manual-herdr", "wrong-conversation", "conflict", "unobserved", "wrong-run", "wrong-home", "unfinished-index", "no-identity", "no-identity-active", "no-identity-wrong-process", "no-identity-unindexed-title", "no-identity-manual"} {
		t.Run(scenario, func(t *testing.T) {
			root := visibilityTestRoot(t)
			t.Setenv(originalEnvKey, "")
			os.Unsetenv(originalEnvKey)
			const conversation = "01912345-6789-7abc-8def-0123456789ab"
			ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: conversation}
			socket := filepath.Join(root, "state.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			s := session{Version: 3, ID: "0123456789abcdef0123456789abcdef", CLI: "codex", Name: "project · local", AutoNamePending: true, AutoNameSync: true, RunToken: "run", CodexHome: os.Getenv("CODEX_HOME"), HerdrPane: "w1:p1", HerdrTab: "w1:t1", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket)}
			if scenario == "no-identity-active" {
				s.AutoNamePending = false
			}
			s.Conversation = &conversationEvidence{Reference: ref, Status: "observed", RunToken: s.RunToken, Pane: s.HerdrPane, SocketIdentity: s.HerdrSocketIdentity}
			if strings.HasPrefix(scenario, "no-identity") || scenario == "conflict" {
				s.ProcessGroup = startSessionProcessGroupFixture(t, root, map[string]string{"AH_SESSION_RUN_TOKEN": s.RunToken})
			}
			if strings.HasPrefix(scenario, "no-identity") {
				s.Conversation = nil
			}
			if scenario == "conflict" {
				s.Conversation.Status = "conflict"
			}
			if scenario == "unobserved" {
				s.Conversation.Status = "initial_unverified"
			}
			var mu sync.Mutex
			paneLabel, tabLabel := s.Name, s.Name
			terminalTitle := "修复登录页面 | project"
			renamed := 0
			if scenario == "manual-herdr" || scenario == "no-identity-manual" {
				paneLabel = "用户自己的名字"
			}
			if scenario == "wrong-run" {
				s.Conversation.RunToken = "old"
			}
			if scenario == "wrong-home" {
				s.CodexHome = filepath.Join(root, "other")
			}
			go func() {
				for {
					c, err := listener.Accept()
					if err != nil {
						return
					}
					func() {
						defer c.Close()
						var req struct {
							Method string            `json:"method"`
							Params map[string]string `json:"params"`
						}
						if json.NewDecoder(c).Decode(&req) != nil {
							return
						}
						mu.Lock()
						defer mu.Unlock()
						var result any
						switch req.Method {
						case "pane.get":
							var observed *herdrIdentity
							if s.Conversation != nil {
								copy := ref
								observed = &copy
							}
							if scenario == "wrong-conversation" || scenario == "conflict" {
								observed.Value = "other"
							}
							if scenario == "no-identity-unindexed-title" {
								terminalTitle = "Unrelated terminal | project"
							}
							result = map[string]any{"pane": map[string]any{"pane_id": s.HerdrPane, "tab_id": s.HerdrTab, "label": paneLabel, "agent_session": observed, "terminal_title_stripped": terminalTitle, "agent": "codex", "terminal_id": "terminal", "workspace_id": "w1"}}
						case "pane.process_info":
							group := s.ProcessGroup
							if scenario == "no-identity-wrong-process" {
								group++
							}
							result = map[string]any{"process_info": map[string]any{"pane_id": s.HerdrPane, "foreground_process_group_id": group}}
						case "tab.get":
							result = map[string]any{"tab": map[string]any{"tab_id": s.HerdrTab, "label": tabLabel}}
						case "pane.rename":
							paneLabel = req.Params["label"]
							renamed++
							result = map[string]any{}
						case "tab.rename":
							tabLabel = req.Params["label"]
							renamed++
							result = map[string]any{}
						}
						json.NewEncoder(c).Encode(map[string]any{"result": result})
					}()
				}
			}()
			if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
				t.Fatal(err)
			}
			if scenario == "manual-harbor" {
				if err := renameSession(s.ID, "我的名字"); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(os.Getenv("CODEX_HOME"), "session_index.jsonl")
			data := `{"id":"01912345-6789-7abc-8def-0123456789ac","thread_name":"Wrong title"}` + "\n" + `{"id":"` + conversation + `","thread_name":"修复登录页面"}`
			if scenario != "unfinished-index" {
				data += "\n"
			}
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "no-identity" {
				if err := refreshAutomaticSessionNames(); err != nil {
					t.Fatal(err)
				}
			} else {
				captureAutomaticSessionName(s.ID, s.RunToken)
			}
			// Later Codex title changes update the same Harbor record.
			os.WriteFile(path, []byte(`{"id":"`+conversation+`","thread_name":"Later title"}`+"\n"), 0600)
			mu.Lock()
			terminalTitle = "Later title | project"
			mu.Unlock()
			if scenario == "automatic" || scenario == "no-identity" {
				captureAutomaticSessionName(s.ID, s.RunToken)
			} else if scenario == "no-identity-active" {
				if err := refreshAutomaticSessionNames(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := getSession(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if scenario == "automatic" || scenario == "no-identity" || scenario == "no-identity-active" {
				if got.Name != "Later title" || got.AutoNamePending || !got.AutoNameSync || paneLabel != got.Name || tabLabel != got.Name || renamed != 4 || (scenario == "automatic" && got.Conversation.Reference != ref) || (scenario == "no-identity" && got.Conversation != nil) {
					t.Fatalf("title changes did not follow the verified Codex title: %+v labels %q %q writes %d", got, paneLabel, tabLabel, renamed)
				}
			} else if renamed != 0 {
				t.Fatalf("overwrote manual or uncertain target: %d", renamed)
			}
		})
	}
}

func TestAutomaticSessionNameFollowsNewConversationTitleWithoutChangingRecoveryTarget(t *testing.T) {
	root := visibilityTestRoot(t)
	oldID := "01912345-6789-7abc-8def-0123456789ab"
	newID := "01912345-6789-7abc-8def-0123456789ac"
	oldRef := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: oldID}
	socket := filepath.Join(root, "state.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s := session{
		Version: 3, ID: "0123456789abcdef0123456789abcdef", CLI: "codex", Name: "project · local",
		AutoNamePending: true, AutoNameSync: true, RunToken: "run", CodexHome: os.Getenv("CODEX_HOME"),
		HerdrPane: "w1:p1", HerdrTab: "w1:t1", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket),
		CodexSessionID: oldID, ProcessGroup: startSessionProcessGroupFixture(t, root, map[string]string{"AH_SESSION_RUN_TOKEN": "run"}),
		Conversation: &conversationEvidence{Reference: oldRef, Status: "observed", RunToken: "run", Pane: "w1:p1", SocketIdentity: socketIdentity(socket)},
	}
	var mu sync.Mutex
	paneLabel, tabLabel, terminalTitle := s.Name, s.Name, "Old title | project"
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				var req struct {
					Method string            `json:"method"`
					Params map[string]string `json:"params"`
				}
				if json.NewDecoder(conn).Decode(&req) != nil {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				var result any
				switch req.Method {
				case "pane.get":
					result = map[string]any{"pane": map[string]any{"pane_id": s.HerdrPane, "tab_id": s.HerdrTab, "label": paneLabel, "agent_session": oldRef, "terminal_title_stripped": terminalTitle, "agent": "codex", "terminal_id": "terminal", "workspace_id": "w1"}}
				case "pane.process_info":
					result = map[string]any{"process_info": map[string]any{"pane_id": s.HerdrPane, "foreground_process_group_id": s.ProcessGroup}}
				case "tab.get":
					result = map[string]any{"tab": map[string]any{"tab_id": s.HerdrTab, "label": tabLabel}}
				case "pane.rename":
					paneLabel = req.Params["label"]
					result = map[string]any{}
				case "tab.rename":
					tabLabel = req.Params["label"]
					result = map[string]any{}
				}
				_ = json.NewEncoder(conn).Encode(map[string]any{"result": result})
			}()
		}
	}()
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(s.CodexHome, "session_index.jsonl")
	if err := os.WriteFile(index, []byte(`{"id":"`+oldID+`","thread_name":"Old title"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	captureAutomaticSessionName(s.ID, s.RunToken)
	if got, _ := getSession(s.ID); got.Name != "Old title" {
		t.Fatalf("initial Codex title was not captured: %q", got.Name)
	}
	if err := os.WriteFile(index, []byte(`{"id":"`+oldID+`","thread_name":"Old title"}`+"\n"+`{"id":"`+newID+`","thread_name":"New title"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	terminalTitle = "New title | project"
	mu.Unlock()
	captureAutomaticSessionName(s.ID, s.RunToken)
	if got, _ := getSession(s.ID); got.Name != "New title" {
		t.Fatalf("/new title did not update while Herdr retained the old reference: %q", got.Name)
	}
	thirdID := "01912345-6789-7abc-8def-0123456789ad"
	thirdRef := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: thirdID}
	if err := os.WriteFile(index, []byte(`{"id":"`+oldID+`","thread_name":"Old title"}`+"\n"+`{"id":"`+newID+`","thread_name":"New title"}`+"\n"+`{"id":"`+thirdID+`","thread_name":"Third title"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	terminalTitle = "Third title | project"
	mu.Unlock()
	if err := withSessions(func(all []session) ([]session, error) {
		all[0].Conversation.Status = "conflict"
		all[0].Conversation.ObservedReference = &thirdRef
		all[0].ReportedCodexID = thirdID
		all[0].Recovery = "needs_review"
		return all, nil
	}); err != nil {
		t.Fatal(err)
	}
	captureAutomaticSessionName(s.ID, s.RunToken)
	got, err := getSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Name != "Third title" || paneLabel != got.Name || tabLabel != got.Name {
		t.Fatalf("/new title did not reach Harbor and Herdr: record=%q pane=%q tab=%q", got.Name, paneLabel, tabLabel)
	}
	if got.CodexSessionID != oldID || got.Conversation.Reference != oldRef || got.ReportedCodexID != thirdID || got.Recovery != "needs_review" {
		t.Fatalf("title sync changed recovery identity or review state: %+v", got)
	}
}

func TestSessionNamePolicyAtCreation(t *testing.T) {
	visibilityTestRoot(t)
	for _, name := range []string{"", "My session"} {
		s, err := createSessionIntent(sessionDraft{Directory: "/project", Client: "codex", Connection: "local", Name: name}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if s.AutoNamePending != (name == "") || s.AutoNameSync != (name == "") {
			t.Fatalf("incorrect name policy: %+v", s)
		}
		if err := renameSession(s.ID, "Manual"); err != nil {
			t.Fatal(err)
		}
		got, _ := getSession(s.ID)
		if got.AutoNamePending || got.Name != "Manual" {
			t.Fatal("manual rename did not disable capture")
		}
	}
}

func TestConversationNameUsesBoundProfile(t *testing.T) {
	h, _ := profileFixture(t)
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	ab, bb := bindingFor(a), bindingFor(b)
	ae, err := h.ResolveProfileClient(profileClientRequest{ab})
	if err != nil {
		t.Fatal(err)
	}
	be, err := h.ResolveProfileClient(profileClientRequest{bb})
	if err != nil {
		t.Fatal(err)
	}
	const id = "01912345-6789-7abc-8def-0123456789ab"
	for _, e := range []struct{ home, title string }{{ae.Home, "Profile A"}, {be.Home, "Profile B"}} {
		if err := os.WriteFile(filepath.Join(e.home, "session_index.jsonl"), []byte(`{"id":"`+id+`","thread_name":"`+e.title+`"}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", be.Home)
	s := session{CLI: "codex", Profile: &ab, CodexHome: ae.Home, Conversation: &conversationEvidence{Reference: herdrIdentity{Agent: "codex", Kind: "id", Value: id}, Status: "observed"}}
	if name := codexNameIndex(s)[id]; name != "Profile A" {
		t.Fatalf("read another Profile's name: %q", name)
	}
	if err := os.Rename(ae.Home, ae.Home+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	if name := codexNameIndex(s)[id]; name != "" {
		t.Fatalf("fell back from unavailable Profile: %q", name)
	}
}
