package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

type shortcutPreferences struct {
	Harbor   string `json:"harbor"`
	Settings string `json:"settings"`
	Magpie   string `json:"magpie"`
	Prefix   string `json:"prefix,omitempty"`
	New      string `json:"new_session,omitempty"`
}

func defaultShortcuts() shortcutPreferences {
	return shortcutPreferences{Harbor: "prefix+a", Settings: "prefix+,", Magpie: "prefix+m"}
}

const (
	shortcutHarbor = iota
	shortcutSettings
	shortcutMagpie
	shortcutPrefix
	shortcutNew
	shortcutCount
)

var settingsLabels = [shortcutCount]string{"Harbor", "Settings", "Magpie", "Herdr prefix", "New session"}

func (p shortcutPreferences) prefixes() []string {
	if p.Prefix == "" {
		return []string{"ctrl+b"}
	}
	return strings.Fields(p.Prefix)
}

func (p shortcutPreferences) bindings(action int) []string {
	if action == shortcutPrefix {
		return p.prefixes()
	}
	values := [shortcutCount]string{p.Harbor, p.Settings, p.Magpie, p.Prefix, p.New}
	return strings.Fields(values[action])
}

func (p *shortcutPreferences) setBindings(action int, bindings []string) {
	values := [shortcutCount]*string{&p.Harbor, &p.Settings, &p.Magpie, &p.Prefix, &p.New}
	value := strings.Join(bindings, " ")
	if action == shortcutPrefix && value == "ctrl+b" {
		value = ""
	}
	*values[action] = value
}

// Keep the existing grammar for prefix actions. Herdr also checks the effective
// keymap, including aliases and conflicts with its built-in actions, before save.
var prefixActionSyntax = regexp.MustCompile(`^prefix\+[a-z,.;/]$`)
var directShortcutSyntax = regexp.MustCompile(`^((ctrl\+(alt\+)?|alt\+)[a-z]|f([1-9]|1[0-2]))$`)

func validateShortcuts(p shortcutPreferences) error {
	seen := map[string]int{}
	for _, action := range []int{shortcutPrefix, shortcutHarbor, shortcutSettings, shortcutMagpie, shortcutNew} {
		keys := p.bindings(action)
		if len(keys) == 0 && action != shortcutNew {
			return fmt.Errorf("%s needs at least one shortcut", settingsLabels[action])
		}
		for _, key := range keys {
			if err := validateShortcutKey(action, key); err != nil {
				return err
			}
			if owner, exists := seen[key]; exists {
				return fmt.Errorf("%s already uses %s", settingsLabels[owner], key)
			}
			seen[key] = action
		}
	}
	return nil
}

func validateShortcutKey(action int, key string) error {
	prefixAction := action != shortcutPrefix && prefixActionSyntax.MatchString(key)
	if !prefixAction && !directShortcutSyntax.MatchString(key) {
		return fmt.Errorf("%s has an unsupported shortcut: %s", settingsLabels[action], key)
	}
	if key == "ctrl+v" || prefixAction && strings.Contains("bceghjklnopqrsvwxz", strings.TrimPrefix(key, "prefix+")) {
		return fmt.Errorf("%s is reserved by Herdr", key)
	}
	return nil
}

