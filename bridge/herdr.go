package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const herdrSession = "agent-harbor"

func openHerdr() error {
	if err := ensureOnboarding(onboardingAppService); err != nil {
		return err
	}
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	env, snapshot, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	if out, err := herdrCommand(env, "--version").Output(); err != nil || strings.TrimSpace(string(out)) != "herdr 0.9.5" {
		return errors.New("Herdr 0.9.5 is required; check the bundled executable or AGENT_HARBOR_HERDR_BIN")
	}
	lock, err := os.OpenFile(filepath.Join(root, "herdr-open.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	err = prepareHerdr(root, env, snapshot)
	unlockErr := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err != nil {
		return err
	}
	if unlockErr != nil {
		return unlockErr
	}
	cmd := herdrCommand(env, "--session", herdrSession)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Herdr exited: %w", err)
	}
	return nil
}

func prepareHerdr(root string, env []string, snapshot string) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return err
	}
	configPath := filepath.Join(root, "herdr", "config", "herdr.toml")
	pluginDir := filepath.Join(root, "herdr", "plugin")
	preferences, err := readShortcuts(root)
	if err != nil {
		return err
	}
	if _, e := os.Stat(filepath.Join(root, "shortcuts.pending")); !os.IsNotExist(e) {
		return errors.New("unfinished settings save; close workspace and run agent-harbor settings-recover")
	}
	config := configWithShortcuts(bin, preferences)
	digest := sha256.Sum256([]byte(snapshot))
	fingerprint := hex.EncodeToString(digest[:])
	fingerprintPath := filepath.Join(root, "herdr", "environment.sha256")
	running, err := herdrRunning(env)
	if err != nil {
		return err
	}
	if running {
		previous, err := os.ReadFile(fingerprintPath)
		if err != nil || string(previous) != fingerprint {
			return errors.New("the managed Herdr server uses different user paths; run stop before reopening")
		}
		previous, err = os.ReadFile(configPath)
		if err != nil || string(previous) != config && !(preferences == defaultShortcuts() && string(previous) == legacyHerdrConfig(bin)) {
			return errors.New("the managed Herdr configuration changed; run stop before reopening")
		}
	} else {
		if err := writePrivate(configPath, []byte(config)); err != nil {
			return err
		}
		if err := writePrivate(fingerprintPath, []byte(fingerprint)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(pluginDir, "herdr-plugin.toml"), []byte(herdrManifest(bin))); err != nil {
		return err
	}
	if err := writeHerdrWrapper(root, env); err != nil {
		return err
	}
	if err := herdrCommand(env, "--session", herdrSession, "plugin", "link", pluginDir).Run(); err != nil {
		return fmt.Errorf("cannot register bridge in isolated Herdr: %w", err)
	}
	if running {
		return nil
	}
	server := herdrCommand(env, "--session", herdrSession, "server")
	server.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := server.Start(); err != nil {
		return fmt.Errorf("cannot start managed Herdr: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			running, err := herdrRunning(env)
			if err == nil && running {
				return nil
			}
		case err := <-done:
			// A server that exits before it is reachable did not start.
			if running, _ := herdrRunning(env); running {
				return nil
			}
			return fmt.Errorf("managed Herdr stopped during startup: %v", err)
		case <-deadline.C:
			_ = server.Process.Kill()
			return errors.New("managed Herdr did not become ready")
		}
	}
}

