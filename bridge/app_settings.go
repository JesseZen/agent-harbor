package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type settingsSaved struct{ err error }

type settingsModel struct {
	details, help         bool
	scroll                int
	width, height, cursor int
	values, saved         shortcutPreferences
	input                 textinput.Model
	bindingsOpen          bool
	bindingCursor         int
	overviewScroll        int
	editing, busy, closed bool
	notice, bindingNotice string
	query                 string
	searching             bool
	hover, nav            string
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
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+z":
			return m, tea.Suspend
		case "tab", "shift+tab":
			return m, nil
		}
		if !m.searching && !m.editing {
			msg = navigationKey(key)
		}
	}
	n, cmd := m.update(msg)
	n.scroll = max(0, min(n.scroll, n.layout().MaxScroll))
	_, resized := msg.(tea.WindowSizeMsg)
	if !n.help && !n.details && !n.searching && !n.editing &&
		(resized || m.cursor != n.cursor || m.bindingCursor != n.bindingCursor ||
			m.bindingsOpen != n.bindingsOpen || m.editing || m.help || m.details || m.searching && !n.searching) {
		n.revealSelection()
	}
	return n, cmd
}

func (m *settingsModel) revealSelection() {
	id := "control:" + strconv.Itoa(m.cursor)
	if m.bindingsOpen {
		id = "binding:" + strconv.Itoa(m.bindingCursor)
	}
	page := m.shortcutContent()
	available := max(1, page.Height-len(page.shortcutBar().Lines))
	for _, hit := range page.Hits {
		if hit.ID == id {
			m.scroll = max(hit.Y+hit.H-available, min(m.scroll, hit.Y))
			return
		}
	}
}

func (m settingsModel) update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case settingsSaved:
		m.busy = false
		if v.err != nil {
			m.notice = v.err.Error()
		} else {
			m.notice = "Saved for this workspace and restart."
			m.saved = m.values
		}
	case tea.MouseMsg:
		return m.updateMouse(v)
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		key := v.String()
		if key == "ctrl+c" {
			m.closed = true
			return m, tea.Quit
		}
		if m.details || m.help {
			switch key {
			case "esc", "i", "?":
				m.details, m.help = false, false
				m.scroll = 0
			case "pgdown":
				m.scroll += max(1, m.height/2)
			case "pgup":
				m.scroll -= max(1, m.height/2)
			case "down":
				m.scroll++
			case "up":
				m.scroll--
			}
			return m, nil
		}
		if m.bindingsOpen {
			return m.updateBindings(v)
		}
		if m.searching {
			switch key {
			case "esc":
				if m.query != "" {
					m.query = ""
				} else {
					m.searching = false
				}
			case "enter", "up", "down":
				m.searching = false
				rows := m.filtered()
				m.cursor = -1
				if len(rows) > 0 {
					m.cursor = rows[0]
				}
			case "backspace":
				if r := []rune(m.query); len(r) > 0 {
					m.query = string(r[:len(r)-1])
				}
			default:
				if v.Type == tea.KeyRunes {
					m.query += string(v.Runes)
				}
			}
			m.scroll = 0
			return m, nil
		}
		switch key {
		case "?":
			m.help = true
			m.scroll = 0
		case "/":
			m.searching = true
			m.scroll = 0
		case "i":
			m.details = true
			m.scroll = 0
		case "esc", "q":
			m.closed = true
			return m, tea.Quit
		case "left", "right":
			if tab := adjacentTab("settings", key); tab != "" {
				n, cmd := m.navigate(tab)
				return n.(settingsModel), cmd
			}
		case "down", "up":
			delta := 1
			if key == "up" {
				delta = -1
			}
			m.cursor = m.nextControl(delta)
		case "ctrl+r":
			m.values = defaultShortcuts()
			m.notice = "Defaults restored in draft. Save to apply."
		case "ctrl+s":
			if err := validateShortcuts(m.values); err != nil {
				m.notice = err.Error()
				return m, nil
			}
			m.busy = true
			p, expected := m.values, m.saved
			return m, func() tea.Msg { return settingsSaved{saveShortcuts(p, expected)} }
		case "enter":
			if slices.Contains(m.filtered(), m.cursor) {
				m.bindingsOpen = true
				m.bindingCursor = 0
				m.overviewScroll, m.scroll = m.scroll, 0
				m.bindingNotice = ""
			}
		}
	}
	return m, nil
}