func herdrShortcutValue(keys []string) string {
	if len(keys) == 1 {
		return strconv.Quote(keys[0])
	}
	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = strconv.Quote(key)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
func readShortcuts(root string) (shortcutPreferences, error) {
	p := defaultShortcuts()
	data, err := os.ReadFile(filepath.Join(root, "shortcuts.json"))
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	p = shortcutPreferences{}
	if err = d.Decode(&p); err != nil {
		return p, errors.New("invalid shortcuts.json; repair it or move it aside to restore defaults")
	}
	if err = validateShortcuts(p); err != nil {
		return p, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return p, errors.New("invalid shortcuts.json")
	}
	return p, nil
}
func configWithShortcuts(bin string, p shortcutPreferences) string {
	config := legacyHerdrConfig(bin)
	config = strings.Replace(config, `rows = [["state_icon", "machine", "workspace", "tab"], ["agent"], ["$harbor"]]`, `rows = [
  ["state_icon", { token = "pane", fg = "#cdd6f4", bold = true, dim = false }],
  [{ token = "machine", fg = "#9399b2", dim = false }, { token = "workspace", fg = "#9399b2", bold = false, dim = false }, { token = "agent", fg = "#89b4fa", bold = false, rules = [{ equals = "claude", fg = "#fab387" }, { equals = "pi", fg = "#94e2d5" }] }],
  [{ token = "$harbor", fg = "#9399b2", dim = false }],
]`, 1)
	config = strings.NewReplacer(
		`key = "prefix+a"`, "key = "+herdrShortcutValue(p.bindings(shortcutHarbor)),
		`key = "prefix+m"`, "key = "+herdrShortcutValue(p.bindings(shortcutMagpie)),
	).Replace(config)
	if !slices.Equal(p.prefixes(), defaultShortcuts().prefixes()) {
		config = strings.Replace(config, "[[keys.command]]", "[keys]\nprefix = "+herdrShortcutValue(p.prefixes())+"\n\n[[keys.command]]", 1)
	}
	config += shortcutPopup(bin, "_settings", "Agent Harbor settings", p.bindings(shortcutSettings))
	if keys := p.bindings(shortcutNew); len(keys) > 0 {
		config += shortcutPopup(bin, "_new", "New Harbor session", keys)
	}
	return config
}

func shortcutPopup(bin, entry, description string, keys []string) string {
	quote := "'" + strings.ReplaceAll(bin, "'", "'\\''") + "'"
	command, _ := json.Marshal(quote + " " + entry)
	return fmt.Sprintf("\n[[keys.command]]\nkey = %s\ntype = \"popup\"\ncommand = %s\ndescription = %q\nwidth = \"80%%\"\nheight = \"80%%\"\n", herdrShortcutValue(keys), command, description)
}
func herdrConfig(bin string) string { return configWithShortcuts(bin, defaultShortcuts()) }

// Save and workspace preparation serialize on the same lock. The journal makes
// a crash between the two atomic renames visible rather than silently accepted.
func saveShortcuts(p, expected shortcutPreferences) error {
	if err := validateShortcuts(p); err != nil {
		return err
	}
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err = ensureHome(root); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "herdr-open.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	journal := filepath.Join(root, "shortcuts.pending")
	if _, err = os.Stat(journal); !os.IsNotExist(err) {
		return errors.New("unfinished settings save; close this workspace and run agent-harbor settings-recover")
	}
	old, err := readShortcuts(root)
	if err != nil {
		return err
	}
	if old != expected {
		return errors.New("shortcuts changed in another window. Reopen Settings to load them")
	}
	env, snapshot, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	if original := os.Getenv(originalEnvKey); original != "" {
		snapshot = original
	}
	if out, e := herdrCommand(env, "--version").Output(); e != nil || strings.TrimSpace(string(out)) != "herdr 0.9.5" {
		return errors.New("Herdr 0.9.5 is required to save shortcuts")
	}
	bin, err := resolvedExecutable()
	if err != nil {
		return err
	}
	entry, err := packageEntryFor(bin)
	if err != nil {
		return err
	}
	files := workspacePopupFiles(root)
	path := files.config
	prior, err := os.ReadFile(path)
	if err != nil {
		return errors.New("open the Harbor workspace before saving settings")
	}
	if entry.selector != "" && string(prior) == configWithShortcuts(workspacePackageEntry(root), old) {
		bin = workspacePackageEntry(root)
		if !regularContent(bin, entry.workspaceScript()) || workspaceEntryMode(bin) != nil {
			return errors.New("workspace Harbor entry changed; settings were not changed")
		}
	}
	if _, err := os.Lstat(files.pending); !os.IsNotExist(err) {
		return errors.New("finish the workspace popup upgrade by reopening Harbor before saving settings")
	}
	if string(prior) != configWithShortcuts(bin, old) && !(old == defaultShortcuts() && string(prior) == legacyHerdrConfig(bin)) {
		return errors.New("owned Herdr configuration differs; settings were not changed")
	}
	running, err := herdrRunning(env)
	if err != nil {
		return err
	}
	if running {
		digest := sha256.Sum256([]byte(snapshot))
		stamp, e := os.ReadFile(filepath.Join(root, "herdr", "environment.sha256"))
		if e != nil || string(stamp) != hex.EncodeToString(digest[:]) {
			return errors.New("cannot verify owned server paths; settings were not changed")
		}
	}
	next := []byte(configWithShortcuts(bin, p))
	if err := checkHerdrConfig(env, path, next); err != nil {
		return fmt.Errorf("old settings retained: %w", err)
	}
	prefsPath := filepath.Join(root, "shortcuts.json")
	oldPrefs, e := os.ReadFile(prefsPath)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	backup := shortcutBackup{prior, oldPrefs, e == nil}
	data, _ := json.Marshal(backup)
	if err = writePrivate(journal, data); err != nil {
		return err
	}
	rollback := func(cause error) error {
		e := restoreShortcutBackup(root, backup)
		if e == nil && running {
			e = reloadShortcuts(env)
		}
		if e != nil {
			return fmt.Errorf("settings save failed; recovery incomplete. Close workspace and run agent-harbor settings-recover: %w", e)
		}
		os.Remove(journal)
		return fmt.Errorf("settings were restored after save failed: %w", cause)
	}
	if err = writePrivate(path, next); err != nil {
		return rollback(err)
	}
	data, _ = json.Marshal(p)
	if err = writePrivate(prefsPath, data); err != nil {
		return rollback(err)
	}
	if running {
		if err = reloadShortcuts(env); err != nil {
			return rollback(err)
		}
	}
	return os.Remove(journal)
}

func checkHerdrConfig(env []string, path string, config []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-check-*")
	if err != nil {
		return err
	}
	checkPath := temp.Name()
	defer os.Remove(checkPath)
	_, writeErr := temp.Write(config)
	if err := errors.Join(writeErr, temp.Close()); err != nil {
		return err
	}
	checkEnv := replaceEnvironment(env, map[string]*string{"HERDR_CONFIG_PATH": &checkPath})
	if out, err := herdrCommand(checkEnv, "config", "check").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "config: ok" {
		return errors.New("Herdr rejected the generated configuration")
	}
	return nil
}

type shortcutBackup struct {
	Config         []byte
	Preferences    []byte
	HadPreferences bool
}

func restoreShortcutBackup(root string, b shortcutBackup) error {
	if err := writePrivate(filepath.Join(root, "herdr", "config", "herdr.toml"), b.Config); err != nil {
		return err
	}
	path := filepath.Join(root, "shortcuts.json")
	if b.HadPreferences {
		return writePrivate(path, b.Preferences)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func reloadShortcuts(env []string) error {
	out, err := herdrCommand(env, "--session", herdrSession, "server", "reload-config").Output()
	if err != nil {
		return errors.New("owned Herdr reload failed")
	}
	var report struct {
		Result struct {
			Type        string   `json:"type"`
			Status      string   `json:"status"`
			Diagnostics []string `json:"diagnostics"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(out, &report) != nil || len(report.Error) != 0 || report.Result.Type != "config_reload" || report.Result.Status != "applied" || len(report.Result.Diagnostics) != 0 {
		return errors.New("owned Herdr did not acknowledge a complete configuration reload")
	}
	return nil
}

func recoverShortcuts() error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "herdr-open.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	running, err := herdrRunning(env)
	if err != nil {
		return err
	}
	if running {
		return errors.New("close clients and explicitly stop the owned Harbor workspace before settings-recover")
	}
	path := filepath.Join(root, "shortcuts.pending")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var b shortcutBackup
	if err = json.Unmarshal(data, &b); err != nil {
		return err
	}
	if err = restoreShortcutBackup(root, b); err != nil {
		return err
	}
	return os.Remove(path)
}
