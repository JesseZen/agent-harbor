package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func remoteFixture(t *testing.T) (testBridge, remoteWorkspace) {
	t.Helper()
	b := newTestBridge(t)
	stub := `#!/bin/sh
case "$*" in
 *--version*) echo 'herdr 0.9.5';;
 *'session list --json'*) echo '{"sessions":[{"name":"agent-harbor","running":true}]}';;
 *'plugin link'*) echo '{}';;
 *) printf '%s\n%s\n' "$XDG_CONFIG_HOME" "$AGENT_HARBOR_BRIDGE_HOME" > "$CAPTURE"; printf '%s\n' "$@" >> "$CAPTURE";;
esac
`
	if err := os.WriteFile(filepath.Join(b.root, "bin", "herdr"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	// A running fixture must have the matching managed configuration first.
	stub = strings.Replace(stub, `echo '{"sessions":[{"name":"agent-harbor","running":true}]}'`, `if test -f "$HOME/server-started"; then echo '{"sessions":[{"name":"agent-harbor","running":true}]}'; else echo '{"sessions":[]}'; fi`, 1)
	stub = strings.Replace(stub, ` *'plugin link'*)`, " *' server') touch \"$HOME/server-started\";;\n *'plugin link'*)", 1)
	if err := os.WriteFile(filepath.Join(b.root, "bin", "herdr"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := b.command("remote", "prepare", "--state", filepath.Join(b.root, "bridge"), "--native")
	if err != nil {
		t.Fatalf("prepare: %s %v", output, err)
	}
	var w remoteWorkspace
	if err := json.Unmarshal([]byte(output), &w); err != nil {
		t.Fatalf("descriptor: %s %v", output, err)
	}
	return b, w
}

func TestRemotePreparationKeepsIdentityAndDoesNotExposeEnvironment(t *testing.T) {
	b, w := remoteFixture(t)
	if !validRemoteID(w.ID) || w.Environment != nil || !strings.HasSuffix(w.Executable, w.ID) {
		t.Fatalf("invalid descriptor: %+v", w)
	}
	output, err := b.command("remote", "prepare", "--state", w.State, "--native")
	if err != nil {
		t.Fatalf("second prepare: %s %v", output, err)
	}
	var second remoteWorkspace
	if err := json.Unmarshal([]byte(output), &second); err != nil {
		t.Fatal(err)
	}
	if second.ID != w.ID || second.Executable != w.Executable {
		t.Fatal("preparing existing state changed identity")
	}
	data, err := os.ReadFile(remoteContextPath(w.State))
	if err != nil {
		t.Fatal(err)
	}
	var stored remoteWorkspace
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Environment) == 0 {
		t.Fatal("missing server context")
	}
	info, err := os.Stat(remoteContextPath(w.State))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("context permissions: %v %v", info, err)
	}
}

func TestRemoteBridgePinsWorkspaceAndPreservesPayloadArguments(t *testing.T) {
	b, w := remoteFixture(t)
	output, err := b.command("_remote-herdr", w.State, w.ID, "--session", herdrSession, "pane", "run", "w1:p1", "--session")
	if err != nil {
		t.Fatalf("bridge: %s %v", output, err)
	}
	data, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n--session\nagent-harbor\npane\nrun\nw1:p1\n--session\n") {
		t.Fatalf("payload changed: %s", data)
	}
	before := string(data)
	for _, args := range [][]string{
		{"_remote-herdr", w.State, strings.Repeat("a", 32), "status", "client"},
		{"_remote-herdr", w.State, w.ID, "--session", "foreign", "status", "client"},
	} {
		if output, err := b.command(args...); err == nil {
			t.Fatalf("unsafe bridge accepted: %s", output)
		}
		after, _ := os.ReadFile(filepath.Join(b.root, "capture"))
		if string(after) != before {
			t.Fatal("invalid identity/session invoked Herdr")
		}
	}
	// A new context at the same path cannot satisfy an old saved executable.
	var stored remoteWorkspace
	context, _ := os.ReadFile(remoteContextPath(w.State))
	json.Unmarshal(context, &stored)
	stored.ID = strings.Repeat("b", 32)
	changed, _ := json.Marshal(stored)
	os.WriteFile(remoteContextPath(w.State), changed, 0600)
	if _, err := b.command("_remote-herdr", w.State, w.ID, "status", "client"); err == nil {
		t.Fatal("old bridge crossed workspace replacement")
	}
}

func TestRemoteAddSavesExactServerWorkspaceAndQuotesPreparation(t *testing.T) {
	b := newTestBridge(t)
	id := strings.Repeat("c", 32)
	state := "/srv/remote state"
	w := remoteWorkspace{Version: 1, ID: id, State: state, Session: herdrSession, Executable: filepath.Join(state, "herdr", "bin", "remote-herdr-"+id)}
	data, _ := json.Marshal(w)
	os.WriteFile(filepath.Join(b.root, "descriptor.json"), data, 0600)
	ssh := `#!/bin/sh
printf '%s\n' "$@" > "$HOME/ssh-arguments"
cat "$HOME/descriptor.json"
`
	herdr := `#!/bin/sh
case "$*" in
 *'machine add --help'*) echo '--remote-executable';;
 *'machine list --json'*) echo '[]';;
 *'machine add'*) printf '%s\n' "$@" > "$CAPTURE";;
 *) exit 1;;
esac
`
	os.WriteFile(filepath.Join(b.root, "bin", "ssh"), []byte(ssh), 0700)
	os.WriteFile(filepath.Join(b.root, "bin", "herdr"), []byte(herdr), 0700)
	binary := "/srv/Harbor's install/agent-harbor"
	out, err := b.command("remote", "add", "Build", "--ssh", "workbox", "--binary", binary, "--state", state, "--native")
	if err != nil {
		t.Fatalf("add: %s %v", out, err)
	}
	captured, _ := os.ReadFile(filepath.Join(b.root, "capture"))
	if !strings.Contains(string(captured), "--remote-executable\n"+w.Executable+"\n") {
		t.Fatalf("wrong endpoint: %s", captured)
	}
	sshArgs, _ := os.ReadFile(filepath.Join(b.root, "ssh-arguments"))
	if !strings.Contains(string(sshArgs), shellQuote(binary)+" 'remote' 'prepare' '--state' "+shellQuote(state)+" '--native'") {
		t.Fatalf("unsafe preparation: %s", sshArgs)
	}
	os.Remove(filepath.Join(b.root, "capture"))
	w.ID = "invalid"
	data, _ = json.Marshal(w)
	os.WriteFile(filepath.Join(b.root, "descriptor.json"), data, 0600)
	if _, err := b.command("remote", "add", "Build", "--ssh", "workbox", "--binary", binary); err == nil {
		t.Fatal("invalid descriptor saved")
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatal("Herdr targeted with invalid workspace")
	}
}
