package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *harborModel) updateRowHover(previousScreen string, msg tea.Msg) {
	m.rowHover = ""
	if m.screen != previousScreen || m.settings != nil {
		m.mousePosition = nil
		return
	}
	if mouse, ok := msg.(tea.MouseMsg); ok && mouse.Action == tea.MouseActionMotion {
		m.mousePosition = &mouse
	}
	if m.mousePosition == nil {
		return
	}
	for _, hit := range m.mouseRegions() {
		if hit.contains(m.mousePosition.X, m.mousePosition.Y) {
			m.rowHover = hit.ID
			return
		}
	}
}

// Geometry comes exclusively from the final rendered page.
func (m harborModel) mouseRegions() []uiRect {
	if p, ok := m.additionalPage(); ok {
		return p.Hits
	}
	if m.screen == "confirm" || m.screen == "confirm-evidence" {
		return m.reviewLayout().Hits
	}
	if m.screen == "sessions" || m.screen == "sessions-more" {
		return m.sessionsLayout().Hits
	}
	return nil
}
func (m *harborModel) pinList(start int) {
	m.mouseListStart = start
	m.mouseListScreen = m.screen
}

func (m harborModel) listStart(cursor, count int) int {
	if m.mouseListScreen == m.screen && cursor >= m.mouseListStart && cursor < m.mouseListStart+count {
		return m.mouseListStart
	}
	return max(0, cursor-count+1)
}
