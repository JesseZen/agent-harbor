package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLogCgroupIsolationDecision_WiredIntoBootstrap is the end-to-end gate
// proving the OBS-01 call site in main.go actually fires on TUI startup. Two
// independent arms catch two distinct regression classes:
//
//   - "wire_up_line_exists" — line-level grep over main.go. Fails fast (no
//     subprocess required) when a future refactor deletes the call line.
//   - "tui_startup_emits_line" — subprocess integration. Builds the binary,
//     launches the TUI under an isolated HOME, kills it after a short window,
//     and greps the resulting debug.log for the canonical OBS-01 substring.
//     Catches the failure mode where the call line is present but unreachable
//     (e.g. moved after an early os.Exit) — line-level grep cannot detect this.
//
// Both arms must be GREEN simultaneously for OBS-01 to be considered wired.
func TestLogCgroupIsolationDecision_WiredIntoBootstrap(t *testing.T) {
	t.Run("wire_up_line_exists", func(t *testing.T) {
		// Line-level grep — catches "call line deleted in a refactor" regressions.
		data, err := os.ReadFile("main.go")
		if err != nil {
			t.Fatalf("read main.go: %v", err)
		}
		if !strings.Contains(string(data), "session.LogCgroupIsolationDecision()") {
			t.Fatalf("OBS-01-WIRE-UP-MISSING: main.go does not contain session.LogCgroupIsolationDecision() call")
		}
	})

	t.Run("tui_startup_emits_line", func(t *testing.T) {
		// Behavior-level subprocess gate — catches "call line present but
		// unreachable" wire-up bugs (e.g. placed after an early os.Exit).
		if testing.Short() {
			t.Skip("skipping subprocess integration test in short mode")
		}
		tmpHome := t.TempDir()
		xdgConfigHome := filepath.Join(tmpHome, ".config")
		xdgDataHome := filepath.Join(tmpHome, ".local", "share")
		xdgCacheHome := filepath.Join(tmpHome, ".cache")
		if err := os.MkdirAll(filepath.Join(xdgCacheHome, "agent-deck"), 0o755); err != nil {
			t.Fatal(err)
		}
		binPath := filepath.Join(t.TempDir(), "agent-deck-test")
		build := exec.Command("go", "build", "-o", binPath, ".")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\noutput: %s", err, out)
		}

		// Strip TMUX* and AGENTDECK_* env from the parent process so the
		// nested-session guard in main.go (isNestedSession → GetCurrentSessionID)
		// does not early-exit when the test runs under an outer
		// agent-deck-managed tmux session. Without this filter, the binary
		// prints "Cannot launch the agent-deck TUI inside an agent-deck
		// session" on stderr and never reaches logging.Init.
		var env []string
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "TMUX") {
				continue
			}
			if strings.HasPrefix(kv, "AGENTDECK_") {
				continue
			}
			if strings.HasPrefix(kv, "HOME=") {
				continue
			}
			env = append(env, kv)
		}
		env = append(env,
			"HOME="+tmpHome,
			"XDG_CONFIG_HOME="+xdgConfigHome,
			"XDG_DATA_HOME="+xdgDataHome,
			"XDG_CACHE_HOME="+xdgCacheHome,
			"AGENTDECK_DEBUG=1",
			"AGENTDECK_PROFILE=test-obs01",
			"TERM=dumb",
		)
		cmd := exec.Command(binPath)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		// TUI blocks on stdin — detach with its own pgroup and SIGTERM the
		// whole group once the observable log line appears.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start binary: %v", err)
		}
		logPath := filepath.Join(xdgCacheHome, "agent-deck", "debug.log")
		wait := make(chan error, 1)
		go func() { wait <- cmd.Wait() }()
		stopped := false
		t.Cleanup(func() {
			if !stopped {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-wait
			}
		})

		var data []byte
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(string(data), "tmux cgroup isolation:") && time.Now().Before(deadline) {
			select {
			case err := <-wait:
				stopped = true
				t.Fatalf("TUI exited before OBS-01 log: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
			default:
			}
			data, _ = os.ReadFile(logPath)
			time.Sleep(25 * time.Millisecond)
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-wait
		}
		stopped = true

		if !strings.Contains(string(data), "tmux cgroup isolation:") {
			t.Fatalf("OBS-01-WIRE-UP-MISSING: debug.log at %s missing line\ncontents=%s\nstdout=%s\nstderr=%s",
				logPath, data, stdout.String(), stderr.String())
		}
	})
}
