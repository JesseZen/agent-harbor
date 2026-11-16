package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIOnlyCatalogAndResume(t *testing.T) {
	visibilityTestRoot(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/hello" {
			w.Write([]byte(`{"name":"magpie","codex_caller_only":true}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"relay/api","display_name":"API"}]}`))
	}))
	defer server.Close()
	models, err := fetchCatalogWithOfficial(server.URL, nil)
	if err != nil || len(models) != 1 || models[0].ID != "relay/api" {
		t.Fatal(models, err)
	}
	s := session{CLI: "codex", Mode: "magpie", CodexConnection: "api", MagpieURL: server.URL, MagpieModel: "relay/api"}
	start := strings.Join(apiOnlyCodexArgs(s, newConversation), " ")
	resume := strings.Join(apiOnlyCodexArgs(s, resumeConversation), " ")
	if !strings.Contains(start, `requires_openai_auth=false`) || strings.Contains(resume, `model_provider=`) || strings.Contains(resume, `model=`) || strings.Contains(resume, `model_reasoning_effort=`) {
		t.Fatal(start, resume)
	}
	if joinedCodex(s) {
		t.Fatal("API connection became joined")
	}
}

func TestBundledCodexAuthLookup(t *testing.T) {
	if os.Getenv("HARBOR_AUTH_LOOKUP_CHILD") == "1" {
		path, err := codexAuthProgram()
		if err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(path).Output()
		if err != nil || string(output) != "bundled" {
			t.Fatal("PATH tool used", err)
		}
		return
	}
	root := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	copy := filepath.Join(root, "harbor-test")
	if err = os.WriteFile(copy, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "codex-auth"), []byte("#!/bin/sh\nprintf bundled"), 0700); err != nil {
		t.Fatal(err)
	}
	pathTool := filepath.Join(root, "path")
	os.Mkdir(pathTool, 0700)
	os.WriteFile(filepath.Join(pathTool, "codex-auth"), []byte("#!/bin/sh\nprintf wrong"), 0700)
	cmd := exec.Command(copy, "-test.run=^TestBundledCodexAuthLookup$")
	cmd.Env = replaceEnvironment(os.Environ(), map[string]*string{"AGENT_HARBOR_CODEX_AUTH_BIN": nil, "PATH": &pathTool})
	cmd.Env = append(cmd.Env, "HARBOR_AUTH_LOOKUP_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
}
