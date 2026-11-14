package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLongConfigContext(t *testing.T) {
	b := newTestBridge(t)
	long := filepath.Join(b.root, strings.Repeat("路径", 32))
	b.env = append(b.env, "AGENT_HARBOR_BRIDGE_HOME="+long)
	stub := `#!/bin/sh
case "$*" in
 *--version*) echo 'herdr 0.9.5';;
 *'session list --json'*) if test -f "$HOME/started"; then echo '{"sessions":[{"name":"agent-harbor","running":true}]}'; else echo '{"sessions":[]}'; fi;;
 *'plugin link'*) test $(printf %s "$XDG_CONFIG_HOME/herdr-dev/sessions/agent-harbor/herdr-client.sock" | wc -c) -lt 104;;
 *' server') touch "$HOME/started";;
 *) printf %s "$XDG_CONFIG_HOME" > "$CAPTURE";;
esac
`
	if err := os.WriteFile(filepath.Join(b.root, "bin", "herdr"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if out, err := b.command("open"); err != nil {
			t.Fatalf("open %d: %s %v", i, out, err)
		}
	}
	config, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := filepath.EvalSymlinks(string(config))
	if err != nil || actual != filepath.Join(long, "herdr", "config") {
		t.Fatalf("lost config: %s %v", actual, err)
	}
	if out, err := b.command("_herdr-config-check", long, string(config)); err != nil {
		t.Fatal(out, err)
	}
	// A collision must not be repaired by overwriting another target.
	if err := os.Remove(string(config)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b.project, string(config)); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(string(config))
	if out, err := b.command("open"); err == nil || !strings.Contains(out, "alias changed") {
		t.Fatalf("tampered alias accepted: %s %v", out, err)
	}
	if out, err := b.command("_herdr-config-check", long, string(config)); err == nil {
		t.Fatalf("wrapper check accepted tampering: %s", out)
	}
	target, err := os.Readlink(string(config))
	if err != nil || target != b.project {
		t.Fatal("collision overwritten", err)
	}
	wrapper, err := os.ReadFile(filepath.Join(long, "herdr", "bin", "herdr"))
	if err != nil || !strings.Contains(string(wrapper), string(config)) {
		t.Fatal("wrapper context differs", err)
	}
}

func TestManagedCommandsAcceptOnlyOwnedCanonicalSocket(t *testing.T) {
	b := newTestBridge(t)
	root := filepath.Join(b.root, strings.Repeat("long", 30))
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	if err := ensureHome(root); err != nil {
		t.Fatal(err)
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		t.Fatal(err)
	}
	config := ""
	for _, pair := range env {
		if strings.HasPrefix(pair, "XDG_CONFIG_HOME=") {
			config = strings.TrimPrefix(pair, "XDG_CONFIG_HOME=")
		}
	}
	socket := filepath.Join(config, "herdr", "sessions", herdrSession, "herdr.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer os.Remove(config)
	stub := filepath.Join(b.root, "bin", "herdr")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncase \"$*\" in *'session list --json'*) echo '{\"sessions\":[{\"name\":\"agent-harbor\",\"running\":true}]}' ;; *) exit 0;; esac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_HERDR_BIN", stub)
	s := session{HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket)}
	if err := sessionHerdr(s, "pane", "focus", "test"); err != nil {
		t.Fatalf("owned alias rejected: %v", err)
	}
	s.HerdrSocketIdentity = "stale"
	if err := sessionHerdr(s, "pane", "focus", "test"); err == nil {
		t.Fatal("stale socket accepted")
	}
	foreignDir, err := os.MkdirTemp("/tmp", "hfs-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(foreignDir)
	foreign := filepath.Join(foreignDir, "foreign.sock")
	other, err := net.Listen("unix", foreign)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	nested := filepath.Join(root, "herdr", "foreign")
	if err := os.Symlink(foreignDir, nested); err != nil {
		t.Fatal(err)
	}
	s.HerdrSocket = filepath.Join(nested, "foreign.sock")
	s.HerdrSocketIdentity = socketIdentity(s.HerdrSocket)
	if err := sessionHerdr(s, "pane", "focus", "test"); err == nil {
		t.Fatal("foreign canonical socket accepted")
	}
}
