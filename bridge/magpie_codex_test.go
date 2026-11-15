package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMagpieCodexCatalogUsesUpstreamContentAndRejectsOtherAddresses(t *testing.T) {
	const body = `{"models":[{"slug":"group/a","supported_reasoning_levels":[{"effort":"high"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/codex/models" || r.Header.Get("Authorization") != "Bearer "+magpieCodexKey {
			t.Errorf("unexpected catalog request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	path, err := magpieCodexCatalog(server.URL, t.TempDir(), "group/a", "high")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != body {
		t.Fatalf("catalog changed: %s (%v)", content, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("catalog is not private: %v %v", info, err)
	}
	if _, err := magpieCodexCatalog("http://example.com:80", t.TempDir(), "group/a", ""); err == nil {
		t.Fatal("accepted a non-loopback catalog")
	}
	if _, err := magpieCodexCatalog(server.URL, t.TempDir(), "group/hidden", ""); !errors.Is(err, errMagpieCodexModelHidden) {
		t.Fatalf("hidden selection accepted: %v", err)
	}
	if _, err := magpieCodexCatalog(server.URL, t.TempDir(), "group/a", "low"); !errors.Is(err, errMagpieCodexEffortHidden) {
		t.Fatalf("unavailable effort accepted: %v", err)
	}
}

func TestMagpieCodexCatalogFailureDoesNotExposeServerBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("private diagnostic"))
	}))
	defer server.Close()
	_, err := magpieCodexCatalog(server.URL, t.TempDir(), "group/a", "")
	if err == nil || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "private diagnostic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMagpieCodexProfilesAreSeparateAndNeverReplaceExistingFiles(t *testing.T) {
	dir := t.TempDir()
	env := []string{"HOME=" + dir, "CODEX_HOME=" + filepath.Join(dir, "codex")}
	idA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	idB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	nameA, err := magpieCodexProfile(idA, "group/a", env)
	if err != nil {
		t.Fatal(err)
	}
	nameB, err := magpieCodexProfile(idB, "group/b", env)
	if err != nil || nameA == nameB {
		t.Fatalf("profiles are not separate: %q %q %v", nameA, nameB, err)
	}
	profileA := filepath.Join(dir, "codex", nameA+".config.toml")
	if err := os.WriteFile(profileA, []byte("# Agent Harbor session "+idA+"\nmodel = \"relay/model\"\nmodel_reasoning_effort = \"high\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(profileA)
	if _, err := magpieCodexProfile(idA, "group/a", env); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(profileA)
	if string(before) != string(after) {
		t.Fatal("existing profile was rewritten")
	}
	conflict := filepath.Join(dir, "codex", "agent-harbor-cccccccccccccccccccccccccccccccc.config.toml")
	if err := os.WriteFile(conflict, []byte("user settings"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := magpieCodexProfile("cccccccccccccccccccccccccccccccc", "group/c", env); err == nil {
		t.Fatal("accepted someone else's profile")
	}
	contents, _ := os.ReadFile(conflict)
	if string(contents) != "user settings" {
		t.Fatal("overwrote someone else's profile")
	}
}
