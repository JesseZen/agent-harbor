package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// A stable workspace identity survives a server restart when its state survives.
// The wrapper checks it on every Herdr probe/bridge invocation, including reconnects.
type remoteWorkspace struct {
	Version     int      `json:"version"`
	ID          string   `json:"id"`
	State       string   `json:"state"`
	Executable  string   `json:"executable"`
	Session     string   `json:"session"`
	Herdr       string   `json:"herdr"`
	Environment []string `json:"environment,omitempty"`
}

func remoteContextPath(root string) string { return filepath.Join(root, "remote-context.json") }

func readRemoteWorkspace(root string) (remoteWorkspace, error) {
	var w remoteWorkspace
	info, err := os.Lstat(remoteContextPath(root))
	if err != nil {
		return w, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return w, errors.New("remote workspace context is not a private regular file")
	}
	data, err := os.ReadFile(remoteContextPath(root))
	if err != nil {
		return w, err
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return w, errors.New("invalid remote workspace context")
	}
	if w.Version != 1 || !validRemoteID(w.ID) || w.State != root || w.Session != herdrSession || !filepath.IsAbs(w.Herdr) {
		return w, errors.New("remote workspace context changed; explicitly prepare and review it")
	}
	if w.Executable != filepath.Join(root, "herdr", "bin", "remote-herdr-"+w.ID) {
		return w, errors.New("remote workspace executable identity changed")
	}
	values := map[string]string{}
	allowed := remoteEnvironmentKeys()
	for _, pair := range w.Environment {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || !allowed[key] {
			return w, errors.New("invalid remote workspace environment")
		}
		if _, duplicate := values[key]; duplicate {
			return w, errors.New("duplicate remote workspace environment")
		}
		values[key] = value
	}
	if values["AGENT_HARBOR_BRIDGE_HOME"] != root || values[originalEnvKey] == "" || values["XDG_STATE_HOME"] != filepath.Join(root, "herdr", "state") || values["HERDR_CONFIG_PATH"] != filepath.Join(root, "herdr", "config", "herdr.toml") {
		return w, errors.New("remote workspace environment is incomplete or changed")
	}
	return w, nil
}

func validRemoteID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && id == strings.ToLower(id)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func prepareRemoteWorkspace(root string, native bool) (remoteWorkspace, error) {
	var w remoteWorkspace
	if !filepath.IsAbs(root) {
		return w, errors.New("remote state must be an absolute path")
	}
	root = filepath.Clean(root)
	if err := os.Setenv("AGENT_HARBOR_BRIDGE_HOME", root); err != nil {
		return w, err
	}
	if err := ensureHome(root); err != nil {
		return w, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "remote-prepare.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return w, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return w, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	w, err = readRemoteWorkspace(root)
	if err != nil && !os.IsNotExist(err) {
		return w, err
	}
	if os.IsNotExist(err) {
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return w, err
		}
		w = remoteWorkspace{Version: 1, ID: hex.EncodeToString(id), State: root, Session: herdrSession}
	}
	report, err := onboardingAppService.Onboarding()
	if err != nil {
		return w, err
	}
	if !report.Completed {
		if !native {
			return w, errors.New("remote setup needs onboarding; use --native to select this server's native client configuration")
		}
		if !report.Ready {
			return w, errors.New("remote dependencies are unavailable; install the Harbor candidate and a native Codex executable")
		}
		if err := onboardingAppService.FinishOnboarding(onboardingChoice{Kind: "local"}); err != nil {
			return w, err
		}
	}
	if err := openHerdrMode(false); err != nil {
		return w, err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return w, err
	}
	command := herdrCommand(env)
	herdr, err := filepath.Abs(command.Path)
	if err != nil {
		return w, err
	}
	// LookPath is required when an operator supplies a PATH executable.
	if !strings.ContainsRune(command.Path, filepath.Separator) {
		herdr, err = exec.LookPath(command.Path)
		if err != nil {
			return w, err
		}
	}
	w.Herdr = herdr
	w.Executable = filepath.Join(root, "herdr", "bin", "remote-herdr-"+w.ID)
	w.Environment = nil
	allowed := remoteEnvironmentKeys()
	for _, pair := range env {
		key, _, _ := strings.Cut(pair, "=")
		if allowed[key] {
			w.Environment = append(w.Environment, pair)
		}
	}
	encoded, err := json.Marshal(w)
	if err != nil {
		return w, err
	}
	if err := writePrivate(remoteContextPath(root), encoded); err != nil {
		return w, err
	}
	binary, err := resolvedExecutable()
	if err != nil {
		return w, err
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %s _remote-herdr %s %s \"$@\"\n", shellQuote(binary), shellQuote(root), shellQuote(w.ID))
	if err := writePrivate(w.Executable, []byte(script)); err != nil {
		return w, err
	}
	if err := os.Chmod(w.Executable, 0700); err != nil {
		return w, err
	}
	w.Environment = nil
	return w, nil
}

func remoteEnvironmentKeys() map[string]bool {
	allowed := map[string]bool{originalEnvKey: true, "AGENT_HARBOR_BRIDGE_HOME": true, "AGENT_HARBOR_BRIDGE_LAUNCH_CWD": true}
	for _, key := range userPathKeys {
		allowed[key] = true
	}
	return allowed
}

func remoteHerdr(args []string) error {
	if len(args) < 2 {
		return errors.New("invalid remote workspace invocation")
	}
	root, id := filepath.Clean(args[0]), args[1]
	if !filepath.IsAbs(root) {
		return errors.New("invalid remote state path")
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	w, err := readRemoteWorkspace(root)
	if err != nil {
		return fmt.Errorf("remote workspace unavailable: %w", err)
	}
	if id != w.ID {
		return errors.New("remote workspace identity changed; reconnect requires explicit review")
	}
	forwarded := args[2:]
	if len(forwarded) > 0 && forwarded[0] == "--session" {
		if len(forwarded) < 2 || forwarded[1] != herdrSession {
			return errors.New("remote workspace session mismatch")
		}
		forwarded = forwarded[2:]
	} else if len(forwarded) > 0 && strings.HasPrefix(forwarded[0], "--session=") {
		if strings.TrimPrefix(forwarded[0], "--session=") != herdrSession {
			return errors.New("remote workspace session mismatch")
		}
		forwarded = forwarded[1:]
	}
	updates := map[string]*string{"HERDR_SOCKET_PATH": nil, "HERDR_CLIENT_SOCKET_PATH": nil, "HERDR_SESSION": nil}
	allowed := remoteEnvironmentKeys()
	// Absence is part of the saved context too. A later SSH login environment
	// must not introduce a client home that was unset during preparation.
	for key := range allowed {
		updates[key] = nil
	}
	for _, pair := range w.Environment {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || !allowed[key] {
			return errors.New("invalid remote workspace environment")
		}
		updates[key] = &value
	}
	env := replaceEnvironment(os.Environ(), updates)
	return syscall.Exec(w.Herdr, append([]string{w.Herdr, "--session", herdrSession}, forwarded...), env)
}
