package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type secondaryRenameResult struct {
	generation uint64
	target     string
	err        error
}

func (m harborModel) secondaryContent() (string, string, []uiAction, bool) {
	back := uiAction{ID: "secondary:esc", Label: "Back", Key: "esc"}
	switch m.screen {
	case "new-project-loading":
		return "Preparing new session", "Loading project, client and connection choices.\nThe new session has not been created.", []uiAction{{ID: "secondary:esc", Label: "Cancel", Key: "esc"}}, true
	case "detail":
		body := "Selected session is unavailable. Return and select a session."
		if s, ok := m.selected(); ok {
			body = m.details(s)
		}
		return "Session details", body, []uiAction{back}, true
	case "help":
		return "Keyboard help", m.helpView(), []uiAction{back}, true
	case "delete-history":
		return "Client history", historyRemovalExplanation, []uiAction{back}, true
	case "rename", "filter":
		title := "Rename session"
		label := "Name"
		if m.screen == "filter" {
			title = "Filter sessions"
			label = "Search"
		}
		return title, label + " (editing)\n│ " + m.input.View() + " │\n" + m.notice + "\nEnter applies this edit. Esc cancels.", []uiAction{{ID: "secondary:enter", Label: "Apply", Key: "enter", Primary: true}, {ID: "secondary:esc", Label: "Cancel", Key: "esc"}}, true
	case "error":
		actions := []uiAction{back, {ID: "secondary:r", Label: "Refresh", Key: "r", Primary: true}}
		if m.errorReturn == "magpie" && m.noticeCanStart {
			actions = append(actions, uiAction{ID: "secondary:s", Label: "Start managed", Key: "s"})
		}
		return "Action needs attention", m.notice + "\n\nNext: return to the target to review or edit it, or refresh its current state before trying again.", actions, true
	case "busy":
		target := m.reviewed.Name
		if target == "" {
			target = m.draft.Name
		}
		if target == "" {
			target = "selected session"
		}
		action := m.action
		if action == "" {
			action = "Inspect or open"
		}
		if m.creating {
			action = "Create session"
		}
		return "Working", action + " · " + target + "\n\nWaiting for the service. Repeated actions are disabled.\nThis operation cannot be cancelled here.", nil, true
	}
	return "", "", nil, false
}

