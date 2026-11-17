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
	"strings"
	"syscall"
)

type shortcutPreferences struct {
	Harbor   string `json:"harbor"`
	Settings string `json:"settings"`
	Magpie   string `json:"magpie"`
}

func defaultShortcuts() shortcutPreferences {
	return shortcutPreferences{"prefix+a", "prefix+,", "prefix+m"}
}
func (p shortcutPreferences) values() []string { return []string{p.Harbor, p.Settings, p.Magpie} }

// Pinned Herdr0.9.5 model.rs KeysConfig defaults. Restrict grammar so shifted,
// indexed and navigation aliases cannot displace effective built-in bindings.
var shortcutSyntax = regexp.MustCompile(`^(prefix\+[a-z,.;/]|ctrl\+[a-z])$`)

func validateShortcuts(p shortcutPreferences) error {
	seen := map[string]bool{}
	for _, k := range p.values() {
		if !shortcutSyntax.MatchString(k) {
			return errors.New("use prefix+ followed by a lowercase letter or , . ; /, or ctrl+letter")
		}
		if seen[k] {
			return errors.New("each shortcut must be different")
		}
		seen[k] = true
		if k == "ctrl+b" || k == "ctrl+v" || strings.HasPrefix(k, "prefix+") && strings.Contains("bceghjklnopqrsvwxz", strings.TrimPrefix(k, "prefix+")) {
			return fmt.Errorf("%s is reserved by Herdr 0.9.5", k)
		}
	}
	return nil
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
	canonical, _ := json.Marshal(p)
	var extra any
	if d.Decode(&extra) != io.EOF || len(canonical) == 0 {
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
	config = strings.Replace(config, `key = "prefix+a"`, `key = "HARBOR_KEY"`, 1)
	config = strings.Replace(config, `key = "prefix+m"`, `key = "`+p.Magpie+`"`, 1)
	config = strings.Replace(config, `key = "HARBOR_KEY"`, `key = "`+p.Harbor+`"`, 1)
	quote := "'" + strings.ReplaceAll(bin, "'", "'\\''") + "'"
	command, _ := json.Marshal(quote + " _settings")
	return config + fmt.Sprintf("\n[[keys.command]]\nkey = %q\ntype = \"popup\"\ncommand = %s\ndescription = \"Agent Harbor settings\"\nwidth = \"80%%\"\nheight = \"80%%\"\n", p.Settings, command)
}
func herdrConfig(bin string) string { return configWithShortcuts(bin, defaultShortcuts()) }

// Save and workspace preparation serialize on the same lock. The journal makes
// a crash between the two atomic renames visible rather than silently accepted.
func saveShortcuts(p shortcutPreferences) error {
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
	path := filepath.Join(root, "herdr", "config", "herdr.toml")
	prior, err := os.ReadFile(path)
	if err != nil {
		return errors.New("open the Harbor workspace before saving settings")
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
	temp, err := os.CreateTemp(filepath.Dir(path), ".settings-check-*")
	if err != nil {
		return err
	}
	checkPath := temp.Name()
	defer os.Remove(checkPath)
	if _, err = temp.Write(next); err != nil {
		temp.Close()
		return err
	}
	temp.Close()
	checkEnv := replaceEnvironment(env, map[string]*string{"HERDR_CONFIG_PATH": &checkPath})
	if out, e := herdrCommand(checkEnv, "config", "check").CombinedOutput(); e != nil || strings.TrimSpace(string(out)) != "config: ok" {
		return errors.New("Herdr rejected shortcut configuration; old settings retained")
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
