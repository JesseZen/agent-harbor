package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Resolve native and official npm Codex installations on PATH.
// Unknown wrappers are not executed by the dependency probe.
func nativeCodexPath() (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", err
	}
	return resolveNativeCodex(path)
}

func resolveNativeCodex(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if nativeExecutable(resolved) {
		// Herdr owns process recognition. Preserve the installed launch name
		// (including a codex symlink) after validating its native target.
		return filepath.Abs(path)
	}
	if native := npmCodexBinary(resolved); native != "" {
		return native, nil
	}
	return "", errors.New("candidate discovery requires a native executable named codex on PATH; install the native binary explicitly")
}

// Resolve the official npm package's payload without executing its JS launcher.
// Both optional platform packages and the older in-package vendor layout exist.
func npmCodexBinary(entry string) string {
	if filepath.Base(entry) != "codex.js" || filepath.Base(filepath.Dir(entry)) != "bin" {
		return ""
	}
	root := filepath.Dir(filepath.Dir(entry))
	var pkg struct {
		Name string            `json:"name"`
		Bin  map[string]string `json:"bin"`
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil || json.Unmarshal(data, &pkg) != nil || pkg.Name != "@openai/codex" || filepath.ToSlash(filepath.Clean(pkg.Bin["codex"])) != "bin/codex.js" {
		return ""
	}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	triple := map[string]string{"darwin/amd64": "x86_64-apple-darwin", "darwin/arm64": "aarch64-apple-darwin", "linux/amd64": "x86_64-unknown-linux-musl", "linux/arm64": "aarch64-unknown-linux-musl"}[runtime.GOOS+"/"+runtime.GOARCH]
	if arch == "" || triple == "" {
		return ""
	}
	platformPackage := "codex-" + runtime.GOOS + "-" + arch
	// Match Node's ancestor node_modules lookup, including hoisted installs.
	for dir := root; ; dir = filepath.Dir(dir) {
		binary := filepath.Join(dir, "node_modules", "@openai", platformPackage, "vendor", triple, "bin", "codex")
		if nativeExecutable(binary) {
			return binary
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	binary := filepath.Join(root, "vendor", triple, "bin", "codex")
	if nativeExecutable(binary) {
		return binary
	}
	return ""
}
