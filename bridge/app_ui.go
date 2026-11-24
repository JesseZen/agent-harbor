package main

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"strings"
)

// uiAction is the single source for a control's label, key, availability and
// help. IDs belong to the page; object IDs must remain stable across refresh.
type uiAction struct {
	ID, Label, Key, Disabled string
	Primary                  bool
}
type uiRect struct {
	X, Y, W, H int
	ID         string
}

func (r uiRect) contains(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

type uiPage struct {
	Width, Height int
	MaxScroll     int // derived from content and the actual wrapped footer
	ListStart     int // first row of the rendered list window
	FieldColumn   int
	Lines         []string
	Hits          []uiRect
	Actions       []uiAction
	Hint, Focus   string
	chromeReady   bool
}

func uiCycle(current string, ids []string, reverse bool) string {
	if len(ids) == 0 {
		return ""
	}
	for i, id := range ids {
		if id == current {
			step := 1
			if reverse {
				step = len(ids) - 1
			}
			return ids[(i+step)%len(ids)]
		}
	}
	if reverse {
		return ids[len(ids)-1]
	}
	return ids[0]
}

func newUIPage(w, h int) uiPage      { return uiPage{Width: max(1, w), Height: max(1, h)} }
func (p *uiPage) title(title string) { p.line(paint(fg("brand"), "⚓ Harbor › "+cleanUI(title))) }
func (p *uiPage) line(s string) {
	p.Lines = append(p.Lines, ansi.Truncate(s, max(1, p.Width-2), "…"))
}
func (p *uiPage) hit(id string, x, y, w, h int) {
	if id != "" && w > 0 && h > 0 && y >= 0 && y+h <= p.Height {
		p.Hits = append(p.Hits, uiRect{x, y, w, h, id})
	}
}
func (p uiPage) target(x, y int) string {
	for _, r := range p.Hits {
		if r.contains(x, y) {
			return r.ID
		}
	}
	return ""
}

// Scroll only document content; keep every footer action in the viewport.
func (p uiPage) withFooter(footer uiPage, height, scroll int) uiPage {
	return p.dockFooter(footer.shortcutBar(), height, scroll)
}
func (p uiPage) dockFooter(footer uiPage, height, scroll int) uiPage {
	if len(footer.Lines) >= height-1 {
		small := newUIPage(p.Width, height)
		small.line("Terminal too small; enlarge this pane.")
		for len(small.Lines) < height-1 {
			small.line("")
		}
		small.line(paintKeys("Esc back · Ctrl+C cancel"))
		return small
	}
	available := max(1, height-len(footer.Lines))
	start := min(max(0, scroll), max(0, len(p.Lines)-available))
	out := newUIPage(p.Width, height)
	out.MaxScroll = max(0, len(p.Lines)-available)
	out.ListStart = p.ListStart
	out.Actions = append(append([]uiAction{}, p.Actions...), footer.Actions...)
	out.Lines = append(out.Lines, p.Lines[start:min(len(p.Lines), start+available)]...)
	for len(out.Lines) < available {
		out.line("")
	}
	for _, r := range p.Hits {
		r.Y -= start
		if r.Y >= 0 && r.Y+r.H <= available {
			out.Hits = append(out.Hits, r)
		}
	}
	out.scrollRail(0, available, len(p.Lines), start)
	offset := len(out.Lines)
	out.Lines = append(out.Lines, footer.Lines...)
	for _, r := range footer.Hits {
		r.Y += offset
		if r.Y+r.H <= height {
			out.Hits = append(out.Hits, r)
		}
	}
	return out
}
func (p uiPage) view() string {
	lines := append([]string(nil), p.Lines...)
	for len(lines) < p.Height {
		lines = append(lines, "")
	}
	if len(lines) > p.Height {
		lines = lines[:p.Height]
	}
	for i, s := range lines {
		if !p.chromeReady {
			s = chromeText(s)
		}
		s = " " + s
		s += strings.Repeat(" ", max(0, p.Width-ansi.StringWidth(s)))
		if !colorOff() {
			// Nested controls reset SGR backgrounds. Restore the page surface after
			// those resets so trailing cells never expose the host terminal theme.
			profile := lipgloss.ColorProfile()
			bg := "\x1b[" + profile.Convert(termenv.RGBColor(harborPalette["bg"])).Sequence(true) + "m"
			foreground := "\x1b[" + profile.Convert(termenv.RGBColor(harborPalette["text"])).Sequence(false) + "m"
			base := bg + foreground
			s = base + strings.NewReplacer("\x1b[0m", "\x1b[0m"+base, "\x1b[m", "\x1b[m"+base, "\x1b[49m", bg, "\x1b[39m", foreground).Replace(s) + "\x1b[0m"
		}
		lines[i] = s
	}

	return strings.Join(lines, "\n")
}

// Actions are rendered once, as shortcuts in the bottom bar.
func (p *uiPage) shortcut(a uiAction, focused bool) {
	p.Actions = append(p.Actions, a)
	if focused {
		p.Focus = a.ID
	}
}

func (p uiPage) shortcutBar() uiPage {
	bar := newUIPage(p.Width, p.Height)
	bar.Actions = p.Actions
	line := ""
	lastWholeHint := 0
	flush := func() {
		if line != "" {
			bar.line(line)
			line = ""
		}
	}
	appendHint := func(key, label, id string, disabled, focused bool) {
		key = keyLabel(key)
		label = cleanUI(label)
		if disabled {
			label += " (unavailable)"
		}
		role := "text"
		if disabled {
			role = "muted"
		}
		text := paint(fg("muted"), label)
		if key != "" {
			text = paint(fg(role).Bold(true), key) + " " + text
		}
		if line != "" && ansi.StringWidth(line)+2+ansi.StringWidth(text) > p.Width-2 {
			flush()
		}
		if line != "" {
			line += "  "
		}
		parts := strings.Split(ansi.Wrap(text, max(1, p.Width-2), ""), "\n")
		for i, part := range parts {
			if i > 0 {
				flush()
			}
			x, y := 1+ansi.StringWidth(line), len(bar.Lines)
			line += part
			if y == 1 && ansi.StringWidth(line) <= p.Width-6 {
				lastWholeHint = ansi.StringWidth(line)
			}
			if id != "" && !disabled {
				bar.hit(id, x, y, ansi.StringWidth(part), 1)
			}
		}
	}
	for _, a := range p.Actions {
		if a.Key != "" {
			appendHint(a.Key, a.Label, a.ID, a.Disabled != "", p.Focus == a.ID)
		}
	}
	for _, hint := range strings.Split(p.Hint, " · ") {
		if hint == "" {
			continue
		}
		key, label := splitHint(hint)
		appendHint(key, label, "", false, false)
	}
	flush()
	if len(bar.Lines) > 2 {
		bar.Lines = bar.Lines[:2]
		width := max(1, bar.Width-2)
		marker := strings.Repeat(".", min(3, width))
		line := ""
		if lastWholeHint > 0 {
			line = ansi.Truncate(bar.Lines[1], lastWholeHint, "")
			marker = " " + marker
		}
		bar.Lines[1] = line + paint(fg("text"), marker)
		end := 1 + ansi.StringWidth(line)
		hits := bar.Hits[:0]
		for _, hit := range bar.Hits {
			if hit.Y == 0 || (hit.Y == 1 && hit.X+hit.W <= end) {
				hits = append(hits, hit)
			}
		}
		bar.Hits = hits
	}
	return bar
}

const (
	scrollHint = "↑↓/jk PgUp/PgDn scroll"
	tabHint    = "←→/hl tabs"
)

// Hints and actions share one layout and the same bright-key/dim-description pair.
func splitHint(hint string) (string, string) {
	words := strings.Fields(symbolHints(hint))
	at := 0
	for at < len(words) {
		word := words[at]
		if word != "/" && word != "or" && word != "⇥" && word != "⇧⇥" && word != "↵" && word != "⎋" && !strings.ContainsAny(word, "↑↓←→^") && !strings.HasPrefix(word, "Pg") && word != "Tab" && word != "Shift+Tab" && word != "Esc" && word != "Enter" {
			break
		}
		at++
	}
	if at == 0 {
		if len(words) > 0 && words[0] == "Type" {
			return "", strings.Join(words, " ")
		}
		key, label, _ := strings.Cut(strings.Join(words, " "), " ")
		return key, label
	}
	return strings.Join(words[:at], " "), strings.Join(words[at:], " ")
}

func (p uiPage) finish(scroll int) uiPage {
	footer := p.shortcutBar()
	p.Actions = nil
	return p.dockFooter(footer, p.Height, scroll)
}

func (p *uiPage) field(id, icon, label, value string, focused, editable bool) {
	mark := "  "
	if focused {
		mark = "› "
	}
	base := fg("text")
	if focused {
		base = base.Background(lipgloss.Color(harborPalette["selBg"]))
	}
	p.fieldLine(id, icon, label, value, mark, base, base.Foreground(lipgloss.Color(harborPalette["value"])), editable)
}

type uiRowStyle struct {
	cursor, selected, hovered bool
}

func (r uiRowStyle) marker() string {
	if r.cursor {
		return "> "
	}
	return "  "
}

func (r uiRowStyle) style(role string) lipgloss.Style {
	if role == "text" {
		switch {
		case r.selected:
			role = "section"
		case r.cursor:
			role = "brand"
		}
	}
	style := fg(role)
	if r.hovered {
		style = style.Background(lipgloss.Color(harborPalette["selBg"]))
	}
	return style
}

func (r uiRowStyle) line(text, role string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return paint(r.style(role), text+strings.Repeat(" ", max(0, width-ansi.StringWidth(text))))
}

func (m harborModel) rowStyle(id string, cursor, selected bool) uiRowStyle {
	return uiRowStyle{cursor: cursor, selected: selected, hovered: id != "" && id == m.rowHover}
}

func (p *uiPage) listRow(id, icon, label, value string, row uiRowStyle) {
	p.fieldLine(id, icon, label, value, row.marker(), row.style("text"), row.style("value"), false)
}

func (p *uiPage) fieldLine(id, icon, label, value, mark string, labelStyle, valueStyle lipgloss.Style, editable bool) {
	y := len(p.Lines)
	prefix := mark + icon + strings.Repeat(" ", max(1, 4-ansi.StringWidth(icon)))
	col := min(24, max(20, p.Width/3))
	col = max(col, p.FieldColumn)
	key := prefix + label
	key += strings.Repeat(" ", max(2, col-ansi.StringWidth(key)))
	suffix := ""
	if editable {
		suffix = " ›"
	}
	available := max(1, p.Width-2-ansi.StringWidth(key)-ansi.StringWidth(suffix))
	value = ansi.Truncate(value, available, "…")
	value += strings.Repeat(" ", max(0, available-ansi.StringWidth(value)))
	p.line(paint(labelStyle, key) + paint(valueStyle, value+suffix))
	p.hit(id, 1, y, p.Width-2, 1)
}

// Search and edit fields share the native Bubbles cursor and placeholder renderer.
func (p *uiPage) search(value, placeholder string, focused bool, inputs ...textinput.Model) {
	field := textinput.New()
	field.SetValue(value)
	field.CursorEnd()
	if len(inputs) > 0 && inputs[0].Value() == value {
		field = inputs[0]
	}
	y := len(p.Lines)
	for _, line := range strings.Split(boxedInput(field, p.Width, placeholder, "⌕ ", focused), "\n") {
		p.line(line)
	}
	p.hit("search", 1, y, max(1, p.Width-2), 3)
}

var mainTabs = []uiAction{
	{ID: "sessions", Label: "🚢 Sessions"},
	{ID: "extensions", Label: "🧩 Extensions"},
	{ID: "magpie", Label: "🐦 Connections"},
	{ID: "accounts", Label: "👤 Accounts"},
	{ID: "settings", Label: "⚙ Settings"},
}

func adjacentTab(current, key string) string {
	offset := 0
	switch key {
	case "left":
		offset = -1
	case "right":
		offset = 1
	default:
		return ""
	}
	for i, tab := range mainTabs {
		if tab.ID == current && i+offset >= 0 && i+offset < len(mainTabs) {
			return mainTabs[i+offset].ID
		}
	}
	return ""
}

func (p *uiPage) tabs(current, hover, focus string) {
	x := 1
	y := len(p.Lines)
	line := ""
	for _, a := range mainTabs {
		label := a.Label
		if p.Width < 76 {
			if current != a.ID && focus != a.ID && focus != "nav:"+a.ID {
				label = strings.Fields(a.Label)[0]
			}
		}
		text := " " + label + " "
		if current == a.ID && colorOff() {
			text = "[" + label + "]"
		}
		if focus == a.ID || focus == "nav:"+a.ID {
			text = "›" + label + "‹"
		}
		width := ansi.StringWidth(text)
		if x+width > p.Width-1 {
			p.line(line)
			line = ""
			x = 1
			y = len(p.Lines)
		}
		st := fg("key")
		if current == a.ID {
			st = fg("ink").Background(lipgloss.Color(harborPalette["section"]))
		} else if hover == a.ID {
			st = st.Underline(true)
		}
		line += paint(st, text)
		p.hit("nav:"+a.ID, x, y, width, 1)
		x += width
	}
	p.line(line)
}
func actionHints(actions []uiAction) string {
	out := []string{}
	for _, a := range actions {
		if a.Key != "" {
			s := a.Key + " " + a.Label
			if a.Disabled != "" {
				s += " (" + a.Disabled + ")"
			}
			out = append(out, s)
		}
	}
	return strings.Join(out, "  ")
}

// Register a group without adding a second visual control row.
func (p *uiPage) shortcuts(actions []uiAction, focused string) {
	for _, a := range actions {
		p.shortcut(a, focused == a.ID)
	}
}

// textEntry keeps optional guidance inside the field, where Bubbles replaces it
// with the user's value as soon as typing begins.
func textEntry(input textinput.Model, width int, placeholder string) string {
	return boxedInput(input, width, placeholder, "", input.Focused())
}
func boxedInput(input textinput.Model, width int, placeholder, prompt string, focused bool) string {
	fieldWidth := max(4, width-2)
	contentWidth := max(1, fieldWidth-4)
	input.Prompt = prompt
	input.Width = max(1, contentWidth-ansi.StringWidth(prompt)-1)
	input.Placeholder = placeholder
	input.PlaceholderStyle = fg("muted")
	input.TextStyle = fg("text")
	input.PromptStyle = fg("muted")
	if focused {
		input.Focus()
	} else {
		input.Blur()
	}
	value := ansi.Truncate(input.View(), contentWidth, "")
	value += strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(value)))
	role := "muted"
	if focused {
		role = "section"
	}
	border := func(s string) string { return paint(fg(role), s) }
	return border("╭"+strings.Repeat("─", fieldWidth-2)+"╮") + "\n" + border("│ ") + value + border(" │") + "\n" + border("╰"+strings.Repeat("─", fieldWidth-2)+"╯")
}

