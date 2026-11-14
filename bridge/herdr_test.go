package main

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOpenIsolatesHerdrAndDisablesAutomaticRecovery(t *testing.T) {
	b := newTestBridge(t)
	herdr := filepath.Join(b.root, "bin", "herdr")
	stub := `#!/bin/sh
case "$*" in
  *"--version"*) printf 'herdr 0.9.5\n';;
  *"session list --json"*)
    if test -f "$HOME/server-started"; then printf '{"sessions":[{"name":"agent-harbor","running":true}]}\n'; else printf '{"sessions":[]}\n'; fi;;
  *"plugin link"*) printf '{}\n';;
  *" server") touch "$HOME/server-started";;
  *) printf '%s\n%s\n%s\n%s\n' "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$HERDR_CONFIG_PATH" > "$CAPTURE";;
esac
`
	if err := os.WriteFile(herdr, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("open")
	if err != nil {
		t.Fatalf("open: %s: %v", out, err)
	}
	got, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(b.root, "bridge", "herdr")
	fields := strings.Split(string(got), "\n")
	if len(fields) != 5 {
		t.Fatalf("unexpected environment %q", got)
	}
	resolved, err := filepath.EvalSymlinks(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	fields[1] = resolved
	got = []byte(strings.Join(fields, "\n"))
	want := b.root + "\n" + filepath.Join(root, "config") + "\n" + filepath.Join(root, "state") + "\n" + filepath.Join(root, "config", "herdr.toml") + "\n"
	if string(got) != want {
		t.Fatalf("Herdr environment: got %q, want %q", got, want)
	}
	config, err := os.ReadFile(filepath.Join(root, "config", "herdr.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "resume_agents_on_restore = false") || !strings.Contains(string(config), "type = \"popup\"") {
		t.Fatalf("unsafe or incomplete Herdr configuration: %s", config)
	}
	b.env = append(b.env, "XDG_STATE_HOME="+filepath.Join(b.root, "other-state"))
	out, err = b.command("open")
	if err == nil || !strings.Contains(out, "different user paths") {
		t.Fatalf("changed paths must not attach to a stale server: %s", out)
	}
}

func TestOpenRejectsUnknownHerdrServerState(t *testing.T) {
	b := newTestBridge(t)
	herdr := filepath.Join(b.root, "bin", "herdr")
	stub := `#!/bin/sh
case "$*" in
  *"--version"*) printf 'herdr 0.9.5\n';;
  *"session list --json"*) printf '{"sessions":[{"name":"agent-harbor","running":false,"connection_error":"socket unavailable"}]}\n';;
  *) printf called > "$CAPTURE";;
esac
`
	if err := os.WriteFile(herdr, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("open")
	if err == nil || !strings.Contains(out, "check its socket and permissions") {
		t.Fatalf("unknown server state: %s", out)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatalf("Herdr started with unknown state: %v", err)
	}
}

func TestOfficialHerdrReportRecordsTheCodexSessionID(t *testing.T) {
	b := newTestBridge(t)
	herdr := filepath.Join(b.root, "bin", "herdr")
	stubHerdr := `#!/bin/sh
if test -f "$HOME/switch"; then
  printf '{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ac"}}}}\n'
elif test -f "$CAPTURE"; then
  printf '{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}\n'
else
  printf '{"result":{"pane":{}}}\n'
fi
`
	stubCodex := "#!/bin/sh\nprintf ready > \"$CAPTURE\"\nread reply\n"
	if err := os.WriteFile(herdr, []byte(stubHerdr), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(stubCodex), 0700); err != nil {
		t.Fatal(err)
	}
	b.sessions()
	b.env = append(b.env, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+herdr)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--directory", b.project)
	cmd.Env = b.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	firstReported := false
	for time.Now().Before(deadline) {
		all := b.sessions()
		if len(all) == 1 && all[0].CodexSessionID == "01912345-6789-7abc-8def-0123456789ab" && all[0].Recovery == "ready" && !firstReported {
			if err := os.WriteFile(filepath.Join(b.root, "switch"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			firstReported = true
		}
		if len(all) == 1 && all[0].ReportedCodexID == "01912345-6789-7abc-8def-0123456789ac" && all[0].Recovery == "needs_review" {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("initial and changed Codex identities were not recorded for review")
}

func TestExplicitHerdrExecutableIsUsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-herdr")
	t.Setenv("AGENT_HARBOR_HERDR_BIN", path)
	if got := herdrCommand(os.Environ(), "--version").Path; got != path {
		t.Fatalf("Herdr executable: got %q, want %q", got, path)
	}
}

func TestExistingPaneIdentityIsNotAttributedToANewSession(t *testing.T) {
	b := newTestBridge(t)
	herdr := filepath.Join(b.root, "bin", "herdr")
	stub := `#!/bin/sh
printf '{"result":{"pane":{"agent_session":{"source":"herdr:codex","agent":"codex","kind":"id","value":"01912345-6789-7abc-8def-0123456789ab"}}}}\n'
`
	if err := os.WriteFile(herdr, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	b.sessions()
	b.env = append(b.env, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH="+queryTestSocket(t, b), "HERDR_BIN_PATH="+herdr)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	if got := b.sessions(); len(got) != 1 || got[0].CodexSessionID != "" || got[0].Recovery != "needs_review" {
		t.Fatalf("stale identity attributed to session: %+v", got)
	}
}

func TestManagedSessionRestoresUnsetAndEmptyUserPaths(t *testing.T) {
	b := newTestBridge(t)
	paths := map[string]*string{}
	for _, key := range userPathKeys {
		paths[key] = nil
	}
	empty := ""
	codexHome := filepath.Join(b.root, "user-codex")
	paths["HOME"], paths["XDG_CONFIG_HOME"], paths["CODEX_HOME"] = &b.root, &empty, &codexHome
	data, err := json.Marshal(paths)
	if err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "XDG_CONFIG_HOME=/managed/config", "XDG_STATE_HOME=/managed/state", "CODEX_HOME=/wrong", "BRIDGE_TEST_EXTRA=untouched", originalEnvKey+"="+base64.RawStdEncoding.EncodeToString(data))
	stub := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n%s\\n' \"${XDG_CONFIG_HOME+set}:$XDG_CONFIG_HOME\" \"${XDG_STATE_HOME+set}:$XDG_STATE_HOME\" \"$CODEX_HOME\" \"$BRIDGE_TEST_EXTRA\" > \"$CAPTURE\"\n"
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	got, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "set:\n:\n" + codexHome + "\nuntouched\n"; string(got) != want {
		t.Fatalf("user paths: got %q, want %q", got, want)
	}
}

func TestLaunchedSessionReportsConnectionToOwnedHerdrPane(t *testing.T) {
	b := newTestBridge(t)
	ownedRoot, err := os.MkdirTemp("/tmp", "ah03-sidebar-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ownedRoot) })
	if err := ensureHome(ownedRoot); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "AGENT_HARBOR_BRIDGE_HOME="+ownedRoot)
	runtime := filepath.Join(ownedRoot, "herdr", "runtime")
	if err := os.MkdirAll(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(runtime, "fixture.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				var req any
				json.NewDecoder(c).Decode(&req)
				json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []any{}}})
			}()
		}
	}()

	herdr := filepath.Join(b.root, "bin", "herdr")
	stub := `#!/bin/sh
case "$*" in
 *"session list --json"*) printf '{"sessions":[{"name":"agent-harbor","running":true}]}';;
 *"pane report-metadata"*) printf '%s\n' "$*" >> "$CAPTURE_METADATA";;
 *) printf '{}';;
esac
`
	if err := os.WriteFile(herdr, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(b.root, "metadata")
	b.env = append(b.env, "HERDR_PANE_ID=w1:p1", "HERDR_TAB_ID=w1:t1", "HERDR_SOCKET_PATH="+socket, "AGENT_HARBOR_HERDR_BIN="+herdr, "CAPTURE_METADATA="+metadata)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatal(out, err)
	}
	out, err := os.ReadFile(metadata)
	if err != nil || !strings.Contains(string(out), "pane report-metadata w1:p1 --source agent-harbor --token harbor=Local Codex") {
		t.Fatal("missing connection metadata", string(out), err)
	}
}
