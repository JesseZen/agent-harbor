package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// An explicit development override wins. Packaged use never searches PATH.
func findCodexAuth(override string) (string, error) {
	if override != "" {
		return exec.LookPath(override)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(executable), "codex-auth")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", errors.New("bundled codex-auth is missing or not executable; reinstall the matching Harbor package, or set AGENT_HARBOR_CODEX_AUTH_BIN for development")
	}
	return path, nil
}
