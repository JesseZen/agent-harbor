package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAutomaticSessionNameUsesObservedConversationOnce(t *testing.T) {
	for _, scenario := range []string{"automatic", "manual-harbor", "manual-herdr", "wrong-conversation", "unobserved", "wrong-run", "wrong-home", "unfinished-index"} {
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
			s := session{Version: 3, ID: "0123456789abcdef0123456789abcdef", CLI: "codex", Name: "project · local", AutoNamePending: true, RunToken: "run", CodexHome: os.Getenv("CODEX_HOME"), HerdrPane: "w1:p1", HerdrTab: "w1:t1", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket)}
			s.Conversation = &conversationEvidence{Reference: ref, Status: "observed", RunToken: s.RunToken, Pane: s.HerdrPane, SocketIdentity: s.HerdrSocketIdentity}
			if scenario == "unobserved" {
				s.Conversation.Status = "initial_unverified"
			}
			var mu sync.Mutex
			paneLabel, tabLabel := s.Name, s.Name
			renamed := 0
			if scenario == "manual-herdr" {
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
							observed := ref
							if scenario == "wrong-conversation" {
								observed.Value = "other"
							}
							result = map[string]any{"pane": map[string]any{"pane_id": s.HerdrPane, "tab_id": s.HerdrTab, "label": paneLabel, "agent_session": observed}}
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
			data := `{"id":"another-thread","thread_name":"Wrong title"}` + "\n" + `{"id":"` + conversation + `","thread_name":"修复登录页面"}`
			if scenario != "unfinished-index" {
				data += "\n"
			}
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			captureAutomaticSessionName(s.ID, s.RunToken)
			// Later Codex changes must not replace the first title, including after
			// persistence/reload. Manual naming still works at any time.
			os.WriteFile(path, []byte(`{"id":"`+conversation+`","thread_name":"Later title"}`+"\n"), 0600)
			if scenario == "automatic" {
				captureAutomaticSessionName(s.ID, s.RunToken)
			}
			got, err := getSession(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if scenario == "automatic" {
				if got.Name != "修复登录页面" || got.AutoNamePending || paneLabel != got.Name || tabLabel != got.Name || renamed != 2 || got.Conversation.Reference != ref {
					t.Fatalf("title not applied once to the same conversation: %+v labels %q %q writes %d", got, paneLabel, tabLabel, renamed)
				}
			} else if renamed != 0 {
				t.Fatalf("overwrote manual or uncertain target: %d", renamed)
			}
		})
	}
}

func TestSessionNamePolicyAtCreation(t *testing.T) {
	visibilityTestRoot(t)
	for _, name := range []string{"", "My session"} {
		s, err := createSessionIntent(sessionDraft{Directory: "/project", Client: "codex", Connection: "local", Name: name}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if s.AutoNamePending != (name == "") {
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
	if name := codexConversationName(s); name != "Profile A" {
		t.Fatalf("read another Profile's name: %q", name)
	}
	if err := os.Rename(ae.Home, ae.Home+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	if name := codexConversationName(s); name != "" {
		t.Fatalf("fell back from unavailable Profile: %q", name)
	}
}
