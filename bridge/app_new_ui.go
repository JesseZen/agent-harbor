package main

import tea "github.com/charmbracelet/bubbletea"

// Compatibility hook: the page owner migrates reachable states here.
func (m harborModel) newSessionPage() (uiPage, bool) { return uiPage{}, false }
func (m harborModel) newSessionPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	return m, nil, false
}
