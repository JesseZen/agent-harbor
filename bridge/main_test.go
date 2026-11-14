package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test process exposes the same command boundary as the standalone binary.
func TestBridgeProcess(t *testing.T) {
	if os.Getenv("BRIDGE_TEST_PROCESS") != "1" {
		return
	}
	// Existing process fixtures exercise Herdr/session behavior with a
	// completed, dependency-ready preflight. Dedicated onboarding tests use
	// the real service against the same isolated fixture environment.
	if os.Getenv("BRIDGE_TEST_ONBOARDING") != "1" {
		onboardingAppService = &fakeOnboardingService{report: onboardingReport{Ready: true, Completed: true}}
	}
	os.Exit(main1(os.Args[3:]))
}

type testBridge struct {
	t       *testing.T
	root    string
	project string
	env     []string
}

// A managed runner may receive approval through a pipe rather than a TTY.
// Launch must preserve the requested project and execute without job-control
// ioctls against that pipe (including platforms that accept TIOCGPGRP on pipes).
func TestManagedCreateAcceptsPipeInput(t *testing.T) {
	b := newTestBridge(t)
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := writer.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--dir", b.project)
	cmd.Env = b.env
	cmd.Stdin = input
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pipe-backed managed create failed: %s %v", out, err)
	}
	records := b.sessions()
	if len(records) != 1 || records[0].WorkingDirectory != b.project {
		t.Fatalf("project intent lost: %+v", records)
	}
	if capture, err := os.ReadFile(filepath.Join(b.root, "capture")); err != nil || len(capture) == 0 {
		t.Fatalf("client did not execute: %s %v", capture, err)
	}
}

func newTestBridge(t *testing.T) testBridge {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project with spaces")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{project, bin, filepath.Join(root, "codex"), filepath.Join(root, "sqlite"), filepath.Join(root, "tmp"), filepath.Join(root, "runtime")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	project, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	// A native process fixture crosses the same executable boundary as a client.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "codex", "config.toml"), []byte("check_for_update_on_startup = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return testBridge{t, root, project, []string{
		"BRIDGE_TEST_PROCESS=1", "BRIDGE_NATIVE_CLIENT=1", "HOME=" + root, "PATH=" + bin + ":" + os.Getenv("PATH"),
		"AGENT_HARBOR_BRIDGE_HOME=" + filepath.Join(root, "bridge"), "XDG_CONFIG_HOME=",
		"CAPTURE=" + filepath.Join(root, "capture"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"), "XDG_STATE_HOME=" + filepath.Join(root, "state"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "XDG_RUNTIME_DIR=" + filepath.Join(root, "runtime"), "TMPDIR=" + filepath.Join(root, "tmp"), "CODEX_HOME=" + filepath.Join(root, "codex"), "CODEX_SQLITE_HOME=" + filepath.Join(root, "sqlite"),
	}}
}

func (b testBridge) command(args ...string) (string, error) {
	b.t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestBridgeProcess$", "--"}, args...)...)
	cmd.Env = b.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = strings.NewReader("yes\n")
	if len(args) > 1 && args[0] == "resume" && args[1] == "--development-query" {
		cmd.Stdin = strings.NewReader("accept\nyes\n")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (b testBridge) sessions() []session {
	b.t.Helper()
	out, err := b.command("sessions", "--json")
	if err != nil {
		b.t.Fatalf("sessions: %s: %v", out, err)
	}
	var sessions []session
	if err := json.Unmarshal([]byte(out), &sessions); err != nil {
		b.t.Fatalf("sessions JSON: %s: %v", out, err)
	}
	return sessions
}

func TestLocalSessionRequiresExplicitIdentityForRecovery(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project, "--mode", "local"); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	sessions := b.sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions: %+v", sessions)
	}
	s := sessions[0]
	if s.WorkingDirectory != b.project || s.CLI != "codex" || s.Mode != "local" || s.CodexSessionID != "" || s.Recovery != "needs_review" {
		t.Fatalf("unexpected session: %+v", s)
	}
	out, err := b.command("resume", s.ID)
	if err == nil || !strings.Contains(out, "upstream-observed reference") {
		t.Fatalf("missing identity must stop recovery: %s", out)
	}
	const codexID = "01912345-6789-7abc-8def-0123456789ab"
	if out, err := b.command("bind", s.ID, codexID); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	b.prepareQuery(s.ID)
	if out, err := b.command("resume", "--development-query", s.ID); err != nil {
		t.Fatalf("resume: %s: %v", out, err)
	}
	got, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	want := b.project + "\nresume\n" + codexID + "\nset:\n"
	if string(got) != want {
		t.Fatalf("Codex invocation: got %q, want %q", got, want)
	}
}

func TestMagpieRequiresInteractiveSelection(t *testing.T) {
	b := newTestBridge(t)
	out, err := b.command("create", "--directory", b.project, "--mode", "magpie")
	if err == nil || !strings.Contains(out, "session menu") {
		t.Fatalf("Magpie must require interactive selection: %s", out)
	}
	if got := b.sessions(); len(got) != 0 {
		t.Fatalf("unexpected sessions: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(b.root, "capture")); !os.IsNotExist(err) {
		t.Fatalf("Codex was launched: %v", err)
	}
}

func TestExistingNonPrivateStateDirectoryIsNotModified(t *testing.T) {
	b := newTestBridge(t)
	shared := filepath.Join(b.root, "shared")
	if err := os.Mkdir(shared, 0755); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "AGENT_HARBOR_BRIDGE_HOME="+shared)
	out, err := b.command("sessions")
	if err == nil || !strings.Contains(out, "must be private") {
		t.Fatalf("unsafe directory accepted: %s", out)
	}
	info, err := os.Stat(shared)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("existing directory permissions changed: %o", info.Mode().Perm())
	}
}

