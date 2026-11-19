package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// The integration owner owns page ordering, navigation and shared dispatch.
func (m harborModel) additionalPage() (uiPage, bool) {
	for _, render := range []func() (uiPage, bool){m.codexOptionsPage, m.profilePage, m.newSessionPage, m.connectionPage, m.accountsPage, m.secondaryPage} {
		if p, ok := render(); ok {
			return p, true
		}
	}
	return uiPage{}, false
}
func (m harborModel) additionalPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if p, ok := m.additionalPage(); ok {
		if mouse, ok := msg.(tea.MouseMsg); ok && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			if target := p.target(mouse.X, mouse.Y); strings.HasPrefix(target, "nav:") {
				next, cmd := m.activateSessions(target)
				return next, cmd, true
			}
		}
	}
	for _, update := range []func(tea.Msg) (tea.Model, tea.Cmd, bool){m.codexOptionsInput, m.profilePageInput, m.newSessionPageInput, m.connectionPageInput, m.accountsPageInput, m.secondaryPageInput} {
		if next, cmd, ok := update(msg); ok {
			return next, cmd, true
		}
	}
	return m, nil, false
}
