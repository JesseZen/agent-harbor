//go:build darwin || linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionClientForegroundHandoff(t *testing.T) {
	const child = "HARBOR_TEST_FOREGROUND_TTY"
	if os.Getenv(child) == "1" {
		checkSessionClientForeground(t)
		return
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed to allocate the test PTY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-c", "import os, pty, sys; sys.exit(os.waitstatus_to_exitcode(pty.spawn(sys.argv[1:])))",
		os.Args[0], "-test.run=^TestSessionClientForegroundHandoff$")
	cmd.Env = append(os.Environ(), child+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PTY handoff: %v\n%s", err, output)
	}
}

func checkSessionClientForeground(t *testing.T) {
	t.Helper()
	group := func() int32 {
		t.Helper()
		current, err := terminalForegroundGroup(os.Stdin.Fd())
		if err != nil || current <= 0 {
			t.Fatalf("read foreground group: %d, %v", current, err)
		}
		return current
	}
	previous := group()
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Stdin = os.Stdin
	restore, err := startSessionClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	foreground, queryErr := terminalForegroundGroup(os.Stdin.Fd())
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	restore()
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if foreground != int32(cmd.Process.Pid) {
		t.Fatalf("client started outside the foreground group: got %d, want %d", foreground, cmd.Process.Pid)
	}
	if restored := group(); restored != previous {
		t.Fatalf("foreground not restored: got %d, want %d", restored, previous)
	}
	broken := filepath.Join(t.TempDir(), "bad-interpreter")
	if err := os.WriteFile(broken, []byte("#!/no-such-harbor-test-interpreter\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(broken)
	cmd.Stdin = os.Stdin
	if _, err := startSessionClient(cmd); err == nil {
		t.Fatal("started an executable with a missing interpreter")
	}
	if restored := group(); restored != previous {
		t.Fatalf("exec failure did not restore foreground: got %d, want %d", restored, previous)
	}
}