func TestExistingUnownedStateDirectoryIsNotModified(t *testing.T) {
	b := newTestBridge(t)
	other := filepath.Join(b.root, "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, "sessions.json")
	if err := os.WriteFile(path, []byte("user data"), 0600); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "AGENT_HARBOR_BRIDGE_HOME="+other)
	out, err := b.command("sessions")
	if err == nil || !strings.Contains(out, "ownership marker") {
		t.Fatalf("unowned directory accepted: %s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "user data" {
		t.Fatalf("user data changed: %q %v", data, err)
	}
}

func TestInvalidUTF8WorkingDirectoryIsRejected(t *testing.T) {
	b := newTestBridge(t)
	path := filepath.Join(b.root, string([]byte{'p', 0xff}))
	if err := os.Mkdir(path, 0700); err != nil {
		t.Skip("filesystem does not accept non-UTF-8 paths")
	}
	out, err := b.command("create", "--directory", path)
	if err == nil || !strings.Contains(out, "valid UTF-8") {
		t.Fatalf("invalid path accepted: %s", out)
	}
	if got := b.sessions(); len(got) != 0 {
		t.Fatalf("invalid path was saved: %+v", got)
	}
}

func TestConcurrentFirstUseClaimsStateOnce(t *testing.T) {
	b := newTestBridge(t)
	var commands []*exec.Cmd
	for range 8 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "sessions", "--json")
		cmd.Env = b.env
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("concurrent state initialization failed: %v", err)
		}
	}
}

func TestBindingRejectsAnIDWithAdditionalHyphens(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	id := b.sessions()[0].ID
	out, err := b.command("bind", id, "01912345-6789-7abc-8def-0123--6789ab")
	if err == nil || !strings.Contains(out, "must be a UUID") {
		t.Fatalf("invalid ID accepted: %s", out)
	}
	if got := b.sessions()[0].CodexSessionID; got != "" {
		t.Fatalf("recorded invalid ID: %q", got)
	}
}

