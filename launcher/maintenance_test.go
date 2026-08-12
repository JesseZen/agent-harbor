package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectExternalLauncherChecksInterpreterIdentity(t *testing.T) {
	bin := t.TempDir()
	node := filepath.Join(bin, "node")
	codex := filepath.Join(bin, "codex")
	if err := os.WriteFile(node, []byte("native"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/usr/bin/env node\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	detail, err := inspectExternalLauncher("codex", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, codex) || !strings.Contains(detail, node) {
		t.Fatalf("detail=%q", detail)
	}
}

func TestExternalLauncherInterpreterMatchesCorePolicy(t *testing.T) {
	for _, test := range []struct {
		contents string
		want     string
		wantErr  bool
	}{
		{contents: "native", want: ""},
		{contents: "#!/usr/bin/env node\n", want: "node"},
		{contents: "#!/usr/bin/node\n", want: "/usr/bin/node"},
		{contents: "#!/bin/sh\n", wantErr: true},
		{contents: "#!/usr/bin/env -S node\n", wantErr: true},
	} {
		got, err := externalLauncherInterpreter([]byte(test.contents))
		if (err != nil) != test.wantErr || got != test.want {
			t.Fatalf("contents=%q got=%q err=%v", test.contents, got, err)
		}
	}
}

func TestTrustedLauncherUIDAcceptsRootOrCurrentOnly(t *testing.T) {
	if !trustedLauncherUID(0, 1000) || !trustedLauncherUID(1000, 1000) || trustedLauncherUID(1001, 1000) {
		t.Fatal("unexpected launcher ownership policy")
	}
}
