package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

func uiKey(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
func (m harborModel) sessionActions(more bool) []uiAction {
	actions := []uiAction{{ID: "action:n", Label: "New session", Key: "n", Primary: true}, {ID: "action:r", Label: "Refresh", Key: "r"}}
	s, ok := m.selected()
	if !ok {
		return actions
	}
	label := "Review target"
	switch s.Runtime.Presence {
	case "observed":
		label = "Enter session"
	case "not_started":
		label = "Start session"
	case "historical_exit", "confirmed_exit":
		label = "Review recovery"
	}
	if s.External {
		label = "Enter external"
	}
	reason := s.DiscoveryWarning
	main := []uiAction{{ID: "action:enter", Label: label, Key: "enter", Primary: true, Disabled: reason}, {ID: "action:i", Label: "Details", Key: "i"}, {ID: "more", Label: "More…", Key: "."}}
	if !more {
		return append(main, actions...)
	}
	actions = []uiAction{{ID: "action:i", Label: "Details", Key: "i"}}
	if !s.External {
		actions = append(actions, uiAction{ID: "action:e", Label: "Rename", Key: "e"}, uiAction{ID: "action:x", Label: "Remove record", Key: "x"})
		if s.Running || s.Runtime.Presence == "observed" {
			actions = append(actions, uiAction{ID: "action:s", Label: "Stop process", Key: "s"})
		}
		if !s.Running && s.Runtime.Presence != "observed" {
			if _, err := savedReferenceForReview(s.session); err == nil {
				actions = append(actions, uiAction{ID: "action:v", Label: "Review saved", Key: "v"})
			}
			if s.HerdrPane != "" {
				actions = append(actions, uiAction{ID: "action:c", Label: "Inspect previous shell", Key: "c"})
			}
		}
	}
	return append(actions, uiAction{ID: "back", Label: "Back", Key: "esc"})
}
func (m harborModel) sessionsHeader() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor · Profile: "+m.activeProfileLabel()))
	p.tabs("sessions", m.uiHover, m.uiFocus, true)
	p.shortcuts([]uiAction{{ID: "action:n", Label: "New session", Key: "n", Primary: true}, {ID: "action:r", Label: "Refresh", Key: "r"}}, m.uiFocus)
	p.search(m.searchQuery(), "Search sessions, projects or connections", m.searchFocus == "sessions", m.input)
	return p
}
func (m harborModel) sessionsBodyHeight() int {
	return max(1, m.height-len(m.sessionsHeader().Lines)-m.sessionsFooterHeight())
}
func (m harborModel) sessionsFooterHeight() int {
	p := newUIPage(m.width, m.height)
	p.shortcuts(m.sessionActions(false), m.uiFocus)
	p.Hint = m.sessionsHint()
	if m.searchFocus == "sessions" {
		p.Actions = nil
	}
	return len(p.shortcutBar().Lines) + 1
}
func (m harborModel) sessionRowHeight(s sessionView) int {
	if sessionAttention(s) != "" || m.selectionMissing {
		return 3
	}
	return 2
}
func (m harborModel) sessionsWindow() (int, int) {
	rows := m.visible()
	if len(rows) == 0 {
		return 0, 0
	}
	cursor := max(0, min(m.cursor, len(rows)-1))
	start, used := cursor, m.sessionRowHeight(rows[cursor])
	for start > 0 && used+m.sessionRowHeight(rows[start-1]) <= m.sessionsBodyHeight() {
		start--
		used += m.sessionRowHeight(rows[start])
	}
	// Preserve the visible window on a click when it can still contain the selection.
	if m.mouseListScreen == m.screen && m.mouseListStart <= cursor {
		total := 0
		for i := m.mouseListStart; i <= cursor; i++ {
			total += m.sessionRowHeight(rows[i])
		}
		if total <= m.sessionsBodyHeight() {
			start = m.mouseListStart
		}
	}
	end, used := start, 0
	for end < len(rows) && (end == start || used+m.sessionRowHeight(rows[end]) <= m.sessionsBodyHeight()) {
		used += m.sessionRowHeight(rows[end])
		end++
	}
	return start, end
}
func (m harborModel) sessionsCount() int { start, end := m.sessionsWindow(); return max(1, end-start) }
func (m harborModel) sessionsLayout() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor · Profile: "+m.activeProfileLabel()))
	p.tabs("sessions", m.uiHover, m.uiFocus, true)
	if m.screen == "sessions-more" {
		if s, ok := m.selected(); ok {
			p.line("Actions for " + cleanUI(s.Name))
		}
		for _, a := range m.sessionActions(true) {
			p.shortcut(a, m.uiFocus == a.ID)
		}
		p.Hint = "Tab focus · ? help"
		return p.finish(0)
	}
	p = m.sessionsHeader()
	rows := m.visible()
	start, end := m.sessionsWindow()
	top := len(p.Lines)
	bodyH := m.sessionsBodyHeight()
	leftW := p.Width
	split := p.Width >= 90
	if split {
		leftW = max(30, p.Width*45/100)
	}
	left := newUIPage(leftW, bodyH)
	if len(rows) == 0 {
		left.emptyState("No matching sessions")
	}
	for i := start; i < end; i++ {
		s := rows[i]
		y := len(left.Lines)
		id := "session:" + s.ID
		row := m.rowStyle(id, i == m.cursor && !m.selectionMissing && m.searchFocus != "sessions" && (m.uiFocus == "" || m.uiFocus == "list"), false)
		name := row.marker() + cleanUI(s.Name)
		status := statusDot(orUnknown(s.Status))
		status = ansi.Truncate(status, max(1, leftW-7), "…")
		text := row.line(name, "text", leftW-2-ansi.StringWidth(status)-1) +
			paint(row.style(statusRole(s.Status)), " "+status)
		text = highlightSearch(text, m.filter)
		left.line(text)
		connection := "Native"
		if s.Mode == "magpie" {
			connection = "Magpie · " + s.MagpieModel
		}
		if s.External {
			connection = "External · connection unknown"
		}
		left.line(row.line("  "+cleanUI(s.CLI)+" · "+connection+" · "+m.profileDisplayLabel(s.Profile), "value", leftW-2))
		reason := sessionAttention(s)
		if m.selectionMissing {
			reason = "Selected target changed. Select a session."
		}
		if reason != "" {
			left.line(row.line(cleanUI(reason), "warn", leftW-2))
		}
		left.hit(id, 1, y, leftW-2, min(len(left.Lines)-y, bodyH-y))
	}
	total, offset := 0, 0
	for i, row := range rows {
		if i < start {
			offset += m.sessionRowHeight(row)
		}
		total += m.sessionRowHeight(row)
	}
	left.scrollRail(0, min(bodyH, len(left.Lines)), total, offset)
	for _, r := range left.Hits {
		p.hit(r.ID, r.X, r.Y+top, r.W, r.H)
	}
	right := newUIPage(max(1, p.Width-leftW-2), 10000)
	if split {
		if s, ok := m.selected(); ok {
			right.line(paint(fg("text"), cleanUI(s.Name)))
			right.field("", "📂", "Project", cleanUI(s.WorkingDirectory), false, false)
			right.field("", "🤖", "Client", cleanUI(s.CLI), false, false)
			connection := "Native settings and login"
			if s.Mode == "magpie" {
				connection = s.InstanceID + " / " + s.MagpieModel
			}
			if s.External {
				connection = "Unknown · external session"
			}
			right.field("", "🔌", "Connection", cleanUI(connection), false, false)
			conversation := s.CodexSessionID
			if s.Conversation != nil {
				conversation = s.Conversation.Reference.Value
			}
			right.field("", "💬", "Conversation", orUnknown(conversation), false, false)
			if warning := sessionAttention(s); warning != "" {
				for _, r := range strings.Split(ansi.Wrap(cleanUI(warning), max(1, right.Width-2), ""), "\n") {
					right.line(paint(fg("warn"), r))
				}
			}
		}
	}
	rightStart := min(m.detailScroll, max(0, len(right.Lines)-bodyH))
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(left.Lines) {
			l = left.Lines[i]
		}
		if split {
			l += strings.Repeat(" ", max(0, leftW-ansi.StringWidth(l)))
			if i+rightStart < len(right.Lines) {
				l += "  " + right.Lines[i+rightStart]
			}
		}
		p.line(l)
	}
	actions := m.sessionActions(false)
	// Short popups reserve two rows for actions rather than clipping Create/Back.
	if _, ok := m.selected(); ok {
		actions = actions[:3]
	} else {
		actions = nil
	}
	if m.searchFocus == "sessions" {
		p.Actions = nil
		actions = nil
	}
	p.shortcuts(actions, m.uiFocus)
	notice := m.notice
	if notice == "" {
		notice = m.discoveryNotice
	}
	if notice == "" {
		notice = ""
	}
	p.line(paint(fg("value"), ansi.Truncate(cleanUI(notice), max(1, p.Width-2), "…")))
	hint := m.sessionsHint()
	p.Hint = hint
	return p.finish(0)
}
func (m harborModel) sessionsFocusIDs() []string {
	p := m.sessionsLayout()
	ids := []string{"list"}
	for _, r := range p.focusHits() {
		if !strings.HasPrefix(r.ID, "session:") && r.ID != "" {
			ids = append(ids, r.ID)
		}
	}
	return ids
}
func (m harborModel) activateSessions(id string) (tea.Model, tea.Cmd) {
	m.uiFocus = ""
	m.mouse = mouseClick{}
	if id != "search" {
		m.searchFocus = ""
		m.input.Blur()
	}
	if strings.HasPrefix(id, "nav:") {
		nav := strings.TrimPrefix(id, "nav:")
		m.profileReturn = ""
		if nav == "settings" {
			settings := newSettingsModel()
			settings.width, settings.height = m.width, m.height
			m.settings = &settings
			return m, nil
		}
		if nav == "sessions" {
			m.screen = "sessions"
			return m, nil
		}
		m.screen = "sessions"
		switch nav {
		case "profiles":
			return m.Update(uiKey("p"))
		case "settings":
			return m.Update(uiKey(","))
		case "magpie":
			return m.Update(uiKey("m"))
		case "accounts":
			return m.Update(uiKey("a"))
		}
	}
	if id == "more" {
		m.screen = "sessions-more"
		m.uiFocus = m.sessionActions(true)[0].ID
		return m, nil
	}
	if id == "back" {
		m.screen = "sessions"
		return m, nil
	}
	if id == "search" {
		m.searchFocus = "sessions"
		m.input.SetValue(m.filter)
		m.input.CursorEnd()
		m.input.Focus()
		return m, nil
	}
	if strings.HasPrefix(id, "action:") {
		key := strings.TrimPrefix(id, "action:")
		m.screen = "sessions"
		return m.Update(uiKey(key))
	}
	return m, nil
}
func (m harborModel) updateSessionsUI(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	return m.updateSessionsUIAt(msg, time.Now())
}
func (m harborModel) updateSessionsUIAt(msg tea.Msg, now time.Time) (tea.Model, tea.Cmd, bool) {
	if m.screen != "sessions" && m.screen != "sessions-more" {
		return m, nil, false
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		k := key.String()
		if m.screen == "sessions-more" && (k == "down" || k == "j" || k == "up" || k == "k" || k == "tab" || k == "shift+tab") {
			ids := []string{}
			for _, a := range m.sessionActions(true) {
				ids = append(ids, a.ID)
			}
			m.uiFocus = uiCycle(m.uiFocus, ids, k == "up" || k == "k" || k == "shift+tab")
			return m, nil, true
		}
		if k == "tab" || k == "shift+tab" {
			ids := m.sessionsFocusIDs()
			current := m.uiFocus
			if m.searchFocus == "sessions" {
				current = "search"
				m.searchFocus = ""
				m.input.Blur()
			}
			at := 0
			for i, id := range ids {
				if id == current {
					at = i
				}
			}
			delta := 1
			if k == "shift+tab" {
				delta = -1
			}
			m.uiFocus = ids[(at+delta+len(ids))%len(ids)]
			if m.uiFocus == "search" {
				next, cmd := m.activateSessions("search")
				return next, cmd, true
			}
			return m, nil, true
		}
		if k == "enter" && m.searchFocus == "sessions" {
			m.searchFocus = ""
			m.input.Blur()
			m.uiFocus = "list"
			return m, nil, true
		}
		if k == "enter" && m.uiFocus != "" && m.uiFocus != "list" {
			next, cmd := m.activateSessions(m.uiFocus)
			return next, cmd, true
		}
		if m.screen == "sessions-more" {
			if k == "?" {
				n, c := m.update(msg)
				return n, c, true
			}
			if k == "esc" {
				m.screen = "sessions"
				return m, nil, true
			}
			for _, a := range m.sessionActions(true) {
				if a.Key == k {
					next, cmd := m.activateSessions(a.ID)
					return next, cmd, true
				}
			}
			return m, nil, true
		}
		if k == "." && m.searchFocus != "sessions" {
			next, cmd := m.activateSessions("more")
			return next, cmd, true
		}
		return m, nil, false
	}
	mouse, ok := msg.(tea.MouseMsg)
	if !ok {
		return m, nil, false
	}
	target := m.sessionsLayout().target(mouse.X, mouse.Y)
	if mouse.Action == tea.MouseActionMotion {
		m.uiHover = strings.TrimPrefix(target, "nav:")
		return m, nil, true
	}
	if mouse.Button == tea.MouseButtonWheelUp || mouse.Button == tea.MouseButtonWheelDown {
		m.mouse = mouseClick{}
		delta := 1
		if mouse.Button == tea.MouseButtonWheelUp {
			delta = -1
		}
		if m.width >= 90 && mouse.X >= max(30, m.width*45/100)+2 {
			m.detailScroll = max(0, m.detailScroll+delta)
			return m, nil, true
		}
		m.cursor = max(0, min(len(m.visible())-1, m.cursor+delta))
		m.selectionMissing = false
		return m, nil, true
	}
	if mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
		return m, nil, true
	}
	if strings.HasPrefix(target, "session:") {
		id := strings.TrimPrefix(target, "session:")
		double := m.mouse.id == id && m.mouse.x == mouse.X && m.mouse.y == mouse.Y && !now.Before(m.mouse.at) && now.Sub(m.mouse.at) < 350*time.Millisecond
		for i, s := range m.visible() {
			if s.ID == id {
				m.mouseListStart, _ = m.sessionsWindow()
				m.mouseListScreen = "sessions"
				m.cursor = i
				m.selectionMissing = false
				break
			}
		}
		m.searchFocus = ""
		m.uiFocus = "list"
		m.detailScroll = 0
		m.mouse = mouseClick{id, mouse.X, mouse.Y, now}
		if double {
			next, cmd := m.activateSessions("action:enter")
			return next, cmd, true
		}
		return m, nil, true
	}
	next, cmd := m.activateSessions(target)
	return next, cmd, true
}

func (m harborModel) sessionsHelp() string {
	lines := []string{"🚢 Sessions", "", "↑↓ select · Tab / Shift+Tab focus · / search", ""}
	seen := map[string]bool{}
	actions := append(m.sessionActions(false), m.sessionActions(true)...)
	actions = append(actions, uiAction{ID: "settings", Key: ",", Label: "Settings"}, uiAction{ID: "connections", Key: "m", Label: "Connections"})
	actions = append(actions, uiAction{ID: "accounts", Key: "a", Label: "Accounts"})
	for _, a := range actions {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		label := a.Label
		if a.Disabled != "" {
			label += " · " + a.Disabled
		}
		lines = append(lines, kv(a.Key, label))
	}
	return strings.Join(append(lines, "", "Single click selects. Use the shortcuts in the bottom bar to act.", "External sessions remain client-managed; unknown connections stay unknown.", "Ctrl+Z suspends on supported terminals; resume refreshes runtime state."), "\n")
}

func (m harborModel) sessionsHint() string {
	if m.searchFocus == "sessions" {
		return "Type search · ↵ list · Tab focus · Esc clear/back"
	}
	return "↑↓ select · / search · Tab focus · ? help"
}
