package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeCodexNPMInstallation(t *testing.T) {
	for _, layout := range []string{"optional", "hoisted", "legacy"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			pkg := filepath.Join(root, "node_modules", "@openai", "codex")
			entry := filepath.Join(pkg, "bin", "codex.js")
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write(entry, "#!/usr/bin/env node\nthrow Error('launcher must not execute');\n")
			write(filepath.Join(pkg, "package.json"), `{"name":"@openai/codex","bin":{"codex":"bin/codex.js"}}`)
			arch, triple := "x64", "x86_64-unknown-linux-musl"
			if runtime.GOARCH == "arm64" {
				arch, triple = "arm64", "aarch64-unknown-linux-musl"
			}
			if runtime.GOOS == "darwin" {
				triple = map[string]string{"amd64": "x86_64-apple-darwin", "arm64": "aarch64-apple-darwin"}[runtime.GOARCH]
			}
			vendor := filepath.Join(pkg, "node_modules", "@openai", "codex-"+runtime.GOOS+"-"+arch, "vendor")
			if layout == "hoisted" {
				vendor = filepath.Join(root, "node_modules", "@openai", "codex-"+runtime.GOOS+"-"+arch, "vendor")
			}
			if layout == "legacy" {
				vendor = filepath.Join(pkg, "vendor")
			}
			binary := filepath.Join(vendor, triple, "bin", "codex")
			native, err := os.ReadFile(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			write(binary, string(native))
			bin := filepath.Join(root, "bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(entry, filepath.Join(bin, "codex")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			got, err := nativeCodexPath()
			if err != nil || got != binary {
				t.Fatalf("npm Codex rejected: %q %v", got, err)
			}
			if v := codexVersion(append(os.Environ(), "BRIDGE_STUB_VERSION=1")); !strings.HasPrefix(v, "codex-cli ") {
				t.Fatal("npm version probe failed", v)
			}
			got, err = profileAccountCodex(profileClientEnvironment{Environment: []string{"PATH=" + bin}})
			if err != nil || got != binary {
				t.Fatalf("Profile npm Codex rejected: %q %v", got, err)
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := nativeCodexPath(); err == nil {
				t.Fatal("non-native payload accepted")
			}
			write(binary, string(native))
			write(filepath.Join(pkg, "package.json"), `{"name":"unrelated","bin":{"codex":"bin/codex.js"}}`)
			if _, err := nativeCodexPath(); err == nil {
				t.Fatal("unrelated wrapper accepted")
			}
		})
	}
}

func TestReadyDependenciesSkipFirstRunChoice(t *testing.T) {
	f := &fakeOnboardingService{report: onboardingReport{Ready: true}}
	if err := ensureOnboarding(f); err != nil {
		t.Fatalf("ready first run blocked: %v", err)
	}
	// Opening Harbor must not start or bind a Magpie instance.
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}
