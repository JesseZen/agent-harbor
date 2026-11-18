package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// The saved form is the applied value; list cursors only express consideration.
func selectorMark(selected bool) string {
	if selected {
		return "✓"
	}
	return ""
}
func (m harborModel) connectionSelectorPage() (uiPage, bool) {
	p := newUIPage(m.width, 10000)
	wrap := func(text string) {
		for _, line := range strings.Split(ansi.Wrap(text, max(1, m.width-2), ""), "\n") {
			p.line(documentLine(line))
		}
	}
	footer := newUIPage(m.width, 10000)
	switch m.screen {
	case "new-instance-loading", "new-catalog-loading":
		p.title("Loading connection…")
	case "new-instance":
		p.title("Magpie · Choose instance")
		p.search(m.instanceFilter, "Filter instances", m.uiFocus == "selector-search", m.input)
		rows := m.instanceRows()
		listTop := len(p.Lines)
		count := max(1, m.height-12)
		start := m.listStart(m.instanceCursor, count)
		p.ListStart = start
		for i := start; i < min(len(rows), start+count); i++ {
			r := rows[i]
			id := "selector-instance:" + r.ID
			selected := m.formSaved.Connection == "magpie" && m.formSaved.Instance == r.ID
			p.listRow(id, selectorMark(selected), r.Name, r.ID+" · "+r.Health, m.rowStyle(id, i == m.instanceCursor && m.uiFocus != "selector-search", selected))
			if !selected {
				p.Lines[len(p.Lines)-1] = highlightSearch(p.Lines[len(p.Lines)-1], m.instanceFilter)
			}
		}
		p.scrollRail(listTop, len(p.Lines)-listTop, len(rows), start)
		if len(rows) > 0 {
			r := rows[min(max(0, m.instanceCursor), len(rows)-1)]
			p.line(cleanUI(r.Address))
			wrap("Configuration: " + cleanUI(orUnknown(r.ConfigDir)))
			if r.Detail != "" {
				wrap(cleanUI(r.Detail))
			}
			for _, warning := range r.Warnings {
				wrap(cleanUI(warning))
			}
		}
		footer.shortcut(uiAction{ID: "selector-select", Label: "Select instance", Key: "enter", Primary: true}, m.uiFocus == "selector-select")
	case "new-catalog":
		p.title("Magpie · Model or routing group")
		p.line(cleanUI(m.catalogInstanceLabel()))
		p.search(m.catalogFilter, "Filter models and groups", m.uiFocus == "selector-search", m.input)
		rows := m.filteredModels()
		listTop := len(p.Lines)
		count := max(1, m.height-15)
		start := m.listStart(m.catalogCursor, count)
		p.ListStart = start
		for i := start; i < min(len(rows), start+count); i++ {
			r := rows[i]
			label := r.MagpieLabel
			if label == "" {
				label = r.DisplayName
			}
			if r.OwnedBy != "" {
				label += " · " + r.OwnedBy
			}
			id := "selector-model:" + r.ID
			selected := m.formSaved.Connection == "magpie" && m.formSaved.Instance == m.draft.Instance && m.formSaved.Model == r.ID
			p.listRow(id, selectorMark(selected), r.ID, label, m.rowStyle(id, i == m.catalogCursor && m.uiFocus != "selector-search", selected))
			if !selected {
				p.Lines[len(p.Lines)-1] = highlightSearch(p.Lines[len(p.Lines)-1], m.catalogFilter)
			}
		}
		p.scrollRail(listTop, len(p.Lines)-listTop, len(rows), start)
		wrap("Auxiliary requests may send conversation parts to other providers.")
		wrap("Groups may switch members; an ID does not prove the destination.")
		action := uiAction{ID: "selector-apply", Label: "Acknowledge & apply", Key: "enter", Primary: true}
		if len(rows) == 0 {
			action.Disabled = "No matching models"
		}
		footer.shortcut(action, m.uiFocus == "selector-apply")
		footer.shortcut(uiAction{ID: "selector-instances", Label: "Change instance", Key: "i"}, m.uiFocus == "selector-instances")
	case "new-notice":
		p.line("Magpie · " + m.draft.Instance)
		wrap("Auxiliary requests may send conversation parts to other providers.")
		wrap("Groups may switch members; an ID does not prove the destination.")
		footer.shortcut(uiAction{ID: "selector-ack", Label: "Acknowledge & apply", Key: "enter", Primary: true}, m.uiFocus == "selector-ack")
	default:
		return p, false
	}
	footer.shortcut(uiAction{ID: "selector-cancel", Label: "Cancel", Key: "esc"}, m.uiFocus == "selector-cancel")
	if m.notice != "" {
		wrap(cleanUI(m.notice))
	}
	hint := "↑↓/jk select · / search · PgUp/PgDn scroll"
	if m.screen == "new-notice" || strings.HasSuffix(m.screen, "-loading") {
		hint = ""
	}
	if m.uiFocus == "selector-search" {
		hint = "Type search · ↵ list · Esc clear/back"
	}
	footer.Hint = hint
	return p.withFooter(footer, m.height, m.scroll), true
}
func (m harborModel) connectionSelectorInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	p, ok := m.connectionSelectorPage()
	if !ok {
		return m, nil, false
	}
	act := func(id string) (tea.Model, tea.Cmd, bool) {
		switch {
		case id == "selector-cancel":
			n, c := m.updateForm(uiKey("esc"))
			return n, c, true
		case id == "search":
			m.uiFocus = "selector-search"
			m.input.SetValue(m.catalogFilter)
			if m.screen == "new-instance" {
				m.input.SetValue(m.instanceFilter)
			}
			m.input.CursorEnd()
			m.input.Focus()
		case id == "selector-instances":
			m.generation++
			g := m.generation
			m.scroll = 0
			m.screen = "new-instance-loading"
			m.uiFocus = ""
			return m, func() tea.Msg { r, e := m.service.InstanceReports(); return appNewInstances{g, r, e} }, true
		case id == "selector-apply":
			rows := m.filteredModels()
			if m.catalogCursor < 0 || m.catalogCursor >= len(rows) {
				return m, nil, true
			}
			// Explicit consent and selection are one action; Create alone launches.
			model, instance := rows[m.catalogCursor].ID, m.draft.Instance
			return m.serviceTask(func() error {
				ack, err := m.service.AuxiliaryAcknowledged(instance)
				if err == nil && !ack {
					err = m.service.AcknowledgeAuxiliary(instance)
				}
				return err
			}, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
					return nil
				}
				n.draft.Model = model
				n.rememberConnection()
				n.screen = "new-form"
				n.uiFocus = ""
				n.notice = ""
				return nil
			})
		case id == "selector-select" || id == "selector-ack":
			n, c := m.updateForm(uiKey("enter"))
			return n, c, true
		}
		return m, nil, true
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Button == tea.MouseButtonWheelDown {
			m.scroll++
			return m, nil, true
		}
		if mouse.Button == tea.MouseButtonWheelUp {
			m.scroll = max(0, m.scroll-1)
			return m, nil, true
		}
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			id := p.target(mouse.X, mouse.Y)
			switch {
			case strings.HasPrefix(id, "selector-instance:"):
				for i, r := range m.instanceRows() {
					if id == "selector-instance:"+r.ID {
						if m.instanceCursor == i && m.uiFocus != "selector-search" {
							return act("selector-select")
						}
						m.pinList(p.ListStart)
						m.instanceCursor = i
						break
					}
				}
			case strings.HasPrefix(id, "selector-model:"):
				for i, r := range m.filteredModels() {
					if id == "selector-model:"+r.ID {
						if m.catalogCursor == i && m.uiFocus != "selector-search" {
							return act("selector-apply")
						}
						m.pinList(p.ListStart)
						m.catalogCursor = i
						break
					}
				}
			default:
				return act(id)
			}
			m.uiFocus = ""
			m.input.Blur()
		}
		return m, nil, true
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	k := key.String()
	if m.uiFocus == "selector-search" {
		if k == "esc" && m.input.Value() != "" {
			m.input.SetValue("")
			if m.screen == "new-instance" {
				m.instanceFilter = ""
				m.instanceCursor = 0
			} else {
				m.catalogFilter = ""
				m.catalogCursor = 0
			}
			return m, nil, true
		}
		if k == "esc" || k == "enter" {
			m.uiFocus = ""
			m.input.Blur()
			return m, nil, true
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(key)
		if m.screen == "new-instance" {
			m.instanceFilter = m.input.Value()
			m.instanceCursor = 0
		} else {
			m.catalogFilter = m.input.Value()
			m.catalogCursor = 0
		}
		return m, cmd, true
	}
	switch k {
	case "?", "ctrl+c", "ctrl+g":
		n, c := m.update(key)
		return n, c, true
	case "pgdown":
		m.scroll += max(1, m.height/2)
		return m, nil, true
	case "pgup":
		m.scroll = max(0, m.scroll-max(1, m.height/2))
		return m, nil, true
	case "esc":
		return act("selector-cancel")
	case "/":
		return act("search")
	case "i":
		if m.screen == "new-catalog" {
			return act("selector-instances")
		}
	case "enter":
		if m.screen == "new-catalog" {
			return act("selector-apply")
		}
		if m.screen == "new-notice" {
			return act("selector-ack")
		}
		return act("selector-select")
	case "up", "k", "down", "j":
		m.uiFocus = ""
		n, c := m.updateForm(key)
		return n, c, true
	}
	return m, nil, true
}
