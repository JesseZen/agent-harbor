package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"path/filepath"
	"strings"
)

type appFormCreated struct {
	generation uint64
	result     appCreated
}

func (m harborModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	back := func() (tea.Model, tea.Cmd) { m.screen = "new-form"; m.scroll = 0; return m, nil }
	if k == "esc" {
		m.generation++
		if m.screen == "new-form" {
			m.formActive = false
			m.screen = "sessions"
			return m, nil
		}
		if m.screen != "new-name" && m.screen != "new-cli" {
			m.draft = m.formSaved
		}
		return back()
	}
	switch m.screen {
	case "new-form":
		switch k {
		case "tab", "down":
			m.formFocus = (m.formFocus + 1) % 7
		case "shift+tab", "up":
			m.formFocus = (m.formFocus + 6) % 7
		case "enter":
			m.notice = ""
			m.formSaved = m.draft
			switch m.formFocus {
			case 0:
				m.input.SetValue(m.draft.Directory)
				cmd := m.openDirectoryPicker()
				return m, cmd
			case 1:
				m.screen = "new-cli"
				m.input.SetValue("")
				m.uiFocus = ""
				m.clientCursor = 0
			case 2:
				m.screen = "new-connection"
				m.connectionCursor = 0
				if m.draft.Connection == "magpie" {
					m.connectionCursor = 1
				}
			case 3:
				if m.draft.Connection != "magpie" {
					m.notice = "Native " + m.selectedClient() + " uses its own model configuration and login; choose the model in that client."
					return m, nil
				}
				cmd := m.formCatalog()
				return m, cmd
			case 4:
				m.screen = "new-name"
				m.uiFocus = ""
				m.input.SetValue(m.draft.Name)
				m.input.Focus()
			case 5:
				if m.creating {
					return m, nil
				}
				if err := m.service.ValidateDirectory(m.draft.Directory); err != nil {
					m.formFocus = 0
					m.notice = err.Error()
					return m, nil
				}
				if m.selectedClient() == "codex" && (m.environment.CodexVersion == "" || m.environment.CodexVersion == "unknown") {
					m.formFocus = 1
					m.notice = "Install the supported native Codex CLI and retry."
					return m, nil
				}
				if m.draft.Connection == "magpie" {
					if m.draft.Instance == "" || m.draft.Model == "" {
						m.formFocus = 2
						m.notice = "Choose a Magpie instance and model or routing group."
						return m, nil
					}
					ack, e := m.service.AuxiliaryAcknowledged(m.draft.Instance)
					if e != nil {
						m.formFocus = 2
						m.notice = e.Error()
						return m, nil
					}
					if !ack {
						m.screen = "new-notice"
						return m, nil
					}
				}
				if m.draft.Name != "" {
					if e := validateSessionName(m.draft.Name); e != nil {
						m.formFocus = 4
						m.notice = e.Error()
						return m, nil
					}
				}
				d, e := m.service.Draft(m.draft)
				if e != nil {
					m.notice = e.Error()
					if m.draft.Connection == "magpie" {
						m.formFocus = 3
					}
					return m, nil
				}
				m.draft = d
				m.creating = true
				m.screen = "busy"
				g := m.generation
				return m, func() tea.Msg { s, e := m.service.Create(d); return appFormCreated{g, appCreated{s, e}} }
			case 6:
				m.formActive = false
				m.screen = "sessions"
			}
		}
	case "new-name":
		if k == "enter" {
			m.draft.Name = m.input.Value()
			return back()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case "new-cli":
		if k == "down" || k == "j" {
			m.clientCursor = min(len(m.environment.Clients), m.clientCursor+1)
		}
		if k == "up" || k == "k" {
			m.clientCursor = max(0, m.clientCursor-1)
		}
		if k == "enter" {
			if m.clientCursor > 0 {
				c := m.environment.Clients[m.clientCursor-1]
				if c.nativeExecutableReady() {
					if m.draft.Connection == "magpie" {
						m.notice = "Additional client Magpie isolation unproven; explicitly choose native configuration before changing client."
						return m, nil
					}
					m.draft.Client = c.Target
					return back()
				}
				m.notice = "Native startup has no upstream capability/kind contract; inspect upstream support or choose the supported Codex path."
				return m, nil
			}
			m.draft.Client = "codex"
			return back()
		}
	case "new-notice":
		if k == "enter" {
			if e := m.service.AcknowledgeAuxiliary(m.draft.Instance); e != nil {
				m.notice = e.Error()
				return m, nil
			}
			return back()
		}
	case "new-connection":
		if k == "up" || k == "down" {
			m.connectionCursor = 1 - m.connectionCursor
		}
		if k == "enter" {
			if m.connectionCursor == 1 && m.selectedClient() != "codex" {
				m.notice = "Additional client Magpie isolation unproven; choose native configuration."
				return m, nil
			}
			if m.connectionCursor == 0 {
				m.draft.Connection = "local"
				m.draft.Instance = ""
				m.draft.Model = ""
				return back()
			}
			m.generation++
			g := m.generation
			m.screen = "new-instance-loading"
			return m, func() tea.Msg {
				r, e := m.service.InstanceReports()
				return appNewInstances{generation: g, instances: r, err: e}
			}
		}
	case "new-instance":
		rows := m.instanceRows()
		if k == "up" || k == "k" {
			m.instanceCursor = max(0, m.instanceCursor-1)
		}
		if k == "down" || k == "j" {
			m.instanceCursor = min(max(0, len(rows)-1), m.instanceCursor+1)
		}
		if k == "enter" && len(rows) > 0 {
			id := rows[m.instanceCursor].ID
			if m.draft.Instance != id {
				m.draft.Model = ""
			}
			m.draft.Connection = "magpie"
			m.draft.Instance = id
			cmd := m.formCatalog()
			return m, cmd
		}
	case "new-catalog":
		rows := m.filteredModels()
		if k == "up" || k == "k" {
			m.catalogCursor = max(0, m.catalogCursor-1)
		}
		if k == "down" || k == "j" {
			m.catalogCursor = min(max(0, len(rows)-1), m.catalogCursor+1)
		}
		if k == "enter" && len(rows) > 0 {
			m.draft.Model = rows[m.catalogCursor].ID
			ack, err := m.service.AuxiliaryAcknowledged(m.draft.Instance)
			if err != nil {
				m.notice = err.Error()
				return m, nil
			}
			if !ack {
				m.screen = "new-notice"
				return m, nil
			}
			return back()
		}
	}
	return m, nil
}
func (m *harborModel) formCatalog() tea.Cmd {
	m.generation++
	g, id := m.generation, m.draft.Instance
	m.screen = "new-catalog-loading"
	return func() tea.Msg {
		c, e := m.service.Catalog(id)
		return appCatalog{generation: g, instance: id, models: c, err: e}
	}
}
func (m harborModel) formView() string {
	connection, model := "💻 Native "+m.selectedClient()+" configuration and login", "Native configuration"
	if m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
		model = m.environment.NativeConfig
	}
	if m.draft.Connection == "magpie" {
		connection = "🐦 Magpie · " + orUnknown(m.draft.Instance)
		model = orUnknown(m.draft.Model)
	}
	if m.draft.Connection != "local" && m.draft.Connection != "magpie" {
		connection = "Unknown · choose explicitly"
		model = "Unknown"
	}
	name := m.draft.Name
	if name == "" {
		name = "Automatic: " + filepath.Base(m.draft.Directory) + " · " + m.draft.Connection
	} else {
		name = "Custom: " + name
	}
	values := []string{"📂 Directory  " + m.draft.Directory, "🤖 CLI        " + m.selectedClient(), "Connection    " + connection, "Model / group " + model, "Name          " + name, "🚀 Create", "Cancel"}
	lines := []string{"New session", ""}
	for i, v := range values {
		mark := "  "
		if i == m.formFocus {
			mark = "› "
		}
		lines = append(lines, ansi.Truncate(cleanUI(mark+v), max(20, min(140, m.width-4)), "…"))
	}
	if m.draft.Connection == "magpie" {
		lines = append(lines, "", "Auxiliary requests may use other providers.", "Groups may change. An ID does not verify the destination.")
	} else {
		lines = append(lines, "", "Native shares settings and login:", ansi.Truncate(cleanUI(m.environment.LocalHome), max(20, min(140, m.width-4)), "…"))
		if m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
			lines = append(lines, cleanUI(m.environment.NativeConfig))
		}
	}
	return strings.Join(lines, "\n")
}
