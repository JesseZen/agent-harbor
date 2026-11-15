package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"path/filepath"
	"strings"
)

// Client rows carry upstream target IDs rather than indices into a changing list.
func (m harborModel) creationClients() []clientCapability {
	out := []clientCapability{{Target: "codex", Label: "Codex", Available: true}}
	seen := map[string]bool{"codex": true}
	for _, c := range m.environment.Clients {
		if c.Target != "" && !seen[c.Target] {
			out = append(out, c)
			seen[c.Target] = true
		}
	}
	return out
}
func (m harborModel) visibleCreationClients() []clientCapability {
	rows := m.creationClients()
	out := []clientCapability{}
	query := strings.ToLower(m.input.Value())
	for _, c := range rows {
		if strings.Contains(strings.ToLower(c.Target+" "+c.Label), query) {
			out = append(out, c)
		}
	}
	return out
}
func (m harborModel) newSessionPage() (uiPage, bool) {
	if !m.formActive {
		return uiPage{}, false
	}
	p := newUIPage(m.width, m.height)
	switch m.screen {
	case "new-form":
		p.line(paint(fg("brand"), "New session"))
		model := "Native " + m.selectedClient() + " configuration (read-only)"
		connection := "Native · " + m.selectedClient() + " settings and login"
		if m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
			model = m.environment.NativeConfig
		}
		if m.draft.Connection == "magpie" {
			connection = "Magpie · " + orUnknown(m.draft.Instance)
			model = orUnknown(m.draft.Model)
		}
		if m.draft.Connection != "local" && m.draft.Connection != "magpie" {
			connection = "Unknown · choose explicitly"
		}
		name := m.draft.Name
		if name == "" {
			name = "Automatic · " + filepath.Base(m.draft.Directory)
		}
		values := []string{m.draft.Directory, m.selectedClient(), connection, model, name}
		labels := []string{"Project", "Client", "Connection", "Model", "Name"}
		icons := []string{"📂", "🤖", "🔌", "🧠", "💬"}
		for i := range values {
			p.field("new-field:"+labels[i], icons[i], labels[i], values[i], m.formFocus == i, i != 3 || m.draft.Connection == "magpie")
		}
		actions := []uiAction{{ID: "new-create", Label: "Create", Key: "enter", Primary: true}, {ID: "new-cancel", Label: "Cancel", Key: "esc"}}
		if m.creating {
			actions[0].Disabled = "Creating…"
		}
		focus := ""
		if m.formFocus == 5 {
			focus = "new-create"
		}
		if m.formFocus == 6 {
			focus = "new-cancel"
		}
		p.buttons(actions, focus)
		if m.notice != "" {
			p.line(cleanUI(m.notice))
			p.button(uiAction{ID: "new-details", Label: "Details", Key: "!"}, false)
		} else {
			p.line(paint(fg("muted"), "Tab move · Enter open · Esc cancel"))
		}
	case "new-cli":
		p.line("🤖 Choose client")
		query := m.input.Value()
		p.search(query, "Filter clients", m.uiFocus == "new-client-search")
		rows := m.visibleCreationClients()
		count := max(1, p.Height-12)
		start := max(0, m.clientCursor-count+1)
		for i := start; i < min(len(rows), start+count); i++ {
			c := rows[i]
			label := c.Label
			if label == "" {
				label = c.Target
			}
			status := "Native start: unknown · hook only"
			if c.Target == "codex" {
				status = "Native + Magpie · " + orUnknown(m.environment.CodexVersion)
			} else if c.nativeExecutableReady() {
				status = "Native executable · recovery unverified"
			}
			p.field("new-client:"+c.Target, "🤖", label, status, i == m.clientCursor, false)
		}
		rows = m.visibleCreationClients()
		if len(rows) > 0 {
			c := rows[min(max(0, m.clientCursor), len(rows)-1)]
			if c.Target != "codex" {
				p.line("Identity: unknown · Recovery: unknown")
				p.line("Magpie: unavailable · isolation unproven")
			}
		}
		p.buttons([]uiAction{{ID: "new-client-select", Label: "Select", Key: "enter", Primary: true}, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
		if m.notice != "" {
			p.line(cleanUI(m.notice))
		} else {
			p.line(paint(fg("muted"), "Hook discovery is a subset, not proof of launch support."))
		}
	case "new-name":
		p.line("🏷 Session name")
		p.line(m.input.View())
		p.buttons([]uiAction{{ID: "new-name-save", Label: "Apply", Key: "enter", Primary: true}, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
	case "new-connection":
		p.line("🔌 Connection")
		p.field("new-native", "💻", "Native", m.selectedClient()+" settings and login", m.connectionCursor == 0, false)
		p.field("new-magpie", "🐦", "Magpie", "Choose instance and model", m.connectionCursor == 1, false)
		p.buttons([]uiAction{{ID: "new-connection-select", Label: "Select", Key: "enter", Primary: true}, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
		if m.notice != "" {
			p.line(cleanUI(m.notice))
		}
	default:
		return uiPage{}, false
	}
	return p, true
}
func (m harborModel) newSessionPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	p, ok := m.newSessionPage()
	if !ok {
		return m, nil, false
	}
	activate := func(id string) (tea.Model, tea.Cmd, bool) {
		switch {
		case strings.HasPrefix(id, "new-field:"):
			for i, label := range []string{"Project", "Client", "Connection", "Model", "Name"} {
				if id == "new-field:"+label {
					m.formFocus = i
				}
			}
			next, cmd := m.updateForm(uiKey("enter"))
			n := next.(harborModel)
			if n.screen == "new-cli" {
				n.uiFocus = ""
				n.clientCursor = 0
				n.input.SetValue("")
			}
			return n, cmd, true
		case strings.HasPrefix(id, "new-client:"):
			for i, c := range m.visibleCreationClients() {
				if id == "new-client:"+c.Target {
					m.clientCursor = i
				}
			}
			return m, nil, true
		case id == "search":
			m.uiFocus = "new-client-search"
			m.input.Focus()
			return m, nil, true
		case id == "new-client-select":
			rows := m.visibleCreationClients()
			if m.clientCursor < 0 || m.clientCursor >= len(rows) {
				return m, nil, true
			}
			c := rows[m.clientCursor]
			if c.Target != "codex" && !c.nativeExecutableReady() {
				m.notice = "Hook integration only; upstream native startup is unverified."
				return m, nil, true
			}
			if c.Target != "codex" && m.draft.Connection == "magpie" {
				m.notice = "Choose Native before selecting this client; Magpie isolation is unverified."
				return m, nil, true
			}
			m.draft.Client = c.Target
			m.screen = "new-form"
			m.uiFocus = ""
			m.notice = ""
			return m, nil, true
		case id == "new-native" || id == "new-magpie":
			if id == "new-native" {
				m.connectionCursor = 0
			} else {
				m.connectionCursor = 1
			}
			return m, nil, true
		case id == "new-details":
			n, c := m.update(uiKey("!"))
			return n, c, true
		case id == "new-create":
			m.formFocus = 5
		case id == "new-cancel":
			m.formFocus = 6
		case id == "new-back":
			n, c := m.updateForm(uiKey("esc"))
			return n, c, true
		case id == "new-name-save" || id == "new-connection-select":
		default:
			return m, nil, true
		}
		n, c := m.updateForm(uiKey("enter"))
		return n, c, true
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			return activate(p.target(mouse.X, mouse.Y))
		}
		return m, nil, true
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	if m.screen == "new-name" {
		k := key.String()
		if k == "tab" || k == "shift+tab" {
			if m.uiFocus == "new-name-save" {
				m.uiFocus = "new-back"
			} else if m.uiFocus == "new-back" {
				m.uiFocus = ""
			} else {
				m.uiFocus = "new-name-save"
			}
			return m, nil, true
		}
		if k == "enter" && m.uiFocus == "new-back" {
			return activate("new-back")
		}
	}
	if m.screen == "new-connection" && (key.String() == "tab" || key.String() == "shift+tab") {
		m.connectionCursor = 1 - m.connectionCursor
		return m, nil, true
	}
	if m.screen == "new-cli" {
		k := key.String()
		if m.uiFocus == "new-client-search" {
			if k == "esc" || k == "enter" || k == "tab" {
				m.uiFocus = ""
				m.clientCursor = 0
				return m, nil, true
			}
			var c tea.Cmd
			m.input, c = m.input.Update(key)
			m.clientCursor = 0
			return m, c, true
		}
		switch k {
		case "/":
			return activate("search")
		case "down", "j", "tab":
			m.clientCursor = min(max(0, len(m.visibleCreationClients())-1), m.clientCursor+1)
		case "up", "k", "shift+tab":
			m.clientCursor = max(0, m.clientCursor-1)
		case "enter":
			return activate("new-client-select")
		case "esc":
			return activate("new-back")
		}
		return m, nil, true
	}
	if (key.String() == "?" || key.String() == "!") && m.screen != "new-name" {
		n, c := m.update(key)
		return n, c, true
	}
	n, c := m.updateForm(key)
	return n, c, true
}
