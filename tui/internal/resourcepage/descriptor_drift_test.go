package resourcepage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResourceDescriptorsMatchGenerated(t *testing.T) {
	repoRoot := findRepoRoot(t)
	script := filepath.Join(repoRoot, "scripts", "check-resource-descriptors.sh")
	cmd := exec.Command(script)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("descriptor drift:\n%s", output)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "scripts", "check-resource-descriptors.sh")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
