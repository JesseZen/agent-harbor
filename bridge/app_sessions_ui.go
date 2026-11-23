package main

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func uiKey(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
func navigationKey(key tea.KeyMsg) tea.KeyMsg {
	switch key.String() {
	case "h":
		return uiKey("left")
	case "j":
		return uiKey("down")
	case "k":
		return uiKey("up")
	case "l":
		return uiKey("right")
	}
	return key
}

func (m harborModel) sessionActions(more bool) []uiAction {
	actions := []uiAction{{ID: "action:n", Label: "New session", Key: "n", Primary: true}, {ID: "action:r", Label: "Refresh", Key: "r"}}
	s, ok := m.selected()
	if !ok {
		return actions
	}
	label := "Check recovery"
	switch s.Runtime.Presence {
	case "observed":
		label = "Enter terminal"
	case "not_started":
		label = "Start session"
	case "historical_exit", "confirmed_exit":
		label = "Resume conversation"
	}
	if s.Runtime.Presence != "observed" && recoveryConflict(s.session) {
		label = "Choose recovery target"
	}
	if s.External {
		label = "Enter terminal"
	}
	if m.width < 50 {
		label = map[string]string{"Enter terminal": "Terminal", "Start session": "Start", "Resume conversation": "Resume", "Choose recovery target": "Choose target", "Check recovery": "Recovery"}[label]
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
func (m harborModel) sessionsTitle() string {
	if len(m.profiles) > 1 && m.activeProfile != nil {
		return "⚓ Harbor · " + m.profileShortLabel(m.activeProfile)
	}
	return "⚓ Harbor"
}
func (m harborModel) sessionsHeader() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), m.sessionsTitle()))
	p.tabs("sessions", m.uiHover, m.uiFocus)
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

type sessionListEntry struct {
	isHeader     bool
	title, meta  string
	index        int
	view         sessionView
	top          int
	firstInGroup bool
}

func (m harborModel) sessionListPlan(bodyH int) ([]sessionListEntry, int, int) {
	groups := m.sessionGroups()
	if len(groups) == 0 {
		return nil, 0, 0
	}
	entries := make([]sessionListEntry, 0, len(groups)*4)
	top, flatIdx := 0, 0
	for _, g := range groups {
		entries = append(entries, sessionListEntry{
			isHeader: true,
			title:    g.Title,
			meta:     g.Meta,
			top:      top,
		})
		top++
		for i, s := range g.Sessions {
			entries = append(entries, sessionListEntry{
				index:        flatIdx,
				view:         s,
				top:          top,
				firstInGroup: i == 0,
			})
			top += 2
			flatIdx++
		}
	}
	totalLines := top
	cursor := max(0, min(m.cursor, flatIdx-1))
	itemTop, itemBottom, preferredTop := 0, 2, 0
	for _, e := range entries {
		if !e.isHeader && e.index == cursor {
			itemTop = e.top
			itemBottom = e.top + 2
			preferredTop = itemTop
			if e.firstInGroup && bodyH >= 3 {
				preferredTop = itemTop - 1
			}
			break
		}
	}
	scrollTop := 0
	if itemBottom > bodyH {
		scrollTop = itemBottom - bodyH
	}
	if scrollTop > preferredTop && preferredTop+bodyH >= itemBottom {
		scrollTop = preferredTop
	}
	// Preserve the visible scroll offset on a click when it still contains the clicked row.
	if m.mouseListScreen == m.screen && m.mouseListStart >= 0 && m.mouseListStart <= itemTop && itemBottom <= m.mouseListStart+bodyH {
		scrollTop = m.mouseListStart
	}
	scrollTop = max(0, min(scrollTop, max(0, totalLines-bodyH)))
	return entries, totalLines, scrollTop
}

func sessionProjectShort(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(dir))
	if base == "." || base == string(osPathSep) {
		return cleanUI(dir)
	}
	return cleanUI(base)
}

const osPathSep = filepath.Separator