// Keep the action row outside the scrollable document, including short windows.
func secondaryDocument(w, h int, title, body string, actions []uiAction, focus string, scroll int) uiPage {
	p := newUIPage(w, h)
	p.line(paint(fg("title").Bold(true), "⚓ HARBOR › "+cleanUI(title)))
	controls := newUIPage(w, h)
	controls.buttons(actions, focus)
	budget := max(1, h-2-len(controls.Lines))
	wrapped := strings.Split(ansi.Wrap(body, max(1, w-2), ""), "\n")
	start := min(max(0, scroll), max(0, len(wrapped)-budget))
	for _, line := range wrapped[start:min(len(wrapped), start+budget)] {
		p.line(line)
	}
	for len(p.Lines) < h-1-len(controls.Lines) {
		p.line("")
	}
	p.line(paint(fg("muted"), "↑↓ scroll · "+actionHints(actions)))
	p.buttons(actions, focus)
	return p
}
func (m harborModel) secondaryPage() (uiPage, bool) {
	title, body, actions, ok := m.secondaryContent()
	if !ok {
		return uiPage{}, false
	}
	return secondaryDocument(m.width, m.height, title, body, actions, m.uiFocus, m.scroll), true
}
func (m harborModel) secondaryPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	// Rename owns its result envelope even if its page is no longer visible.
	if result, ok := msg.(secondaryRenameResult); ok {
		if m.screen != "busy" || m.action != "rename" || result.generation != m.generation || result.target != m.reviewed.ID {
			return m, nil, true
		}
		if result.err != nil {
			m.screen = "rename"
			m.notice = result.err.Error()
			return m, nil, true
		}
		m.screen = "sessions"
		m.notice = "Action completed"
		next, cmd := m.requestRefresh()
		return next, cmd, true
	}
	_, _, actions, ok := m.secondaryContent()
	if !ok {
		return m, nil, false
	}
	var key string
	switch v := msg.(type) {
	case tea.MouseMsg:
		if v.Button == tea.MouseButtonWheelDown {
			key = "down"
		} else if v.Button == tea.MouseButtonWheelUp {
			key = "up"
		} else if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft {
			p, _ := m.secondaryPage()
			id := p.target(v.X, v.Y)
			m.uiFocus = id
			key = strings.TrimPrefix(id, "secondary:")
			if id == "" {
				return m, nil, true
			}
		} else {
			return m, nil, true
		}
	case tea.KeyMsg:
		key = v.String()
		if m.screen == "rename" || m.screen == "filter" {
			if key != "enter" && key != "esc" && key != "ctrl+c" && key != "ctrl+g" && key != "tab" && key != "shift+tab" {
				m.uiFocus = ""
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(v)
				return m, cmd, true
			}
		}
	default:
		return m, nil, false
	}
	if m.screen == "busy" {
		if key == "ctrl+c" {
			next, cmd := m.update(tea.KeyMsg{Type: tea.KeyCtrlC})
			return next, cmd, true
		}
		return m, nil, true
	}
	if key == "tab" || key == "shift+tab" {
		index := -1
		for i, a := range actions {
			if a.ID == m.uiFocus {
				index = i
			}
		}
		delta := 1
		if key == "shift+tab" {
			delta = -1
		}
		if len(actions) > 0 {
			m.uiFocus = actions[(index+delta+len(actions))%len(actions)].ID
		}
		return m, nil, true
	}
	if key == "enter" && strings.HasPrefix(m.uiFocus, "secondary:") {
		key = strings.TrimPrefix(m.uiFocus, "secondary:")
	}
	if key == "down" || key == "j" || key == "up" || key == "k" || key == "pgdown" || key == "pgup" {
		_, body, _, _ := m.secondaryContent()
		controls := newUIPage(m.width, m.height)
		controls.buttons(actions, m.uiFocus)
		budget := max(1, m.height-2-len(controls.Lines))
		limit := max(0, len(strings.Split(ansi.Wrap(body, max(1, m.width-2), ""), "\n"))-budget)
		delta := 1
		if key == "up" || key == "k" {
			delta = -1
		}
		if key == "pgdown" {
			delta = budget
		}
		if key == "pgup" {
			delta = -budget
		}
		m.scroll = max(0, min(limit, m.scroll+delta))
		return m, nil, true
	}
	if m.screen == "rename" && key == "enter" {
		name := m.input.Value()
		if strings.TrimSpace(name) == "" {
			m.notice = "Enter a non-empty session name."
			return m, nil, true
		}
		target := sessionView{session: m.reviewed}
		if m.reviewedObservation != nil {
			target.Runtime = *m.reviewedObservation
		}
		m.generation++
		generation := m.generation
		m.action = "rename"
		m.screen = "busy"
		return m, func() tea.Msg {
			return secondaryRenameResult{generation, target.ID, m.service.RenameObserved(target, name)}
		}, true
	}
	// Legacy service/navigation semantics remain authoritative.
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "esc":
		k.Type = tea.KeyEsc
	case "enter":
		k.Type = tea.KeyEnter
	case "ctrl+c":
		k.Type = tea.KeyCtrlC
	case "ctrl+g":
		k.Type = tea.KeyCtrlG
	}
	next, cmd := m.update(k)
	return next, cmd, true
}

