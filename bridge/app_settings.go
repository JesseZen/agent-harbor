package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type settingsSaved struct{ err error }

const (
	settingProfiles = shortcutCount
	settingTotal    = shortcutCount + 1
)

type settingsSection struct {
	id    string
	title string
	items []int
}

var settingsSections = []settingsSection{
	{
		id:    "shortcuts",
		title: "Workspace shortcuts",
		items: []int{shortcutPrefix, shortcutHarbor, shortcutMagpie, shortcutNew, shortcutSettings},
	},
	{
		id:    "environment",
		title: "Environment & Profiles",
		items: []int{settingProfiles},
	},
}

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
	unsavedWarn           bool
	notice, bindingNotice string
	query                 string
	searching             bool
	hover, nav            string
	profileSummary        string
}

func newSettingsModel() settingsModel {
	m := settingsModel{width: 80, height: 24, values: defaultShortcuts(), input: textinput.New(), profileSummary: "Default"}
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

func (m settingsModel) hasUnsaved() bool {
	return m.values != m.saved
}

func (m settingsModel) controlModified(control int) bool {
	if control < 0 || control >= shortcutCount {
		return false
	}
	return !slices.Equal(m.values.bindings(control), m.saved.bindings(control))
}

func (m settingsModel) controlLabel(control int) string {
	if control == settingProfiles {
		return "Profiles & environments"
	}
	if control >= 0 && control < shortcutCount {
		return settingsLabels[control]
	}
	return ""
}

func (m settingsModel) controlValue(control int) string {
	if control == settingProfiles {
		if m.profileSummary != "" {
			return m.profileSummary
		}
		return "Default"
	}
	if control >= 0 && control < shortcutCount {
		value := strings.Join(m.values.bindings(control), " · ")
		if value == "" {
			return "Unbound"
		}
		return value
	}
	return ""
}

func (m settingsModel) controlDescription(control int) string {
	switch control {
	case shortcutPrefix:
		return "Herdr prefix key(s) used before prefix+<key> workspace actions."
	case shortcutHarbor:
		return "Open the Agent Harbor popup from any pane in this workspace."
	case shortcutMagpie:
		return "Open the Magpie gateway management popup for this workspace."
	case shortcutNew:
		return "Directly open Harbor's New session form from any pane."
	case shortcutSettings:
		return "Open Harbor workspace settings from any pane."
	case settingProfiles:
		return "Manage isolated Profiles, default Magpie instances, and Codex defaults."
	default:
		return shortcutScope
	}
}

func (m settingsModel) activeSectionID() string {
	for _, sec := range settingsSections {
		if slices.Contains(sec.items, m.cursor) {
			return sec.id
		}
	}
	if len(settingsSections) > 0 {
		return settingsSections[0].id
	}
	return ""
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
	if !n.hasUnsaved() {
		n.unsavedWarn = false
	}
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
	if !m.bindingsOpen {
		// Overview layout keeps the focused row visible via virtual scroll directly.
		return
	}
	id := "binding:" + strconv.Itoa(m.bindingCursor)
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
			m.unsavedWarn = false
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
		case "p":
			n, cmd := m.navigate("profiles")
			return n.(settingsModel), cmd
		case "esc", "q":
			if m.hasUnsaved() && !m.unsavedWarn {
				m.unsavedWarn = true
				m.notice = "Unsaved changes — press ^S to save, or Esc again to discard."
				return m, nil
			}
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
			m.unsavedWarn = false
		case "ctrl+r":
			m.values = defaultShortcuts()
			m.unsavedWarn = false
			m.notice = "Defaults restored in draft. Save to apply."
		case "ctrl+s":
			if err := validateShortcuts(m.values); err != nil {
				m.notice = err.Error()
				return m, nil
			}
			m.busy = true
			m.unsavedWarn = false
			p, expected := m.values, m.saved
			return m, func() tea.Msg { return settingsSaved{saveShortcuts(p, expected)} }
		case "enter":
			if slices.Contains(m.filtered(), m.cursor) {
				if m.cursor == settingProfiles {
					n, cmd := m.navigate("profiles")
					return n.(settingsModel), cmd
				}
				m.bindingsOpen = true
				m.bindingCursor = 0
				m.overviewScroll, m.scroll = m.scroll, 0
				m.bindingNotice = ""
				m.unsavedWarn = false
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
	case "section":
		filtered := m.filtered()
		for _, sec := range settingsSections {
			if sec.id == suffix {
				for _, item := range sec.items {
					if slices.Contains(filtered, item) {
						m.cursor = item
						m.searching = false
						m.unsavedWarn = false
						return m, nil
					}
				}
			}
		}
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
		m.unsavedWarn = false
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
	if m.hasUnsaved() {
		m.unsavedWarn = true
		m.notice = "Unsaved changes — Save changes (^S) or cancel this draft before changing pages."
		return m, nil
	}
	m.nav = nav
	m.closed = true
	return m, tea.Quit
}

func (m settingsModel) filtered() []int {
	var rows []int
	q := strings.ToLower(m.query)
	for _, sec := range settingsSections {
		for _, item := range sec.items {
			text := sec.title + " " + m.controlLabel(item) + " " + m.controlValue(item)
			if q == "" || strings.Contains(strings.ToLower(text), q) {
				rows = append(rows, item)
			}
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
	if m.bindingsOpen {
		return m.bindingContent().finish(m.scroll)
	}
	return m.overviewLayout()
}

func (m settingsModel) shortcutContent() uiPage {
	if m.bindingsOpen {
		return m.bindingContent()
	}
	return m.overviewLayout()
}

func (m settingsModel) overviewFooter() uiPage {
	f := newUIPage(m.width, m.height)
	if m.searching {
		f.Hint = "Type search · Enter fields · Esc clear/back"
		return f
	}
	if m.busy {
		return f
	}
	rows := m.filtered()
	if len(rows) > 0 {
		label := "Edit bindings"
		if m.cursor == settingProfiles {
			label = "Open Profiles"
		}
		f.shortcut(uiAction{ID: "accept", Key: "Enter", Label: label}, false)
	}
	f.shortcuts([]uiAction{
		{ID: "save", Key: "Ctrl+S", Label: "Save changes", Primary: true},
		{ID: "back", Key: "Esc", Label: "Back"},
		{ID: "restore", Key: "Ctrl+R", Label: "Restore defaults"},
		{ID: "details", Key: "i", Label: "Details"},
	}, "")
	f.Hint = "↑↓/jk fields · " + tabHint + " · / search · ? help"
	return f
}

// renderSettingsRow renders one option row using Harbor's shared cursor/hover
// interaction model plus omp-style active-section brightness and yellow unsaved
// indicators.
func (m settingsModel) renderSettingsRow(control, width int, activeSection bool) string {
	id := "control:" + strconv.Itoa(control)
	cursor := m.cursor == control && !m.searching
	hovered := m.hover == id
	modified := m.controlModified(control)
	marker := "  "
	if cursor {
		marker = "> "
	}
	labelRole := "muted"
	valueRole := "muted"
	if activeSection {
		labelRole = "text"
		valueRole = "value"
	}
	if cursor {
		labelRole = "brand"
	}
	if modified {
		// Unsaved modifications always show in yellow, matching Accounts'
		// persistent selection foreground rule even under cursor/hover.
		labelRole = "warn"
		valueRole = "warn"
	}
	styleFor := func(role string) lipgloss.Style {
		st := fg(role)
		if hovered {
			st = st.Background(lipgloss.Color(harborPalette["selBg"]))
		}
		return st
	}
	label := marker + cleanUI(m.controlLabel(control))
	val := cleanUI(m.controlValue(control))
	col := min(28, max(18, width/2))
	if col > width-8 {
		col = max(10, width/2)
	}
	labelText := ansi.Truncate(label, max(1, col-1), "…")
	pad := strings.Repeat(" ", max(1, col-ansi.StringWidth(labelText)))
	valAvail := max(1, width-ansi.StringWidth(labelText)-ansi.StringWidth(pad))
	valText := ansi.Truncate(val, valAvail, "…")
	trailing := strings.Repeat(" ", max(0, width-ansi.StringWidth(labelText)-ansi.StringWidth(pad)-ansi.StringWidth(valText)))
	return paint(styleFor(labelRole), labelText+pad) + paint(styleFor(valueRole), valText+trailing)
}

func (m settingsModel) overviewLayout() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor · Settings"))
	if p.Height >= 13 {
		p.tabs("settings", strings.TrimPrefix(m.hover, "nav:"), "")
	}
	p.search(m.query, "Search settings", m.searching)

	footer := m.overviewFooter()
	bar := footer.shortcutBar()

	// Bottom info zone: optional prominent red Unsaved changes warning above a
	// muted gray description of the focused option (just like omp settings).
	var bottomLines []string
	if m.busy {
		bottomLines = append(bottomLines, paint(fg("value"), "Saving and reloading workspace…"))
	} else if m.unsavedWarn && m.hasUnsaved() {
		msg := m.notice
		if msg == "" {
			msg = "Unsaved changes — press ^S to save or Esc again to discard."
		}
		bottomLines = append(bottomLines, paint(fg("bad").Bold(true), "⚠ "+cleanUI(msg)))
	} else if m.notice != "" {
		bottomLines = append(bottomLines, paint(fg("value"), cleanUI(m.notice)))
	}
	// Reserve gray description line when there is room for all core rows.
	if m.height >= 16 {
		desc := m.controlDescription(m.cursor)
		bottomLines = append(bottomLines, paint(fg("muted"), ansi.Truncate(cleanUI(desc), max(1, p.Width-2), "…")))
	}

	top := len(p.Lines)
	bodyH := max(1, m.height-top-len(bottomLines)-len(bar.Lines))

	filtered := m.filtered()
	activeSec := m.activeSectionID()
	split := p.Width >= 76
	leftW := 0
	dividerW := 0
	rightW := max(1, p.Width-2)
	if split {
		leftW = min(24, max(18, p.Width/4))
		dividerW = 3 // " │ "
		rightW = max(16, p.Width-2-leftW-dividerW)
	}

	// Build right-column virtual lines (underlined section headings + items).
	type rightLine struct {
		text     string
		hitID    string
		isCursor bool
	}
	var rLines []rightLine
	showSectionHeaders := m.height >= 16 || split
	for _, sec := range settingsSections {
		secItems := []int{}
		for _, item := range sec.items {
			if slices.Contains(filtered, item) {
				secItems = append(secItems, item)
			}
		}
		if len(secItems) == 0 {
			continue
		}
		isActive := sec.id == activeSec
		if showSectionHeaders {
			headRole := "muted"
			if isActive {
				headRole = "text"
			}
			headStyle := fg(headRole).Underline(true)
			rLines = append(rLines, rightLine{text: "  " + paint(headStyle, cleanUI(sec.title))})
		}
		for _, item := range secItems {
			rLines = append(rLines, rightLine{
				text:     m.renderSettingsRow(item, rightW, isActive),
				hitID:    "control:" + strconv.Itoa(item),
				isCursor: item == m.cursor && !m.searching,
			})
		}
	}
	if len(rLines) == 0 {
		rLines = append(rLines, rightLine{text: paint(fg("muted"), "No matching settings")})
	}

	// Keep the cursor row inside the visible viewport.
	scrollTop := max(0, min(m.scroll, max(0, len(rLines)-bodyH)))
	for idx, rl := range rLines {
		if rl.isCursor {
			if idx < scrollTop {
				scrollTop = idx
				if idx > 0 && rLines[idx-1].hitID == "" {
					scrollTop = idx - 1
				}
			} else if idx >= scrollTop+bodyH {
				scrollTop = idx - bodyH + 1
			}
			break
		}
	}
	scrollTop = max(0, min(scrollTop, max(0, len(rLines)-bodyH)))

	// Build left-column clickable section list (wide mode only).
	type leftLine struct {
		text  string
		hitID string
	}
	var lLines []leftLine
	if split {
		for _, sec := range settingsSections {
			hasMatch := false
			for _, item := range sec.items {
				if slices.Contains(filtered, item) {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				continue
			}
			id := "section:" + sec.id
			isActive := sec.id == activeSec
			st := fg("muted")
			if isActive {
				st = fg("brand").Bold(true)
			}
			if m.hover == id {
				st = st.Background(lipgloss.Color(harborPalette["selBg"]))
			}
			label := ansi.Truncate(cleanUI(sec.title), max(1, leftW-1), "…")
			label += strings.Repeat(" ", max(0, leftW-ansi.StringWidth(label)))
			lLines = append(lLines, leftLine{text: paint(st, label), hitID: id})
		}
	}

	divider := paint(fg("border"), " │ ")
	for i := 0; i < bodyH; i++ {
		y := len(p.Lines)
		var line string
		if split {
			if i < len(lLines) {
				line = lLines[i].text
				p.hit(lLines[i].hitID, 1, y, leftW, 1)
			} else {
				line = strings.Repeat(" ", leftW)
			}
			line += divider
		}
		rIdx := scrollTop + i
		if rIdx < len(rLines) {
			rText := rLines[rIdx].text
			line += rText
			if rLines[rIdx].hitID != "" {
				hitX := 1
				if split {
					hitX = 1 + leftW + dividerW
				}
				p.hit(rLines[rIdx].hitID, hitX, y, rightW, 1)
			}
		}
		p.line(line)
	}
	p.scrollRail(top, bodyH, len(rLines), scrollTop)
	for _, bl := range bottomLines {
		p.line(bl)
	}
	p.Actions = footer.Actions
	p.Hint = footer.Hint
	out := p.finish(0)
	out.MaxScroll = max(0, len(rLines)-bodyH)
	return out
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
		if m.screen == "profiles" {
			m.screen = "profile-list"
		}
		_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	}
	return err
}
