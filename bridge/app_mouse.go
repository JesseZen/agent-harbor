package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

type mouseClick struct {
	id   string
	x, y int
	at   time.Time
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
func (m harborModel) mouseLayoutKey() string {
	ids, _ := m.searchIDs()
	return fmt.Sprintf("%s/%d/%d/%s/%s/%d/%d/%s/%d/%v", m.screen, m.width, m.height, m.searchQuery(), strings.Join(ids, "\x00"), m.scroll, m.picker.cursor, m.picker.query.Value(), m.formFocus, m.mouseRegions())
}
func (m harborModel) updateMouse(msg tea.MouseMsg, now time.Time) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updateSessionsUIAt(msg, now); handled {
		return next, cmd
	}
	if next, cmd, handled := m.additionalPageInput(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateReviewUI(msg); handled {
		return next, cmd
	}
	return m, nil
}

func (m harborModel) listStart(cursor, count int) int {
	if m.mouseListScreen == m.screen && cursor >= m.mouseListStart && cursor < m.mouseListStart+count {
		return m.mouseListStart
	}
	return max(0, cursor-count+1)
}
