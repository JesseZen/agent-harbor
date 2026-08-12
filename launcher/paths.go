package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type productPaths struct {
	Home        string
	ConfigRoot  string
	StateRoot   string
	TUIConfig   string
	TUIData     string
	Runtime     string
	RuntimeBase string
}

func resolveProductPaths(manifest bundleManifest) (productPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return productPaths{}, fmt.Errorf("resolve HOME: %w", err)
	}
	paths := productPaths{Home: filepath.Clean(home)}
	if runtime.GOOS == "darwin" {
		paths.ConfigRoot = filepath.Join(home, ".agent-harbor")
		paths.StateRoot = filepath.Join(paths.ConfigRoot, "state")
		paths.RuntimeBase = filepath.Join(home, "Library", "Application Support", "Agent Harbor", "runtime")
	} else {
		legacy := filepath.Join(home, ".agent-harbor")
		if _, err := os.Stat(filepath.Join(legacy, "config.yaml")); err == nil {
			paths.ConfigRoot = legacy
			paths.StateRoot = filepath.Join(legacy, "state")
		} else {
			paths.ConfigRoot = filepath.Join(xdgBase(home, "XDG_CONFIG_HOME", ".config"), "agent-harbor")
			paths.StateRoot = filepath.Join(xdgBase(home, "XDG_STATE_HOME", filepath.Join(".local", "state")), "agent-harbor")
		}
		paths.RuntimeBase = filepath.Join(xdgBase(home, "XDG_DATA_HOME", filepath.Join(".local", "share")), "agent-harbor", "runtime")
	}
	paths.TUIConfig = pickTUIPath(home, filepath.Join(xdgBase(home, "XDG_CONFIG_HOME", ".config"), "agent-deck"), "config.toml", "config.json")
	paths.TUIData = pickTUIPath(home, filepath.Join(xdgBase(home, "XDG_DATA_HOME", filepath.Join(".local", "share")), "agent-deck"), "skills", "conductor", "watcher", "watchers")
	if override := strings.TrimSpace(os.Getenv("AGENT_HARBOR_RUNTIME_DIR")); override != "" {
		if !filepath.IsAbs(override) {
			return productPaths{}, errors.New("AGENT_HARBOR_RUNTIME_DIR must be absolute")
		}
		paths.RuntimeBase = filepath.Clean(override)
	}
	paths.Runtime = filepath.Join(paths.RuntimeBase, manifest.BundleID)
	return paths, nil
}

func pickTUIPath(home, xdg string, markers ...string) string {
	legacy := filepath.Join(home, ".agent-deck")
	for _, marker := range markers {
		if _, err := os.Lstat(filepath.Join(legacy, marker)); err == nil {
			return legacy
		}
	}
	return xdg
}

func xdgBase(home, envName, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(home, fallback)
}

func directoryEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
