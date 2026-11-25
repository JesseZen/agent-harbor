package main

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	formProject = iota
	formClient
	formConnection
	formModel
	formOptions
	formName
	formResponses
	formCreate
	formCancel
)

var formLabels = []string{"Project", "Client", "Connection", "Model", "Options", "Name", "Responses format"}

type appFormCreated struct {
	generation uint64
	result     appCreated
}

func (m harborModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	back := func() (tea.Model, tea.Cmd) {
		m.screen = "new-form"
		m.formFocus = min(formResponses, m.formFocus)
		m.scroll = 0
		m.uiFocus = ""
		m.input.Blur()
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
		case "down", "up", "j", "k":
			step := 1
			if k == "up" || k == "k" {
				step = -1
			}
			for {
				m.formFocus = (m.formFocus + len(formLabels) + step) % len(formLabels)
				if m.formFieldVisible(m.formFocus) {
					break
				}
			}
		case "enter":
			m.notice = ""
			if m.formFocus < formCreate {
				m.draft.RecoveryChoice = ""
			}
			m.formSaved = m.draft
			switch m.formFocus {
			case formProject:
				m.input.SetValue(m.draft.Directory)
				cmd := m.openDirectoryPicker()
				return m, cmd
			case formClient:
				m.screen = "new-cli"
				m.input.SetValue("")
				m.uiFocus = ""
				m.clientCursor = 0
			case formConnection:
				m.chooseConnection()
			case formModel:
				if m.draft.Connection != "magpie" {
					m.notice = "Native " + m.selectedClient() + " uses its own model configuration and login; choose the model in that client."
					return m, nil
				}
				if m.draft.Instance != "" {
					cmd := m.formCatalog(m.draft.Instance)
					return m, cmd
				}
				cmd := m.formInstances(false)
				return m, cmd
			case formOptions:
				if m.selectedClient() == "codex" {
					m.openCodexOptions(codexOptionEditor{returnScreen: "new-form", value: codexOptionsValue(m.draft.CodexOptions)})
				}
			case formResponses:
				m.screen = "new-responses-format"
				m.responsesCursor = 0
				for i, format := range responsesFormats {
					if m.draft.ResponsesFormat == format {
						m.responsesCursor = i
					}
				}
			case formName:
				m.screen = "new-name"
				m.uiFocus = ""
				m.input.SetValue(m.draft.Name)
				m.input.CursorEnd()
				m.input.Focus()
			case formCreate:
				if m.creating {
					return m, nil
				}
				m.creating = true
				m.screen = "busy"
				g, d, client, version := m.generation, m.draft, m.selectedClient(), m.environment.CodexVersion
				canChooseResponses := m.formFieldVisible(formResponses)
				return m, func() tea.Msg {
					reject := func(focus int, page string, err error) tea.Msg { return appFormRejected{g, focus, page, err} }
					if err := m.service.ValidateDirectory(d.Directory); err != nil {
						return reject(formProject, "new-form", err)
					}
					if client == "codex" && (version == "" || version == "unknown") {
						return reject(formClient, "new-form", fmt.Errorf("Install the supported native Codex CLI and retry."))
					}
					if d.Connection == "magpie" {
						if d.Instance == "" || d.Model == "" {
							return reject(formConnection, "new-form", fmt.Errorf("Choose a Magpie instance and model or routing group."))
						}
						ack, err := m.service.AuxiliaryAcknowledged(d.Instance)
						if err != nil {
							return reject(formConnection, "new-form", err)
						}
						if !ack {
							return reject(formCreate, "new-notice", nil)
						}
					}
					if d.Name != "" {
						if err := validateSessionName(d.Name); err != nil {
							return reject(formName, "new-form", err)
						}
					}
					d, err := m.service.Draft(d)
					if err != nil {
						focus := formCreate
						if d.Connection == "magpie" {
							focus = formModel
						}
						if errors.Is(err, errMagpieResponsesLiteUnavailable) && canChooseResponses {
							focus = formResponses
						}
						var option *codexOptionError
						if errors.As(err, &option) {
							focus = formOptions
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
			case formCancel:
				m.formActive = false
				m.screen = "sessions"
			}
		}
	case "new-responses-format":
		switch k {
		case "up", "k":
			m.responsesCursor = max(0, m.responsesCursor-1)
		case "down", "j":
			m.responsesCursor = min(len(responsesFormats)-1, m.responsesCursor+1)
		case "enter":
			m.draft.ResponsesFormat = responsesFormats[m.responsesCursor]
			return back()
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
					m.setDraftClient(c.Target)
					return back()
				}
				m.notice = "Native startup has no upstream capability/kind contract; inspect upstream support or choose the supported Codex path."
				return m, nil
			}
			m.setDraftClient("codex")
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
				m.draft.ResponsesFormat = ""
				m.rememberConnection()
				return back()
			}
			cmd := m.formInstances(false)
			return m, cmd
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
			cmd := m.formCatalog(rows[m.instanceCursor].ID)
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
			m.setDraftModel(rows[m.catalogCursor].ID)
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

func (m *harborModel) setDraftClient(client string) {
	if m.selectedClient() != client {
		m.draft.CodexOptions = nil
		if client == "codex" {
			m.draft.CodexOptions = m.environment.CodexOptions.saved()
		}
	}
	m.draft.Client = client
}
func (m *harborModel) chooseConnection() {
	m.screen = "new-connection"
	m.connectionCursor = 0
	if m.draft.Connection == "magpie" {
		m.connectionCursor = 1
	}
}

func (m *harborModel) rememberConnection() {
	if m.draft.Profile == nil {
		return
	}
	if m.creationConnections == nil {
		m.creationConnections = make(map[profileBinding]string)
	}
	m.creationConnections[*m.draft.Profile] = m.draft.Connection
}

func (m *harborModel) formInstances(chooseInstance bool) tea.Cmd {
	m.generation++
	g, service := m.generation, m.service
	m.screen, m.uiFocus, m.scroll = "new-instance-loading", "", 0
	m.input.Blur()
	return func() tea.Msg {
		reports, err := service.InstanceReports()
		return appNewInstances{generation: g, instances: reports, err: err, chooseInstance: chooseInstance}
	}
}

func (m *harborModel) formCatalog(id string) tea.Cmd {
	if m.draft.Instance != id {
		m.setDraftModel("")
	}
	m.draft.Connection, m.draft.Instance = "magpie", id
	m.generation++
	g := m.generation
	draft := m.draft
	if draft.Profile != nil {
		b := *draft.Profile
		draft.Profile = &b
	}
	m.screen, m.uiFocus, m.scroll = "new-catalog-loading", "", 0
	m.input.Blur()
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

func (m *harborModel) setDraftModel(id string) {
	if m.draft.Model != id {
		m.draft.ResponsesFormat = ""
	}
	m.draft.Model = id
}

func (m harborModel) formFieldVisible(field int) bool {
	switch field {
	case formOptions:
		return m.selectedClient() == "codex"
	case formResponses:
		if m.selectedClient() == "codex" && m.draft.Connection == "magpie" && m.draft.Instance == m.modelsInstance {
			for _, model := range m.models {
				if model.ID == m.draft.Model && model.SupportsResponsesLite {
					return true
				}
			}
		}
		return false
	default:
		return true
	}
}
