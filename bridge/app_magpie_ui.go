package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

func (m harborModel) connectionActions() []uiAction {
	a := []uiAction{{ID: "conn:b", Label: "Bind external", Key: "b"}, {ID: "conn:r", Label: "Refresh", Key: "r"}}
	rows := m.instanceRows()
	if len(rows) > 0 {
		r := rows[min(m.instanceCursor, len(rows)-1)]
		reason := ""
		if r.Health != instanceVerified {
			reason = managementFailure(r)
		}
		a = append(a, uiAction{ID: "conn:enter", Label: "Manage selected providers", Key: "enter", Primary: true, Disabled: reason}, uiAction{ID: "conn:i", Label: "Details", Key: "i"}, uiAction{ID: "conn:u", Label: "Default for future sessions", Key: "u"})
	}
	return append(a, uiAction{ID: "conn:s", Label: "Start Harbor-managed", Key: "s"}, uiAction{ID: "conn:x", Label: "Stop Harbor-managed", Key: "x"}, uiAction{ID: "conn:w", Label: "Harbor-managed web", Key: "w"})
}
func (m harborModel) connectionPage() (uiPage, bool) {
	if !strings.HasPrefix(m.screen, "magpie") {
		return uiPage{}, false
	}
	if m.screen != "magpie" {
		return m.connectionDocument(), true
	}
	p := newUIPage(m.width, 10000)
	p.line(paint(fg("brand"), "🐦 Harbor · Connections"))
	p.tabs("magpie", m.uiHover, m.uiFocus, true)

	if m.notice != "" {
		for _, line := range strings.Split(ansi.Wrap(m.notice, max(1, m.width-2), ""), "\n") {
			p.line(line)
		}
	}
	p.search(m.instanceFilter, "Search gateways", m.searchFocus == "magpie", m.input)
	if m.searchFocus == "magpie" {
		p.Hint = "Type search · tab list · Esc clear/back"
	}
	a := m.connectionActions()
	p.shortcuts(a[:2], m.uiFocus)
	p.line(paint(fg("section"), "Magpie instances"))
	rows := m.instanceRows()
	for i, r := range rows {
		p.field("instance:"+r.ID, "🐦", r.Name, r.Kind+" · "+r.Health, i == m.instanceCursor, false)
	}
	if len(rows) == 0 {
		p.emptyState("No recorded instances.")
	}
	if len(rows) > 0 {
		r := rows[min(m.instanceCursor, len(rows)-1)]
		p.line(paint(fg("section"), "Selected instance · "+cleanUI(r.Name)))
		p.field("", "🔌", "Address", r.Address, false, false)
		p.field("", "🧩", "Ownership", r.Kind, false, false)
		p.field("", "●", "Status", r.Health, false, false)
		if r.Detail != "" {
			p.line(r.Detail)
		}
		p.shortcuts(a[2:len(a)-3], m.uiFocus)

	}
	p.line(paint(fg("section"), "Harbor-managed gateway · "+managedInstanceID))

	p.shortcuts(a[len(a)-3:], m.uiFocus)
	p.Height = m.height
	if p.Hint == "" {
		p.Hint = "/ search · Tab focus · ↑↓ select · PgUp/PgDown scroll · Esc back"
	}
	if m.searchFocus == "magpie" {
		p.Actions = nil
	}
	return p.finish(m.scroll), true
}

