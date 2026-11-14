package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacySessionIsMigratedWithoutLosingConversation(t *testing.T) {
	b := newTestBridge(t)
	b.sessions()
	path := filepath.Join(b.root, "bridge", "sessions.json")
	legacy := `[{"id":"0123456789abcdef0123456789abcdef","working_directory":` + mustJSON(b.project) + `,"cli":"codex","mode":"local","codex_session_id":"01912345-6789-7abc-8def-0123456789ab","recovery":"ready","started":true,"created_at":"2026-11-11T00:00:00Z"}]`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("show", "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(out, err)
	}
	var got map[string]any
	if err = json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(2) || got["name"] == "" || got["conversation_id_source"] != "legacy-unverified" || got["codex_session_id"] != "01912345-6789-7abc-8def-0123456789ab" {
		t.Fatalf("migration: %s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err = json.Unmarshal(data, &records); err != nil || records[0]["version"] != float64(2) {
		t.Fatalf("migration not persisted: %s / %v", data, err)
	}
}
func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestRenameAndRemoveKeepCodexHistory(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatal(out, err)
	}
	id := b.sessions()[0].ID
	if out, err := b.command("rename", id, "My project"); err != nil {
		t.Fatal(out, err)
	}
	out, err := b.command("show", id)
	if err != nil {
		t.Fatal(out, err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "My project" {
		t.Fatal(out)
	}
	history := filepath.Join(b.root, "codex", "rollout-history-kept.jsonl")
	if err := os.WriteFile(history, []byte("conversation"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("remove", id); err != nil {
		t.Fatal(out, err)
	}
	if len(b.sessions()) != 0 {
		t.Fatal("record retained")
	}
	if data, err := os.ReadFile(history); err != nil || string(data) != "conversation" {
		t.Fatal("history changed", err)
	}
}

func TestRemoveCancellationKeepsTheRecord(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatal(out, err)
	}
	id := b.sessions()[0].ID
	// Empty input cancels; the normal helper deliberately supplies confirmation.
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "remove", id)
	cmd.Env = b.env
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "cancelled") {
		t.Fatalf("cancel: %s %v", out, err)
	}
	if len(b.sessions()) != 1 {
		t.Fatal("cancel removed record")
	}
}

func TestNewerSessionStoreIsPreserved(t *testing.T) {
	b := newTestBridge(t)
	b.sessions()
	path := filepath.Join(b.root, "bridge", "sessions.json")
	data := []byte(`[{"version":4,"id":"0123456789abcdef0123456789abcdef"}]`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("sessions", "--json"); err == nil || !strings.Contains(out, "newer") {
		t.Fatal(out, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(data) {
		t.Fatal("newer store overwritten", err)
	}
}

func TestRetiredHistoryCLILeavesHistoryUnchanged(t *testing.T) {
	b := newTestBridge(t)
	history := filepath.Join(b.root, "codex", "history.jsonl")
	if err := os.WriteFile(history, []byte("synthetic preserved history"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("delete-history", "stale-id")
	if err == nil || !strings.Contains(out, "client’s own tools") {
		t.Fatal(out, err)
	}
	data, err := os.ReadFile(history)
	if err != nil || string(data) != "synthetic preserved history" {
		t.Fatal("history changed", err)
	}
}
