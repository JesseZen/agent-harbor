package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Public status must explain upstream uncertainty without inspecting a private
// client lock, even when old compatibility records retain their version.
func TestReplacementStatusExcludesPrivateWriterLock(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	b.env = append(b.env, "AGENT_HARBOR_REPLACEMENT_CANDIDATE=1")
	s := b.sessions()[0]
	s.Version = 3
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.CLIVersion = "codex-cli 0.167.0"
	s.CodexHome = filepath.Join(b.root, "codex")
	s.Recovery = "ready"
	s.ConversationIDSource = "captured"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", s.CodexSessionID}, Provenance: "herdr-pane-observation", ObservedAt: s.CreatedAt, Pane: "old", SocketIdentity: "old", RunToken: "old", Status: "observed"}
	// A directory here makes private writer inspection fail independently of flock.
	if err := os.MkdirAll(filepath.Join(s.CodexHome, "thread-writer-locks", s.CodexSessionID+".lock"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]session{s})
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("show", s.ID)
	if err != nil {
		t.Fatal(out, err)
	}
	if strings.Contains(out, "writer lock cannot") {
		t.Fatalf("replacement inspected private lock: %s", out)
	}
	var view sessionView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Runtime.Presence != "historical_exit" {
		t.Fatalf("replacement depends on legacy rollout or lock: %s", out)
	}
	out, err = b.command("resume", s.ID)
	if err == nil || !strings.Contains(out, "explicit upstream query") {
		t.Fatal("replacement used legacy recovery syntax", out, err)
	}
}

func TestReplacementRejectsWrapperBeforeClientExecution(t *testing.T) {
	b := newTestBridge(t)
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\necho ran > \"$CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "AGENT_HARBOR_REPLACEMENT_CANDIDATE=1")
	out, err := b.command("create", "--dir", b.project)
	if err == nil || !strings.Contains(out, "native executable") {
		t.Fatalf("wrapper was not rejected: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("unsupported wrapper ran")
	}
}

func TestReplacementPickerOffersExposedNativeSubset(t *testing.T) {
	t.Setenv("AGENT_HARBOR_IDENTITY_CANDIDATE", "")
	t.Setenv("AGENT_HARBOR_REPLACEMENT_CANDIDATE", "1")
	m, _ := formFixture()
	m.screen = "new-cli"
	m.environment.Clients = []clientCapability{{Target: "future-kind", Label: "Future client", Command: "future-kind", Available: true, Integration: "current"}}
	m = appKey(m, "down").(harborModel)
	m = appKey(m, "enter").(harborModel)
	if m.screen != "new-form" || m.draft.Client != "future-kind" {
		t.Fatal("replacement flag hides upstream native subset", m.screen, m.draft)
	}
}

func TestReplacementDraftReachesManagedDiscovery(t *testing.T) {
	b := newTestBridge(t)
	b.env = append(b.env, "AGENT_HARBOR_REPLACEMENT_CANDIDATE=1")
	out, err := b.command("create", "--dir", b.project, "--client", "future-kind")
	if err == nil || !strings.Contains(out, "managed Herdr") {
		t.Fatal("replacement draft stopped before discovery", out, err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("silently substituted Codex")
	}
}

func TestReplacementFailedLaunchRollsBack(t *testing.T) {
	b := newTestBridge(t)
	b.env = append(b.env, "AGENT_HARBOR_REPLACEMENT_CANDIDATE=1")
	// Native header passes the safe discovery contract; the OS refuses this
	// deliberately incomplete executable after beginRun has reserved the record.
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), []byte("\x7fELFbroken"), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("create", "--dir", b.project)
	if err == nil || !strings.Contains(out, "client could not start") {
		t.Fatal(out, err)
	}
	records := b.sessions()
	if len(records) != 1 || records[0].Started || records[0].RunToken != "" || records[0].ProcessGroup != 0 || records[0].Conversation != nil {
		t.Fatalf("failed launch left a running or substituted conversation: %+v", records)
	}
}
