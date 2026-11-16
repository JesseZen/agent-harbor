package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m harborModel) accountActions() []uiAction {
	if m.screen == "accounts-busy" {
		return nil
	}
	if m.screen == "accounts-result" {
		return []uiAction{{ID: "account:back", Label: "Back to accounts", Key: "esc"}}
	}
	return []uiAction{{ID: "account:s", Label: "Select saved login", Key: "s", Primary: true}, {ID: "account:l", Label: "Add login", Key: "l"}, {ID: "account:o", Label: "Not signed in", Key: "o"}, {ID: "account:p", Label: "Choose Profile", Key: "p"}, {ID: "account:r", Label: "Refresh", Key: "r"}, {ID: "account:back", Label: "Back", Key: "esc"}}
}

func (m harborModel) accountContent() uiPage {
	p := newUIPage(m.width, 10000)
	wrap := func(text string) {
		for _, paragraph := range strings.Split(text, "\n") {
			for _, line := range strings.Split(ansi.Wrap(cleanUI(paragraph), max(1, m.width-2), ""), "\n") {
				p.line(line)
			}
		}
	}
	p.line(paint(fg("brand"), "👤 Harbor · Accounts"))
	if m.screen == "accounts" {
		p.tabs("accounts", m.uiHover, m.uiFocus, true)
		if m.accountProfile == nil {
			wrap("Unknown · select a Profile with p. Account actions require its explicit client environment.")
		} else if m.accountSnapshot != nil {
			wrap(accountResultText(*m.accountSnapshot))
			for _, a := range m.accountSnapshot.Accounts {
				label := a.Alias
				if label == "" {
					label = a.Email
				}
				if a.Active {
					label += " · saved selection"
				}
				wrap(label)
			}
		} else {
			wrap("Profile: " + m.accountProfile.ProfileID + " · status not checked")
		}
		wrap("Select a saved login, add one through official Codex login, or choose Not signed in. Saved accounts are retained.")
		wrap("Switch between replies. Active or unknown activity blocks changes. Running sessions report any required handoff; basic management does not restart processes.")
		if experimentalCodexAccounts() {
			wrap("Experimental running-server refresh uses an undocumented side effect. Each identity must be confirmed.")
		}

	} else if m.screen == "accounts-busy" {
		wrap("Waiting for the account action in codex-auth. Complete or cancel there; duplicate Harbor actions are disabled.")
	} else {
		p.line(paint(fg("section"), "Account action result"))
		if m.accountResult == "" {
			wrap("Unknown · no account verification result was returned.")
		} else {
			wrap(m.accountResult)
		}
	}
	return p
}

func (m harborModel) accountsPage() (uiPage, bool) {
	if m.screen != "accounts" && m.screen != "accounts-result" && m.screen != "accounts-busy" {
		return uiPage{}, false
	}
	content := m.accountContent()
	footer := newUIPage(m.width, 10000)
	actions := m.accountActions()
	footer.buttons(actions, m.uiFocus)
	if len(actions) > 0 {
		footer.line(note(actionHints(actions)))
	}
	footer.line(note("↑↓ / PgUp/PgDown scroll · Tab controls"))
	return content.withFooter(footer, m.height, m.scroll), true
}

func (m harborModel) accountActivate(id string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(id, "nav:") {
		return m.activateSessions(id)
	}
	if id == "account:back" {
		m.scroll = 0
		m.uiFocus = "account:s"
		return m.updateAccounts("esc")
	}
	for _, a := range m.accountActions() {
		if a.ID == id {
			m.scroll = 0
			m.uiFocus = ""
			return m.updateAccounts(a.Key)
		}
	}
	return m, nil
}
func (m harborModel) accountsPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if status, ok := msg.(appAccountStatus); ok {
		if status.generation != m.accountGeneration || m.accountProfile == nil || m.service.ValidateProfileResult(status.profile, *m.accountProfile) != nil {
			return m, nil, true
		}
		m.accountSnapshot = &status.result
		if status.err != nil {
			m.accountSnapshot.Detail = status.err.Error()
		}
		return m, nil, true
	}
	if done, ok := msg.(appAccountDone); ok {
		if done.profile != nil && (m.accountProfile == nil || m.service.ValidateProfileResult(*done.profile, *m.accountProfile) != nil) {
			return m, nil, true
		}
		if done.snapshot != nil {
			m.accountSnapshot = done.snapshot
		}
		if m.screen != "accounts-busy" {
			return m, nil, true
		}
		m.screen = "accounts-result"
		m.scroll = 0
		m.uiFocus = "account:back"
		m.accountResult = done.result
		if done.err != nil {
			if m.accountResult != "" {
				m.accountResult += "\n"
			}
			m.accountResult += done.err.Error()
		}
		return m, tea.EnableMouseAllMotion, true
	}
	if _, ok := m.accountsPage(); !ok {
		return m, nil, false
	}
	switch v := msg.(type) {
	case tea.MouseMsg:
		if m.screen == "accounts-busy" {
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
		p, _ := m.accountsPage()
		id := p.target(v.X, v.Y)
		if v.Action == tea.MouseActionMotion {
			m.uiHover = strings.TrimPrefix(id, "nav:")
			return m, nil, true
		}
		if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft {
			m.uiFocus = id
			n, c := m.accountActivate(id)
			return n, c, true
		}
		return m, nil, true
	case tea.KeyMsg:
		if m.screen == "accounts-busy" {
			return m, nil, true
		}
		key := v.String()
		switch key {
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "pgdown":
			m.scroll += max(1, m.height/2)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height/2))
		case "tab", "shift+tab":
			p, _ := m.accountsPage()
			ids := []string{}
			for _, r := range p.Hits {
				if r.ID != "" {
					ids = append(ids, r.ID)
				}
			}
			m.uiFocus = uiCycle(m.uiFocus, ids, key == "shift+tab")
		case "enter":
			n, c := m.accountActivate(m.uiFocus)
			return n, c, true
		case "esc", "q":
			n, c := m.accountActivate("account:back")
			return n, c, true
		default:
			for _, a := range m.accountActions() {
				if key == a.Key {
					n, c := m.accountActivate(a.ID)
					return n, c, true
				}
			}
		}
		return m, nil, true
	}
	return m, nil, false
}
