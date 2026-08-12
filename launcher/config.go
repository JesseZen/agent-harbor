package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func commandInit(paths productPaths, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError("init accepts no arguments")
	}
	created, err := initializeConfig(paths)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(stdout, "Initialized Agent Harbor configuration at %s\n", filepath.Join(paths.ConfigRoot, "config.yaml"))
	} else {
		fmt.Fprintf(stdout, "Agent Harbor is already initialized at %s\n", filepath.Join(paths.ConfigRoot, "config.yaml"))
	}
	return nil
}

func initializeConfig(paths productPaths) (bool, error) {
	configPath := filepath.Join(paths.ConfigRoot, "config.yaml")
	if info, err := os.Lstat(configPath); err == nil {
		if info.Mode().IsRegular() {
			return false, nil
		}
		return false, fmt.Errorf("configuration path exists but is not a regular file: %s", configPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if entries, err := os.ReadDir(paths.ConfigRoot); err == nil && len(entries) != 0 {
		return false, fmt.Errorf("refusing to initialize partially populated configuration directory %s", paths.ConfigRoot)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := ensurePrivatePath(paths.ConfigRoot); err != nil {
		return false, err
	}
	if err := ensurePrivatePath(paths.StateRoot); err != nil {
		return false, err
	}
	source := []byte(minimalConfig(paths.StateRoot))
	if err := atomicPrivateWrite(configPath, source); err != nil {
		return false, fmt.Errorf("write initial configuration: %w", err)
	}
	return true, nil
}

func minimalConfig(stateRoot string) string {
	return fmt.Sprintf(`schema_version: 1
instance:
  state_root: %q
  log_level: simple
  gateway:
    host: 127.0.0.1
    port: 19091
  admin:
    unix_socket_name: agent-harbor-admin.sock
    loopback_tcp_enabled: false
    loopback_tcp_port: 19092
  tmux:
    socket_name: agent-harbor
    session_name: agent-harbor-tui
    status_bar_height: 1
client_profiles: []
content_policies: []
routes: []
backend_sets: []
endpoints: []
credentials: []
quota_groups: []
targets: []
model_policies: []
model_projections: []
compatibility_transforms: []
`, stateRoot)
}

func atomicPrivateWrite(destination string, source []byte) error {
	if err := ensurePrivatePath(filepath.Dir(destination)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".write-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(source); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return err
	}
	committed = true
	return syncDirectory(filepath.Dir(destination))
}
