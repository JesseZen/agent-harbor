package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryRecoveryRequiresObservedReference(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.Recovery = "ready"
	data, _ := json.Marshal([]session{s})
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("resume", "--development-query", s.ID)
	if err == nil || !strings.Contains(out, "upstream-observed reference") {
		t.Fatalf("wanted missing provenance refusal: %s %v", out, err)
	}
}

func TestQueryRecoveryRejectsEmptyOperandsWithoutLegacyFallback(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.Version = 3
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.Recovery = "ready"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", s.CodexSessionID}, Provenance: "herdr-pane-observation", ObservedAt: s.CreatedAt, Pane: "old", SocketIdentity: "old", RunToken: "old", Status: "observed"}
	data, _ := json.Marshal([]session{s})
	os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600)
	os.Remove(filepath.Join(b.root, "capture"))
	herdr := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"version\":1,\"kind\":\"codex\",\"argv\":[\"codex\",\"resume\",\"\"]}'\n"), 0700)
	b.env = append(b.env, "AGENT_HARBOR_HERDR_BIN="+herdr, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=new", "HERDR_SOCKET_PATH="+queryTestSocket(t, b))
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", s.ID)
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader("accept\nyes\n")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "invalid Herdr query") {
		t.Fatalf("empty operand accepted: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("client executed")
	}
}

func TestQueryRecoveryFailsClosedForIncompatibleResponses(t *testing.T) {
	for _, response := range []string{`{"version":2,"kind":"codex","argv":["codex","resume","saved"]}`, `{"version":1,"kind":"claude","argv":["claude","--resume","saved"]}`, `{"version":1,"kind":"codex","argv":[]}`, `not json`} {
		t.Run(response, func(t *testing.T) {
			b := newTestBridge(t)
			if out, err := b.command("create", "--dir", b.project); err != nil {
				t.Fatal(out, err)
			}
			s := b.sessions()[0]
			s.Version = 3
			s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
			s.Recovery = "ready"
			s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", s.CodexSessionID}, Provenance: "herdr-pane-observation", ObservedAt: s.CreatedAt, Pane: "old", SocketIdentity: "old", RunToken: "old", Status: "observed"}
			data, _ := json.Marshal([]session{s})
			os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600)
			os.Remove(filepath.Join(b.root, "capture"))
			herdr := filepath.Join(b.root, "bin", "herdr")
			os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '"+response+"'\n"), 0700)
			b.env = append(b.env, "AGENT_HARBOR_HERDR_BIN="+herdr, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=new", "HERDR_SOCKET_PATH="+queryTestSocket(t, b))
			cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", s.ID)
			cmd.Env = b.env
			cmd.Stdin = strings.NewReader("accept\nyes\n")
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "incompatible Herdr query") {
				t.Fatalf("%s %v", out, err)
			}
			if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
				t.Fatal("client launched")
			}
			if got := b.sessions()[0]; got.RunToken != s.RunToken {
				t.Fatal("query failure mutated run")
			}
		})
	}
}

func TestNeutralConflictRequiresReviewAndPreservesRecord(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.Version = 3
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.Recovery = "ready"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", "other"}, Provenance: "herdr-pane-observation"}
	data, _ := json.Marshal([]session{s})
	path := filepath.Join(b.root, "bridge", "sessions.json")
	os.WriteFile(path, data, 0600)
	out, err := b.command("resume", s.ID)
	if err == nil || !strings.Contains(out, "conflicting legacy") {
		t.Fatal(out, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("conflict rewritten")
	}
}

func queryTestSocket(t *testing.T, b testBridge) string {
	t.Helper()
	// Exercise a genuinely dialable owned socket on Darwin as well as Linux.
	// A hardlink with a long pathname passes inode checks but cannot be dialed.
	root := filepath.Join(b.root, "bridge")
	actualConfig := filepath.Join(root, "herdr", "config")
	if err := os.MkdirAll(actualConfig, 0700); err != nil {
		t.Fatal(err)
	}
	config, err := managedConfigContext(root)
	if err != nil {
		t.Fatal(err)
	}
	if config != actualConfig {
		t.Cleanup(func() { os.Remove(config) })
	}
	dir := filepath.Join(config, "herdr", "sessions", herdrSession)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "herdr.sock")
	// Repeated prepareQuery calls in one fixture share its owned API listener.
	if socketIdentity(path) != "" {
		return path
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var req any
				json.NewDecoder(c).Decode(&req)
				json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []any{}}})
			}()
		}
	}()
	return path
}

