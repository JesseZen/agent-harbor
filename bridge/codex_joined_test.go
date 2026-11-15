package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJoinedCodexUsesNativeLoginAndLetsResumeRestoreSelection(t *testing.T) {
	s := session{CLI: "codex", Mode: "magpie", CodexConnection: "caller", MagpieURL: "http://127.0.0.1:3425", MagpieModel: "relay/model"}
	start := joinedCodexArgs(s, newConversation)
	if want := []string{"-c", `model_provider="openai"`, "-c", `openai_base_url="http://127.0.0.1:3425/backend-api/codex"`, "-c", `model="relay/model"`}; !reflect.DeepEqual(start, want) {
		t.Fatalf("start args %q, want %q", start, want)
	}
	resume := joinedCodexArgs(s, resumeConversation)
	if want := []string{"-c", `openai_base_url="http://127.0.0.1:3425/backend-api/codex"`}; !reflect.DeepEqual(resume, want) {
		t.Fatalf("resume args %q, want %q; Codex must restore model and effort", resume, want)
	}
}

func TestJoinedRecoveryUsesCodexSelectionWithoutHarborProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			_, _ = w.Write([]byte(`{"name":"magpie","codex_caller_only":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	b := newTestBridge(t)
	b.sessions()
	const id = "0123456789abcdef0123456789abcdef"
	const thread = "01912345-6789-7abc-8def-0123456789ab"
	ended := time.Now().UTC()
	record := []session{{LastExitAt: &ended, ID: id, WorkingDirectory: b.project, CLI: "codex", Mode: "magpie", CodexConnection: "caller", MagpieURL: server.URL, MagpieModel: "relay/no-longer-in-catalog", CodexSessionID: thread, Recovery: "ready", Started: true}}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	b.prepareQuery(id)
	out, err := b.command("resume", "--development-query", "--override-instance", "isolated fixture reviewed", id)
	if err != nil {
		t.Fatalf("resume: %s: %v", out, err)
	}
	args, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	for _, want := range []string{`openai_base_url="` + server.URL + `/backend-api/codex"`, "resume\n" + thread} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for _, notWanted := range []string{"--profile", "model_provider=", "model_reasoning_effort=", "model_catalog_json=", "model=\""} {
		if strings.Contains(got, notWanted) {
			t.Fatalf("recovery overrides Codex selection with %q: %s", notWanted, got)
		}
	}
}

func TestJoinedCatalogOffersCachedOfficialModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(`{"models":[{"slug":"gpt-example","display_name":"GPT Example","visibility":"list"},{"slug":"internal","visibility":"hide"},{"slug":"old-relay/m","visibility":"list"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			_, _ = w.Write([]byte(`{"name":"magpie","codex_caller_only":true}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"relay/model"}]}`))
		}
	}))
	defer server.Close()
	models, err := fetchCatalog(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	if want := []string{"@codex-default", "gpt-example", "relay/model"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("initial choices %q, want %q", ids, want)
	}
}

func TestJoinedCodexRequiresCallerOnlyGateway(t *testing.T) {
	callerOnly := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if callerOnly {
			_, _ = w.Write([]byte(`{"name":"magpie","codex_caller_only":true}`))
		} else {
			_, _ = w.Write([]byte(`{"name":"magpie"}`))
		}
	}))
	defer server.Close()
	if err := requireCallerOnlyGateway(server.URL); err == nil {
		t.Fatal("an unpatched gateway must not be treated as caller-only")
	}
	callerOnly = true
	if err := requireCallerOnlyGateway(server.URL); err != nil {
		t.Fatalf("caller-only gateway refused: %v", err)
	}
}
