package main

import (
	"errors"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Only the isolated PTY gate enables this probe. The model and program options
// are production Harbor; its service and the injected panic are test-owned.
type terminalProbe struct {
	harborModel
	panicOnKey bool
}

func (m terminalProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && m.panicOnKey && key.String() == "!" {
		panic("isolated terminal cleanup probe")
	}
	next, cmd := m.harborModel.Update(msg)
	m.harborModel = next.(harborModel)
	return m, cmd
}
func TestHarborTerminalLifecycleProbe(t *testing.T) {
	mode := os.Getenv("HARBOR_TERMINAL_PROBE")
	if mode == "" {
		t.Skip("opt-in isolated PTY gate")
	}
	m := newHarborModel(&fakeApplicationService{})
	defer m.cancelRefresh()
	_, err := tea.NewProgram(terminalProbe{m, mode == "panic"}, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	if mode == "panic" {
		if !errors.Is(err, tea.ErrProgramPanic) {
			t.Fatalf("expected framework panic recovery, got %v", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
}