func (m harborModel) renderSessionPrimaryLine(s sessionView, row uiRowStyle, width int) string {
	statusWord := orUnknown(s.Status)
	statusPlain := statusDot(statusWord)
	var extraPlain, extraStyled string
	if s.External {
		extraPlain = "  External"
		extraStyled = paint(row.style("muted"), extraPlain)
	} else if badge, role := sessionStatusBadge(s); badge != "" {
		extraPlain = " [" + badge + "]"
		extraStyled = paint(row.style(role), extraPlain)
	}
	maxStatusWidth := max(1, width-6)
	rightPlainWidth := ansi.StringWidth(statusPlain) + ansi.StringWidth(extraPlain)
	var statusStyled string
	if rightPlainWidth > maxStatusWidth {
		if ansi.StringWidth(statusPlain) > maxStatusWidth {
			statusPlain = ansi.Truncate(statusPlain, maxStatusWidth, "…")
			extraPlain, extraStyled = "", ""
		} else {
			rem := max(0, maxStatusWidth-ansi.StringWidth(statusPlain))
			if rem < 4 {
				extraPlain, extraStyled = "", ""
			} else {
				extraPlain = ansi.Truncate(extraPlain, rem, "…")
				if s.External {
					extraStyled = paint(row.style("muted"), extraPlain)
				} else if _, role := sessionStatusBadge(s); role != "" {
					extraStyled = paint(row.style(role), extraPlain)
				}
			}
		}
		rightPlainWidth = ansi.StringWidth(statusPlain) + ansi.StringWidth(extraPlain)
	}
	statusStyled = paint(row.style(statusRole(statusWord)), statusPlain) + extraStyled
	nameWidth := max(1, width-rightPlainWidth-1)
	name := row.line(row.marker()+cleanUI(s.Name), "text", nameWidth)
	name = highlightSearch(name, m.filter)
	return name + paint(row.style("value"), " ") + statusStyled
}

func (m harborModel) renderSessionSecondaryLine(s sessionView, row uiRowStyle, width int) string {
	type segment struct{ text, role string }
	parts := make([]segment, 0, 4)
	if proj := sessionProjectShort(s.WorkingDirectory); proj != "" {
		parts = append(parts, segment{proj, "key"})
	}
	if cli := cleanUI(s.CLI); cli != "" {
		parts = append(parts, segment{cli, clientRole(s.CLI)})
	}
	if !s.External {
		conn := "Native"
		if s.Mode == "magpie" {
			conn = "Magpie"
			if model := cleanUI(s.MagpieModel); model != "" {
				conn += " · " + model
			}
		}
		parts = append(parts, segment{conn, "muted"})
		if len(m.profiles) > 1 || s.Profile == nil {
			parts = append(parts, segment{cleanUI(m.profileShortLabel(s.Profile)), "section"})
		}
	}
	var b strings.Builder
	used := 2
	b.WriteString(paint(row.style("value"), "  "))
	for i, part := range parts {
		if used >= width {
			break
		}
		if i > 0 {
			if used+2 >= width {
				break
			}
			b.WriteString(paint(row.style("value"), "  "))
			used += 2
		}
		avail := width - used
		text := ansi.Truncate(part.text, avail, "…")
		w := ansi.StringWidth(text)
		if w == 0 {
			continue
		}
		b.WriteString(paint(row.style(part.role), text))
		used += w
	}
	if used < width {
		b.WriteString(paint(row.style("value"), strings.Repeat(" ", width-used)))
	}
	return b.String()
}