func TestQueryRecoveryExecutesUpstreamArgumentsInOriginalProject(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.Version = 3
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.Recovery = "ready"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", s.CodexSessionID}, Provenance: "herdr-pane-observation", ObservedAt: s.CreatedAt, Pane: "old", SocketIdentity: "old", RunToken: "old", Status: "observed"}
	data, _ := json.Marshal([]session{s})
	os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600)
	os.Remove(filepath.Join(b.root, "capture"))
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > \"$CAPTURE\"\n"), 0700)
	herdr := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"version\":1,\"kind\":\"codex\",\"argv\":[\"codex\",\"upstream-defined\",\"opaque argument\"]}'\n"), 0700)
	b.env = append(b.env, "AGENT_HARBOR_HERDR_BIN="+herdr, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=new", "HERDR_SOCKET_PATH="+queryTestSocket(t, b))
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", s.ID)
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader("accept\nyes\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	capture, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(capture), b.project+"\n--no-daemon\n-c\ncheck_for_update_on_startup=false\nupstream-defined\nopaque argument\n") {
		t.Fatalf("upstream argv not preserved: %s", capture)
	}
	got := b.sessions()[0]
	if got.CodexSessionID != s.CodexSessionID || got.Mode != s.Mode {
		t.Fatal("conversation or connection replaced")
	}
}

func TestQueryRecoveryRechecksApprovalAfterQuery(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.Version = 3
	s.CodexSessionID = "11111111-1111-4111-8111-111111111111"
	s.Recovery = "ready"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", s.CodexSessionID}, Provenance: "herdr-pane-observation", ObservedAt: s.CreatedAt, Pane: "old", SocketIdentity: "old", RunToken: "old", Status: "observed"}
	data, _ := json.Marshal([]session{s})
	os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600)
	os.Remove(filepath.Join(b.root, "capture"))
	changed := s
	changed.MagpieModel = "changed after approval"
	data, _ = json.Marshal([]session{changed})
	os.WriteFile(filepath.Join(b.root, "changed.json"), data, 0600)
	herdr := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\ncp \"$QUERY_CHANGED_RECORD\" \"$AGENT_HARBOR_BRIDGE_HOME/sessions.json\"\nprintf '%s\\n' '{\"version\":1,\"kind\":\"codex\",\"argv\":[\"codex\",\"resume\",\"saved\"]}'\n"), 0700)
	b.env = append(b.env, "QUERY_CHANGED_RECORD="+filepath.Join(b.root, "changed.json"), "AGENT_HARBOR_HERDR_BIN="+herdr, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=new", "HERDR_SOCKET_PATH="+queryTestSocket(t, b))
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", s.ID)
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader("accept\nyes\n")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "session changed") {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("client launched after record change")
	}
}

func TestQueryRecoveryDefersOpaqueCodexReferenceToHerdr(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--dir", b.project); err != nil {
		t.Fatal(out, err)
	}
	s := b.sessions()[0]
	s.Version = 3
	s.CodexSessionID = ""
	s.Recovery = "ready"
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{"herdr:codex", "codex", "id", "opaque-upstream-reference"}, Provenance: "herdr-pane-observation", ObservedAt: s.CreatedAt, Pane: "old", SocketIdentity: "old", RunToken: "old", Status: "observed"}
	data, _ := json.Marshal([]session{s})
	os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600)
	os.Remove(filepath.Join(b.root, "capture"))
	writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > \"$CAPTURE\"\n"), 0700)
	herdr := filepath.Join(b.root, "bin", "herdr")
	os.WriteFile(herdr, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"version\":1,\"kind\":\"codex\",\"argv\":[\"codex\",\"upstream-defined\",\"opaque argument\"]}'\n"), 0700)
	b.env = append(b.env, "AGENT_HARBOR_HERDR_BIN="+herdr, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=new", "HERDR_SOCKET_PATH="+queryTestSocket(t, b))
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "resume", "--development-query", s.ID)
	cmd.Env = b.env
	cmd.Stdin = strings.NewReader("accept\nyes\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	capture, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(capture), b.project+"\n--no-daemon\n-c\ncheck_for_update_on_startup=false\nupstream-defined\nopaque argument\n") {
		t.Fatalf("upstream argv not preserved: %s", capture)
	}
	got := b.sessions()[0]
	if got.CodexSessionID != s.CodexSessionID || got.Mode != s.Mode {
		t.Fatal("conversation or connection replaced")
	}
}
