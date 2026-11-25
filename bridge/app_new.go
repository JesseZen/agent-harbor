package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type appDirectoryValidated struct {
	generation uint64
	path       string
	err        error
}

type appNewInstances struct {
	generation     uint64
	instances      []instanceReport
	err            error
	chooseInstance bool
}
type appCatalog struct {
	generation uint64
	instance   string
	models     []catalogModel
	err        error
}

func (m harborModel) filteredModels() []catalogModel {
	out := []catalogModel{}
	texts := make([]string, len(m.models))
	for i, c := range m.models {
		texts[i] = c.ID + " " + c.DisplayName + " " + c.MagpieLabel + " " + c.OwnedBy
	}
	for _, hit := range searchMatches(m.catalogFilter, texts) {
		out = append(out, m.models[hit.Index])
	}
	return out
}
func (m *harborModel) prepareNewSession() tea.Cmd {
	m.generation++
	generation, profile, service := m.generation, m.activeProfile, m.service
	preferred := ""
	if profile != nil {
		preferred = m.creationConnections[*profile]
	}
	m.screen = "new-project-loading"
	return func() tea.Msg {
		var env appEnvironment
		var err error
		if profile != nil {
			env, err = service.ProfileEnvironment(*profile)
		} else {
			env, err = service.Environment()
		}
		var models []catalogModel
		if err == nil && (preferred == "magpie" || preferred == "" && env.LastConnection == "magpie") && env.LastMagpieInstance != "" && env.LastMagpieModel != "" {
			models, _ = catalogForDraft(service, sessionDraft{Profile: env.Profile}, env.LastMagpieInstance)
		}
		return appNewReady{environment: env, models: models, err: err, generation: generation}
	}
}