func TestRecoveryPinsTheResolvedWorkingDirectory(t *testing.T) {
	b := newTestBridge(t)
	link := filepath.Join(b.root, "project-link")
	other := filepath.Join(b.root, "other-project")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b.project, link); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("create", "--directory", link); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	s := b.sessions()[0]
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if out, err := b.command("bind", s.ID, "01912345-6789-7abc-8def-0123456789ab"); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	b.prepareQuery(s.ID)
	if out, err := b.command("resume", "--development-query", s.ID); err != nil {
		t.Fatalf("resume: %s: %v", out, err)
	}
	captured, err := os.ReadFile(filepath.Join(b.root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(b.project)
	if err != nil {
		t.Fatal(err)
	}
	if s.WorkingDirectory != resolved || !strings.HasPrefix(string(captured), resolved+"\n") {
		t.Fatalf("recovery did not stay in the original directory: %q / %q", s.WorkingDirectory, captured)
	}
}

func TestRecoveryStopsWhenWorkingDirectoryDisappears(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	id := b.sessions()[0].ID
	if out, err := b.command("bind", id, "01912345-6789-7abc-8def-0123456789ab"); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	if err := os.Rename(b.project, b.project+"-moved"); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("resume", id)
	if err == nil || !strings.Contains(out, "working directory is unavailable") {
		t.Fatalf("missing directory: %s", out)
	}
}

func TestSessionCannotResumeWhileAlreadyRunning(t *testing.T) {
	b := newTestBridge(t)
	stub := "#!/bin/sh\nprintf ready > \"$CAPTURE\"\nread reply\n"
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
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
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(b.root, "capture")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Codex did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	id := b.sessions()[0].ID
	if out, err := b.command("bind", id, "01912345-6789-7abc-8def-0123456789ab"); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	out, err := b.command("resume", id)
	if err == nil || !strings.Contains(out, "still running") {
		t.Fatalf("concurrent recovery: %s", out)
	}
}

func TestCorrectingAnIDRequiresExplicitVerification(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatalf("create: %s: %v", out, err)
	}
	id := b.sessions()[0].ID
	if out, err := b.command("bind", id, "01912345-6789-7abc-8def-0123456789ab"); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	out, err := b.command("verify", "--directory", b.project, id, "01912345-6789-7abc-8def-0123456789ac")
	if err != nil {
		t.Fatalf("verify: %s: %v", out, err)
	}
	got := b.sessions()[0]
	if got.CodexSessionID != "01912345-6789-7abc-8def-0123456789ac" || got.Recovery != "ready" {
		t.Fatalf("identity not corrected: %+v", got)
	}
}

func TestRecoveryWaitsForChildAfterBridgeCrash(t *testing.T) {
	b := newTestBridge(t)
	stub := "#!/bin/sh\nprintf ready > \"$CAPTURE\"\nwhile :; do sleep 10; done\n"
	if err := writeNativeClientFixture(filepath.Join(b.root, "bin", "codex"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "create", "--directory", b.project)
	cmd.Env, cmd.SysProcAttr = b.env, &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	childGroup := 0
	t.Cleanup(func() {
		if childGroup > 1 {
			_ = syscall.Kill(-childGroup, syscall.SIGKILL)
		}
		_ = in.Close()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(b.root, "capture")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Codex did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	id := b.sessions()[0].ID
	if out, err := b.command("bind", id, "01912345-6789-7abc-8def-0123456789ab"); err != nil {
		t.Fatalf("bind: %s: %v", out, err)
	}
	childGroup = b.sessions()[0].ProcessGroup
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if err := syscall.Kill(-childGroup, 0); err != nil {
		t.Fatalf("test child did not survive: %v", err)
	}
	out, err := b.command("resume", id)
	if err == nil || !strings.Contains(out, "still running") {
		t.Fatalf("recovery ignored the surviving child: %s", out)
	}
}

func TestHelpExplainsFirstUseAndRecovery(t *testing.T) {
	b := newTestBridge(t)
	out, err := b.command("help")
	if err != nil {
		t.Fatal(err)
	}
	for _, hint := range []string{"Codex 0.167.0", "Ctrl+B a", "Ctrl+B m", "local configuration", "explicit confirmation"} {
		if !strings.Contains(out, hint) {
			t.Fatalf("missing %q: %s", hint, out)
		}
	}
}

func (b *testBridge) prepareQuery(id string) {
	b.t.Helper()
	var chosen session
	for _, s := range b.sessions() {
		if s.ID == id {
			chosen = s
		}
	}
	ref := herdrIdentity{"herdr:codex", "codex", "id", chosen.CodexSessionID}
	data, _ := json.Marshal(ref)
	if out, err := b.command("reassociate", id, string(data), "isolated fixture original conversation reviewed"); err != nil {
		b.t.Fatal(out, err)
	}
	h := filepath.Join(b.root, "bin", "query-herdr")
	plan, _ := json.Marshal(resumeCommandPlan{1, "codex", []string{"codex", "resume", chosen.CodexSessionID}})
	script := "#!/bin/sh\nif test \"$1 $2\" = \"agent resume-command\"; then cat >/dev/null; printf '%s\n' '" + string(plan) + "'; else echo '{\"result\":{\"pane\":{}}}'; fi\n"
	if err := os.WriteFile(h, []byte(script), 0700); err != nil {
		b.t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(b.root, "bridge", "herdr", "socket"))
	b.env = append(b.env, "AGENT_HARBOR_HERDR_BIN="+h, "HERDR_BIN_PATH="+h, "HERDR_PLUGIN_ID=agent-harbor.bridge", "HERDR_PLUGIN_ENTRYPOINT_ID=codex", "HERDR_PANE_ID=query-pane", "HERDR_SOCKET_PATH="+queryTestSocket(b.t, *b))
}
