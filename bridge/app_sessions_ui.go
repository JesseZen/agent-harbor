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

func (m harborModel) archivedActionLabel() string {
	if n := m.archivedCount(); n > 0 {
		return fmt.Sprintf("Archived (%d)", n)
	}
	return "Archived"
}

func (m harborModel) collapseActionLabel() string {
	if m.width < 55 {
		return "Collapse"
	}
	return "Collapse group"
}

func (m harborModel) sessionActions(more bool) []uiAction {
	if m.archivedView {
		if !more {
			actions := []uiAction{{ID: "action:r", Label: "Refresh", Key: "r"}}
			if _, ok := m.selected(); ok {
				return append([]uiAction{
					{ID: "action:i", Label: "Details", Key: "enter"},
					{ID: "action:u", Label: "Unarchive", Key: "u", Primary: true},
					{ID: "action:x", Label: "Remove record", Key: "x"},
					{ID: "more", Label: "More…", Key: "."},
					{ID: "action:esc", Label: "Back", Key: "esc"},
				}, actions...)
			}
			return append(actions, uiAction{ID: "action:esc", Label: "Back", Key: "esc"})
		}
		actions := []uiAction{}
		if _, ok := m.selected(); ok {
			actions = append(actions,
				uiAction{ID: "action:i", Label: "Details", Key: "i"},
				uiAction{ID: "action:u", Label: "Unarchive", Key: "u", Primary: true},
				uiAction{ID: "action:x", Label: "Remove record", Key: "x"},
			)
		}
		return append(actions, uiAction{ID: "back", Label: "Back", Key: "esc"})
	}

	actions := []uiAction{{ID: "action:n", Label: "New session", Key: "n", Primary: true}, {ID: "action:r", Label: "Refresh", Key: "r"}}
	if row, ok := m.selectedRow(); ok && row.Kind == sessionRowToggle {
		if !more {
			toggle := []uiAction{{ID: "action:enter", Label: row.ToggleLabel(), Key: "enter", Primary: true}}
			if row.Expanded {
				toggle = append(toggle, uiAction{ID: "action:z", Label: m.collapseActionLabel(), Key: "z"})
			}
			return append(toggle, actions...)
		}
		moreActions := []uiAction{{ID: "action:enter", Label: row.ToggleLabel(), Key: "enter", Primary: true}}
		if n := m.archivedCount(); n > 0 {
			moreActions = append(moreActions, uiAction{ID: "action:g", Label: m.archivedActionLabel(), Key: "g"})
		}
		return append(moreActions, uiAction{ID: "back", Label: "Back", Key: "esc"})
	}
	s, ok := m.selected()
	if !ok {
		if !more {
			if n := m.archivedCount(); n > 0 {
				actions = append(actions, uiAction{ID: "action:g", Label: m.archivedActionLabel(), Key: "g"})
			}
			return actions
		}
		moreActions := []uiAction{{ID: "action:g", Label: m.archivedActionLabel(), Key: "g"}}
		return append(moreActions, uiAction{ID: "back", Label: "Back", Key: "esc"})
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
		pinLabel := "Pin"
		if s.Pinned {
			pinLabel = "Unpin"
		}
		actions = append(actions,
			uiAction{ID: "action:e", Label: "Rename", Key: "e"},
			uiAction{ID: "action:x", Label: "Remove record", Key: "x"},
			uiAction{ID: "action:P", Label: pinLabel, Key: "P"},
			uiAction{ID: "action:A", Label: "Archive", Key: "A", Disabled: archiveBlockReason(s)},
		)
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
	actions = append(actions, uiAction{ID: "action:g", Label: m.archivedActionLabel(), Key: "g"})
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
	if m.archivedView {
		p.title("Archived")
		p.shortcuts([]uiAction{{ID: "action:r", Label: "Refresh", Key: "r"}}, m.uiFocus)
		p.search(m.searchQuery(), "Search archived sessions", m.searchFocus == "sessions", m.input)
		return p
	}
	p.line(paint(fg("brand"), m.sessionsTitle()))
	p.tabs("sessions", m.uiHover, m.uiFocus)
	p.shortcuts([]uiAction{{ID: "action:n", Label: "New session", Key: "n", Primary: true}, {ID: "action:r", Label: "Refresh", Key: "r"}}, m.uiFocus)
	p.search(m.searchQuery(), "Search sessions, projects or connections", m.searchFocus == "sessions", m.input)
	return p
}
func (m harborModel) sessionsFooter(p *uiPage) {
	if m.searchFocus == "sessions" {
		p.Actions = nil
		p.Hint = m.sessionsHint()
		return
	}
	if m.archivedView {
		if _, ok := m.selected(); ok {
			p.shortcuts([]uiAction{
				{ID: "action:i", Label: "Details", Key: "enter"},
				{ID: "action:u", Label: "Unarchive", Key: "u", Primary: true},
				{ID: "action:x", Label: "Remove record", Key: "x"},
				{ID: "more", Label: "More…", Key: "."},
				{ID: "action:esc", Label: "Back", Key: "esc"},
			}, m.uiFocus)
		} else {
			p.shortcut(uiAction{ID: "action:esc", Label: "Back", Key: "esc"}, m.uiFocus == "action:esc")
		}
		p.Hint = m.sessionsHint()
		return
	}
	if row, ok := m.selectedRow(); ok {
		if row.Kind == sessionRowToggle {
			p.shortcut(uiAction{ID: "action:enter", Label: row.ToggleLabel(), Key: "enter", Primary: true}, m.uiFocus == "action:enter")
			if row.Expanded {
				p.shortcut(uiAction{ID: "action:z", Label: m.collapseActionLabel(), Key: "z"}, m.uiFocus == "action:z")
			}
		} else {
			p.shortcuts(m.sessionActions(false)[:3], m.uiFocus)
			if _, expanded := m.currentGroupExpanded(); expanded {
				p.shortcut(uiAction{ID: "action:z", Label: m.collapseActionLabel(), Key: "z"}, m.uiFocus == "action:z")
			}
			if m.width-2 >= sessionsSplitWidth {
				p.shortcut(uiAction{ID: "action:o", Label: "Context", Key: "o"}, m.uiFocus == "action:o")
				if !row.View.External {
					pinLabel := "Pin"
					if row.View.Pinned {
						pinLabel = "Unpin"
					}
					p.shortcut(uiAction{ID: "action:P", Label: pinLabel, Key: "P"}, m.uiFocus == "action:P")
					if archiveBlockReason(row.View) == "" {
						p.shortcut(uiAction{ID: "action:A", Label: "Archive", Key: "A"}, m.uiFocus == "action:A")
					}
				}
			}
		}
	}
	if m.archivedCount() > 0 {
		p.shortcut(uiAction{ID: "action:g", Label: m.archivedActionLabel(), Key: "g"}, m.uiFocus == "action:g")
	}
	p.Hint = m.sessionsHint()
}

type sessionGeometry struct {
	body, top, left, right, row int
	split                       bool
}

const sessionsSplitWidth = 90

func sessionColumns(width, body int) sessionGeometry {
	g := sessionGeometry{body: body, left: width, row: 2}
	if width >= sessionsSplitWidth && body >= 12 {
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
	isToggle     bool
	title, meta  string
	groupID      string
	expanded     bool
	hidden       int
	index        int
	view         sessionView
	top          int
	firstInGroup bool
}

func (e sessionListEntry) height(rowH int) int {
	if e.isHeader || e.isToggle {
		return 1
	}
	return rowH
}

func (m harborModel) sessionListPlan(geometry sessionGeometry) ([]sessionListEntry, int, int) {
	bodyH, rowH := geometry.body, geometry.row
	groups := m.sessionDisplayGroups()
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
			groupID:  g.ID,
			top:      top,
		})
		top++
		for i, r := range g.Rows {
			if r.Kind == sessionRowToggle {
				entries = append(entries, sessionListEntry{
					isToggle:     true,
					title:        r.GroupTitle,
					groupID:      r.GroupID,
					expanded:     r.Expanded,
					hidden:       r.Hidden,
					index:        flatIdx,
					top:          top,
					firstInGroup: i == 0,
				})
				top++
				flatIdx++
				continue
			}
			entries = append(entries, sessionListEntry{
				index:        flatIdx,
				groupID:      r.GroupID,
				view:         r.View,
				top:          top,
				firstInGroup: i == 0,
			})
			top += rowH
			flatIdx++
		}
	}
	totalLines := top
	cursor := max(0, min(m.cursor, flatIdx-1))
	itemTop, itemBottom, preferredTop := 0, rowH, 0
	for _, e := range entries {
		if !e.isHeader && e.index == cursor {
			h := e.height(rowH)
			itemTop = e.top
			itemBottom = e.top + h
			preferredTop = itemTop
			if e.firstInGroup && bodyH >= h+1 {
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
	preservedClick := m.mouseListScreen == m.screen && m.mouseListStart >= 0 && m.mouseListStart <= itemTop && itemBottom <= m.mouseListStart+bodyH
	if preservedClick {
		scrollTop = m.mouseListStart
	}
	scrollTop = max(0, min(scrollTop, max(0, totalLines-bodyH)))
	if !preservedClick && rowH == 2 && bodyH >= rowH {
		for _, entry := range entries {
			if !entry.isHeader && !entry.isToggle && entry.top+1 == scrollTop && itemBottom <= entry.top+bodyH {
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
	pinPrefix := ""
	if !s.External && s.Pinned {
		pinPrefix = "📌 "
	}
	pinW := ansi.StringWidth(pinPrefix)
	space := max(1, width-4-ansi.StringWidth(word))
	if pinW >= space {
		pinPrefix = ""
		pinW = 0
	}
	nameSpace := max(1, space-pinW)
	suffix = ansi.Truncate(suffix, max(0, nameSpace-2), "…")
	name := ansi.Truncate(cleanUI(s.Name), max(1, nameSpace-ansi.StringWidth(suffix)), "…") + suffix
	lead := paint(row.style("text"), row.marker()) + icon + paint(row.style("text"), " ")
	if pinPrefix != "" {
		lead += paint(row.style("text"), pinPrefix)
	}
	return lead + highlightSearch(row.line(name, "text", nameSpace), m.searchQuery()) + word
}

func (m harborModel) renderSessionToggleLine(entry sessionListEntry, row uiRowStyle, width int) string {
	glyph := "▸"
	label := fmt.Sprintf("Show %d more", entry.hidden)
	if entry.expanded {
		glyph = "▾"
		label = "Show less"
	}
	role := "value"
	if row.cursor {
		role = "text"
	}
	lead := paint(row.style(role), row.marker()+glyph+" ")
	return lead + row.line(label, role, max(1, width-4))
}

func (m harborModel) renderTogglePreview(row sessionRow, width int) []string {
	title := cleanUI(row.GroupTitle) + " · "
	detail := "Press Enter to expand this group."
	if row.Expanded {
		title += "expanded"
		detail = "Press Enter or z to collapse this group."
	} else {
		title += fmt.Sprintf("%d hidden", row.Hidden)
	}
	return []string{
		paint(fg("text").Bold(true), ansi.Truncate(title, width, "…")),
		paint(fg("muted"), ansi.Truncate(detail, width, "…")),
	}
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
	if m.archivedView {
		p.title("Archived")
	} else {
		p.line(paint(fg("brand"), m.sessionsTitle()))
		p.tabs("sessions", m.uiHover, m.uiFocus)
	}
	if m.screen == "sessions-more" {
		if s, ok := m.selected(); ok {
			p.line("Actions for " + cleanUI(s.Name))
		} else if row, ok := m.selectedRow(); ok && row.Kind == sessionRowToggle {
			p.line("Actions for " + cleanUI(row.GroupTitle))
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
		switch {
		case m.archivedView && m.archivedFilter != "":
			left.emptyState("No matching archived sessions")
		case m.archivedView:
			left.emptyState("No archived sessions")
		case m.filter == "" && m.archivedCount() > 0:
			left.emptyState("No active or stopped sessions")
		default:
			left.emptyState("No matching sessions")
		}
	}
	railReserve := 0
	if totalLines > bodyH {
		railReserve = 1
	}
	bandPage := newUIPage(max(3, leftW+2-railReserve), 10)
	for _, entry := range entries {
		h := entry.height(g.row)
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
		if entry.isToggle {
			if entry.top >= scrollTop && len(left.Lines) < bodyH {
				id := "toggle:" + entry.groupID
				row := m.rowStyle(id, m.sessionCursorAt(entry.index), false)
				y := len(left.Lines)
				lineW := max(1, leftW-railReserve)
				left.line(m.renderSessionToggleLine(entry, row, lineW))
				left.hit(id, 1, y, lineW, 1)
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
		if row, ok := m.selectedRow(); ok {
			if row.Kind == sessionRowToggle {
				right = m.renderTogglePreview(row, g.right)
			} else {
				right = m.previewPane(row.View, g)
			}
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
		m.archivedView = false
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
		m.input.SetValue(m.searchQuery())
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
					if a.Disabled != "" {
						m.screen = "sessions"
						m.notice = a.Disabled
						return m, nil, true
					}
					next, cmd := m.activateSessions(a.ID)
					return next, cmd, true
				}
			}
			return m, nil, true
		}
		if m.searchFocus != "sessions" {
			switch k {
			case "z":
				if !m.archivedView && m.collapseCurrentGroup() {
					return m, nil, true
				}
				return m, nil, true
			case "g":
				if !m.archivedView {
					m.sessionsReturnID = m.searchSelectedID()
					m.archivedView = true
					m.archivedFilter = ""
					m.cursor = 0
					m.keepSelectedID = ""
					m.selectionMissing = false
					m.mouseListScreen = ""
					m.notice = ""
					return m, nil, true
				}
				return m, nil, true
			case "P":
				if m.archivedView {
					return m, nil, true
				}
				s, ok := m.selected()
				if !ok {
					return m, nil, true
				}
				if s.External {
					m.notice = "External sessions cannot be pinned."
					return m, nil, true
				}
				nextPinned := !s.Pinned
				var updated *sessionView
				return m.serviceTask(func() error {
					if err := m.service.SetPinned(s, nextPinned); err != nil {
						return err
					}
					if v, err := m.service.GetContext(m.refreshContext, s.ID); err == nil {
						updated = &v
					}
					return nil
				}, func(n *harborModel, err error) tea.Cmd {
					if err != nil {
						n.notice = err.Error()
						return nil
					}
					for i := range n.sessions {
						if n.sessions[i].ID == s.ID {
							if updated != nil {
								n.sessions[i] = *updated
							} else {
								n.sessions[i].Pinned = nextPinned
							}
							break
						}
					}
					n.selectionMissing = false
					n.focusSessionRowID(s.ID)
					if nextPinned {
						n.notice = "Pinned " + cleanUI(s.Name)
					} else {
						n.notice = "Unpinned " + cleanUI(s.Name)
					}
					next, cmd := n.forceRefresh()
					*n = next.(harborModel)
					return cmd
				})
			case "A":
				if m.archivedView {
					return m, nil, true
				}
				s, ok := m.selected()
				if !ok {
					return m, nil, true
				}
				if reason := archiveBlockReason(s); reason != "" {
					m.notice = reason
					return m, nil, true
				}
				nextID := m.neighborRowIDAfterRemoval(s.ID)
				var updated *sessionView
				return m.serviceTask(func() error {
					if err := m.service.Archive(s); err != nil {
						return err
					}
					if v, err := m.service.GetContext(m.refreshContext, s.ID); err == nil {
						updated = &v
					}
					return nil
				}, func(n *harborModel, err error) tea.Cmd {
					if err != nil {
						n.notice = err.Error()
						next, cmd := n.forceRefresh()
						*n = next.(harborModel)
						return cmd
					}
					for i := range n.sessions {
						if n.sessions[i].ID == s.ID {
							if updated != nil {
								n.sessions[i] = *updated
							} else {
								n.sessions[i].Archived = true
							}
							break
						}
					}
					n.selectionMissing = false
					n.focusSessionRowID(nextID)
					n.notice = "Archived " + cleanUI(s.Name)
					next, cmd := n.forceRefresh()
					*n = next.(harborModel)
					return cmd
				})
			case "u":
				if !m.archivedView {
					return m, nil, true
				}
				s, ok := m.selected()
				if !ok {
					return m, nil, true
				}
				nextID := m.neighborRowIDAfterRemoval(s.ID)
				var updated *sessionView
				return m.serviceTask(func() error {
					if err := m.service.Unarchive(s); err != nil {
						return err
					}
					if v, err := m.service.GetContext(m.refreshContext, s.ID); err == nil {
						updated = &v
					}
					return nil
				}, func(n *harborModel, err error) tea.Cmd {
					if err != nil {
						n.notice = err.Error()
						next, cmd := n.forceRefresh()
						*n = next.(harborModel)
						return cmd
					}
					for i := range n.sessions {
						if n.sessions[i].ID == s.ID {
							if updated != nil {
								n.sessions[i] = *updated
							} else {
								n.sessions[i].Archived = false
								n.sessions[i].ArchivedAt = nil
							}
							break
						}
					}
					n.selectionMissing = false
					n.focusSessionRowID(nextID)
					n.notice = "Unarchived " + cleanUI(s.Name)
					next, cmd := n.forceRefresh()
					*n = next.(harborModel)
					return cmd
				})
			case "o":
				if view, ok := m.selected(); ok {
					geometry := m.sessionGeometry()
					if m.previewExpanded {
						m.previewExpanded = false
						return m, nil, true
					}
					if geometry.split && m.previewContextFits(view, geometry) {
						m.previewExpanded = !m.previewExpanded
						return m, nil, true
					}
					next, cmd := m.activateSessions("action:i")
					return next, cmd, true
				}
				return m, nil, true
			case ".":
				next, cmd := m.activateSessions("more")
				return next, cmd, true
			}
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
		m.moveSessionCursor(delta)
		m.mouseListScreen = ""
		return m, nil, true
	}
	if mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
		return m, nil, true
	}
	if strings.HasPrefix(target, "session:") || strings.HasPrefix(target, "toggle:") {
		for i, r := range m.visibleRows() {
			if r.HitID() == target {
				if m.sessionCursorAt(i) {
					next, cmd := m.activateSessions("action:enter")
					return next, cmd, true
				}
				_, _, m.mouseListStart = m.sessionListPlan(m.sessionGeometry())
				m.mouseListScreen = "sessions"
				m.focusSessionRowID(r.RowID())
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
	if !m.archivedView {
		actions = append(actions,
			uiAction{ID: "pin", Key: "P", Label: "Pin / Unpin"},
			uiAction{ID: "archive", Key: "A", Label: "Archive"},
			uiAction{ID: "archived", Key: "g", Label: "Archived view"},
			uiAction{ID: "collapse", Key: "z", Label: "Collapse group"},
			uiAction{ID: "settings", Key: ",", Label: "Settings"},
			uiAction{ID: "connections", Key: "m", Label: "Connections"},
			uiAction{ID: "accounts", Key: "a", Label: "Accounts"},
		)
	}
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
	return strings.Join(append(lines, "", "o toggles full project and connection context; opens Details if space is tight.", "Colored dots: orange Working, red Blocked, green Done, gray Idle. ? Unknown.", "Without color: ◐ Working (~) · ◆ Blocked (!) · ● Done (+) · ○ Idle (.)", "Scroll the preview to inspect this captured screen. Scroll down to follow.", "NO_COLOR and ASCII use static states. AGENT_HARBOR_REDUCED_MOTION=1 keeps colors static.", "Click a row to move the cursor. Click the current row to open it.", "External sessions remain client-managed; unknown connections stay unknown.", "Ctrl+Z suspends on supported terminals; resume refreshes runtime state."), "\n")
}

func (m harborModel) sessionsHint() string {
	if m.searchFocus == "sessions" {
		return "Type search · ↵ list · Esc clear/back"
	}
	if m.archivedView {
		return "↑↓/jk select · / search · ? help"
	}
	return "↑↓/jk select · " + tabHint + " · / search · ? help"
}
