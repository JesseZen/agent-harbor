package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const harborPluginID = "agent-harbor.bridge"

type popupFiles struct {
	config, manifest, pending string
}

func workspacePopupFiles(root string) popupFiles {
	herdr := filepath.Join(root, "herdr")
	return popupFiles{filepath.Join(herdr, "config", "herdr.toml"), filepath.Join(herdr, "plugin", "herdr-plugin.toml"), filepath.Join(herdr, "popup-upgrade.pending")}
}

func readPopupFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return "", fmt.Errorf("expected a regular Harbor file at %s", path)
	}
	data, err := os.ReadFile(path)
	if err == nil && len(data) == 0 {
		err = fmt.Errorf("empty Harbor file at %s", path)
	}
	return string(data), err
}

func generatedConfigExecutable(config string, shortcuts shortcutPreferences) string {
	_, status, found := strings.Cut(config, `tab_bar_right = [{ type = "command", command = `)
	var command string
	if !found || json.NewDecoder(strings.NewReader(status)).Decode(&command) != nil || !strings.HasPrefix(command, "'") {
		return ""
	}
	quoted, ok := strings.CutSuffix(command[1:], "' _status")
	if !ok {
		return ""
	}
	executable := strings.ReplaceAll(quoted, "'\\''", "'")
	if filepath.IsAbs(executable) && (config == configWithShortcuts(executable, shortcuts) || shortcuts == defaultShortcuts() && config == legacyHerdrConfig(executable)) {
		return executable
	}
	return ""
}

func (f popupFiles) prepare(root string, env []string, entry packageEntry, bin string, shortcuts shortcutPreferences, running bool) error {
	previous, err := readPopupFile(f.config)
	if err != nil {
		return err
	}
	manifest, err := readPopupFile(f.manifest)
	if err != nil {
		return err
	}
	pending, err := readPopupFile(f.pending)
	if err != nil {
		return err
	}
	nextBin := bin
	if entry.selector != "" {
		nextBin = workspacePackageEntry(root)
	}
	next := configWithShortcuts(nextBin, shortcuts)
	nextManifest := herdrManifest(nextBin)
	priorBin := generatedConfigExecutable(previous, shortcuts)
	var server popupServer
	if pending != "" {
		var backup popupBackup
		if json.Unmarshal([]byte(pending), &backup) != nil || !entry.ownsVersion(backup.Executable) || backup.Selector != entry.selector ||
			generatedConfigExecutable(backup.Config, shortcuts) != backup.Executable ||
			(previous != backup.Config && previous != next) || (manifest != herdrManifest(backup.Executable) && manifest != nextManifest) {
			return errors.New("popup upgrade journal does not match the workspace files or installation")
		}
		if running {
			server, err = ownedPopupServer(root, env)
			if err != nil {
				return err
			}
		}
		if err := f.restore(env, server, backup); err != nil {
			return fmt.Errorf("popup upgrade recovery is incomplete: %w", err)
		}
		previous, priorBin, manifest = backup.Config, backup.Executable, herdrManifest(backup.Executable)
	}
	if (running || previous != "") && (priorBin == "" || priorBin != bin && priorBin != nextBin && !entry.ownsVersion(priorBin)) {
		return errors.New("the managed Herdr configuration is not recognized for this Harbor installation; it was left in place")
	}
	if manifest != "" && manifest != herdrManifest(priorBin) && !(previous == "" && manifest == nextManifest) {
		return errors.New("the managed Harbor plugin manifest was modified; it was left in place")
	}
	if entry.selector != "" {
		if err := entry.installWorkspaceEntry(nextBin); err != nil {
			return err
		}
	}
	if running {
		if priorBin == nextBin {
			if manifest == "" {
				return errors.New("managed Harbor plugin manifest is missing")
			}
			return nil
		}
		if manifest == "" {
			return errors.New("cannot migrate a live workspace without its original Harbor plugin manifest")
		}
		if server.socket == "" {
			server, err = ownedPopupServer(root, env)
			if err != nil {
				return err
			}
		}
		return f.migrate(env, server, popupBackup{previous, priorBin, entry.selector, false}, next, nextBin)
	}
	if err := os.MkdirAll(filepath.Dir(f.manifest), 0700); err != nil {
		return err
	}
	if err := writePrivate(f.config, []byte(next)); err != nil {
		return err
	}
	if err := writePrivate(f.manifest, []byte(nextManifest)); err != nil {
		return err
	}
	if err := herdrCommand(env, "--session", herdrSession, "plugin", "link", filepath.Dir(f.manifest)).Run(); err != nil {
		return fmt.Errorf("cannot register bridge in isolated Herdr: %w", err)
	}
	return nil
}

type popupBackup struct {
	Config, Executable, Selector string
	Enabled                      bool
}

