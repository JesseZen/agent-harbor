package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func reviewFixture(t *testing.T) (testBridge, harborService, session) {
	t.Helper()
	b := newTestBridge(t)
	for _, e := range b.env {
		if k, v, ok := strings.Cut(e, "="); ok {
			t.Setenv(k, v)
		}
	}
	h := harborService{}
	s, err := h.Create(sessionDraft{Directory: b.project, Connection: "local"})
	if err != nil {
		t.Fatal(err)
	}
	return b, h, s
}
func TestVerifyPreservesNeutralIdentityAndEvidence(t *testing.T) {
	b, h, s := reviewFixture(t)
	a := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"
	s.Version = 3
	s.CodexSessionID = a
	s.ReportedCodexID = other
	s.ConversationIDSource = "herdr"
	s.RunToken = "stale-owner"
	s.Recovery = "needs_review"
	s.IdentityWarning = "changed identity"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", a}, Provenance: "upstream-observed", Status: "observed"}
	data, _ := json.Marshal([]session{s})
	if err := os.WriteFile(filepath.Join(b.root, "bridge/sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Verify(s, other, b.project); err == nil {
		t.Fatal("conflicting verify accepted")
	}
	v, _ := h.Get(s.ID)
	if !sameSession(s, v.session) {
		t.Fatal("rejected verify changed evidence")
	}
	if err := h.Verify(v.session, a, b.project); err != nil {
		t.Fatal(err)
	}
	got, _ := h.Get(s.ID)
	if got.RunToken != "" || got.Recovery != "ready" || got.IdentityWarning != "" || got.ReportedCodexID != "" || got.ConversationIDSource != "herdr" || got.Conversation.Reference.Value != a || len(got.Reassociations) != 1 {
		t.Fatalf("compatible review lost evidence: %+v", got.session)
	}
	var prior session
	if json.Unmarshal(got.Reassociations[0].Original, &prior) != nil || prior.ReportedCodexID != other || prior.ConversationIDSource != "herdr" {
		t.Fatal("review lost conflicting report/provenance")
	}
	if err := checkSession(got.session, resumeConversation, ""); err != nil {
		t.Fatalf("compatible choice still stranded: %v", err)
	}
	if err := h.Verify(s, a, b.project); err == nil {
		t.Fatal("stale approval accepted")
	}
}
func TestRepeatedReviewHasBoundedIncrementAndPreservesHistory(t *testing.T) {
	_, h, s := reviewFixture(t)
	ref := herdrIdentity{"herdr:codex", "codex", "id", "11111111-1111-4111-8111-111111111111"}
	var previous int
	for i := 0; i < 12; i++ {
		v, err := h.Get(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Reassociate(sessionApproval{Session: v.session, Confirmed: i%2 == 1}, ref, "review original intent"); err != nil {
			t.Fatal(err)
		}
		v, err = h.Get(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(v.session)
		if i > 2 && len(data)-previous > 2400 {
			t.Fatalf("unbounded review append: %d bytes", len(data)-previous)
		}
		previous = len(data)
		if len(v.Reassociations) != i+1 {
			t.Fatal("history lost")
		}
		for j, d := range v.Reassociations {
			if d.Accepted != (j%2 == 1) || d.Reason != "review original intent" {
				t.Fatal("prior decision changed")
			}
		}
	}
	v, _ := h.Get(s.ID)
	var original session
	if json.Unmarshal(v.Reassociations[0].Original, &original) != nil || !sameSession(original, s) {
		t.Fatal("original baseline lost")
	}
}
func TestCreateRejectsInvalidIntegrationResponses(t *testing.T) {
	valid := `{"integrations":[{"target":"future-kind","command":"future-kind","available":true}]}`
	for name, response := range map[string]string{
		"error-with-result": `{"error":{"code":-1},"result":` + valid + `}`,
		"oversized":         `{"result":` + valid + `,"padding":"` + strings.Repeat("x", 65536) + `"}`,
		"truncated":         `{"result":` + valid,
		"missing-result":    `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			b, h, _ := reviewFixture(t)
			dir, err := os.MkdirTemp("/tmp", "h-review-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "s")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			t.Setenv("HERDR_SOCKET_PATH", socket)
			go func() {
				for {
					c, e := listener.Accept()
					if e != nil {
						return
					}
					go func() {
						defer c.Close()
						var request any
						json.NewDecoder(c).Decode(&request)
						c.Write([]byte(response + "\n"))
					}()
				}
			}()
			before, _ := h.List()
			if _, err := h.Create(sessionDraft{Directory: b.project, Client: "future-kind", Connection: "local"}); err == nil {
				t.Fatal("invalid upstream response granted client eligibility")
			}
			after, _ := h.List()
			if len(before) != len(after) {
				t.Fatal("failed discovery persisted intent")
			}
			clients, explanation := discoverClientSubset()
			if clients != nil || !strings.Contains(explanation, "unavailable") {
				t.Fatal("discovery display accepted invalid transport")
			}
		})
	}
}

func TestCreateStoreFailureDoesNotPublishFallback(t *testing.T) {
	b, h, _ := reviewFixture(t)
	path := filepath.Join(b.root, "bridge/sessions.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Create(sessionDraft{Directory: b.project, Connection: "local", Name: "Must not survive"}); err == nil {
		t.Fatal("store failure reported success")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".retained", path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing record changed")
	}
	views, err := h.List()
	if err != nil || len(views) != 1 || views[0].Name == "Must not survive" {
		t.Fatal("failed create published fallback")
	}
}
func TestCreateReadsIntegrationFrameWithoutSocketEOF(t *testing.T) {
	b, h, _ := reviewFixture(t)
	dir, err := os.MkdirTemp("/tmp", "h-frame-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	release := make(chan struct{})
	defer close(release)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var request any
		json.NewDecoder(c).Decode(&request)
		c.Write([]byte(`{"result":{"integrations":[{"target":"future-kind","command":"future-kind","available":true}]}}` + "\n"))
		<-release
	}()
	s, err := h.Create(sessionDraft{Directory: b.project, Client: "future-kind", Connection: "local", Name: "Selected client"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.Get(s.ID)
	if err != nil || v.CLI != "future-kind" || v.Name != "Selected client" {
		t.Fatal("framed response lost selected client")
	}
}
func TestReviewPreservesExistingNestedAudit(t *testing.T) {
	b, h, s := reviewFixture(t)
	ref := herdrIdentity{"herdr:codex", "codex", "id", "11111111-1111-4111-8111-111111111111"}
	// Existing releases serialized older audits inside original_record. Retain
	// those exact snapshots and begin flat appends without rewriting history.
	old := json.RawMessage(`{"cli":"codex","reassociations":[{"reason":"older decision","original_record":{"cli":"codex"}}]}`)
	s.Reassociations = []reassociationDecision{{Original: old, Reference: ref, Reason: "historical review"}}
	data, _ := json.Marshal([]session{s})
	if err := os.WriteFile(filepath.Join(b.root, "bridge/sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Reassociate(sessionApproval{Session: s, Confirmed: true}, ref, "review original intent"); err != nil {
		t.Fatal(err)
	}
	v, err := h.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	json.Unmarshal(v.Reassociations[0].Original, &got)
	json.Unmarshal(old, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("historical nested snapshot changed")
	}
	var flat session
	if json.Unmarshal(v.Reassociations[1].Original, &flat) != nil || len(flat.Reassociations) != 0 || flat.ID != s.ID {
		t.Fatal("new decision recursively embedded history")
	}
}
