package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

type appNoticeExpired struct{ id uint64 }

// Text fields own printable input, including the details shortcut.
func (m harborModel) editingText() bool {
	if m.screen == "new-directory" {
		return !m.picker.list
	}
	if m.searchFocus == m.screen {
		return true
	}
	switch m.screen {
	case "profile-new", "profile-rename", "filter", "rename", "magpie-bind", "magpie-filter", "new-project", "new-name", "new-instance-filter", "new-catalog-filter":
		return true
	}
	return false
}

func (m harborModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settings != nil && !isRuntimeRefresh(msg) {
		return m.update(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && (key.Type == tea.KeyCtrlC || key.Type == tea.KeyCtrlG) {
		return m.update(msg)
	}
	if next, cmd, handled := m.additionalPageInput(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateReviewUI(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateSessionsUI(msg); handled {
		return next, cmd
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		return m.updateMouse(mouse, time.Now())
	}
	// Any keyboard or asynchronous change that alters geometry invalidates clicks.
	beforeMouseLayout := m.mouseLayoutKey()
	if _, ok := msg.(tea.KeyMsg); ok {
		m.mouseListScreen = ""
	}
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		m.mouseListScreen = ""
	}
	deferReset := func(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
		n := next.(harborModel)
		if n.screen == "confirm" && m.screen != "confirm" {
			n.reviewOpenedAt = time.Now()
			n.uiFocus = "review:esc"
		}
		if n.screen == "sessions" && n.searchSelectedID() != m.searchSelectedID() {
			n.detailScroll = 0
		}
		if n.screen != m.screen || n.searchQuery() != m.searchQuery() {
			n.mouseListScreen = ""
		}
		if n.mouseLayoutKey() != beforeMouseLayout {
			n.mouse = mouseClick{}
		}
		if _, ok := msg.(tea.WindowSizeMsg); ok {
			n.mouse = mouseClick{}
		}
		return n, cmd
	}

	if expired, ok := msg.(appNoticeExpired); ok {
		if expired.id == m.noticeID && m.noticeTransient {
			m.notice = ""
			m.noticeTransient = false
		}
		return deferReset(m, nil)
	}
	if next, cmd, handled := m.updateSearch(msg); handled {
		return deferReset(next, cmd)
	}
	if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "up" || key.String() == "down" || key.String() == "j" || key.String() == "k" || key.String() == "/") {
		m.selectionMissing = false
	}
	oldID := m.searchSelectedID()
	next, cmd := m.update(msg)
	n := next.(harborModel)
	switch msg.(type) {
	case appLoaded:
		if oldID != "" && !strings.HasPrefix(m.screen, "magpie") && m.screen != "new-instance" && m.screen != "new-catalog" {
			found := false
			for _, v := range n.sessions {
				if v.ID == oldID {
					found = true
				}
			}
			if !found {
				n.selectionMissing = true
				n.notice = "Selected session disappeared or changed; refresh and select a target."
			}
		}
		n.restoreSearchSelection(oldID)
	case appInstances:
		if m.contextInstance == "" {
			n.restoreSearchSelection(oldID)
		}
	}
	if n.screen == "sessions" && n.searchSelectedID() != oldID {
		n.detailScroll = 0
	}
	if n.screen != m.screen {
		n.searchFocus = ""
	}
	renew := false
	switch v := msg.(type) {
	case appResult:
		renew = v.err == nil
	case appMagpieDone:
		renew = v.err == nil
	case appDirectoryValidated:
		renew = v.err == nil && m.screen == "new-project" && v.generation == m.generation && v.path == m.input.Value()
	}
	if n.notice != m.notice || n.noticeID != m.noticeID || renew {
		n.noticeID = m.noticeID + 1
		if !strings.HasPrefix(n.notice, "Cannot open management.") {
			n.noticeCanStart = false
		}
		n.noticeTransient = n.notice == "Action completed" || n.notice == "Magpie action completed" || n.notice == "Directory ready. Enter continues."
		if n.noticeTransient {
			id := n.noticeID
			cmd = tea.Batch(cmd, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return appNoticeExpired{id} }))
		}
	}
	if n.screen == "error" && m.screen != "error" {
		n.errorReturn = m.screen
		if n.errorReturn == "busy" {
			n.errorReturn = "sessions"
		}
	}
	return deferReset(n, cmd)
}

func (m harborModel) feedbackLine() string {
	if m.notice == "" || m.screen == "error" {
		return ""
	}
	if m.noticeTransient {
		return m.notice + " · ctrl+g dismiss"
	}
	hint := "! details · ctrl+g dismiss · ⚠ "
	if m.editingText() {
		hint = "ctrl+g dismiss · ⚠ "
	}
	return hint + strings.TrimSpace(m.notice)
}