func (m settingsModel) updateMouse(v tea.MouseMsg) (settingsModel, tea.Cmd) {
	target := m.layout().target(v.X, v.Y)
	if v.Action == tea.MouseActionMotion {
		m.hover = target
		return m, nil
	}
	if v.Button == tea.MouseButtonWheelDown {
		m.scroll++
	}
	if v.Button == tea.MouseButtonWheelUp {
		m.scroll--
	}
	if v.Action != tea.MouseActionPress || v.Button != tea.MouseButtonLeft || m.busy {
		return m, nil
	}
	switch target {
	case "search":
		m.searching = true
		m.scroll = 0
	case "details":
		m.searching = false
		return m.update(uiKey("i"))
	case "back":
		return m.update(uiKey("esc"))
	case "accept":
		return m.update(uiKey("enter"))
	case "save":
		return m.update(tea.KeyMsg{Type: tea.KeyCtrlS})
	case "restore":
		return m.update(tea.KeyMsg{Type: tea.KeyCtrlR})
	case "remove":
		return m.update(tea.KeyMsg{Type: tea.KeyDelete})
	}
	kind, suffix, _ := strings.Cut(target, ":")
	switch kind {
	case "nav":
		n, cmd := m.navigate(suffix)
		return n.(settingsModel), cmd
	case "binding":
		index, _ := strconv.Atoi(suffix)
		if m.bindingCursor == index {
			return m.update(uiKey("enter"))
		}
		m.bindingCursor = index
		m.bindingNotice = ""
	case "control":
		index, _ := strconv.Atoi(suffix)
		if m.cursor == index && !m.searching {
			return m.update(uiKey("enter"))
		}
		m.cursor = index
		m.searching = false
	}
	return m, nil
}

