package main

import tea "github.com/charmbracelet/bubbletea"

// A mutation owns its page until completion. Runtime polling and resize remain
// responsive; only its completion message may apply the result to UI state.
type appServiceDone struct {
	id    uint64
	err   error
	apply func(*harborModel, error) tea.Cmd
}

func (m harborModel) serviceTask(work func() error, apply func(*harborModel, error) tea.Cmd) (tea.Model, tea.Cmd, bool) {
	m.serviceTaskID++
	m.servicePending = true
	id := m.serviceTaskID
	return m, func() tea.Msg { return appServiceDone{id, work(), apply} }, true
}
