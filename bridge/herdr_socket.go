package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Darwin allows 104 bytes including the NUL. Use the longer debug app name
// and client socket for both platforms so an installed build can change safely.
const herdrSocketSuffix = "/herdr-dev/sessions/agent-harbor/herdr-client.sock"

func managedConfigContext(root string) (string, error) {
	config := filepath.Join(root, "herdr", "config")
	if len(config+herdrSocketSuffix) < 104 {
		return config, nil
	}
	// TMPDIR on macOS commonly exceeds the socket budget itself. /tmp is a
	// system temporary directory on supported Unix platforms; resolve its system
	// alias before creating a private namespace, never follow a namespace link.
	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return "", err
	}
	namespace := filepath.Join(base, fmt.Sprintf("ah-%d", os.Getuid()))
	if err := os.Mkdir(namespace, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(namespace)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("unsafe managed Herdr socket namespace; inspect %s", namespace)
	}
	canonical, err := filepath.EvalSymlinks(config)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonical))
	alias := filepath.Join(namespace, fmt.Sprintf("%x", digest[:16]))
	if len(alias+herdrSocketSuffix) >= 104 {
		return "", fmt.Errorf("managed Herdr socket context exceeds platform path limit")
	}
	if err := os.Symlink(canonical, alias); err != nil && !os.IsExist(err) {
		return "", err
	}
	target, err := os.Readlink(alias)
	if err != nil || target != canonical {
		return "", fmt.Errorf("managed Herdr config alias changed; inspect %s", alias)
	}
	return alias, nil
}

// Resolve directory aliases but retain socketIdentity's no-final-symlink rule.
// Canonical containment prevents a nested link from granting access to another
// workspace, while aliases outside the root must be our validated config alias.
func managedSocket(root, path string) bool {
	if socketIdentity(path) == "" {
		return false
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	managed, err := filepath.EvalSymlinks(filepath.Join(root, "herdr"))
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(managed, canonical)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	lexical, err := filepath.Rel(filepath.Join(root, "herdr"), path)
	if err == nil && lexical != ".." && !strings.HasPrefix(lexical, ".."+string(filepath.Separator)) {
		return true
	}
	config, err := managedConfigContext(root)
	if err != nil {
		return false
	}
	for _, app := range []string{"herdr", "herdr-dev"} {
		if path == filepath.Join(config, app, "sessions", herdrSession, "herdr.sock") {
			return true
		}
	}
	return false
}