func (m harborModel) connectionDocument() uiPage {
	p := newUIPage(m.width, 10000)
	p.line(paint(fg("brand"), "🐦 Harbor · Connections"))
	if m.screen == "magpie-bind" {
		p.line("Bind an external running Magpie")
		p.search(m.input.Value(), "Gateway address", true, m.input)
		p.line(note("Harbor verifies identity and never starts or stops external gateways."))
	} else {
		for _, line := range strings.Split(m.magpieView(), "\n") {
			if strings.HasPrefix(line, keysMark) {
				continue
			}
			for _, wrapped := range strings.Split(ansi.Wrap(line, max(1, m.width-2), ""), "\n") {
				p.line(wrapped)
			}
		}
	}
	footer := newUIPage(m.width, 10000)
	actions := []uiAction{}
	if m.screen != "magpie-busy" {
		actions = append(actions, uiAction{ID: "conn:esc", Label: "Cancel / Back", Key: "esc"})
		if m.screen == "magpie-confirm" || m.screen == "magpie-bind" || m.screen == "magpie-context" {
			actions = append(actions, uiAction{ID: "conn:enter", Label: "Confirm", Key: "enter", Primary: true})
		}
	}
	footer.shortcuts(actions, m.uiFocus)
	footer.Hint = "↑↓ / PgUp/PgDown scroll"
	return p.withFooter(footer, m.height, m.scroll)
}
func (m harborModel) connectionActivate(id string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(id, "nav:") {
		return m.activateSessions(id)
	}
	if strings.HasPrefix(id, "instance:") {
		for i, r := range m.instanceRows() {
			if r.ID == strings.TrimPrefix(id, "instance:") {
				m.instanceCursor = i
				break
			}
		}
		return m, nil
	}
	if id == "search" && m.screen == "magpie-bind" {
		m.input.Focus()
		return m, nil
	}
	if id == "search" {
		m.searchFocus = "magpie"
		m.input.SetValue(m.instanceFilter)
		m.input.Focus()
		return m, nil
	}
	key := strings.TrimPrefix(id, "conn:")
	if key == "i" {
		m.screen = "magpie-detail"
		m.scroll = 0
		return m, nil
	}
	if m.screen == "magpie" {
		for _, a := range m.connectionActions() {
			if a.ID == id && a.Disabled != "" {
				m.notice = a.Disabled
				return m, nil
			}
		}
	}
	if key == "b" {
		m.uiFocus = ""
	}
	return m.updateMagpie(uiKey(key))
}
func (m harborModel) connectionPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if !strings.HasPrefix(m.screen, "magpie") {
		return m, nil, false
	}
	switch v := msg.(type) {
	case tea.MouseMsg:
		if m.screen == "magpie-confirm" && time.Since(m.reviewOpenedAt) < 200*time.Millisecond {
			return m, nil, true
		}
		if v.Button == tea.MouseButtonWheelDown {
			m.scroll++
			return m, nil, true
		}
		if v.Button == tea.MouseButtonWheelUp {
			m.scroll = max(0, m.scroll-1)
			return m, nil, true
		}
		p, _ := m.connectionPage()
		id := p.target(v.X, v.Y)
		if v.Action == tea.MouseActionMotion {
			m.uiHover = strings.TrimPrefix(id, "nav:")
			return m, nil, true
		}
		if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft && id != "" {
			m.uiFocus = id
			n, c := m.connectionActivate(id)
			return n, c, true
		}
		return m, nil, true
	case tea.KeyMsg:
		key := v.String()
		if m.screen == "magpie-busy" {
			return m, nil, true
		}
		if key == "ctrl+g" {
			m.notice = ""
			return m, nil, true
		}
		if m.screen != "magpie" && m.screen != "magpie-bind" && (key == "down" || key == "j" || key == "up" || key == "k") {
			if key == "down" || key == "j" {
				m.scroll++
			} else {
				m.scroll = max(0, m.scroll-1)
			}
			return m, nil, true
		}
		if m.screen == "magpie-confirm" && key == "enter" && time.Since(m.reviewOpenedAt) < 200*time.Millisecond {
			return m, nil, true
		}
		if m.screen == "magpie-busy" {
			return m, nil, true
		}
		if m.screen == "magpie" && m.searchFocus == "magpie" && key != "tab" && key != "shift+tab" {
			if key == "enter" {
				m.searchFocus = ""
				m.input.Blur()
				return m, nil, true
			}
			n, c, handled := m.updateSearch(msg)
			if handled {
				return n, c, true
			}
			return m, nil, true
		}
		// Text fields own ordinary characters, including global help/quit keys.
		if m.screen == "magpie-filter" {
			if key == "esc" || key == "enter" {
				m.screen = "magpie"
				return m, nil, true
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(v)
			return m, cmd, true
		}
		if (m.screen == "magpie-bind" || m.screen == "magpie-filter") && key != "tab" && key != "shift+tab" && key != "enter" && key != "esc" {
			m.uiFocus = "search"
			n, c := m.updateMagpie(v)
			return n, c, true
		}
		if key == "?" || key == "q" {
			n, c := m.update(msg)
			return n, c, true
		}
		if key == "pgdown" {
			m.scroll += max(1, m.height/2)
			return m, nil, true
		}
		if key == "pgup" {
			m.scroll = max(0, m.scroll-max(1, m.height/2))
			return m, nil, true
		}
		if key == "tab" || key == "shift+tab" {
			if m.screen != "magpie" {
				p, _ := m.connectionPage()
				ids := []string{}
				for _, r := range p.Hits {
					if r.ID != "" {
						ids = append(ids, r.ID)
					}
				}
				m.uiFocus = uiCycle(m.uiFocus, ids, key == "shift+tab")
				return m, nil, true
			}
			h, s := m.height, m.scroll
			m.height = 10000
			m.scroll = 0
			p, _ := m.connectionPage()
			m.height = h
			m.scroll = s
			ids := []uiRect{}
			for _, r := range p.Hits {
				if r.ID != "" {
					ids = append(ids, r)
				}
			}
			if len(ids) > 0 {
				names := []string{}
				for _, r := range ids {
					names = append(names, r.ID)
				}
				m.uiFocus = uiCycle(m.uiFocus, names, key == "shift+tab")
				i := 0
				for j, r := range ids {
					if r.ID == m.uiFocus {
						i = j
						break
					}
				}
				m.searchFocus = ""
				m.input.Blur()
				if strings.HasPrefix(m.uiFocus, "conn:") {
					return m, nil, true
				}
				if ids[i].Y < m.scroll {
					m.scroll = ids[i].Y
				}
				if ids[i].Y >= m.scroll+max(1, m.height-m.connectionFooterHeight()) {
					m.scroll = ids[i].Y - max(1, m.height-m.connectionFooterHeight()) + 1
				}
			}
			return m, nil, true
		}
		if key == "enter" && m.screen == "magpie-bind" && m.uiFocus == "search" {
			m.uiFocus = ""
		}
		if key == "enter" && m.uiFocus != "" {
			n, c := m.connectionActivate(m.uiFocus)
			return n, c, true
		}
		if key == "/" && m.screen == "magpie" {
			n, c := m.connectionActivate("search")
			return n, c, true
		}
		if m.screen == "magpie" && (key == "up" || key == "down" || key == "j" || key == "k") {
			m.uiFocus = ""
			n, c := m.updateMagpie(v)
			n = n.(harborModel).connectionRevealSelected()
			return n, c, true
		}
		if m.screen == "magpie" {
			for _, a := range m.connectionActions() {
				if a.Key == key {
					n, c := m.connectionActivate(a.ID)
					return n, c, true
				}
			}
		}
		n, c := m.updateMagpie(v)
		return n, c, true
	}
	return m, nil, false
}

// Keep keyboard selection visible without making scrolling change the target.
func (m harborModel) connectionRevealSelected() harborModel {
	rows := m.instanceRows()
	if len(rows) == 0 {
		return m
	}
	h, s := m.height, m.scroll
	m.height = 10000
	m.scroll = 0
	p, _ := m.connectionPage()
	m.height = h
	m.scroll = s
	id := "instance:" + rows[min(m.instanceCursor, len(rows)-1)].ID
	for _, r := range p.Hits {
		if r.ID == id {
			if r.Y < m.scroll {
				m.scroll = r.Y
			}
			if r.Y >= m.scroll+max(1, m.height-m.connectionFooterHeight()) {
				m.scroll = r.Y - max(1, m.height-m.connectionFooterHeight()) + 1
			}
		}
	}
	return m
}

func (m harborModel) connectionFooterHeight() int {
	p := newUIPage(m.width, m.height)
	p.shortcuts(m.connectionActions(), m.uiFocus)
	p.Hint = "/ search · Tab focus · ↑↓ select · PgUp/PgDown scroll · Esc back"
	return len(p.shortcutBar().Lines)
}