func (m harborModel) updateNew(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.formActive {
		return m.updateForm(msg)
	}
	key := msg.String()
	rows := m.instanceRows()
	if key == "esc" {
		m.generation++
		m.notice = ""
		m.scroll = 0
		switch m.screen {
		case "new-project-choices":
			m.screen = "new-project"
		case "new-cli":
			m.screen = "new-project"
			m.input.SetValue(m.draft.Directory)
			m.input.Focus()
		case "new-connection":
			m.screen = "new-cli"
		case "new-instance", "new-instance-loading":
			m.screen = "new-connection"
		case "new-catalog", "new-catalog-loading":
			m.screen = "new-instance"
		case "new-catalog-filter":
			m.screen = "new-catalog"
		case "new-notice":
			m.screen = "new-catalog"
		case "new-name":
			m.draft.Name = m.input.Value()
			m.screen = "new-connection"
			if m.draft.Connection == "magpie" {
				m.screen = "new-notice"
			}
		case "new-review":
			m.screen = "new-name"
			m.input.SetValue(m.draft.Name)
			m.input.Focus()
		default:
			m.screen = "sessions"
		}
		return m, nil
	}
	switch m.screen {
	case "new-project-loading", "new-instance-loading", "new-catalog-loading":
		return m, nil
	case "new-project-choices":
		switch key {
		case "down", "j":
			m.directoryCursor = min(m.directoryCursor+1, len(m.directoryChoices)-1)
		case "up", "k":
			m.directoryCursor = max(0, m.directoryCursor-1)
		case "enter":
			m.input.SetValue(m.directoryChoices[m.directoryCursor])
			m.input.CursorEnd()
			m.screen = "new-project"
		}
		return m, nil
	case "new-project":
		if key == "down" {
			cmd := m.openDirectoryPicker()
			return m, cmd
		}
		if key == "enter" {
			if e := m.service.ValidateDirectory(m.input.Value()); e != nil {
				m.notice = e.Error()
				return m, nil
			}
			m.draft.Directory = m.input.Value()
			m.screen = "new-cli"
			m.notice = ""
			return m, nil
		}
	case "new-cli":
		if key == "enter" {
			if m.environment.CodexVersion == "unknown" || m.environment.CodexVersion == "" {
				m.notice = "Install the supported native Codex CLI and retry."
				return m, nil
			}
			m.screen = "new-connection"
			return m, nil
		}
	case "new-connection":
		if key == "down" || key == "j" || key == "up" || key == "k" {
			m.connectionCursor = 1 - m.connectionCursor
			return m, nil
		}
		if key == "enter" {
			if m.connectionCursor == 1 {
				m.generation++
				generation := m.generation
				m.screen = "new-instance-loading"
				return m, func() tea.Msg {
					i, e := m.service.InstanceReports()
					return appNewInstances{instances: i, err: e, generation: generation}
				}
			}
			m.draft.Connection = "local"
			m.screen = "new-name"
			m.input.SetValue(m.draft.Name)
			m.input.Focus()
			return m, nil
		}
	case "new-instance":
		if key == "down" || key == "j" {
			m.instanceCursor = min(m.instanceCursor+1, max(0, len(rows)-1))
		}
		if key == "up" || key == "k" {
			m.instanceCursor = max(0, m.instanceCursor-1)
		}
		if key == "enter" && len(rows) > 0 {
			m.draft.Connection = "magpie"
			m.draft.Instance = rows[m.instanceCursor].ID
			id := m.draft.Instance
			m.generation++
			generation := m.generation
			m.screen = "new-catalog-loading"
			return m, func() tea.Msg {
				c, e := catalogForDraft(m.service, m.draft, id)
				return appCatalog{models: c, err: e, instance: id, generation: generation}
			}
		}
	case "new-catalog":
		if key == "/" {
			m.screen = "new-catalog-filter"
			m.input.SetValue(m.catalogFilter)
			m.input.Focus()
			return m, nil
		}
		models := m.filteredModels()
		if key == "down" || key == "j" {
			m.catalogCursor = min(m.catalogCursor+1, max(0, len(models)-1))
		}
		if key == "up" || key == "k" {
			m.catalogCursor = max(0, m.catalogCursor-1)
		}
		if key == "enter" && len(models) > 0 {
			m.draft.Model = models[m.catalogCursor].ID
			m.screen = "new-notice"
			acknowledged, err := m.service.AuxiliaryAcknowledged(m.draft.Instance)
			if err != nil {
				m.notice = err.Error()
				return m, nil
			}
			if acknowledged {
				m.screen = "new-name"
				m.input.SetValue(m.draft.Name)
				m.input.Focus()
			}
			return m, nil
		}
	case "new-catalog-filter":
		if key == "enter" {
			m.catalogFilter = m.input.Value()
			m.catalogCursor = 0
			m.screen = "new-catalog"
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case "new-notice":
		if key == "enter" {
			if err := m.service.AcknowledgeAuxiliary(m.draft.Instance); err != nil {
				m.notice = err.Error()
				return m, nil
			}
			m.screen = "new-name"
			m.input.SetValue(m.draft.Name)
			m.input.Focus()
			return m, nil
		}
	case "new-name":
		if key == "enter" {
			m.draft.Name = m.input.Value()
			draft, e := m.service.Draft(m.draft)
			if e != nil {
				m.notice = e.Error()
				return m, nil
			}
			m.draft = draft
			m.screen = "new-review"
			return m, nil
		}
	case "new-review":
		if key == "enter" {
			d := m.draft
			m.screen = "busy"
			return m, func() tea.Msg { s, e := m.service.Create(d); return appCreated{s, e} }
		}
	}
	if m.screen == "new-project" || m.screen == "new-name" {
		var cmd tea.Cmd
		before := m.input.Value()
		m.input, cmd = m.input.Update(msg)
		if m.screen == "new-project" && m.input.Value() != before {
			m.generation++
			generation, path := m.generation, m.input.Value()
			m.notice = "Checking directory…"
			return m, tea.Batch(cmd, func() tea.Msg { return appDirectoryValidated{generation, path, m.service.ValidateDirectory(path)} })
		}
		return m, cmd
	}
	return m, nil
}

// stepHeader renders "3 / 5   Title" with a dot progress trail.
func stepHeader(n int, title string) string {
	return fmt.Sprintf("%d / 5   %s   %s%s", n, title, strings.Repeat("●", n), strings.Repeat("○", 5-n))
}

func (m harborModel) newView() string {
	rows := m.instanceRows()
	width := max(20, min(140, m.width-4))
	switch m.screen {
	case "new-form":
		copy := m
		copy.formActive = true
		p, _ := copy.newSessionPage()
		return p.view()
	case "new-instance-detail":
		return m.instanceDetails()
	case "new-instance-filter":
		return "🔎 Filter Magpie instances\n\n" + m.input.View()
	case "new-project-loading", "new-instance-loading", "new-catalog-loading":
		return "⏳ Loading…\n\n" + note("Esc cancels this request.")
	case "new-directory":
		return m.directoryPage().view()
	case "new-project-choices":
		lines := []string{"📂 Choose a project directory", keysMark + "↑↓ move  enter select  esc return", ""}
		count := max(1, m.height-10)
		start := max(0, m.directoryCursor-count+1)
		for i := start; i < min(len(m.directoryChoices), start+count); i++ {
			mark := "  "
			if i == m.directoryCursor {
				mark = "› "
			}
			lines = append(lines, ansi.Truncate(cleanUI(mark+m.directoryChoices[i]), width, "…"))
		}
		return strings.Join(lines, "\n")
	case "new-project":
		lines := []string{stepHeader(1, "Project directory"), "", m.input.View(), "", keysMark + "↓ choose directory"}
		if len(m.environment.Projects) > 0 {
			lines = append(lines, "", "📂 RECENT PROJECTS")
			for _, p := range m.environment.Projects {
				lines = append(lines, "  "+p)
			}
		}
		return strings.Join(lines, "\n")
	case "new-cli":
		if m.formActive {
			return m.clientPickerView()
		}
		return strings.Join([]string{stepHeader(2, "CLI"), "", "› 🤖 Codex  " + orUnknown(m.environment.CodexVersion), "", note("Enter selects Codex."), note("If missing, install Codex and retry.")}, "\n")
	case "new-connection":
		opts := []string{"💻 Local Codex configuration and login", "🐦 Magpie model or routing group"}
		title := stepHeader(3, "Connection")
		if m.formActive {
			title = "Connection"
		}
		lines := []string{title, ""}
		for i, o := range opts {
			mark := "  "
			if i == m.connectionCursor {
				mark = "› "
			}
			lines = append(lines, mark+o)
			if i == 0 {
				lines = append(lines, "     "+m.environment.LocalHome)
			} else {
				lines = append(lines, "     Choose an instance, then a model.")
			}
			lines = append(lines, "")
		}
		lines = append(lines, "Selected: "+[]string{"Local", "Magpie"}[m.connectionCursor], note("Local shares your settings and login; changes to them affect this session."))
		if m.connectionCursor == 0 && m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
			lines = append(lines, note(m.environment.NativeConfig))
		}
		return strings.Join(lines, "\n")
	case "new-instance":
		lines := []string{"🐦 Choose a Magpie instance", m.searchLine(len(rows), len(m.newInstances))}
		count := max(1, m.height-14)
		start := m.listStart(m.instanceCursor, count)
		for i := start; i < min(len(rows), start+count); i++ {
			lines = append(lines, m.instanceRow(rows[i], i == m.instanceCursor))
		}
		if len(rows) > 0 {
			v := rows[m.instanceCursor]
			lines = append(lines, "", kv("Address", orUnknown(v.Address)), "Recorded configuration: "+orUnknown(v.ConfigDir))
		}
		if len(rows) == 0 {
			lines = append(lines, "No matching instances.", note("Change the filter or cancel to set up Magpie."))
		}
		return strings.Join(lines, "\n")
	case "new-catalog-filter":
		m.catalogFilter = m.input.Value()
		m.catalogCursor = 0
		m.screen = "new-catalog"
		m.height = max(10, m.height-4)
		return "🔎 Filter Magpie catalog\n" + m.input.View() + "\n\n" + m.newView()
	case "new-catalog":
		lines := []string{"🐦 Choose a model or routing group", "Instance: " + m.catalogInstanceLabel(), m.searchLine(len(m.filteredModels()), len(m.models)), ""}
		models := m.filteredModels()
		start := m.listStart(m.catalogCursor, max(1, m.height-12))
		for i := start; i < min(len(models), start+max(1, m.height-12)); i++ {
			mark := "  "
			if i == m.catalogCursor {
				mark = "› "
			}
			label := models[i].MagpieLabel
			if label == "" {
				label = models[i].DisplayName
				if models[i].OwnedBy != "" {
					label += " · " + models[i].OwnedBy
				}
			}
			lines = append(lines, mark+models[i].ID+"  ·  "+label)
		}
		if len(models) == 0 {
			lines = append(lines, "No matching models.", note("Press / to change the filter."))
		}
		return strings.Join(lines, "\n")
	case "new-notice":
		return strings.Join([]string{"🐦 Magpie auxiliary routing", "", note("Magpie may send parts of your conversation to other models or providers for titles and auxiliary requests."), note("Routing groups may switch members. An unchanged ID does not prove an unchanged destination."), "", keysMark + "enter acknowledge for this instance  esc back"}, "\n")
	case "new-name":
		if m.formActive {
			return "Session name\n\n" + m.input.View() + "\n\nLeave empty to use the first available Codex conversation title. Until then, the project and connection are shown."
		}
		return strings.Join([]string{stepHeader(4, "Session name"), "", m.input.View(), "", note("Leave empty to use the first available Codex conversation title; the project and connection are shown until then.")}, "\n")
	default:
		lines := []string{stepHeader(5, "Review new session"), "", kv("Name", m.draft.Name), kv("Project", m.draft.Directory), kv("CLI", "Codex "+m.environment.CodexVersion)}
		if m.draft.Connection == "magpie" {
			lines = append(lines, kv("Connection", "Magpie"), kv("Instance", m.draft.Instance), kv("Selected ID", m.draft.Model), "", note("Auxiliary requests may use other providers."), note("Routing groups may switch members."), note("An unchanged ID does not prove an unchanged destination."))
		} else {
			lines = append(lines, kv("Connection", "Local Codex configuration and login"), kv("Config", m.environment.LocalHome))
			if m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
				lines = append(lines, note(m.environment.NativeConfig))
			}
		}
		return strings.Join(append(lines, "", "🚀 Enter opens a focused Herdr tab."), "\n")
	}
}

func (m harborModel) catalogInstanceLabel() string {
	for _, r := range m.newInstances {
		if r.ID == m.draft.Instance {
			return r.ID + " · " + r.Name + " · " + r.Address + " · " + r.Health
		}
	}
	return m.draft.Instance + " · identity unknown"
}
