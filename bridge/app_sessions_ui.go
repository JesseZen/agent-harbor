package main

import (
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
func (m harborModel) sessionsFooter(p *uiPage) {
	if m.searchFocus == "sessions" {
		p.Actions = nil
	} else if _, ok := m.selected(); ok {
		p.shortcuts(m.sessionActions(false)[:3], m.uiFocus)
	}
	p.Hint = m.sessionsHint()
}

type sessionGeometry struct {
	body, top, left, right, row int
	split                       bool
}

func sessionColumns(width, body int) sessionGeometry {
	g := sessionGeometry{body: body, left: width, row: 2}
	if width >= 90 && body >= 12 {
		g.split = true
		g.row = 1
		g.left = max(32, (width-2)*35/100)
		g.right = width - g.left - 2
	}
	return g
}

func (m harborModel) sessionGeometry() sessionGeometry {
	return m.sessionsGeometryFor(m.sessionsHeader())
}

func (m harborModel) sessionsGeometryFor(header uiPage) sessionGeometry {
	m.sessionsFooter(&header)
	top := len(header.Lines)
	body := m.height - top - len(header.shortcutBar().Lines) - 1
	g := sessionColumns(max(1, m.width-2), max(1, body))
	g.top = top
	return g
}

type sessionListEntry struct {
	isHeader     bool
	title, meta  string
	index        int
	view         sessionView
	top          int
	firstInGroup bool
}

func (m harborModel) sessionListPlan(geometry sessionGeometry) ([]sessionListEntry, int, int) {
	bodyH, rowH := geometry.body, geometry.row
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
			top += rowH
			flatIdx++
		}
	}
	totalLines := top
	cursor := max(0, min(m.cursor, flatIdx-1))
	itemTop, itemBottom, preferredTop := 0, 2, 0
	for _, e := range entries {
		if !e.isHeader && e.index == cursor {
			itemTop = e.top
			itemBottom = e.top + rowH
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
	if rowH == 2 && bodyH >= rowH {
		for _, entry := range entries {
			if !entry.isHeader && entry.top+1 == scrollTop && itemBottom <= entry.top+bodyH {
				scrollTop = entry.top
				break
			}
		}
	}
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

func (m harborModel) renderSessionPrimaryLine(s sessionView, row uiRowStyle, width int, wide bool, suffix string) string {
	icon, word := m.sessionIndicator(s, row, !wide)
	badge, role := sessionStatusBadge(s)
	if badge != "" && (wide && badge != "Pending ID" || !wide && badge != sessionWord(s)) {
		word += paint(row.style(role), " ["+badge+"]")
	}
	space := max(1, width-4-ansi.StringWidth(word))
	suffix = ansi.Truncate(suffix, max(0, space-2), "…")
	name := ansi.Truncate(cleanUI(s.Name), max(1, space-ansi.StringWidth(suffix)), "…") + suffix
	return paint(row.style("text"), row.marker()) + icon + paint(row.style("text"), " ") + highlightSearch(row.line(name, "text", space), m.filter) + word
}

func (m harborModel) renderSessionSecondaryLine(s sessionView, row uiRowStyle, width int) string {
	type segment struct{ text, role string }
	parts := make([]segment, 0, 4)
	if proj := sessionProjectShort(s.WorkingDirectory); proj != "" {
		parts = append(parts, segment{proj, "value"})
	}
	if cli := cleanUI(s.CLI); cli != "" {
		parts = append(parts, segment{cli, clientRole(s.CLI)})
	}
	if !s.External {
		conn := "Native"
		if s.Mode == "magpie" {
			conn = "Magpie"
		}
		parts = append(parts, segment{conn, "muted"})
		if len(m.profiles) > 1 || s.Profile == nil {
			parts = append(parts, segment{cleanUI(m.profileShortLabel(s.Profile)), "muted"})
		}
		if s.Mode == "magpie" && s.MagpieModel != "" {
			parts = append(parts, segment{cleanUI(s.MagpieModel), "muted"})
		}
	}
	var b strings.Builder
	used := 4
	b.WriteString(paint(row.style("value"), "    "))
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
		if i == 0 && sessionProjectShort(s.WorkingDirectory) != "" {
			reserve := ansi.StringWidth(cleanUI(s.CLI)) + 2
			if !s.External {
				reserve += 8
			}
			if !s.External && s.Profile == nil {
				reserve += min(12, ansi.StringWidth(m.profileShortLabel(nil))) + 2
			}
			avail = max(1, avail-reserve)
		}
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
	g := m.sessionsGeometryFor(p)
	top, bodyH, leftW, split := g.top, g.body, g.left, g.split
	left := newUIPage(leftW+2, bodyH)
	entries, totalLines, scrollTop := m.sessionListPlan(g)
	suffixes := m.sessionSuffixes(entries)
	if len(entries) == 0 {
		left.emptyState("No matching sessions")
	}
	railReserve := 0
	if totalLines > bodyH {
		railReserve = 1
	}
	bandPage := newUIPage(max(3, leftW+2-railReserve), 10)
	for _, entry := range entries {
		h := g.row
		if entry.isHeader {
			h = 1
		}
		if entry.top+h <= scrollTop || entry.top >= scrollTop+bodyH {
			continue
		}
		for len(left.Lines) < entry.top-scrollTop {
			left.line("")
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
		if bodyH >= h && (entry.top < scrollTop || entry.top+h > scrollTop+bodyH) {
			continue
		}
		s := entry.view
		id := "session:" + s.ID
		row := m.rowStyle(id, m.sessionCursorAt(entry.index), false)
		y := len(left.Lines)
		lineW := max(1, leftW-railReserve)
		rendered := 0
		if entry.top >= scrollTop && len(left.Lines) < bodyH {
			left.line(m.renderSessionPrimaryLine(s, row, lineW, split, suffixes[s.ID]))
			rendered++
		}
		if !split && entry.top+1 >= scrollTop && len(left.Lines) < bodyH {
			left.line(m.renderSessionSecondaryLine(s, row, lineW))
			rendered++
		}
		if rendered > 0 {
			left.hit(id, 1, y, lineW, rendered)
		}
	}
	for len(left.Lines) < bodyH {
		left.line("")
	}
	if split {
		left.Width--
		left.scrollRail(0, bodyH, totalLines, scrollTop)
	}
	for _, r := range left.Hits {
		p.hit(r.ID, r.X, r.Y+top, r.W, r.H)
	}
	var right []string
	if split {
		if s, ok := m.selected(); ok {
			right = m.previewPane(s, g)
		}
	}
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(left.Lines) {
			l = left.Lines[i]
		}
		if split {
			l = chromeText(l)
			l += strings.Repeat(" ", max(0, leftW-ansi.StringWidth(l)))
			if i < len(right) {
				l += "  " + right[i]
			}
		}
		p.line(l)
	}
	if !split && totalLines > bodyH {
		p.scrollRail(top, bodyH, totalLines, scrollTop)
	}
	m.sessionsFooter(&p)
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
	page := p.finish(0)
	if split {
		for index := range page.Lines {
			if index < top || index >= top+bodyH {
				page.Lines[index] = chromeText(page.Lines[index])
			}
		}
		page.chromeReady = true
	}
	return page
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
			return m.updatePage(uiKey("p"))
		case "settings":
			return m.updatePage(uiKey(","))
		case "magpie":
			return m.updatePage(uiKey("m"))
		case "accounts":
			return m.updatePage(uiKey("a"))
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
		return m.updatePage(uiKey(key))
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
		g := m.sessionGeometry()
		if mouse.Y < g.top || mouse.Y >= g.top+g.body || mouse.X < 1 || mouse.X > m.width-2 {
			return m, nil, true
		}
		if g.split && mouse.X > g.left {
			if mouse.X >= g.left+3 {
				if s, ok := m.selected(); ok {
					m.preview.offset = max(0, min(m.previewScrollLimit(s, g), m.preview.offset-delta))
				}
			}
			return m, nil, true
		}
		m.cursor = max(0, min(len(m.visible())-1, m.cursor+delta))
		m.mouseListScreen = ""
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
				_, _, m.mouseListStart = m.sessionListPlan(m.sessionGeometry())
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
	return strings.Join(append(lines, "", "◐ Working (~) · ◆ Blocked (!) · ● Done (+) · ○ Idle (.) · ? Unknown", "Scroll the preview to inspect this captured screen. Scroll down to follow.", "NO_COLOR and ASCII use static states. AGENT_HARBOR_REDUCED_MOTION=1 keeps colors static.", "Click a row to move the cursor. Click the current row to open it.", "External sessions remain client-managed; unknown connections stay unknown.", "Ctrl+Z suspends on supported terminals; resume refreshes runtime state."), "\n")
}

func (m harborModel) sessionsHint() string {
	if m.searchFocus == "sessions" {
		return "Type search · ↵ list · Esc clear/back"
	}
	return "↑↓/jk select · " + tabHint + " · / search · ? help"
}
