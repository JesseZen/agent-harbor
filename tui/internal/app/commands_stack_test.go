package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/spotlight"
	"github.com/asheshgoplani/agent-deck/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSpotlightStackEscPopsInsteadOfClosing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	i18n.SetLocale("en")
	ui.InitTheme("tokyonight")

	model := loadedRootModel(t, &appBackend{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	if !model.commands.IsOpen() {
		t.Fatal("expected Spotlight open")
	}

	for _, r := range "language" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.commandKind != spotlight.KindLanguage || !model.commands.IsOpen() {
		t.Fatalf("expected language submenu open, kind=%q open=%v", model.commandKind, model.commands.IsOpen())
	}
	if len(model.commandStack) != 1 {
		t.Fatalf("expected 1 stacked frame, got %d", len(model.commandStack))
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)
	if !model.commands.IsOpen() {
		t.Fatal("Esc should pop to root Spotlight, not dismiss")
	}
	if model.commandKind != spotlight.KindRoot {
		t.Fatalf("kind after pop=%q", model.commandKind)
	}
	if len(model.commandStack) != 0 {
		t.Fatalf("stack should be empty after pop, got %d", len(model.commandStack))
	}
	view := model.View()
	if !strings.Contains(view, "Language") && !strings.Contains(view, "/language") {
		t.Fatalf("restored root missing Language:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)
	if model.commands != nil && model.commands.IsOpen() {
		t.Fatal("Esc on root should fully dismiss Spotlight")
	}
}

func TestSpotlightLanguageSelectStaysInSubmenu(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	i18n.SetLocale("en")
	ui.InitTheme("tokyonight")

	model := loadedRootModel(t, &appBackend{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	for _, r := range "language" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)

	for _, r := range "zh-CN" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)

	if i18n.GetLocale() != i18n.LocaleZhCN {
		t.Fatalf("locale=%q", i18n.GetLocale())
	}
	if !model.commands.IsOpen() {
		t.Fatal("selecting language should keep Spotlight open")
	}
	if model.commandKind != spotlight.KindLanguage {
		t.Fatalf("should stay on language submenu, kind=%q", model.commandKind)
	}
	if len(model.commandStack) != 1 {
		t.Fatalf("root should remain stacked, got %d", len(model.commandStack))
	}
}