func (m harborModel) sessionsLayout() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), m.sessionsTitle()))
	p.tabs("sessions", m.uiHover, m.uiFocus)
	if m.screen == "sessions-more" {
		if s, ok := m.selected(); ok {
			p.line("Actions for " + cleanUI(s.Name))
		}
		for _, a := range m.sessionActions(true) {
			p.shortcut(a, m.uiFocus == a.ID)
		}
		p.Hint = "↑↓/jk actions · ? help"
		return p.finish(0)
	}
	p = m.sessionsHeader()
	top := len(p.Lines)
	bodyH := m.sessionsBodyHeight()
	leftW := p.Width
	split := p.Width >= 90
	if split {
		leftW = max(30, p.Width*45/100)
	}
	left := newUIPage(leftW, bodyH)
	entries, totalLines, scrollTop := m.sessionListPlan(bodyH)
	if len(entries) == 0 {
		left.emptyState("No matching sessions")
	}
	railReserve := 0
	if !split && totalLines > bodyH {
		railReserve = 1
	}
	bandPage := newUIPage(max(3, leftW-railReserve), 10)
	for _, entry := range entries {
		h := 2
		if entry.isHeader {
			h = 1
		}
		if entry.top+h <= scrollTop || entry.top >= scrollTop+bodyH {
			continue
		}
		if entry.isHeader {
			if entry.top >= scrollTop && len(left.Lines) < bodyH {
				bandPage.Lines = nil
				bandPage.groupBand(entry.title, entry.meta)
				left.line(bandPage.Lines[0])
			}
			continue
		}
		// Never render a half-clipped session row when at least 2 rows fit.
		if bodyH >= 2 && (entry.top < scrollTop || len(left.Lines)+2 > bodyH) {
			continue
		}
		s := entry.view
		id := "session:" + s.ID
		row := m.rowStyle(id, m.sessionCursorAt(entry.index), false)
		y := len(left.Lines)
		lineW := max(1, leftW-2-railReserve)
		rendered := 0
		if entry.top >= scrollTop && len(left.Lines) < bodyH {
			left.line(m.renderSessionPrimaryLine(s, row, lineW))
			rendered++
		}
		if entry.top+1 >= scrollTop && len(left.Lines) < bodyH {
			left.line(m.renderSessionSecondaryLine(s, row, lineW))
			rendered++
		}
		if rendered > 0 {
			left.hit(id, 1, y, lineW, rendered)
		}
	}
	if split {
		left.scrollRail(0, min(bodyH, len(left.Lines)), totalLines, scrollTop)
	}
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
			if !s.External && (len(m.profiles) > 1 || s.Profile == nil) {
				right.field("", "🗂", "Profile", cleanUI(m.profileShortLabel(s.Profile)), false, false)
			}
			conversation := s.CodexSessionID
			if s.Conversation != nil {
				conversation = s.Conversation.Reference.Value
			}
			right.field("", "💬", "Conversation", orUnknown(conversation), false, false)
			if s.External {
				right.line(paint(fg("muted"), "Unmanaged Herdr agent · Enter focuses this terminal."))
			} else if warning := sessionAttention(s); warning != "" {
				for _, r := range strings.Split(ansi.Wrap(cleanUI(warning), max(1, right.Width-2), ""), "\n") {
					right.line(paint(fg("warn"), r))
				}
			}
			if !s.External && s.Conversation != nil && len(s.Conversation.History) > 0 {
				right.line(paint(fg("muted"), fmt.Sprintf("%d prior conversation transitions · press i for Details", len(s.Conversation.History))))
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
	if !split && totalLines > bodyH {
		p.scrollRail(top, min(bodyH, len(left.Lines)), totalLines, scrollTop)
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
	notice, noticeRole := m.notice, "value"
	if notice == "" {
		notice = m.discoveryNotice
		if notice != "" {
			noticeRole = "warn"
		}
	}
	if notice == "" && m.selectionMissing {
		notice = "Selected target changed. Select a session."
		noticeRole = "warn"
	}
	if notice == "" && !split {
		if s, ok := m.selected(); ok {
			if warning := sessionAttention(s); warning != "" {
				notice = warning
				noticeRole = "warn"
			}
		}
	}
	p.line(paint(fg(noticeRole), ansi.Truncate(cleanUI(notice), max(1, p.Width-2), "…")))
	hint := m.sessionsHint()
	p.Hint = hint
	return p.finish(0)
}
func (m harborModel) activateSessions(id string) (tea.Model, tea.Cmd) {
	m.uiFocus = ""
	if id != "search" {
		m.searchFocus = ""
		m.input.Blur()
	}
	if strings.HasPrefix(id, "nav:") {
		nav := strings.TrimPrefix(id, "nav:")
		m.profileReturn = ""
		m.scroll = 0
		if nav == "settings" {
			settings := m.openSettingsModel()
			m.settings = &settings
			return m, nil
		}
		if nav == "sessions" {
			m.screen = "sessions"
			return m, nil
		}
		if nav == "extensions" {
			m.screen = "extensions"
			m.scroll = 0
			return m, m.beginExtensionsInspect()
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

	if m.screen != "sessions" && m.screen != "sessions-more" {
		return m, nil, false
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		k := key.String()
		if m.screen == "sessions-more" && (k == "down" || k == "j" || k == "up" || k == "k") {
			ids := []string{}
			for _, a := range m.sessionActions(true) {
				ids = append(ids, a.ID)
			}
			m.uiFocus = uiCycle(m.uiFocus, ids, k == "up" || k == "k")
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
		for i, s := range m.visible() {
			if s.ID == id {
				if m.sessionCursorAt(i) {
					next, cmd := m.activateSessions("action:enter")
					return next, cmd, true
				}
				_, _, m.mouseListStart = m.sessionListPlan(m.sessionsBodyHeight())
				m.mouseListScreen = "sessions"
				m.cursor = i
				m.selectionMissing = false
				m.searchFocus = ""
				m.input.Blur()
				m.uiFocus = "list"
				m.detailScroll = 0
				break
			}
		}
		return m, nil, true
	}
	next, cmd := m.activateSessions(target)
	return next, cmd, true
}

func (m harborModel) sessionCursorAt(index int) bool {
	return index == m.cursor && !m.selectionMissing && m.searchFocus != "sessions" && (m.uiFocus == "" || m.uiFocus == "list")
}

func (m harborModel) sessionsHelp() string {
	lines := []string{"🚢 Sessions", "", "↑↓/jk select · ←→/hl tabs · / search", ""}
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
	return strings.Join(append(lines, "", "Click a row to move the cursor. Click the current row to open it.", "External sessions remain client-managed; unknown connections stay unknown.", "Ctrl+Z suspends on supported terminals; resume refreshes runtime state."), "\n")
}

func (m harborModel) sessionsHint() string {
	if m.searchFocus == "sessions" {
		return "Type search · ↵ list · Esc clear/back"
	}
	return "↑↓/jk select · " + tabHint + " · / search · ? help"
}
