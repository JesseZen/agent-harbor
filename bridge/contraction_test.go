package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractionRefusesSupportedPackage(t *testing.T) {
	cmd := exec.Command("sh", "package.sh", t.TempDir())
	cmd.Env = append(os.Environ(), "AGENT_HARBOR_DEPENDENCIES_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "experimental contraction") {
		t.Fatalf("normal package not protected: %s %v", out, err)
	}
}

func TestCandidatePackageRefusesWrongHerdrSource(t *testing.T) {
	deps := t.TempDir()
	if err := os.MkdirAll(filepath.Join(deps, "herdr"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deps, "herdr", "herdr-candidate-linux-amd64.sha256"), []byte(strings.Repeat("0", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deps, "herdr", "herdr-candidate-linux-amd64.source"), []byte("stock-release"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "package.sh", t.TempDir())
	cmd.Env = append(os.Environ(), "AGENT_HARBOR_DEVELOPMENT_CANDIDATE=1", "AGENT_HARBOR_DEPENDENCIES_DIR="+deps, "AGENT_HARBOR_CANDIDATE_TARGETS=linux-amd64")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Wrong Herdr candidate source") {
		t.Fatalf("wrong candidate accepted: %s %v", out, err)
	}
}
