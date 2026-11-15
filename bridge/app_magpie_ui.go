package main

import tea "github.com/charmbracelet/bubbletea"

// Compatibility hook: the page owner migrates reachable states here.
func (m harborModel) connectionPage() (uiPage, bool) { return uiPage{}, false }
func (m harborModel) connectionPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	return m, nil, false
}
