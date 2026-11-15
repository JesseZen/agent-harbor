package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type appInstances struct {
	generation uint64
	reports    []instanceReport
	err        error
}
type appMagpieDone struct{ err error }

func (m harborModel) loadInstances() tea.Cmd {
	return func() tea.Msg {
		r, e := m.service.InstanceReports()
		return appInstances{reports: r, err: e, generation: m.generation}
	}
}

// tea.Exec releases the terminal before Magpie's own interactive UI runs.
type managementExec struct {
	service    applicationService
	action, id string
}

func (e managementExec) Run() error        { return e.service.Magpie(e.action, e.id) }
func (managementExec) SetStdin(io.Reader)  {}
func (managementExec) SetStdout(io.Writer) {}
func (managementExec) SetStderr(io.Writer) {}
func (m harborModel) updateMagpie(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	rows := m.instanceRows()
	if m.screen == "magpie-context-loading" {
		if key == "esc" {
			m.screen = "sessions"
			return m.requestRefresh()
		}
		return m, nil
	}
	if m.screen == "magpie-busy" {
		return m, nil
	}
	if key == "esc" {
		m.uiFocus = ""
		m.scroll = 0
		if m.screen == "magpie" {
			m.screen = "sessions"
			return m.requestRefresh()
		} else {
			m.screen = "magpie"
		}
		return m, nil
	}
	if m.screen == "magpie-context" {
		if key == "enter" {
			m.screen = "magpie"
			return m, m.loadInstances()
		}
		return m, nil
	}
	if m.screen == "magpie-busy" {
		return m, nil
	}
	if m.screen == "magpie-bind" {
		if key == "enter" {
			address := m.input.Value()
			m.screen = "magpie-busy"
			return m, func() tea.Msg { return appMagpieDone{m.service.Magpie("bind", address)} }
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.screen == "magpie-confirm" {
		if key != "enter" {
			return m, nil
		}
		action, id := m.action, m.instanceID
		m.screen = "magpie-busy"
		return m, tea.Exec(managementExec{m.service, action, id}, func(e error) tea.Msg { return appMagpieDone{e} })
	}
	switch key {
	case "j", "down":
		m.instanceCursor = min(m.instanceCursor+1, max(0, len(rows)-1))
	case "k", "up":
		m.instanceCursor = max(0, m.instanceCursor-1)
	case "b":
		m.screen = "magpie-bind"
		m.input.SetValue("http://127.0.0.1:3425")
		m.input.CursorEnd()
		m.input.Focus()
	case "r":
		m.generation++
		return m, m.loadInstances()
	case "s", "x", "u", "enter", "w":
		action := map[string]string{"s": "start", "x": "stop", "u": "use", "enter": "manage", "w": "web"}[key]
		id := managedInstanceID
		if action != "start" && action != "stop" && action != "web" {
			if len(rows) == 0 {
				m.notice = "No instance selected. Bind a running gateway or start a managed one."
				return m, nil
			}
			r := rows[m.instanceCursor]
			id = r.ID
			if action == "manage" {
				if r.Health != instanceVerified {
					m.notice = managementFailure(r)
					m.noticeCanStart = r.Kind == "managed" && r.ID == managedInstanceID
					return m, nil
				}
			}
		}
		m.action = action
		m.instanceID = id
		m.scroll = 0
		m.uiFocus = ""
		m.uiFocus = "conn:esc"
		m.mouse = mouseClick{}
		m.reviewOpenedAt = time.Now()
		m.screen = "magpie-confirm"
	}
	return m, nil
}
func (m harborModel) magpieView() string {
	rows := m.instanceRows()
	if m.screen == "magpie-context-loading" {
		return "⏳ Identifying current pane…\n\n" + note("Esc cancels.")
	}
	if m.screen == "magpie-context" {
		return m.notice
	}
	if m.screen == "magpie-detail" {
		return m.instanceDetails()
	}
	if m.screen == "magpie-filter" {
		return "🔎 Filter Magpie instances\n\n" + m.input.View()
	}

	if m.screen == "magpie-bind" {
		return "🔌 Bind an external running Magpie\n\n" + m.input.View() + "\n\n" + note("Harbor verifies its configuration identity.") + "\n" + note("It never starts or stops this external instance.")
	}
	if m.screen == "magpie-confirm" {
		lines := []string{"🐦 Review Magpie action: " + m.action, "", "Instance  " + m.instanceID, ""}
		switch m.action {
		case "start":
			lines = append(lines, note("Creates a Harbor-owned configuration and home."), note("Keeps running after Harbor closes."), note("Only an explicit stop ends it."), note("Subscription accounts are not imported."))
		case "stop":
			lines = append(lines, note("Stops only Harbor's recorded managed process."), note("Sessions using it will lose their gateway."))
		case "use":
			lines = append(lines, note("Sets the default for future new sessions only."), note("Existing session connections stay unchanged."))
		case "web":
			lines = append(lines, note("Opens Harbor-managed Magpie in your browser. Return by closing or switching from the browser."))
		case "manage":
			lines = append(lines, note("Opens Magpie's own provider management."), note("This UI connects to the existing gateway process."), note("Exit Magpie management to return to Harbor; the gateway keeps running."))
			for _, r := range rows {
				if r.ID == m.instanceID && r.Kind != "managed" {
					lines = append(lines, note("Magpie may change client configuration in this instance's environment, affecting native sessions."))
					break
				}
			}
		}
		for _, r := range m.reports {
			if r.ID == m.instanceID {
				for _, w := range r.Warnings {
					lines = append(lines, "⚠ Warning: "+w)
				}
			}
		}
		return strings.Join(append(lines, "", keysMark+"enter confirm  esc cancel"), "\n")
	}
	lines := []string{fmt.Sprintf("🐦 Magpie instances · %d", len(rows)), m.searchLine(len(rows), len(m.reports))}
	count := max(1, m.height-18)
	start := m.listStart(m.instanceCursor, count)
	for i := start; i < min(len(rows), start+count); i++ {
		lines = append(lines, m.instanceRow(rows[i], i == m.instanceCursor))
	}
	if len(rows) > 0 {
		r := rows[m.instanceCursor]
		lines = append(lines, "", kv("Address", orUnknown(r.Address)), kv("Config", orUnknown(r.ConfigDir)))
		if r.Detail != "" {
			lines = append(lines, note(r.Detail))
		}
		for _, w := range r.Warnings {
			lines = append(lines, "⚠ "+w)
		}
	}
	if len(rows) == 0 {
		lines = append(lines, "No recorded instances.", note("Local Codex is available."))
	}
	lines = append(lines, "", "⚡ ACTIONS", keysMark+"s start managed  x stop managed  w managed web", keysMark+"b bind external  u use for new sessions", keysMark+"enter manage providers  r refresh")
	return strings.Join(lines, "\n")
}

// instanceRow renders "› Name ........ kind · ● health" within the screen width.
func (m harborModel) instanceRow(r instanceReport, selected bool) string {
	width := max(20, min(140, m.width-4))
	mark := "  "
	if selected {
		mark = "› "
	}
	tail := r.Kind + " · " + statusDot(orUnknown(r.Health))
	if r.Kind == "" {
		tail = statusDot(orUnknown(r.Health))
	}
	name := ansi.Truncate(cleanUI(mark+r.Name), max(8, width-ansi.StringWidth(tail)-3), "…")
	return ansi.Truncate(cleanUI(name+strings.Repeat(" ", max(2, width-1-ansi.StringWidth(name)-ansi.StringWidth(tail)))+tail), width, "…")
}

func (m harborModel) instanceRows() []instanceReport {
	source := m.reports
	if strings.HasPrefix(m.screen, "new-") {
		source = m.newInstances
	}
	rows := []instanceReport{}
	texts := make([]string, len(source))
	for i, r := range source {
		texts[i] = r.Name + " " + r.ID + " " + r.Address + " " + r.ConfigDir + " " + r.Health
	}
	for _, hit := range searchMatches(m.instanceFilter, texts) {
		rows = append(rows, source[hit.Index])
	}
	return rows
}
func (m harborModel) instanceDetails() string {
	rows := m.instanceRows()
	if len(rows) == 0 {
		return "No matching instance. Esc returns to the list."
	}
	r := rows[min(m.instanceCursor, len(rows)-1)]
	lines := []string{"🐦 " + r.Name, statusDot(orUnknown(r.Health)), "", "🧩 IDENTITY", kv("Instance", r.ID), kv("Kind", orUnknown(r.Kind)), kv("Identity/status", orUnknown(r.Health))}
	if r.Detail != "" {
		lines = append(lines, note(r.Detail))
	}
	lines = append(lines, "", "🔌 GATEWAY", kv("Address", orUnknown(r.Address)), kv("Recorded config", orUnknown(r.ConfigDir)), kv("Executable", orUnknown(r.Executable)), kv("Version", orUnknown(r.Version)),
		"", "🔎 EVIDENCE", kv("Observed config", orUnknown(r.Probe.ConfigDir)), kv("Identity evidence", orUnknown(r.Probe.ConfigEvidence)))
	if !r.VerifiedAt.IsZero() {
		lines = append(lines, kv("Recorded verification", r.VerifiedAt.UTC().Format("2006-01-02 15:04:05 UTC")))
	}
	for _, w := range r.Warnings {
		lines = append(lines, "⚠ Warning: "+w)
	}
	return strings.Join(lines, "\n")
}

// Reports identify ownership; a failed management attempt never starts a process.
func managementFailure(r instanceReport) string {
	reason := strings.TrimSpace(r.Detail)
	reason = strings.TrimRight(reason, ".!?:; ")
	if reason == "" {
		reason = "The gateway is not verified or available"
	}
	recovery := "Start the external gateway separately, then press r to refresh. Harbor never starts external instances."
	if r.Kind == "managed" && r.ID == managedInstanceID {
		recovery = "Press s to review Start managed, then r to refresh. Review identity details before opening management."
	}
	return "Cannot open management. " + reason + ". " + recovery
}