func keyLabel(key string) string {
	if strings.HasPrefix(strings.ToLower(key), "ctrl+") {
		return "^" + strings.ToUpper(key[5:])
	}

	if asciiUI() {
		return key
	}
	switch strings.ToLower(key) {
	case "esc", "escape":
		return "⎋"
	case "enter", "return":
		return "↵"
	case "tab":
		return "⇥"
	case "shift+tab":
		return "⇧⇥"
	}
	return key
}
func symbolHints(s string) string {
	if asciiUI() {
		return s
	}
	words := strings.Fields(controlKeyNotation(s))
	for i, word := range words {
		words[i] = keyLabel(word)
	}
	return strings.Join(words, " ")
}

// A proportional one-cell rail, following omp ScrollView's muted track/accent thumb.
// Only content taller than its viewport gets a rail; the footer never scrolls.
func (p *uiPage) scrollRail(top, height, total, offset int) {
	if height <= 0 || total <= height {
		return
	}
	size := max(1, height*height/total)
	offset = max(0, min(offset, total-height))
	start := (offset*(height-size) + (total-height)/2) / (total - height)
	for y := 0; y < height && top+y < len(p.Lines); y++ {
		line := ansi.Truncate(p.Lines[top+y], max(1, p.Width-2), "…")
		line += strings.Repeat(" ", max(0, p.Width-2-ansi.StringWidth(line)))
		glyph, role := "│", "border"
		if y >= start && y < start+size {
			glyph, role = "█", "brand"
		}
		p.Lines[top+y] = line + paint(fg(role), glyph)
	}
}
func (p *uiPage) emptyState(text string) { p.line(paint(fg("muted"), cleanUI(text))) }