func (f popupFiles) migrate(env []string, server popupServer, backup popupBackup, next, nextBin string) error {
	if err := checkHerdrConfig(env, f.config, []byte(next)); err != nil {
		return err
	}
	plugin, err := server.harborPlugin(f.manifest)
	if err != nil || !plugin.uses(backup.Executable) {
		return errors.New("the live Harbor plugin registration does not match the generated manifest")
	}
	backup.Enabled = plugin.Enabled
	data, _ := json.Marshal(backup)
	if err := writePrivate(f.pending, data); err != nil {
		return err
	}
	apply := func() error {
		if err := writePrivate(f.config, []byte(next)); err != nil {
			return err
		}
		if err := server.reload(env); err != nil {
			return err
		}
		if err := writePrivate(f.manifest, []byte(herdrManifest(nextBin))); err != nil {
			return err
		}
		if err := server.link(f.manifest, nextBin, backup.Enabled); err != nil {
			return err
		}
		return os.Remove(f.pending)
	}
	if err := apply(); err != nil {
		if restoreErr := f.restore(env, server, backup); restoreErr != nil {
			return fmt.Errorf("popup upgrade failed and rollback is incomplete; reopen Harbor to retry rollback: %w", errors.Join(err, restoreErr))
		}
		return fmt.Errorf("popup upgrade failed; previous configuration and registration restored: %w", err)
	}
	return nil
}

func (f popupFiles) restore(env []string, server popupServer, backup popupBackup) error {
	if err := errors.Join(writePrivate(f.config, []byte(backup.Config)), writePrivate(f.manifest, []byte(herdrManifest(backup.Executable)))); err != nil {
		return err
	}
	if server.socket != "" {
		reloadErr := server.reload(env)
		plugin, linkErr := server.harborPlugin(f.manifest)
		if linkErr == nil && (!plugin.uses(backup.Executable) || plugin.Enabled != backup.Enabled) {
			linkErr = server.link(f.manifest, backup.Executable, backup.Enabled)
		}
		if err := errors.Join(reloadErr, linkErr); err != nil {
			return err
		}
	}
	return os.Remove(f.pending)
}

type popupServer struct{ socket, identity string }

func ownedPopupServer(root string, env []string) (popupServer, error) {
	var server popupServer
	config := environmentValue(env, "XDG_CONFIG_HOME")
	for _, app := range []string{"herdr", "herdr-dev"} {
		socket := filepath.Join(config, app, "sessions", herdrSession, "herdr.sock")
		if managedSocket(root, socket) {
			if server.socket != "" {
				return popupServer{}, errors.New("multiple managed Herdr sockets; popup upgrade was not attempted")
			}
			server = popupServer{socket, socketIdentity(socket)}
		}
	}
	if server.identity == "" {
		return popupServer{}, errors.New("cannot verify the live Herdr socket for popup upgrade")
	}
	return server, nil
}

func (s popupServer) same() bool { return s.identity != "" && socketIdentity(s.socket) == s.identity }

func (s popupServer) reload(env []string) error {
	if !s.same() {
		return errors.New("Herdr socket changed during popup upgrade")
	}
	env = replaceEnvironment(env, map[string]*string{"HERDR_SOCKET_PATH": &s.socket})
	if err := reloadShortcuts(env); err != nil {
		return err
	}
	if !s.same() {
		return errors.New("Herdr socket changed during popup reload")
	}
	return nil
}

type popupPlugin struct {
	ID       string `json:"plugin_id"`
	Manifest string `json:"manifest_path"`
	Enabled  bool   `json:"enabled"`
	Source   struct {
		Kind string `json:"kind"`
	} `json:"source"`
	Panes []struct {
		ID      string   `json:"id"`
		Command []string `json:"command"`
	} `json:"panes"`
}

func (p popupPlugin) owns(manifest string) bool {
	canonical, err := filepath.EvalSymlinks(manifest)
	return err == nil && p.ID == harborPluginID && p.Manifest == canonical && p.Source.Kind == "local"
}

func (p popupPlugin) uses(executable string) bool {
	return len(p.Panes) == 2 && p.Panes[0].ID == "codex" && p.Panes[1].ID == "session" &&
		slices.Equal(p.Panes[0].Command, []string{executable, "_pane"}) && slices.Equal(p.Panes[1].Command, p.Panes[0].Command)
}

func (s popupServer) harborPlugin(manifest string) (popupPlugin, error) {
	var result struct {
		Type    string        `json:"type"`
		Plugins []popupPlugin `json:"plugins"`
	}
	if !s.same() || agentRPC(s.socket, "plugin.list", map[string]any{"plugin_id": harborPluginID}, &result) != nil || !s.same() ||
		result.Type != "plugin_list" || len(result.Plugins) != 1 || !result.Plugins[0].owns(manifest) {
		return popupPlugin{}, errors.New("cannot verify the live Harbor plugin registration")
	}
	return result.Plugins[0], nil
}

func (s popupServer) link(manifest, executable string, enabled bool) error {
	var result struct {
		Type   string      `json:"type"`
		Plugin popupPlugin `json:"plugin"`
	}
	params := map[string]any{"path": filepath.Dir(manifest), "enabled": enabled}
	if !s.same() || agentRPC(s.socket, "plugin.link", params, &result) != nil || !s.same() || result.Type != "plugin_linked" ||
		!result.Plugin.owns(manifest) || !result.Plugin.uses(executable) || result.Plugin.Enabled != enabled {
		return errors.New("Herdr did not acknowledge the live Harbor plugin registration")
	}
	return nil
}
