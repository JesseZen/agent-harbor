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
	if m.uiFocus == "new-client-search" || m.uiFocus == "selector-search" {
		return true
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

// Normalize page boundaries even when a specialized input handler returns early.
func (m harborModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+z":
			return m, tea.Suspend
		case "tab", "shift+tab":
			return m, nil
		}
		if m.settings == nil && !m.editingText() {
			msg = navigationKey(key)
		}
	}
	if _, ok := msg.(tea.ResumeMsg); ok {
		return m.requestRefresh()
	}

	if done, ok := msg.(appServiceDone); ok {
		if !m.servicePending || done.id != m.serviceTaskID {
			return m, nil
		}
		previousScreen := m.screen
		m.servicePending = false
		cmd := done.apply(&m, done.err)
		m.noticeID++
		m.noticeTransient = ordinaryNotice(m.notice)
		if m.noticeTransient {
			id := m.noticeID
			cmd = tea.Batch(cmd, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return appNoticeExpired{id} }))
		}
		if m.screen == "sessions" {
			m.uiFocus = "list"
			m.scroll = 0
		}
		m.updateRowHover(previousScreen, msg)
		return m, cmd
	}
	if m.servicePending {
		if key, ok := msg.(tea.KeyMsg); ok && key.Type != tea.KeyCtrlC {
			return m, nil
		}
		if _, ok := msg.(tea.MouseMsg); ok {
			return m, nil
		}
	}

	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "?" && m.settings == nil && !m.editingText() && m.screen != "help" && m.screen != "busy" && m.screen != "magpie-busy" && m.screen != "accounts-busy" && m.screen != "accounts-resetting" {
		m.secondaryHelpScroll = m.scroll
		m.secondaryHelpFocus = m.uiFocus
		m.previous = m.screen
		m.screen = "help"
		m.scroll = 0
		m.updateRowHover(m.previous, msg)
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyUp || msg.Type == tea.KeyDown {
			m.mouseListScreen = ""
		}
	case tea.WindowSizeMsg:
		m.mouseListScreen = ""
	}
	next, cmd := m.updateFrame(msg)
	n := next.(harborModel)
	if n.screen != m.screen {
		n.mouseListScreen = ""
		if n.screen == "sessions" {
			n.uiFocus = "list"
			n.scroll = 0
		}
		if n.notice == m.notice && (m.noticeTransient || ordinaryNotice(m.notice) || !feedbackDocumentTransition(m.screen, n.screen)) {
			n.notice = ""
			n.noticeTransient = false
			n.noticeID++
		}
	}
	if page, ok := n.additionalPage(); ok && n.settings == nil {
		n.scroll = max(0, min(n.scroll, page.MaxScroll))
	}
	if n.screen == "confirm" || n.screen == "confirm-evidence" {
		n.scroll = max(0, min(n.scroll, n.reviewLayout().MaxScroll))
	}
	n.updateRowHover(m.screen, msg)
	return n, cmd
}
func (m harborModel) updateFrame(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case appAccountStatus, appAccountRead, appAccountDone:
		next, cmd, _ := m.accountsPageInput(msg)
		return next, cmd
	}
	if m.settings != nil && !isRuntimeRefresh(msg) {
		return m.update(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && (key.Type == tea.KeyCtrlC || key.Type == tea.KeyCtrlG) {
		return m.update(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && !m.editingText() {
		tab := m.screen
		if tab == "profile-list" {
			tab = "profiles"
		}
		if next := adjacentTab(tab, key.String()); next != "" {
			return m.activateSessions("nav:" + next)
		}
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
	if _, ok := msg.(tea.MouseMsg); ok {
		return m, nil
	}
	deferReset := func(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
		n := next.(harborModel)
		if n.screen == "confirm" && m.screen != "confirm" {
			n.reviewOpenedAt = time.Now()
			n.uiFocus = "review:esc"
			if n.action == "query-resume" || n.action == "resume" {
				n.uiFocus = "review:enter"
			}
		}
		if n.screen == "sessions" && n.searchSelectedID() != m.searchSelectedID() {
			n.detailScroll = 0
		}
		if n.screen != m.screen || n.searchQuery() != m.searchQuery() {
			n.mouseListScreen = ""
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
		n.noticeTransient = ordinaryNotice(n.notice)
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

func ordinaryNotice(s string) bool {
	return s == "Directory selected" || s == "Directory selected. Enter continues." || strings.HasPrefix(s, "Original environment binding recorded;") || s == "Action completed" || s == "Magpie action completed" || s == "Directory ready. Enter continues." || s == "Profile saved" || strings.HasPrefix(s, "Profile selected:") || strings.HasPrefix(s, "Default saved;")
}

// Evidence belongs to the action that produced it. Documents are temporary
// views of that action; cancelling or opening another task ends its feedback.
func feedbackDocumentTransition(from, to string) bool {
	isDocument := func(screen string) bool {
		switch screen {
		case "help", "error", "detail", "confirm-evidence", "magpie-detail", "new-instance-detail", "magpie-context":
			return true
		}
		return false
	}
	return isDocument(from) || isDocument(to)
}