func (m onboardingModel) secondaryActions() []uiAction {
	if m.busy {
		return nil
	}
	if m.help {
		return []uiAction{{ID: "onboard:esc", Label: "Back", Key: "esc"}}
	}
	label := "Review choice"
	if m.review {
		label = "Confirm choice"
	}
	actions := []uiAction{{ID: "onboard:enter", Label: label, Key: "enter", Primary: true}, {ID: "onboard:esc", Label: "Cancel", Key: "esc"}, {ID: "onboard:r", Label: "Refresh", Key: "r"}, {ID: "onboard:?", Label: "Help", Key: "?"}}
	if !m.review {
		actions = append([]uiAction{{ID: "onboard:up", Label: "Previous", Key: "↑"}, {ID: "onboard:down", Label: "Next", Key: "↓"}}, actions...)
	}
	return actions
}
func (m onboardingModel) secondaryPage() uiPage {
	w, h := m.width, m.height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	title := "First run"
	body := "Herdr: " + m.report.Herdr + "\nCodex: " + m.report.Codex + "\nMagpie: " + m.report.Magpie + "\n\nChoose how new sessions connect.\n"
	if m.review {
		c := m.choices()[m.cursor]
		title = "Review: " + c.Kind
		body = "Local uses your client settings and login.\nHarbor-managed Magpie has a separate home and keeps running until explicitly stopped.\nExternal instances remain user-managed."
		if c.Kind == "external" {
			body += "\nAddress: " + c.Probe.Address + "\nVerified config: " + c.Probe.ConfigDir + "\nExecutable: " + c.Probe.Executable
		}
	} else {
		choices := m.choices()
		start := max(0, m.cursor-2)
		for i := start; i < min(len(choices), start+4); i++ {
			c := choices[i]
			mark := "  "
			if i == m.cursor {
				mark = "› "
			}
			body += "\n" + mark + map[string]string{"local": "Local Codex only for now", "managed": "Create Harbor-managed Magpie", "external": "Running Magpie " + c.Probe.Address}[c.Kind]
		}
	}
	if m.help {
		title = "Keyboard help"
		body = "↑/k ↓/j Choose a connection\nEnter Review, then confirm\nEsc Cancel review or exit\nr Refresh checks\nCtrl+D/U Scroll\nq Cancel onboarding\n\nNothing attaches to Herdr until a choice succeeds."
	}
	if m.busy {
		body += "\n\nWorking… " + m.choices()[m.cursor].Kind + " · waiting for the service. Repeated actions are disabled."
	}
	for _, warning := range m.report.Warnings {
		body += "\n⚠ " + warning
	}
	if m.notice != "" {
		body += "\n⚠ " + m.notice
	}
	return secondaryDocument(w, h, title, body, m.secondaryActions(), m.focus, m.scroll)
}
func (m onboardingModel) secondaryInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	key := ""
	switch v := msg.(type) {
	case tea.MouseMsg:
		if m.busy {
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
		if v.Action != tea.MouseActionPress || v.Button != tea.MouseButtonLeft {
			return m, nil, true
		}
		id := m.secondaryPage().target(v.X, v.Y)
		if !strings.HasPrefix(id, "onboard:") {
			return m, nil, true
		}
		key = strings.TrimPrefix(id, "onboard:")
	case tea.KeyMsg:
		if (m.help || m.review) && (v.String() == "down" || v.String() == "up" || v.String() == "j" || v.String() == "k") {
			if v.String() == "down" || v.String() == "j" {
				m.scroll++
			} else {
				m.scroll = max(0, m.scroll-1)
			}
			return m, nil, true
		}
		if v.String() == "tab" || v.String() == "shift+tab" {
			actions := m.secondaryActions()
			i := -1
			for n, a := range actions {
				if a.ID == m.focus {
					i = n
				}
			}
			d := 1
			if v.String() == "shift+tab" {
				d = -1
			}
			if len(actions) > 0 {
				m.focus = actions[(i+d+len(actions))%len(actions)].ID
			}
			return m, nil, true
		}
		if v.String() == "enter" && m.focus != "" {
			key = strings.TrimPrefix(m.focus, "onboard:")
		}
	default:
		return m, nil, false
	}
	if key == "" {
		return m, nil, false
	}
	// Clear focus while dispatching the equivalent key to avoid redispatch loops.
	focus := m.focus
	m.focus = ""
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	if key == "enter" {
		k.Type = tea.KeyEnter
	}
	if key == "esc" {
		k.Type = tea.KeyEsc
	}
	if key == "up" {
		k.Type = tea.KeyUp
	}
	if key == "down" {
		k.Type = tea.KeyDown
	}
	next, cmd := m.Update(k)
	n := next.(onboardingModel)
	n.focus = focus
	return n, cmd, true
}
