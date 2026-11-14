package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentKeepsLaunchProjectWhenPaneIsHome(t *testing.T) {
	newTestRegistry(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	for _, p := range []string{home, project} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", root)
	t.Setenv("HOME", home)
	t.Setenv("AGENT_HARBOR_BRIDGE_LAUNCH_CWD", project)
	t.Setenv("HERDR_ACTIVE_PANE_CWD", home)
	env, err := (harborService{}).Environment()
	if err != nil {
		t.Fatal(err)
	}
	if env.Directory != project {
		t.Fatalf("directory %q, wanted launch project %q", env.Directory, project)
	}
	t.Setenv("HERDR_ACTIVE_PANE_CWD", root)
	env, err = (harborService{}).Environment()
	if err != nil || env.Directory != root {
		t.Fatalf("active project: %q %v", env.Directory, err)
	}
}

func TestManagementContextRequiresMatchingPaneAndWorkspaceIdentity(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "hctx-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	if err := ensureHome(root); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "herdr", "s.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_ACTIVE_PANE_ID", "pane-a")
	err = withSessions(func([]session) ([]session, error) {
		return []session{{ID: "one", Mode: "magpie", InstanceID: "gateway-b", HerdrPane: "pane-a", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := (harborService{}).ManagementContext(); err != nil || id != "gateway-b" {
		t.Fatalf("context %q %v", id, err)
	}
	t.Setenv("HERDR_ACTIVE_PANE_ID", "another-pane")
	if id, err := (harborService{}).ManagementContext(); err != nil || id != "" {
		t.Fatalf("unrelated pane %q %v", id, err)
	}
	t.Setenv("HERDR_ACTIVE_PANE_ID", "pane-a")
	t.Setenv("HERDR_SOCKET_PATH", socket+"-other")
	if id, err := (harborService{}).ManagementContext(); err == nil || id != "" {
		t.Fatalf("changed workspace %q %v", id, err)
	}
}

func TestWizardEnvironmentReflectsPersistedDefaultChanges(t *testing.T) {
	newTestRegistry(t)
	for _, id := range []string{"a", "b"} {
		if err := withMagpieRegistry(func(r *magpieRegistry) error { r.Default = id; return nil }); err != nil {
			t.Fatal(err)
		}
		env, err := (harborService{}).Environment()
		if err != nil {
			t.Fatal(err)
		}
		if env.DefaultInstance != id {
			t.Fatalf("default %q want %q", env.DefaultInstance, id)
		}
	}
}
