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
	if p, ok := m.connectionSelectorPage(); ok {
		return p, true
	}
	p := newUIPage(m.width, m.height)
	switch m.screen {
	case "new-recovery":
		return secondaryDocument(m.width, m.height, "Conversation recovery", "Profile: "+m.profileDisplayLabel(m.draft.Profile)+"\n\nRecovery needs Herdr’s Codex identity hooks in this Profile. Prepare enables hooks; Codex will ask you to trust them.\n\nSkip opens without automatic recovery. No conversation identity will be inferred.\n\nCancel returns to the form without creating a record or terminal.", []uiAction{{ID: "recovery:yes", Label: "Prepare and create", Key: "y"}, {ID: "recovery:skip", Label: "Skip and create", Key: "s"}, {ID: "recovery:esc", Label: "Cancel", Key: "esc"}, {ID: "recovery:?", Label: "Help", Key: "?"}}, m.uiFocus, m.scroll), true
	case "new-directory":
		return m.directoryPage(), true
	case "new-form":
		p.title("New session · Profile: " + m.profileDisplayLabel(m.draft.Profile))
		model := "Native " + m.selectedClient() + " configuration (read-only)"
		connection := "Native · " + m.selectedClient() + " settings and login"
		if m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
			model = m.environment.NativeConfig
		}
		if m.draft.Connection == "magpie" {
			connection = "Magpie · choose instance and model"
			model = "Choose a model or routing group"
			if m.draft.Instance != "" {
				connection = "Magpie · " + m.draft.Instance
			}
			if m.draft.Model != "" {
				model = m.draft.Model
			}
		}
		if m.draft.Connection != "local" && m.draft.Connection != "magpie" {
			connection = "Unknown · choose explicitly"
		}
		name := m.draft.Name
		if name == "" {
			name = "Automatic · " + filepath.Base(m.draft.Directory)
		}
		values := []string{m.draft.Directory, m.selectedClient(), connection, model, codexOptionsValue(m.draft.CodexOptions).summary(), name, m.draft.ResponsesFormat.label()}
		icons := []string{"📂", "🤖", "🔌", "🧠", "🎛️", "💬", ""}
		for i := range values {
			if !m.formFieldVisible(i) {
				continue
			}
			p.field("new-field:"+formLabels[i], icons[i], formLabels[i], values[i], m.formFocus == i, i != formModel || m.draft.Connection == "magpie")
		}
		actions := []uiAction{{ID: "new-create", Label: "Create", Key: "ctrl+s", Primary: true}, {ID: "new-cancel", Label: "Cancel", Key: "esc"}}
		if m.creating {
			actions[0].Disabled = "Creating…"
		}
		focus := ""
		if m.formFocus == formCreate {
			focus = "new-create"
		}
		if m.formFocus == formCancel {
			focus = "new-cancel"
		}
		p.shortcuts(actions, focus)
		p.Hint = "↑↓/jk fields · Enter open"
		if m.notice != "" {
			p.line(cleanUI(m.notice))
			p.shortcut(uiAction{ID: "new-details", Label: "Details", Key: "!"}, false)
		}
	case "new-cli":
		p.title("Choose client")
		query := m.input.Value()
		p.search(query, "Filter clients", m.uiFocus == "new-client-search", m.input)
		rows := m.visibleCreationClients()
		if len(rows) == 0 {
			p.emptyState("No matching clients")
		}
		listTop := len(p.Lines)
		count := max(1, p.Height-12)
		start := m.listStart(m.clientCursor, count)
		p.ListStart = start
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
			id := "new-client:" + c.Target
			p.listRow(id, "🤖", label, status, m.rowStyle(id, i == m.clientCursor && m.uiFocus != "new-client-search", c.Target == m.selectedClient()))
		}
		p.scrollRail(listTop, len(p.Lines)-listTop, len(rows), start)
		p.Hint = "↑↓/jk select · / search"
		if m.uiFocus == "new-client-search" {
			p.Hint = "Type to search · ⎋ clear / leave search"
		}
		rows = m.visibleCreationClients()
		if len(rows) > 0 {
			c := rows[min(max(0, m.clientCursor), len(rows)-1)]
			if c.Target != "codex" {
				p.line("Identity: unknown · Recovery: unknown")
				p.line("Magpie: unavailable · isolation unproven")
			}
		}
		selectAction := uiAction{ID: "new-client-select", Label: "Select", Key: "enter", Primary: true}
		if len(rows) == 0 {
			selectAction.Disabled = "No matching clients; Esc clears search"
		}
		p.shortcuts([]uiAction{selectAction, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
		if m.notice != "" {
			p.line(cleanUI(m.notice))
		}
	case "new-responses-format":
		p.title("Responses format")
		applied := m.draft.ResponsesFormat
		if applied == "" {
			applied = responsesAutomatic
		}
		for i, format := range responsesFormats {
			id := "new-responses:" + string(format)
			selected := applied == format
			p.listRow(id, selectorMark(selected), format.label(), "", m.rowStyle(id, i == m.responsesCursor, selected))
		}
		p.Hint = "↑↓/jk select"
		p.shortcuts([]uiAction{{ID: "new-responses-select", Label: "Apply", Key: "enter", Primary: true}, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
	case "new-name":
		p.title("Session name")
		placeholder := "Project and connection (optional)"
		if m.selectedClient() == "codex" {
			placeholder = "First Codex title (optional)"
		}
		for _, line := range strings.Split(textEntry(m.input, m.width, placeholder), "\n") {
			p.line(documentLine(line))
		}
		p.shortcuts([]uiAction{{ID: "new-name-save", Label: "Apply", Key: "enter", Primary: true}, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
	case "new-connection":
		p.title("Connection")
		native := m.formSaved.Connection == "local"
		magpie := m.formSaved.Connection == "magpie"
		p.listRow("new-native", selectorMark(native), "Native", m.selectedClient()+" settings and login", m.rowStyle("new-native", m.connectionCursor == 0, native))
		p.listRow("new-magpie", selectorMark(magpie), "Magpie", "Choose instance and model", m.rowStyle("new-magpie", m.connectionCursor == 1, magpie))
		p.Hint = "↑↓/jk select"
		p.shortcuts([]uiAction{{ID: "new-connection-select", Label: "Select", Key: "enter", Primary: true}, {ID: "new-back", Label: "Cancel", Key: "esc"}}, m.uiFocus)
		if m.notice != "" {
			p.line(cleanUI(m.notice))
		}
	default:
		return uiPage{}, false
	}
	return p.finish(0), true
}
func (m harborModel) newSessionPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.formActive && m.screen == "new-recovery" {
		key := ""
		switch v := msg.(type) {
		case tea.KeyMsg:
			key = v.String()
		case tea.MouseMsg:
			if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft {
				p, _ := m.newSessionPage()
				key = strings.TrimPrefix(p.target(v.X, v.Y), "recovery:")
			}
		default:
			return m, nil, false
		}
		if key == "tab" || key == "shift+tab" {
			return m, nil, true
		}
		if key == "enter" {
			key = "esc"
		}
		if key == "up" || key == "down" {
			if key == "up" {
				m.scroll = max(0, m.scroll-1)
			} else {
				m.scroll++
			}
			return m, nil, true
		}
		if key == "?" {
			n, c := m.Update(uiKey("?"))
			return n, c, true
		}
		if key == "esc" {
			m.screen = "new-form"
			m.formFocus = 0
			m.uiFocus = ""
			m.draft.RecoveryChoice = ""
			return m, nil, true
		}
		if key == "y" || key == "yes" || key == "s" || key == "skip" {
			m.draft.RecoveryChoice = "yes"
			if key == "s" || key == "skip" {
				m.draft.RecoveryChoice = "skip"
			}
			m.screen = "new-form"
			m.formFocus = formCreate
			m.uiFocus = ""
			n, c := m.updateForm(uiKey("enter"))
			return n, c, true
		}
		if key == "pgdown" {
			m.scroll += max(1, m.height/2)
		}
		if key == "pgup" {
			m.scroll = max(0, m.scroll-max(1, m.height/2))
		}
		return m, nil, true
	}

	if m.formActive {
		if n, c, ok := m.connectionSelectorInput(msg); ok {
			return n, c, true
		}
	}
	if m.screen == "new-directory" {
		return m.directoryPageInput(msg)
	}
	p, ok := m.newSessionPage()
	if !ok {
		return m, nil, false
	}
	activate := func(id string) (tea.Model, tea.Cmd, bool) {
		switch {
		case strings.HasPrefix(id, "new-field:"):
			for i, label := range formLabels {
				if id == "new-field:"+label && m.formFocus != i {
					m.formFocus = i
					return m, nil, true
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
					if m.clientCursor == i && m.uiFocus != "new-client-search" {
						return activateClient(m, c)
					}
					m.pinList(p.ListStart)
					m.clientCursor = i
					m.uiFocus = ""
					m.input.Blur()
					break
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
				m.notice = ""
				return m, nil, true
			}
			return activateClient(m, rows[m.clientCursor])
		case id == "new-native" || id == "new-magpie":
			index := 0
			if id == "new-magpie" {
				index = 1
			}
			if m.connectionCursor != index {
				m.connectionCursor = index
				return m, nil, true
			}
		case strings.HasPrefix(id, "new-responses:"):
			for i, format := range responsesFormats {
				if id == "new-responses:"+string(format) && m.responsesCursor != i {
					m.responsesCursor = i
					return m, nil, true
				}
			}
		case id == "new-details":
			n, c := m.update(uiKey("!"))
			return n, c, true
		case id == "new-create":
			m.formFocus = formCreate
		case id == "new-cancel":
			m.formFocus = formCancel
		case id == "new-back":
			n, c := m.updateForm(uiKey("esc"))
			return n, c, true
		case id == "new-name-save" || id == "new-connection-select" || id == "new-responses-select":
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
	if m.screen == "new-form" && key.String() == "ctrl+s" {
		return activate("new-create")
	}
	if m.screen == "new-cli" {
		k := key.String()
		if m.uiFocus == "new-client-search" {
			if k == "esc" && m.input.Value() != "" {
				m.input.SetValue("")
				m.clientCursor = 0
				return m, nil, true
			}
			if k == "esc" || k == "enter" {
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
		case "down", "j":
			m.clientCursor = min(max(0, len(m.visibleCreationClients())-1), m.clientCursor+1)
		case "up", "k":
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

func activateClient(m harborModel, c clientCapability) (tea.Model, tea.Cmd, bool) {
	if c.Target != "codex" && !c.nativeExecutableReady() {
		m.notice = "Hook integration only; upstream native startup is unverified."
		return m, nil, true
	}
	if c.Target != "codex" && m.draft.Connection == "magpie" {
		m.notice = "Choose Native before selecting this client; Magpie isolation is unverified."
		return m, nil, true
	}
	if m.draft.Profile != nil {
		binding := *m.draft.Profile
		var environment profileClientEnvironment
		return m.serviceTask(func() error {
			var err error
			environment, err = m.service.ResolveProfileClient(profileClientRequest{Binding: binding})
			return err
		}, func(n *harborModel, err error) tea.Cmd {
			if err != nil {
				n.notice = err.Error()
				return nil
			}
			if environment.Kind == profileIsolated && c.Target != "codex" {
				n.notice = "This client requires an explicitly selected existing environment; isolated support is available for Codex."
				return nil
			}
			binding.Client = c.Target
			n.draft.Profile = &binding
			n.setDraftClient(c.Target)
			n.screen = "new-form"
			n.uiFocus = ""
			n.notice = ""
			return nil
		})
	}
	m.setDraftClient(c.Target)
	m.screen = "new-form"
	m.formFocus = 0
	m.uiFocus = ""
	m.notice = ""
	return m, nil, true
}
