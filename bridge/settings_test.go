package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortcutValidation(t *testing.T) {
	for _, key := range []string{"prefix+q", "prefix+s", "prefix+n", "ctrl+b", "ctrl+v", "prefix+shift+a", "prefix+A", "", "prefix+?"} {
		p := defaultShortcuts()
		p.Harbor = key
		if validateShortcuts(p) == nil {
			t.Fatal(key)
		}
	}
	p := defaultShortcuts()
	p.Harbor = p.Magpie
	if validateShortcuts(p) == nil {
		t.Fatal("duplicate")
	}
	p = defaultShortcuts()
	p.Harbor = "ctrl+g"
	if err := validateShortcuts(p); err != nil {
		t.Fatal(err)
	}
}
func TestShortcutBadPreferences(t *testing.T) {
	r := t.TempDir()
	for _, data := range []string{`{}`, `null`, `{"harbor":"prefix+a","settings":"prefix+,","magpie":"prefix+m"} garbage`, `{"harbor":"prefix+a","settings":"prefix+,","magpie":"prefix+m","unknown":true}`} {
		os.WriteFile(filepath.Join(r, "shortcuts.json"), []byte(data), 0600)
		if _, err := readShortcuts(r); err == nil {
			t.Fatal(data)
		}
	}
}
func TestShortcutGenerationSwap(t *testing.T) {
	p := shortcutPreferences{"prefix+m", "prefix+a", "prefix+,"}
	s := configWithShortcuts("/tmp/harbor", p)
	for _, k := range p.values() {
		if strings.Count(s, `key = "`+k+`"`) != 1 {
			t.Fatal(s)
		}
	}
}
func TestSettingsLocalEditingAndMouse(t *testing.T) {
	m := newSettingsModel()
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = n.(settingsModel)
	m.input.SetValue("ctrl+g")
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = n.(settingsModel)
	if m.values != defaultShortcuts() {
		t.Fatal("cancel changed draft")
	}
	hit := m.layout().Hits[0]
	for _, r := range m.layout().Hits {
		if r.ID == "control:1" {
			hit = r
		}
	}
	n, _ = m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = n.(settingsModel)
	if !m.editing || m.cursor != 1 {
		t.Fatal("row mouse")
	}
	m.input.SetValue("prefix+.")
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = n.(settingsModel)
	if m.values.Settings != "prefix+." {
		t.Fatal("accept")
	}
}
func TestSettingsVisual(t *testing.T) {
	out := os.Getenv("HARBOR_SETTINGS_VISUAL")
	if out == "" {
		t.Skip()
	}
	oldColor, hadColor := os.LookupEnv("NO_COLOR")
	defer func() {
		if hadColor {
			os.Setenv("NO_COLOR", oldColor)
		} else {
			os.Unsetenv("NO_COLOR")
		}
	}()
	oldProfile := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(oldProfile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, sz := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		m := newSettingsModel()
		m.width = sz[0]
		m.height = sz[1]
		for _, mode := range []string{"color", "plain"} {
			os.Unsetenv("NO_COLOR")
			if mode == "plain" {
				os.Setenv("NO_COLOR", "1")
			}
			os.WriteFile(fmt.Sprintf("%s/settings-%s-%d.ansi", out, mode, sz[0]), []byte(m.View()), 0600)
		}
	}
}
func TestShortcutSaveTransaction(t *testing.T) {
	for _, failure := range []string{"", "legacy", "reload", "rollback", "partial", "foreign", "check"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			os.Chmod(root, 0700)
			t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
			t.Setenv(originalEnvKey, "")
			if err := ensureHome(root); err != nil {
				t.Fatal(err)
			}
			env, snapshot, err := managedEnvironment(root)
			_ = env
			if err != nil {
				t.Fatal(err)
			}
			bin, _ := os.Executable()
			config := configWithShortcuts(bin, defaultShortcuts())
			if failure == "legacy" {
				config = legacyHerdrConfig(bin)
			}
			path := filepath.Join(root, "herdr", "config", "herdr.toml")
			writePrivate(path, []byte(config))
			digest := sha256.Sum256([]byte(snapshot))
			writePrivate(filepath.Join(root, "herdr", "environment.sha256"), []byte(hex.EncodeToString(digest[:])))
			stub := filepath.Join(root, "herdr-stub")
			script := "#!/bin/sh\ncase \"$*\" in\n *'--version'*) echo 'herdr 0.9.5' ;;\n *'session list --json'*) echo '{\"sessions\":[{\"name\":\"agent-harbor\",\"running\":true}]}' ;;\n *'config check'*) " + map[bool]string{true: "exit 1", false: "echo 'config: ok'"}[failure == "check"] + " ;;\n *'server reload-config'*) "
			if failure == "reload" {
				script += "if test ! -f '" + root + "/failed'; then touch '" + root + "/failed'; exit 1; fi; echo '{\"result\":{\"type\":\"config_reload\",\"status\":\"applied\",\"diagnostics\":[]}}'"
			} else if failure == "partial" {
				script += `echo '{"result":{"type":"config_reload","status":"partial","diagnostics":[]}}'`
			} else if failure == "rollback" {
				script += "exit 1"
			} else {
				script += `echo '{"result":{"type":"config_reload","status":"applied","diagnostics":[]}}'`
			}
			script += " ;;\nesac\n"
			os.WriteFile(stub, []byte(script), 0700)
			t.Setenv("AGENT_HARBOR_HERDR_BIN", stub)
			// Snapshot includes the selected executable, just as workspace startup does.
			_, snapshot, _ = managedEnvironment(root)
			digest = sha256.Sum256([]byte(snapshot))
			writePrivate(filepath.Join(root, "herdr", "environment.sha256"), []byte(hex.EncodeToString(digest[:])))
			if failure == "foreign" {
				writePrivate(path, []byte("foreign"))
			}
			p := defaultShortcuts()
			p.Settings = "prefix+."
			err = saveShortcuts(p)
			if failure == "" || failure == "legacy" {
				if err != nil {
					t.Fatal(err)
				}
				actual, e := readShortcuts(root)
				if e != nil || actual != p {
					t.Fatal(actual, e)
				}
				data, _ := os.ReadFile(path)
				if string(data) != configWithShortcuts(bin, p) {
					t.Fatal("generation drift")
				}
				return
			}
			if err == nil {
				t.Fatal("expected failure")
			}
			data, _ := os.ReadFile(path)
			want := config
			if failure == "foreign" {
				want = "foreign"
			}
			if string(data) != want {
				t.Fatal("old config lost")
			}
			actual, e := readShortcuts(root)
			if e != nil || actual != defaultShortcuts() {
				t.Fatal("preferences lost", actual, e)
			}
			_, e = os.Stat(filepath.Join(root, "shortcuts.pending"))
			if (failure == "rollback" || failure == "partial") != (e == nil) {
				t.Fatal("journal", e)
			}
			if failure == "rollback" {
				if e := prepareHerdr(root, env, snapshot); e == nil || !strings.Contains(e.Error(), "unfinished settings save") {
					t.Fatal("pending journal accepted", e)
				}
				if e := recoverShortcuts(); e == nil {
					t.Fatal("recovery touched running workspace")
				}
				os.WriteFile(stub, []byte(strings.ReplaceAll(script, `"running":true`, `"running":false`)), 0700)
				if e := recoverShortcuts(); e != nil {
					t.Fatal(e)
				}
				if _, e := os.Stat(filepath.Join(root, "shortcuts.pending")); !os.IsNotExist(e) {
					t.Fatal("journal remains")
				}
			}
		})
	}
}

func TestSettingsDetailsOwnMouse(t *testing.T) {
	m := newSettingsModel()
	m.details = true
	m.cursor = 4
	next, cmd := m.Update(tea.MouseMsg{X: 3, Y: m.height - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	n := next.(settingsModel)
	if n.busy || cmd != nil || !n.details {
		t.Fatal("hidden save activated")
	}
	next, _ = n.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if next.(settingsModel).scroll != 1 {
		t.Fatal("details wheel")
	}
}

func TestSettingsEditorCaretAfterShorterValue(t *testing.T) {
	m := newSettingsModel()
	m.input.SetValue("ctrl+g")
	m.input.CursorEnd()
	m.cursor = 2
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	n := next.(settingsModel)
	if n.input.Position() != len(n.values.Magpie) {
		t.Fatal("reused caret not at field end")
	}
}
