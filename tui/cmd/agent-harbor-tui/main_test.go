package main

import (
	"path/filepath"
	"testing"
)

func TestParseOptionsRequiresExplicitIdentityAndCleanUnixPath(t *testing.T) {
	t.Setenv("AGENT_HARBOR_ADMIN_SOCKET", "")
	t.Setenv("AGENT_HARBOR_INSTANCE_ID", "")
	directory := t.TempDir()
	configuration, err := parseOptions([]string{
		"--admin-socket", filepath.Join(directory, "admin.sock"),
		"--instance-id", "ins_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--core-version", "v3-test",
		"--core-binary", filepath.Join(directory, "agent-harbor-core"),
	})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if configuration.instanceID != "ins_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || configuration.coreVersion != "v3-test" {
		t.Fatalf("configuration = %#v", configuration)
	}
	if _, err := parseOptions([]string{"--admin-socket", "relative.sock", "--instance-id", "ins_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err == nil {
		t.Fatal("relative Admin socket was accepted")
	}
}

func TestParseOptionsDebugUISkipsCoreIdentity(t *testing.T) {
	t.Setenv("AGENT_HARBOR_ADMIN_SOCKET", "")
	t.Setenv("AGENT_HARBOR_INSTANCE_ID", "")
	configuration, err := parseOptions([]string{"--debug-ui"})
	if err != nil {
		t.Fatalf("parseOptions --debug-ui: %v", err)
	}
	if !configuration.debugUI {
		t.Fatal("expected debugUI")
	}
}
