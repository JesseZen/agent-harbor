package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func codexVersion(env []string) string {
	path, err := nativeCodexPath()
	if err != nil {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	text := strings.TrimSpace(string(output))
	if !strings.HasPrefix(text, "codex-cli ") || len(text) > 80 {
		return "unknown"
	}
	for _, c := range strings.TrimPrefix(text, "codex-cli ") {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || strings.ContainsRune(".+-", c)) {
			return "unknown"
		}
	}
	return text
}

func nativeExecutable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 && (string(magic[:]) == "\x7fELF" || string(magic[:]) == "\xcf\xfa\xed\xfe" || string(magic[:]) == "\xca\xfe\xba\xbe")
}

// Resolve an explicitly installed native Codex executable on PATH.
// Unknown wrappers are not executed by the dependency probe.
func nativeCodexPath() (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if nativeExecutable(resolved) {
		// Herdr owns process recognition. Preserve the installed launch name
		// (including a codex symlink) after validating its native target.
		return filepath.Abs(path)
	}
	return "", errors.New("candidate discovery requires a native executable named codex on PATH; install the native binary explicitly")
}
