package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func loadedSessionWorkbench(t *testing.T) *Model {
	t.Helper()

	model := New(&appBackend{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updated.(*Model)

	updated, _ = model.Update(model.loadAll(false)())
	model = updated.(*Model)
	if model.sessionHome == nil {
		t.Fatal("Sessions tab must retain the full Agent Deck Home workbench")
	}

	updated, command := model.Update(model.sessionHome.Init()())
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	return model
}

func TestSessionsPreserveNativeNewSessionWorkflow(t *testing.T) {
	model := loadedSessionWorkbench(t)

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)

	if view := model.View(); !strings.Contains(view, "New Session") {
		t.Fatalf("Sessions did not open the native New Session dialog:\n%s", view)
	}
}

func TestSessionsPreserveNativeHelpAndInputOwnership(t *testing.T) {
	model := loadedSessionWorkbench(t)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	model = updated.(*Model)

	if model.help != nil {
		t.Fatal("Sessions opened the resource-page help instead of delegating to Agent Deck Home")
	}
	if view := model.View(); !strings.Contains(view, "KEYBOARD SHORTCUTS") {
		t.Fatalf("Sessions did not open the native Agent Deck help:\n%s", view)
	}
}

func TestSessionWorkbenchSurvivesConfigurationTabNavigation(t *testing.T) {
	model := loadedSessionWorkbench(t)
	workbench := model.sessionHome

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	model = updated.(*Model)
	if model.active != tabTargets {
		t.Fatalf("Ctrl+Right active tab = %d, want Upstreams", model.active)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	model = updated.(*Model)

	if model.active != tabSessions {
		t.Fatalf("Ctrl+Left active tab = %d, want Sessions", model.active)
	}
	if model.sessionHome != workbench {
		t.Fatal("tab navigation rebuilt or replaced the Session workbench")
	}
	if view := model.View(); !strings.Contains(view, "Composed session") {
		t.Fatalf("Session workbench state was lost after tab navigation:\n%s", view)
	}
}
