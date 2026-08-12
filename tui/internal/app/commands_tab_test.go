package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/spotlight"
	"github.com/asheshgoplani/agent-deck/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSpotlightAllowsMainTabClicks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	i18n.SetLocale("en")
	ui.InitTheme("tokyonight")

	model := loadedRootModel(t, &appBackend{})
	model.active = tabOverview
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	if !model.commands.IsOpen() {
		t.Fatal("expected Spotlight open")
	}

	row := ansi.Strip(strings.SplitN(model.View(), "\n", 2)[0])
	x := strings.Index(row, "Sessions") + 1
	if x <= 0 {
		t.Fatalf("Sessions tab not found in %q", row)
	}
	updated, _ = model.Update(tea.MouseMsg{
		X: x, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	model = updated.(*Model)
	if model.active != tabSessions {
		t.Fatalf("active=%d want Sessions", model.active)
	}
	if !model.commands.IsOpen() {
		t.Fatal("tab click must keep Spotlight open")
	}
}

func TestSpotlightNextPrevTabStayOnRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	i18n.SetLocale("en")
	ui.InitTheme("tokyonight")

	model := loadedRootModel(t, &appBackend{})
	model.active = tabOverview
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	for _, r := range "next-tab" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	filterBefore, cursorBefore, _ := model.commands.NavState()
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.active != tabSessions {
		t.Fatalf("next-tab active=%d", model.active)
	}
	if !model.commands.IsOpen() || model.commandKind != spotlight.KindRoot {
		t.Fatalf("next-tab should stay on root Spotlight, kind=%q open=%v",
			model.commandKind, model.commands != nil && model.commands.IsOpen())
	}
	if len(model.commandStack) != 0 {
		t.Fatalf("stack should stay empty on root, got %d", len(model.commandStack))
	}
	filterAfter, cursorAfter, _ := model.commands.NavState()
	if filterAfter != filterBefore || cursorAfter != cursorBefore {
		t.Fatalf("next-tab reset nav: before filter=%q cursor=%d after filter=%q cursor=%d",
			filterBefore, cursorBefore, filterAfter, cursorAfter)
	}

	// Clear filter so prev-tab is reachable, move onto it, then run — nav must stick.
	for model.commands.Filter() != "" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = updated.(*Model)
	}
	for _, r := range "prev-tab" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(*Model)
	}
	filterBefore, cursorBefore, _ = model.commands.NavState()
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.active != tabOverview {
		t.Fatalf("prev-tab active=%d", model.active)
	}
	if !model.commands.IsOpen() || model.commandKind != spotlight.KindRoot {
		t.Fatal("prev-tab should stay on root Spotlight")
	}
	filterAfter, cursorAfter, _ = model.commands.NavState()
	if filterAfter != filterBefore || cursorAfter != cursorBefore {
		t.Fatalf("prev-tab reset nav: before filter=%q cursor=%d after filter=%q cursor=%d",
			filterBefore, cursorBefore, filterAfter, cursorAfter)
	}
}
