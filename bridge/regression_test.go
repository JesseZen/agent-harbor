package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMagpieAddressUsesEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	if err := os.Mkdir(filepath.Join(root, "magpie"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "magpie", "settings.json"), []byte(`{"port":3425}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_ADDR", "127.0.0.1:45678")
	got, err := magpieAddress()
	if err != nil || got != "http://127.0.0.1:45678" {
		t.Fatalf("address = %q, %v", got, err)
	}
}

func TestFailedCodexStartCanRetry(t *testing.T) {
	b := newTestBridge(t)
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), []byte("\x7fELFbroken"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("create", "--directory", b.project, "--mode", "local"); err == nil {
		t.Fatalf("expected start failure: %s", out)
	}
	s := b.sessions()[0]
	if s.Started {
		t.Fatalf("failed exec left session started: %+v", s)
	}
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("start", s.ID); err != nil {
		t.Fatalf("retry failed: %s: %v", out, err)
	}
}