func (m settingsModel) updateBindings(msg tea.KeyMsg) (settingsModel, tea.Cmd) {
	keys := m.values.bindings(m.cursor)
	if m.editing {
		switch msg.String() {
		case "esc":
			m.editing = false
			m.bindingNotice = ""
		case "enter":
			key := strings.ToLower(strings.TrimSpace(m.input.Value()))
			if err := validateShortcutKey(m.cursor, key); err != nil {
				m.bindingNotice = err.Error()
				return m, nil
			}
			for action := range shortcutCount {
				if action != m.cursor && slices.Contains(m.values.bindings(action), key) {
					m.bindingNotice = fmt.Sprintf("%s already uses %s", settingsLabels[action], key)
					return m, nil
				}
			}
			if m.bindingCursor == len(keys) {
				keys = append(keys, key)
			} else {
				keys[m.bindingCursor] = key
			}
			draft := m.values
			draft.setBindings(m.cursor, keys)
			if err := validateShortcuts(draft); err != nil {
				m.bindingNotice = err.Error()
				return m, nil
			}
			m.values = draft
			m.editing = false
			m.bindingNotice = ""
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.bindingsOpen = false
		m.scroll = m.overviewScroll
	case "up":
		m.bindingCursor = max(0, m.bindingCursor-1)
		m.bindingNotice = ""
	case "down":
		m.bindingCursor = min(len(keys), m.bindingCursor+1)
		m.bindingNotice = ""
	case "pgup":
		m.bindingCursor = max(0, m.bindingCursor-max(1, m.height-4))
	case "pgdown":
		m.bindingCursor = min(len(keys), m.bindingCursor+max(1, m.height-4))
	case "enter":
		value := ""
		if m.bindingCursor < len(keys) {
			value = keys[m.bindingCursor]
		}
		m.input.SetValue(value)
		m.input.CursorEnd()
		m.editing = true
		m.bindingNotice = ""
		m.scroll = 0
		return m, m.input.Focus()
	case "delete":
		if m.bindingCursor == len(keys) {
			return m, nil
		}
		if len(keys) == 1 && m.cursor != shortcutNew {
			m.bindingNotice = fmt.Sprintf("%s needs at least one shortcut", settingsLabels[m.cursor])
			return m, nil
		}
		m.values.setBindings(m.cursor, slices.Delete(keys, m.bindingCursor, m.bindingCursor+1))
		m.bindingNotice = ""
	}
	return m, nil
}

func (m settingsModel) navigate(nav string) (tea.Model, tea.Cmd) {
	if nav == "settings" {
		return m, nil
	}
	if m.values != m.saved {
		m.notice = "Save changes or cancel this draft before changing pages."
		return m, nil
	}
	m.nav = nav
	m.closed = true
	return m, tea.Quit
}

func (m settingsModel) filtered() []int {
	var rows []int
	for _, action := range []int{shortcutPrefix, shortcutHarbor, shortcutMagpie, shortcutNew, shortcutSettings} {
		text := settingsLabels[action] + " " + strings.Join(m.values.bindings(action), " ")
		if strings.Contains(strings.ToLower(text), strings.ToLower(m.query)) {
			rows = append(rows, action)
		}
	}
	return rows
}

func (m settingsModel) nextControl(delta int) int {
	rows := m.filtered()
	if len(rows) == 0 {
		return -1
	}
	at := slices.Index(rows, m.cursor)
	if at == -1 {
		return rows[0]
	}
	return rows[(at+delta+len(rows))%len(rows)]
}

const shortcutScope = "Shortcuts affect only this Harbor workspace. Direct keys take priority over the focused terminal. A prefix binding works with every configured Herdr prefix."

func (m settingsModel) layout() uiPage {
	if m.help {
		return secondaryDocument(m.width, m.height, "Settings help", renderHelp(m.width, settingsHelp()), []uiAction{{ID: "back", Key: "Esc", Label: "Back"}}, "back", m.scroll)
	}
	if m.details {
		text := m.notice
		if text == "" {
			text = shortcutScope
		}
		return secondaryDocument(m.width, m.height, "Settings details", text, []uiAction{{ID: "back", Key: "Esc", Label: "Back"}}, "back", m.scroll)
	}
	return m.shortcutContent().finish(m.scroll)
}

func (m settingsModel) shortcutContent() uiPage {
	if m.bindingsOpen {
		return m.bindingContent()
	}

	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor · Settings"))
	if p.Height >= 13 {
		p.tabs("settings", strings.TrimPrefix(m.hover, "nav:"), "")
	}
	p.search(m.query, "Search settings", m.searching)
	if p.Height >= 19 {
		p.line(paint(fg("section"), "Workspace shortcuts"))
	}
	rows := m.filtered()
	if len(rows) == 0 {
		p.line(paint(fg("muted"), "No matching settings"))
	}
	for _, action := range rows {
		value := strings.Join(m.values.bindings(action), " · ")
		if value == "" {
			value = "Unbound"
		}
		icon := [shortcutCount]string{"🚢", "⚙", "🐦", "⌨️", "➕"}[action]
		p.field("control:"+strconv.Itoa(action), icon, settingsLabels[action], value, m.cursor == action && !m.searching, true)
	}
	switch {
	case m.busy:
		p.line(paint(fg("value"), "Saving and reloading workspace…"))
	case m.notice != "":
		p.line(paint(fg("value"), cleanUI(m.notice)))
	case m.values != m.saved:
		p.line(paint(fg("muted"), "Unsaved changes"))
	}
	if m.searching {
		p.Hint = "Type search · Enter fields · Esc clear/back"
		return p
	}
	if m.busy {
		return p
	}
	if len(rows) > 0 {
		p.shortcut(uiAction{ID: "accept", Key: "Enter", Label: "Edit bindings"}, false)
	}
	p.shortcuts([]uiAction{
		{ID: "save", Key: "Ctrl+S", Label: "Save changes", Primary: true},
		{ID: "back", Key: "Esc", Label: "Back"},
		{ID: "restore", Key: "Ctrl+R", Label: "Restore defaults"},
		{ID: "details", Key: "i", Label: "Details"},
	}, "")
	p.Hint = "↑↓/jk fields · " + tabHint + " · / search · ? help"
	return p
}

func (m settingsModel) bindingContent() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Settings · "+settingsLabels[m.cursor]))
	keys := m.values.bindings(m.cursor)
	if m.editing {
		title, placeholder := "Edit shortcut", "e.g. prefix+a or ctrl+g"
		if m.bindingCursor == len(keys) {
			title = "Add shortcut"
		}
		if m.cursor == shortcutPrefix {
			placeholder = "e.g. ctrl+b or f12"
		}
		p.line(paint(fg("section"), title))
		for _, line := range strings.Split(textEntry(m.input, p.Width, placeholder), "\n") {
			p.line(line)
		}
		p.shortcuts([]uiAction{{ID: "accept", Key: "Enter", Label: "Apply to draft", Primary: true}, {ID: "back", Key: "Esc", Label: "Cancel edit"}}, "")
	} else {
		height := p.Height
		p.Height = max(height, len(keys)+2)
		for i := 0; i <= len(keys); i++ {
			label := "Add shortcut"
			if i < len(keys) {
				label = keys[i]
			}
			id := "binding:" + strconv.Itoa(i)
			p.listRow(id, "", label, "", uiRowStyle{cursor: m.bindingCursor == i, hovered: m.hover == id})
		}
		p.Height = height
		label := "Add"
		if m.bindingCursor < len(keys) {
			label = "Edit"
		}
		p.shortcuts([]uiAction{{ID: "accept", Key: "Enter", Label: label, Primary: true}, {ID: "back", Key: "Esc", Label: "Back"}}, "")
		if m.bindingCursor < len(keys) && (len(keys) > 1 || m.cursor == shortcutNew) {
			p.shortcut(uiAction{ID: "remove", Key: "Delete", Label: "Remove"}, false)
		}
		p.Hint = "↑↓/jk select"
	}
	if m.bindingNotice != "" {
		p.line(paint(fg("value"), cleanUI(m.bindingNotice)))
	}
	return p
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
