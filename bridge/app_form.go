package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
)

type appFormCreated struct {
	generation uint64
	result     appCreated
}

func (m harborModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	back := func() (tea.Model, tea.Cmd) {
		m.screen = "new-form"
		m.formFocus = min(4, m.formFocus)
		m.scroll = 0
		return m, nil
	}
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
			m.formFocus = (m.formFocus + 1) % 5
		case "shift+tab", "up":
			m.formFocus = (m.formFocus + 4) % 5
		case "enter":
			m.notice = ""
			if m.formFocus < 5 {
				m.draft.RecoveryChoice = ""
			}
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
				m.input.CursorEnd()
				m.input.Focus()
			case 5:
				if m.creating {
					return m, nil
				}
				m.creating = true
				m.screen = "busy"
				g, d, client, version := m.generation, m.draft, m.selectedClient(), m.environment.CodexVersion
				return m, func() tea.Msg {
					reject := func(focus int, page string, err error) tea.Msg { return appFormRejected{g, focus, page, err} }
					if err := m.service.ValidateDirectory(d.Directory); err != nil {
						return reject(0, "new-form", err)
					}
					if client == "codex" && (version == "" || version == "unknown") {
						return reject(1, "new-form", fmt.Errorf("Install the supported native Codex CLI and retry."))
					}
					if d.Connection == "magpie" {
						if d.Instance == "" || d.Model == "" {
							return reject(2, "new-form", fmt.Errorf("Choose a Magpie instance and model or routing group."))
						}
						ack, err := m.service.AuxiliaryAcknowledged(d.Instance)
						if err != nil {
							return reject(2, "new-form", err)
						}
						if !ack {
							return reject(5, "new-notice", nil)
						}
					}
					if d.Name != "" {
						if err := validateSessionName(d.Name); err != nil {
							return reject(4, "new-form", err)
						}
					}
					d, err := m.service.Draft(d)
					if err != nil {
						focus := 5
						if d.Connection == "magpie" {
							focus = 3
						}
						return reject(focus, "new-form", err)
					}
					if preparer, ok := m.service.(interface{ PrepareRecovery(sessionDraft) error }); ok {
						if err := preparer.PrepareRecovery(d); err != nil {
							return appPreparedDraft{g, d, appCreated{err: err}}
						}
					}
					s, err := m.service.Create(d)
					return appPreparedDraft{g, d, appCreated{s, err}}
				}
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
			instance := m.draft.Instance
			n, c, _ := m.serviceTask(func() error { return m.service.AcknowledgeAuxiliary(instance) }, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
					return nil
				}
				n.screen = "new-form"
				n.scroll = 0
				return nil
			})
			return n, c
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
			instance := m.draft.Instance
			ack := false
			n, c, _ := m.serviceTask(func() error { var err error; ack, err = m.service.AuxiliaryAcknowledged(instance); return err }, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
					return nil
				}
				n.screen = "new-form"
				if !ack {
					n.screen = "new-notice"
				}
				n.scroll = 0
				return nil
			})
			return n, c
		}
	}
	return m, nil
}
func (m *harborModel) formCatalog() tea.Cmd {
	m.generation++
	g, id := m.generation, m.draft.Instance
	draft := m.draft
	if draft.Profile != nil {
		b := *draft.Profile
		draft.Profile = &b
	}
	m.screen = "new-catalog-loading"
	return func() tea.Msg {
		c, e := catalogForDraft(m.service, draft, id)
		return appCatalog{generation: g, instance: id, models: c, err: e}
	}
}

type appFormRejected struct {
	generation uint64
	focus      int
	page       string
	err        error
}
type appPreparedDraft struct {
	generation uint64
	draft      sessionDraft
	result     appCreated
}
