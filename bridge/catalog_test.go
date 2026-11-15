package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMagpieCatalogPreservesExactModelAndGroupIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			_, _ = w.Write([]byte(`{"name":"magpie"}`))
			return
		}
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"account-a/model","display_name":"Same"},{"id":"routing group/模型","display_name":"Same"}]}`))
	}))
	defer server.Close()
	b := newTestBridge(t)
	out, err := b.command("models", "--url", server.URL, "--json")
	if err != nil {
		t.Fatalf("models: %s: %v", out, err)
	}
	var got []catalogModel
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	want := []catalogModel{{ID: "account-a/model", DisplayName: "Same"}, {ID: "routing group/模型", DisplayName: "Same"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog: got %+v, want %+v", got, want)
	}
}

func TestMagpieRecoveryPinsConnectionAndRejectsMissingModel(t *testing.T) {
	var available atomic.Bool
	available.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			_, _ = w.Write([]byte(`{"name":"magpie"}`))
			return
		}
		if r.URL.Path == "/v1/codex/models" {
			_, _ = w.Write([]byte(`{"models":[{"slug":"relay/model","supported_reasoning_levels":[{"effort":"high"}]}]}`))
			return
		}
		if available.Load() {
			_, _ = w.Write([]byte(`{"data":[{"id":"relay/model"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer server.Close()
	b := newTestBridge(t)
	b.sessions() // initialize owned state
	const id = "0123456789abcdef0123456789abcdef"
	const codexID = "01912345-6789-7abc-8def-0123456789ab"
	ended := time.Now().UTC()
	record := []session{{LastExitAt: &ended, ID: id, WorkingDirectory: b.project, CLI: "codex", Mode: "magpie", MagpieURL: server.URL, MagpieModel: "relay/model", CodexSessionID: codexID, Recovery: "ready", Started: true}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	b.prepareQuery(id)
	profile, err := magpieCodexProfile(id, "relay/model", b.env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "codex", profile+".config.toml"), []byte("# Agent Harbor session "+id+"\nmodel = \"relay/model\"\nmodel_reasoning_effort = \"high\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("resume", "--development-query", id); err == nil || !strings.Contains(out, "identity unknown") {
		t.Fatalf("unverified legacy identity: %s %v", out, err)
	}
	out, err := b.command("resume", "--development-query", "--override-instance", "isolated legacy fixture identity reviewed", id)
	if err != nil {
		t.Fatalf("resume: %s: %v", out, err)
	}
	if got := b.sessions()[0]; len(got.Overrides) != 1 || got.Overrides[0].Reason != "isolated legacy fixture identity reviewed" {
		t.Fatalf("override not recorded: %+v", got)
	}
	args, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`model="relay/model"`, `model_reasoning_effort="high"`, `base_url="` + server.URL + `/v1"`, "resume\n" + codexID} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("missing %q in %s", want, args)
		}
	}
	if out, err := b.command("verify", "--directory", b.project, id, codexID); err != nil {
		t.Fatalf("verify: %s: %v", out, err)
	}
	b.prepareQuery(id)
	available.Store(false)
	if err := os.Remove(filepath.Join(b.root, "capture")); err != nil {
		t.Fatal(err)
	}
	out, err = b.command("resume", "--development-query", "--override-instance", "isolated legacy fixture identity reviewed", id)
	if err == nil || !strings.Contains(out, "unavailable") {
		t.Fatalf("removed model: %s", out)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("Codex launched with removed model")
	}
}

func TestCatalogErrorDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			_, _ = w.Write([]byte(`{"name":"magpie"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("untrusted server diagnostic"))
	}))
	defer server.Close()
	b := newTestBridge(t)
	out, err := b.command("models", "--url", server.URL)
	if err == nil || !strings.Contains(out, "401") || strings.Contains(out, "untrusted server diagnostic") {
		t.Fatalf("catalog error: %s", out)
	}
}

func TestCatalogRejectsAnotherLocalService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			t.Error("queried a service that did not identify itself as Magpie")
		}
		_, _ = w.Write([]byte(`{"name":"other"}`))
	}))
	defer server.Close()
	if _, err := fetchCatalog(server.URL); err == nil || !strings.Contains(err.Error(), "did not identify itself") {
		t.Fatalf("unexpected identity check result: %v", err)
	}
}
