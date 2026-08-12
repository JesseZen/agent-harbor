package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/preferences"
	"github.com/asheshgoplani/agent-deck/internal/spotlight"
	"github.com/asheshgoplani/agent-deck/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSpotlightLanguageCommandPersistsLocale(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	i18n.SetLocale("en")
	ui.InitTheme("tokyonight")

	model := loadedRootModel(t, &appBackend{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	if model.commands == nil || !model.commands.IsOpen() {
		t.Fatal("expected Spotlight open")
	}
	view := model.View()
	if !strings.Contains(view, "search:") || !strings.Contains(view, "Language") || !strings.Contains(view, "◆ ") {
		t.Fatalf("v2 Grok chrome missing:\n%s", view)
	}

	for _, r := range "language" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.commandKind != spotlight.KindLanguage {
		t.Fatalf("kind=%q", model.commandKind)
	}
	for _, r := range "zh-CN" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if i18n.GetLocale() != i18n.LocaleZhCN {
		t.Fatalf("locale=%q", i18n.GetLocale())
	}
	prefs, err := preferences.Load()
	if err != nil || prefs.Locale != "zh-CN" {
		t.Fatalf("prefs=%+v err=%v", prefs, err)
	}
	if model.status == "" || !strings.Contains(model.status, "zh-CN") {
		t.Fatalf("expected transient language status, got %q", model.status)
	}
	if cmd == nil {
		t.Fatal("expected transient status clear tick")
	}
	// Stay in language submenu after select; Esc pops to root, Esc again dismisses.
	if !model.commands.IsOpen() || model.commandKind != spotlight.KindLanguage {
		t.Fatalf("expected stay on language submenu, kind=%q open=%v", model.commandKind, model.commands != nil && model.commands.IsOpen())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)
	if model.commands != nil && model.commands.IsOpen() {
		t.Fatal("expected Spotlight dismissed after Esc stack drain")
	}
	// Status sits below Routes strip (and Sessions/Profiles), not between main tabs and strip.
	model.active = tabRoutes
	model.width = 100
	model.height = 30
	model.resizePages()
	routesView := model.View()
	lines := strings.Split(routesView, "\n")
	if len(lines) < 3 {
		t.Fatalf("view too short:\n%s", routesView)
	}
	if strings.Contains(lines[1], "zh-CN") {
		t.Fatalf("status should not sit above secondary strip:\n%s", routesView)
	}
	if !strings.Contains(lines[2], "zh-CN") && !strings.Contains(lines[2], "语言") && !strings.Contains(lines[2], "Language") {
		t.Fatalf("status should be below secondary strip:\n%s", routesView)
	}
	updated, _ = model.Update(clearStatusMsg{generation: model.statusGen, text: model.status})
	model = updated.(*Model)
	if model.status != "" {
		t.Fatalf("transient status should clear, got %q", model.status)
	}
}

func TestSpotlightThemeCommandAppliesPalette(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	ui.InitTheme("tokyonight")
	base := string(ui.ColorAccent)

	model := loadedRootModel(t, &appBackend{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	for _, r := range "theme" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.commandKind != spotlight.KindTheme {
		t.Fatalf("kind=%q", model.commandKind)
	}
	for _, r := range "nord" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if string(ui.ColorAccent) == base {
		t.Fatal("theme did not change accent")
	}
	prefs, err := preferences.Load()
	if err != nil || prefs.Theme != "nord" {
		t.Fatalf("prefs=%+v err=%v", prefs, err)
	}
}
