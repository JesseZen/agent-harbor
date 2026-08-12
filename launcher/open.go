package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func commandOpen(manifest bundleManifest, paths productPaths, args []string, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageError("open accepts no arguments")
	}
	if err := ensureRuntime(manifest, paths); err != nil {
		return err
	}
	if importPath := strings.TrimSpace(os.Getenv("AGENT_HARBOR_IMPORT")); importPath != "" {
		if target, err := portableTargetEmpty(paths); err != nil {
			return err
		} else if target {
			options := []string{}
			if identity := strings.TrimSpace(os.Getenv("AGENT_HARBOR_IMPORT_IDENTITY_FILE")); identity != "" {
				options = append(options, "--identity-file", identity)
			}
			if passphrase := strings.TrimSpace(os.Getenv("AGENT_HARBOR_IMPORT_PASSPHRASE_FILE")); passphrase != "" {
				options = append(options, "--passphrase-file", passphrase)
			}
			options = append(options, importPath)
			if err := commandImport(manifest, paths, options, stdout); err != nil {
				return fmt.Errorf("automatic import: %w", err)
			}
		}
	}
	if _, err := initializeConfig(paths); err != nil {
		return err
	}
	if !coreRunning(paths) {
		if err := startCore(manifest, paths); err != nil {
			return err
		}
	}
	if err := waitForCore(paths); err != nil {
		return err
	}
	tmux, err := runtimeExecutable(manifest, paths, "dependency.tmux")
	if err != nil {
		return err
	}
	terminfo := terminfoRoot(manifest, paths)
	socket := filepath.Join(paths.StateRoot, "tmux", "agent-harbor")
	if err := waitForTUISession(paths, tmux, socket); err != nil {
		return err
	}
	command := exec.Command(tmux, "-f", "/dev/null", "-S", socket, "attach-session", "-t", "agent-harbor-tui")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, stdout, stderr
	command.Env = runtimeEnvironment(manifest, paths, terminfo)
	if err := command.Run(); err != nil {
		return noExecHint(tmux, err)
	}
	return nil
}

func waitForTUISession(paths productPaths, tmux, socket string) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		check := exec.Command(tmux, "-f", "/dev/null", "-S", socket, "has-session", "-t", "agent-harbor-tui")
		if err := check.Run(); err == nil {
			return nil
		}
		if !coreRunning(paths) {
			return errors.New("Core exited before the TUI session became ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("TUI session did not become ready within 30 seconds")
}

func startCore(manifest bundleManifest, paths productPaths) error {
	core, err := runtimeExecutable(manifest, paths, "runtime.core")
	if err != nil {
		return err
	}
	if _, err := runtimeExecutable(manifest, paths, "frontend.tui"); err != nil {
		return err
	}
	logRoot := filepath.Join(paths.StateRoot, "logs")
	if err := ensurePrivatePath(logRoot); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(logRoot, "launcher-core.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	command := exec.Command(core, "--config-dir", paths.ConfigRoot)
	command.Stdout, command.Stderr = logFile, logFile
	command.Stdin = nil
	command.Env = runtimeEnvironment(manifest, paths, terminfoRoot(manifest, paths))
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		logFile.Close()
		return noExecHint(core, err)
	}
	_ = command.Process.Release()
	_ = logFile.Close()
	return nil
}

func waitForCore(paths productPaths) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if coreRunning(paths) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	logPath := filepath.Join(paths.StateRoot, "logs", "launcher-core.log")
	if source, err := os.ReadFile(logPath); err == nil {
		if len(source) > 4096 {
			source = source[len(source)-4096:]
		}
		return fmt.Errorf("Core readiness timed out; last startup output:\n%s", source)
	}
	return errors.New("Core readiness timed out")
}

func runtimeEnvironment(manifest bundleManifest, paths productPaths, terminfo string) []string {
	environment := os.Environ()
	if tmux, err := runtimeExecutable(manifest, paths, "dependency.tmux"); err == nil {
		environment = replaceEnv(environment, "PATH", filepath.Dir(tmux)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if terminfo != "" {
		environment = replaceEnv(environment, "TERMINFO_DIRS", terminfo)
	}
	if term := os.Getenv("TERM"); term == "" || term == "dumb" || term == "unknown" {
		environment = replaceEnv(environment, "TERM", "xterm-256color")
	}
	return environment
}
func replaceEnv(values []string, name, value string) []string {
	prefix := name + "="
	result := values[:0]
	for _, item := range values {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}
func terminfoRoot(manifest bundleManifest, paths productPaths) string {
	_, ok := manifest.componentByRole("data.terminfo")
	if !ok {
		return ""
	}
	return filepath.Join(paths.Runtime, "share", "terminfo")
}
