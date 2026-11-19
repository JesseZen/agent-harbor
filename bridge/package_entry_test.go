package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspacePackageEntryFollowsVerifiedRelease(t *testing.T) {
	for _, method := range []string{"script", "homebrew"} {
		t.Run(method, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			install := filepath.Join(root, "test's install")
			versions, selector, suffix := filepath.Join(install, "releases"), filepath.Join(install, "current"), "agent-harbor"
			if method == "homebrew" {
				versions = filepath.Join(install, "Cellar", "agent-harbor")
				selector = filepath.Join(install, "opt", "agent-harbor")
				suffix = filepath.Join("libexec", suffix)
			}
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, version := range []string{"old", "new"} {
				executable := filepath.Join(versions, version, suffix)
				write(executable, "#!/bin/sh\nprintf '%s|%s|%s' "+version+" \"$HERDR_BIN_PATH\" \"$1\"\n", 0700)
				for _, dependency := range []string{"herdr", "magpie", "codex-auth"} {
					write(filepath.Join(filepath.Dir(executable), dependency), "fixture", 0700)
				}
				if method == "script" {
					write(filepath.Join(versions, version, ".agent-harbor-package"), "agent-harbor-package-v1\n", 0600)
				}
			}
			if method == "script" {
				write(filepath.Join(install, ".agent-harbor-distribution"), "agent-harbor-distribution-v1\n", 0600)
			}
			if err := os.MkdirAll(filepath.Dir(selector), 0700); err != nil {
				t.Fatal(err)
			}
			selectRelease := func(path string) {
				t.Helper()
				if err := os.Remove(selector); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err := os.Symlink(path, selector); err != nil {
					t.Fatal(err)
				}
			}
			selectRelease(filepath.Join(versions, "old"))
			old := filepath.Join(versions, "old", suffix)
			next := filepath.Join(versions, "new", suffix)
			entry, err := packageEntryFor(old)
			if err != nil || entry.selector == "" {
				t.Fatal(entry, err)
			}
			workspace := workspacePackageEntry(filepath.Join(root, "workspace"))
			if err := entry.installWorkspaceEntry(workspace); err != nil {
				t.Fatal(err)
			}
			selectRelease(filepath.Join(versions, "new"))
			if err := os.Remove(old); err != nil {
				t.Fatal(err)
			}
			if newEntry, err := packageEntryFor(next); err != nil || newEntry != entry || !newEntry.ownsVersion(old) {
				t.Fatal(newEntry, err)
			}
			check := func(herdr, want string) {
				t.Helper()
				cmd := exec.Command(workspace, "_pane")
				cmd.Env = []string{"PATH=/nonexistent", "HERDR_BIN_PATH=" + herdr}
				out, err := cmd.CombinedOutput()
				if err != nil || string(out) != "new|"+want+"|_pane" {
					t.Fatalf("entry output %q: %v", out, err)
				}
			}
			check(filepath.Join(filepath.Dir(old), "herdr"), filepath.Join(filepath.Dir(next), "herdr"))
			check("/opt/operator/herdr", "/opt/operator/herdr")
			selectRelease(filepath.Join(root, "foreign"))
			if _, err := packageEntryFor(next); err == nil {
				t.Fatal("accepted foreign package selector")
			}
			if out, err := exec.Command(workspace, "_app").CombinedOutput(); err == nil || !strings.Contains(string(out), "selector is invalid") {
				t.Fatalf("launcher followed foreign selector: %q, %v", out, err)
			}
		})
	}
}

func TestGeneratedPopupConfigRequiresExactOwnership(t *testing.T) {
	shortcuts := shortcutPreferences{"prefix+t", "ctrl+g", "prefix+f"}
	bin := "/uninstalled/homebrew's keg/libexec/agent-harbor"
	config := configWithShortcuts(bin, shortcuts)
	if got := generatedConfigExecutable(config, shortcuts); got != bin {
		t.Fatalf("generated config resolves to %q", got)
	}
	if got := generatedConfigExecutable(legacyHerdrConfig(bin), defaultShortcuts()); got != bin {
		t.Fatalf("legacy config resolves to %q", got)
	}
	for _, changed := range []string{config + "\n# custom\n", strings.Replace(config, " _app", " _status", 1), strings.Replace(config, "prefix+t", "prefix+d", 1)} {
		if got := generatedConfigExecutable(changed, shortcuts); got != "" {
			t.Fatalf("accepted modified config with executable %q", got)
		}
	}
}
