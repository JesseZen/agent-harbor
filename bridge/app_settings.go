package main

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"strconv"
	"strings"
)

type settingsSaved struct{ err error }
type settingsModel struct {
	details               bool
	help                  bool
	scroll                int
	width, height, cursor int
	values                shortcutPreferences
	input                 textinput.Model
	editing, busy, closed bool
	notice                string
	query                 string
	searching             bool
	hover                 string
	nav                   string
	focusNav              string
	saved                 shortcutPreferences
}

func newSettingsModel() settingsModel {
	m := settingsModel{width: 80, height: 24, values: defaultShortcuts(), input: textinput.New()}
	m.input.CharLimit = 32
	root, err := bridgeHome()
	if err == nil {
		m.values, err = readShortcuts(root)
	}
	if err != nil {
		m.notice = err.Error()
	}
	m.saved = m.values
	return m
}
func (m settingsModel) Init() tea.Cmd { return nil }
func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlZ {
		return m, tea.Suspend
	}

	next, cmd := m.update(msg)
	n := next.(settingsModel)
	n.scroll = max(0, min(n.scroll, n.layout().MaxScroll))
	return n, cmd
}
func (m settingsModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case settingsSaved:
		m.busy = false
		m.cursor = 0
		if v.err != nil {
			m.notice = v.err.Error()
		} else {
			m.notice = "Saved for this workspace and restart."
			m.saved = m.values
		}
	case tea.MouseMsg:
		page := m.layout()
		target := page.target(v.X, v.Y)
		if v.Action == tea.MouseActionMotion {
			m.hover = strings.TrimPrefix(target, "nav:")
			return m, nil
		}
		if m.details || m.help {
			if v.Button == tea.MouseButtonWheelDown {
				m.scroll++
			}
			if v.Button == tea.MouseButtonWheelUp {
				m.scroll = max(0, m.scroll-1)
			}
		}
		if v.Action != tea.MouseActionPress || v.Button != tea.MouseButtonLeft || m.busy {
			return m, nil
		}
		if strings.HasPrefix(target, "nav:") && !m.editing {
			return m.navigate(strings.TrimPrefix(target, "nav:"))
		}
		if target == "search" && !m.editing {
			m.searching = true
			m.focusNav = ""
			return m, nil
		}
		if target == "details" {
			m.focusNav = ""
			m.searching = false
			m.details = true
			m.scroll = 0
			return m, nil
		}
		if target == "back" {
			return m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		}
		if target == "accept" {
			return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		if strings.HasPrefix(target, "control:") && !m.editing {
			i, _ := strconv.Atoi(strings.TrimPrefix(target, "control:"))
			m.cursor = i
			m.focusNav = ""
			m.searching = false
			return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		return m, nil
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		key := v.String()
		if key == "ctrl+c" {
			m.closed = true
			return m, tea.Quit
		}
		if key == "?" && !m.editing && !m.searching && !m.details && !m.help {
			m.help = true
			m.scroll = 0
			return m, nil
		}
		if m.focusNav != "" && !m.details && !m.help {
			tabs := m.navIDs()
			at := 0
			for i, id := range tabs {
				if id == m.focusNav {
					at = i
				}
			}
			switch key {
			case "tab":
				if at+1 < len(tabs) {
					m.focusNav = tabs[at+1]
				} else {
					m.focusNav = ""
					m.searching = true
				}
			case "shift+tab":
				if at > 0 {
					m.focusNav = tabs[at-1]
				} else {
					m.focusNav = ""
					m.cursor = 2
				}
			case "enter":
				return m.navigate(m.focusNav)
			case "esc":
				m.focusNav = ""
			}
			return m, nil
		}
		if m.searching {
			switch key {
			case "esc":
				if m.query != "" {
					m.query = ""
				} else {
					m.searching = false
				}
			case "shift+tab":
				m.searching = false
				if m.height >= 19 {
					tabs := m.navIDs()
					m.focusNav = tabs[len(tabs)-1]
				} else {
					m.cursor = 2
				}
			case "enter", "tab":
				m.searching = false
				if rows := m.filtered(); len(rows) > 0 {
					m.cursor = rows[0]
				} else {
					m.cursor = 3
				}
			case "backspace":
				r := []rune(m.query)
				if len(r) > 0 {
					m.query = string(r[:len(r)-1])
				}
			default:
				if v.Type == tea.KeyRunes {
					m.query += string(v.Runes)
				}
			}
			return m, nil
		}
		if m.details || m.help {
			switch key {
			case "esc", "i", "?":
				m.details = false
				m.help = false
			case "pgdown":
				m.scroll += max(1, m.height/2)
			case "pgup":
				m.scroll = max(0, m.scroll-max(1, m.height/2))
			case "down", "j":
				m.scroll++
			case "up", "k":
				m.scroll = max(0, m.scroll-1)
			}
			return m, nil
		}
		if m.editing {
			if key == "esc" {
				m.editing = false
				return m, nil
			}
			if key == "enter" {
				value := strings.TrimSpace(m.input.Value())
				switch m.cursor {
				case 0:
					m.values.Harbor = value
				case 1:
					m.values.Settings = value
				case 2:
					m.values.Magpie = value
				}
				m.editing = false
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch key {
		case "?":
			m.help = true
			m.scroll = 0
		case "/":
			m.searching = true
		case "i":
			m.details = true
			m.scroll = 0
		case "esc", "q", "ctrl+c":
			m.closed = true
			return m, tea.Quit
		case "tab", "down", "j":
			if key == "tab" && (len(m.filtered()) == 0 || m.cursor == m.filtered()[len(m.filtered())-1]) {
				if m.height >= 19 {
					m.focusNav = "sessions"
				} else {
					m.searching = true
				}
			} else {
				m.cursor = m.nextControl(1)
			}
		case "shift+tab", "up", "k":
			rows := m.filtered()
			if key == "shift+tab" && len(rows) > 0 && m.cursor == rows[0] {
				m.searching = true
			} else {
				m.cursor = m.nextControl(-1)
			}
		case "ctrl+s", "ctrl+r":
			if key == "ctrl+s" {
				m.cursor = 4
			} else {
				m.cursor = 3
			}
			return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		case "enter":
			if m.cursor < 3 {
				m.editing = true
				m.input.SetValue(m.values.values()[m.cursor])
				m.input.CursorEnd()
				m.input.Focus()
				return m, textinput.Blink
			}
			if m.cursor == 3 {
				m.values = defaultShortcuts()
				m.notice = "Defaults restored in draft. Save to apply."
				m.cursor = 0
			}
			if m.cursor == 4 {
				if err := validateShortcuts(m.values); err != nil {
					m.notice = err.Error()
					return m, nil
				}
				m.busy = true
				p := m.values
				return m, func() tea.Msg { return settingsSaved{saveShortcuts(p)} }
			}
			if m.cursor == 5 {
				m.closed = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}
func (m settingsModel) navIDs() []string {
	ids := []string{"sessions", "magpie"}
	ids = append(ids, "accounts")
	return append(ids, "profiles", "settings")
}
func (m settingsModel) navigate(nav string) (tea.Model, tea.Cmd) {
	if nav == "settings" {
		m.focusNav = ""
		return m, nil
	}
	if m.values != m.saved {
		m.notice = "Save changes or Cancel this draft before changing pages."
		return m, nil
	}
	m.nav = nav
	m.closed = true
	return m, tea.Quit
}

var settingsLabels = []string{"Harbor", "Settings", "Magpie", "Restore defaults", "Save changes", "Cancel"}

func (m settingsModel) filtered() []int {
	out := []int{}
	for i := 0; i < 3; i++ {
		if strings.Contains(strings.ToLower(settingsLabels[i]+" "+m.values.values()[i]), strings.ToLower(m.query)) {
			out = append(out, i)
		}
	}
	return out
}
func (m settingsModel) nextControl(delta int) int {
	rows := m.filtered()
	if len(rows) == 0 {
		return 0
	}
	at := 0
	for i, n := range rows {
		if n == m.cursor {
			at = i
			break
		}
	}
	return rows[(at+delta+len(rows))%len(rows)]
}
func (m settingsModel) actions() []uiAction {
	disabled := ""
	if m.busy {
		disabled = "Saving"
	}
	return []uiAction{{ID: "control:3", Label: "Restore defaults", Key: "Ctrl+R", Disabled: disabled}, {ID: "control:4", Label: "Save changes", Key: "Ctrl+S", Primary: true, Disabled: disabled}, {ID: "control:5", Label: "Cancel", Key: "Esc", Disabled: disabled}}
}
func (m settingsModel) layout() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor · Settings"))
	if m.help {
		return secondaryDocument(m.width, m.height, "Settings help", renderHelp(m.width, settingsHelp()), []uiAction{{ID: "back", Key: "esc", Label: "Back"}}, "back", m.scroll)
	}
	if m.details {
		text := m.notice
		if m.help {
			text = "Tab / Shift+Tab or arrows select fields. Enter edits a field; Enter accepts the draft, Esc cancels the edit.\n\nCtrl+S saves all changes. Ctrl+R restores defaults in the draft. Esc returns without saving. Unsaved changes block navigation to another tab.\n\n/ searches settings. Esc clears search before leaving it. i shows details for the current message.\n\nCtrl+Z suspends on supported terminals; resume keeps the draft.\n\nShortcuts apply only to this Harbor workspace. prefix is Ctrl+B; native Herdr settings remain prefix+s."
		}
		if text == "" {
			text = actionHints(m.actions()) + "\n\n" + "Shortcuts affect only this owned Harbor workspace. prefix is Ctrl+B. Native Herdr settings remain prefix+s. Direct Ctrl keys intercept input before the CLI and may conflict with terminal encodings or Emacs bindings. Only unoccupied supported chords are accepted."
		}
		title := "Settings details"
		if m.help {
			title = "Settings help"
		}
		return secondaryDocument(m.width, m.height, title, text, []uiAction{{ID: "back", Label: "Back", Key: "esc"}}, "back", m.scroll)
	}
	if p.Height >= 19 {
		p.tabs("settings", m.hover, m.focusNav, true)
	}
	p.search(m.query, "Search settings", m.searching)
	if p.Height >= 23 {
		p.line("")
		p.line(paint(fg("section"), "Workspace shortcuts"))
		p.line("")
	}
	for _, i := range m.filtered() {
		value := m.values.values()[i]
		if m.editing && m.cursor == i {
			value = m.input.View()
		}
		p.field("control:"+strconv.Itoa(i), []string{"🚢", "⚙", "🐦"}[i], settingsLabels[i], value, m.cursor == i && !m.searching && m.focusNav == "", true)

	}
	if len(m.filtered()) == 0 {
		p.line("No matching settings. Clear the search to return.")
	}
	if m.editing {
		p.shortcut(uiAction{ID: "accept", Label: "Accept field", Key: "Enter", Primary: true}, true)
		p.shortcut(uiAction{ID: "back", Label: "Cancel edit", Key: "Esc"}, false)
	} else {
		if p.Height >= 23 {
			p.line("")

		}
		actions := m.actions()
		if p.Height >= 23 {
			p.shortcuts([]uiAction{actions[0], actions[2], actions[1]}, "control:"+strconv.Itoa(m.cursor))
		} else {
			for i, a := range actions {
				p.shortcut(a, m.cursor == i+3)
			}
		}
	}
	notice := m.notice
	if m.busy {
		notice = "Saving and reloading owned workspace…"
	}
	if notice == "" {
		notice = ""
	}
	p.line(paint(fg("value"), cleanUI(notice)))
	hint := "Tab / ↑↓ focus · Enter edit · / search · i Details · ? help"

	if m.editing {
		hint = ""
	}
	if m.searching {
		hint = "Type search  Tab controls  Esc clear/back"
	}
	p.Hint = hint

	return p.finish(0)
}
func (m settingsModel) View() string { return m.layout().view() }
func runSettingsApp() error {
	result, err := tea.NewProgram(newSettingsModel(), tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	if err != nil {
		return err
	}
	settings := result.(settingsModel)
	if settings.nav != "" {
		m := newHarborModel(appService)
		defer m.cancelRefresh()
		m.screen = settings.nav
		_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	}
	return err
}
