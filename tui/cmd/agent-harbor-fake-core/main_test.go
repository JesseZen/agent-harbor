//go:build unix

package main

import (
	"path/filepath"
	"testing"
)

func TestParseConfigRequiresCleanAbsoluteSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	config, err := parseConfig([]string{"--socket", path, "--instance-id", "ins_0123456789abcdef0123456789abcdef"})
	if err != nil || config.socketPath != path {
		t.Fatalf("parseConfig = %#v, %v", config, err)
	}
	if _, err := parseConfig([]string{"--socket", "relative.sock"}); err == nil {
		t.Fatal("relative socket was accepted")
	}
}
