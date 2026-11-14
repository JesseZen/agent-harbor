package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type settingsSaved struct{ err error }
type settingsModel struct {
	details               bool
	scroll                int
	width, height, cursor int
	values                shortcutPreferences
	input                 textinput.Model
	editing, busy, closed bool
	notice                string
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
	return m
}
func (m settingsModel) Init() tea.Cmd { return nil }
func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case settingsSaved:
		m.busy = false
		if v.err != nil {
			m.notice = v.err.Error()
		} else {
			m.notice = "Saved for this workspace and restart."
		}
	case tea.MouseMsg:
		if m.details {
			if v.Button == tea.MouseButtonWheelDown {
				m.scroll++
			}
			if v.Button == tea.MouseButtonWheelUp {
				m.scroll = max(0, m.scroll-1)
			}
			return m, nil
		}
		if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft && !m.editing && !m.busy && v.X >= 0 && v.X < max(20, min(140, m.width-2)) && v.Y >= 1 && v.Y <= min(6, m.height-2) {
			m.cursor = v.Y - 1
			return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		key := v.String()
		if m.details {
			switch key {
			case "esc", "i":
				m.details = false
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
		case "i":
			m.details = true
			m.scroll = 0
		case "esc", "q", "ctrl+c":
			m.closed = true
			return m, tea.Quit
		case "tab", "down", "j":
			m.cursor = (m.cursor + 1) % 6
		case "shift+tab", "up", "k":
			m.cursor = (m.cursor + 5) % 6
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
func (m settingsModel) View() string {
	w := max(20, min(140, m.width-2))
	if m.details {
		text := m.notice
		if text == "" {
			text = "Shortcuts affect only this owned Harbor workspace. prefix is Ctrl+B. Native Herdr settings remain prefix+s. Direct Ctrl keys intercept input before the CLI and may conflict with terminal encodings or Emacs bindings. Only unoccupied supported chords are accepted."
		}
		rows := strings.Split(ansi.Wrap(cleanUI(text), w, ""), "\n")
		start := min(m.scroll, max(0, len(rows)-max(1, m.height-3)))
		return "⚓ Settings details\n" + strings.Join(rows[start:min(len(rows), start+max(1, m.height-3))], "\n") + "\n↑↓ scroll · i details · esc back"
	}
	lines := []string{paint(fg("brand"), "⚓ Harbor · Settings")}
	labels := []string{"Harbor", "Settings", "Magpie", "Restore defaults", "Save", "Cancel"}
	for i, label := range labels {
		line := "  " + label
		if i < 3 {
			value := m.values.values()[i]
			if m.editing && i == m.cursor {
				value = m.input.View()
			}
			line = fmt.Sprintf("  %-10s %s", label, value)
		}
		if i == m.cursor {
			line = "›" + line[1:]
			line = paint(fg("title"), line)
		}
		lines = append(lines, ansi.Truncate(line, w, "…"))
	}
	if m.height >= 16 {
		lines = append(lines, "Scope: this owned Harbor workspace only.", "prefix = Ctrl+B. Herdr settings: prefix+s.", "Direct Ctrl may conflict with terminal / Emacs keys.")
	}
	notice := m.notice
	if m.busy {
		notice = "Saving and reloading owned workspace…"
	}
	if notice == "" {
		notice = "Workspace only. Ctrl keys may conflict with terminal/Emacs."
	}
	// Wrap recovery text into available cells; keep controls visible at45×12.
	available := max(1, m.height-len(lines)-1)
	wrapped := strings.Split(ansi.Wrap(cleanUI(notice), w, ""), "\n")
	if len(wrapped) > available {
		wrapped = wrapped[:available]
	}
	lines = append(lines, wrapped...)
	hint := "tab move · enter · i details · esc back"
	if m.editing {
		hint = "enter accept field · esc cancel edit"
	}
	lines = append(lines, paintKeys(ansi.Truncate(hint, w, "…")))
	return strings.Join(lines, "\n")
}
func runSettingsApp() error {
	_, err := tea.NewProgram(newSettingsModel(), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
