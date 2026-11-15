package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// The saved form is the applied value; list cursors only express consideration.
func selectorMark(selected bool) string {
	if selected {
		return "✓"
	}
	return "·"
}
func (m harborModel) connectionSelectorPage() (uiPage, bool) {
	p := newUIPage(m.width, m.height)
	switch m.screen {
	case "new-instance-loading", "new-catalog-loading":
		p.line("Loading connection…")
	case "new-instance":
		p.line("🐦 Magpie · Choose instance")
		p.search(m.instanceFilter, "Filter instances", m.uiFocus == "selector-search")
		rows := m.instanceRows()
		count := max(1, p.Height-12)
		start := max(0, m.instanceCursor-count+1)
		for i := start; i < min(len(rows), start+count); i++ {
			r := rows[i]
			p.field("selector-instance:"+r.ID, selectorMark(m.formSaved.Connection == "magpie" && m.formSaved.Instance == r.ID), r.Name, r.ID+" · "+r.Health, i == m.instanceCursor, false)
		}
		if len(rows) > 0 {
			r := rows[min(max(0, m.instanceCursor), len(rows)-1)]
			p.line(cleanUI(r.Address))
			p.line("Configuration: " + cleanUI(orUnknown(r.ConfigDir)))
		}
		p.button(uiAction{ID: "selector-select", Label: "Select instance", Key: "enter", Primary: true}, m.uiFocus == "selector-select")
	case "new-catalog":
		p.line("🐦 Magpie · Model or routing group")
		p.line(cleanUI(m.catalogInstanceLabel()))
		p.search(m.catalogFilter, "Filter models and groups", m.uiFocus == "selector-search")
		rows := m.filteredModels()
		count := max(1, p.Height-15)
		start := max(0, m.catalogCursor-count+1)
		for i := start; i < min(len(rows), start+count); i++ {
			r := rows[i]
			label := r.MagpieLabel
			if label == "" {
				label = r.DisplayName
			}
			if r.OwnedBy != "" {
				label += " · " + r.OwnedBy
			}
			p.field("selector-model:"+r.ID, selectorMark(m.formSaved.Connection == "magpie" && m.formSaved.Instance == m.draft.Instance && m.formSaved.Model == r.ID), r.ID, label, i == m.catalogCursor, false)
		}
		p.line("Auxiliary requests may send conversation parts to other providers.")
		p.line("Groups may switch members; an ID does not prove the destination.")
		label := "Apply selection"
		ack, err := m.service.AuxiliaryAcknowledged(m.draft.Instance)
		if !ack {
			label = "Acknowledge & apply"
		}
		action := uiAction{ID: "selector-apply", Label: label, Key: "enter", Primary: true}
		if err != nil {
			action.Disabled = err.Error()
		}
		if len(rows) == 0 {
			action.Disabled = "No matching models"
		}
		p.button(action, m.uiFocus == "selector-apply")
		p.button(uiAction{ID: "selector-instances", Label: "Change instance", Key: "i"}, m.uiFocus == "selector-instances")
	case "new-notice":
		p.line("Magpie · " + m.draft.Instance)
		p.line("Auxiliary requests may send conversation parts to other providers.")
		p.line("Groups may switch members; an ID does not prove the destination.")
		p.button(uiAction{ID: "selector-ack", Label: "Acknowledge & apply", Key: "enter", Primary: true}, m.uiFocus == "selector-ack")
	default:
		return p, false
	}
	p.button(uiAction{ID: "selector-cancel", Label: "Cancel", Key: "esc"}, m.uiFocus == "selector-cancel")
	if m.notice != "" {
		p.line(cleanUI(m.notice))
	}
	return p, true
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
			m.input.Focus()
		case strings.HasPrefix(id, "selector-instance:"):
			for i, r := range m.instanceRows() {
				if id == "selector-instance:"+r.ID {
					m.instanceCursor = i
				}
			}
			m.uiFocus = ""
		case strings.HasPrefix(id, "selector-model:"):
			for i, r := range m.filteredModels() {
				if id == "selector-model:"+r.ID {
					m.catalogCursor = i
				}
			}
			m.uiFocus = ""
		case id == "selector-instances":
			m.generation++
			g := m.generation
			m.screen = "new-instance-loading"
			m.uiFocus = ""
			return m, func() tea.Msg { r, e := m.service.InstanceReports(); return appNewInstances{g, r, e} }, true
		case id == "selector-apply":
			rows := m.filteredModels()
			if m.catalogCursor < 0 || m.catalogCursor >= len(rows) {
				return m, nil, true
			}
			// Explicit consent and selection are one action; Create alone launches.
			ack, e := m.service.AuxiliaryAcknowledged(m.draft.Instance)
			if e == nil && !ack {
				e = m.service.AcknowledgeAuxiliary(m.draft.Instance)
			}
			if e != nil {
				m.notice = e.Error()
				return m, nil, true
			}
			m.draft.Model = rows[m.catalogCursor].ID
			m.screen = "new-form"
			m.uiFocus = ""
			m.notice = ""
		case id == "selector-select" || id == "selector-ack":
			n, c := m.updateForm(uiKey("enter"))
			return n, c, true
		}
		return m, nil, true
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			return act(p.target(mouse.X, mouse.Y))
		}
		return m, nil, true
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	k := key.String()
	if m.uiFocus == "selector-search" {
		if k == "esc" || k == "enter" || k == "tab" {
			m.uiFocus = ""
			return m, nil, true
		}
		var c tea.Cmd
		m.input, c = m.input.Update(key)
		if m.screen == "new-instance" {
			m.instanceFilter = m.input.Value()
			m.instanceCursor = 0
		} else {
			m.catalogFilter = m.input.Value()
			m.catalogCursor = 0
		}
		return m, c, true
	}
	switch k {
	case "esc":
		return act("selector-cancel")
	case "/":
		return act("search")
	case "i":
		if m.screen == "new-catalog" {
			return act("selector-instances")
		}
	case "tab", "shift+tab":
		ids := []string{}
		for _, h := range p.Hits {
			if strings.HasPrefix(h.ID, "selector-") && !strings.Contains(h.ID, ":") {
				ids = append(ids, h.ID)
			}
		}
		if len(ids) > 0 {
			idx := -1
			for i, id := range ids {
				if id == m.uiFocus {
					idx = i
				}
			}
			step := 1
			if k == "shift+tab" {
				step = len(ids) - 1
			}
			m.uiFocus = ids[(idx+step+len(ids))%len(ids)]
		}
		return m, nil, true
	case "enter":
		if m.uiFocus != "" {
			return act(m.uiFocus)
		}
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