// groupBand renders a full-width low-contrast section band (matching Accounts
// client headers) to visually separate groups of items without extra borders.
func (p *uiPage) groupBand(title, meta string) {
	width := max(1, p.Width-2)
	titleText := cleanUI(title)
	metaText := cleanUI(meta)
	if colorOff() {
		label := titleText
		if metaText != "" {
			label += " (" + metaText + ")"
		}
		prefix, fill := "── ", "─"
		if asciiUI() {
			prefix, fill = "-- ", "-"
		}
		text := prefix + label + " "
		rem := max(0, width-ansi.StringWidth(text))
		p.line(text + strings.Repeat(fill, rem))
		return
	}
	left := " " + titleText
	right := ansi.Truncate(metaText, max(0, width-ansi.StringWidth(left)-3), "…")
	padding := max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right)-1)
	band := lipgloss.Color(harborPalette["button"])
	p.line(paint(fg("text").Background(band).Bold(true), left) +
		paint(fg("value").Background(band), strings.Repeat(" ", padding)+right+" "))
}

// Footer hints remain clickable, but are not keyboard focus targets.
func (p uiPage) focusHits() []uiRect {
	actions := map[string]bool{}
	for _, a := range p.Actions {
		actions[a.ID] = true
	}
	hits := []uiRect{}
	for _, hit := range p.Hits {
		if !actions[hit.ID] {
			hits = append(hits, hit)
		}
	}
	return hits
}