// Third-party MCP tools can use this executable without discovering another Herdr server.
func writeHerdrWrapper(root string, env []string) error {
	dir := filepath.Join(root, "herdr", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	cmd := herdrCommand(env)
	path, err := exec.LookPath(cmd.Path)
	if err != nil {
		return err
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	config, err := managedConfigContext(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(&script, "%s _herdr-config-check %s %s || exit 1\n", quote(executable), quote(root), quote(config))
	for _, pair := range env {
		key, value, ok := strings.Cut(pair, "=")
		if ok && (key == "XDG_CONFIG_HOME" || key == "XDG_STATE_HOME" || key == "XDG_DATA_HOME" || key == "XDG_CACHE_HOME" || key == "XDG_RUNTIME_DIR" || key == "HERDR_CONFIG_PATH") {
			fmt.Fprintf(&script, "export %s=%s\n", key, quote(value))
		}
	}
	fmt.Fprintf(&script, "unset HERDR_SOCKET_PATH\nexec %s --session %s \"$@\"\n", quote(path), quote(herdrSession))
	wrapper := filepath.Join(dir, "herdr")
	if err := writePrivate(wrapper, []byte(script.String())); err != nil {
		return err
	}
	return os.Chmod(wrapper, 0700)
}

func herdrCommand(env []string, args ...string) *exec.Cmd {
	name := "herdr"
	if override := os.Getenv("AGENT_HARBOR_HERDR_BIN"); override != "" {
		name = override
	} else if executable, err := os.Executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(executable), "herdr")
		if _, err := os.Lstat(bundled); err == nil || !os.IsNotExist(err) {
			name = bundled
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Env = env
	return cmd
}

func herdrRunning(env []string) (bool, error) {
	out, err := herdrCommand(env, "--session", herdrSession, "session", "list", "--json").Output()
	if err != nil {
		return false, fmt.Errorf("cannot inspect managed Herdr sessions: %w", err)
	}
	var result struct {
		Sessions *[]struct {
			Name            string `json:"name"`
			Running         *bool  `json:"running"`
			ConnectionError string `json:"connection_error"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.Sessions == nil {
		return false, errors.New("Herdr returned an invalid session list")
	}
	for _, s := range *result.Sessions {
		if s.Name == herdrSession {
			if s.Running == nil {
				return false, errors.New("Herdr returned an invalid managed session status")
			}
			if s.ConnectionError != "" {
				return false, errors.New("cannot inspect the managed Herdr server; check its socket and permissions")
			}
			return *s.Running, nil
		}
	}
	return false, nil
}

func stopHerdr() error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	cmd := herdrCommand(env, "session", "stop", herdrSession)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot stop managed Herdr: %w", err)
	}
	return nil
}

func legacyHerdrConfig(bin string) string {
	quote := "'" + strings.ReplaceAll(bin, "'", "'\\''") + "'"
	app, _ := json.Marshal(quote + " _app")
	magpie, _ := json.Marshal(quote + " _magpie")
	status, _ := json.Marshal(quote + " _status")
	return fmt.Sprintf(`onboarding = false

[update]
version_check = false
manifest_check = false

[session]
resume_agents_on_restore = false

[ui]
tab_bar_right = [{ type = "command", command = %s, interval_seconds = 5, timeout_seconds = 1 }]

[ui.sidebar.agents]
rows = [["state_icon", "machine", "workspace", "tab"], ["agent"], ["$harbor"]]

[[keys.command]]
key = "prefix+a"
type = "popup"
command = %s
description = "Agent Harbor sessions"
width = "80%%"
height = "80%%"

[[keys.command]]
key = "prefix+m"
type = "popup"
command = %s
description = "Manage Magpie connections"
width = "80%%"
height = "80%%"
`, status, app, magpie)
}

func herdrManifest(bin string) string {
	path, _ := json.Marshal(bin)
	return fmt.Sprintf("id = \"agent-harbor.bridge\"\nname = \"Agent Harbor\"\nversion = \"0.1.0\"\nmin_herdr_version = \"0.9.5\"\nplatforms = [\"linux\", \"macos\"]\n\n[[panes]]\nid = \"codex\"\ntitle = \"Codex\"\ncommand = [%s, \"_pane\"]\n\n[[panes]]\nid = \"session\"\ntitle = \"Native client\"\ncommand = [%s, \"_pane\"]\n", path, path)
}

func writePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".bridge-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// sessionHerdr checks the recorded managed socket identity before targeting a
// pane. Metadata describes connection intent, never observed provider identity.
func reportSessionConnection(s session) error {
	label := "Local Codex"
	if s.CLI != "codex" {
		label = "Native " + s.CLI
	}
	if s.Mode == "magpie" {
		label = "Magpie " + s.InstanceID + " / " + s.MagpieModel
	}
	return sessionHerdr(s, "pane", "report-metadata", s.HerdrPane, "--source", "agent-harbor", "--token", "harbor="+cleanUI(label))
}
