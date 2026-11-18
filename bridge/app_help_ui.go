package main

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type helpRow struct{ key, description string }
type helpSection struct {
	title string
	rows  []helpRow
}

func renderHelp(width int, sections []helpSection) string {
	lines := []string{}
	w := max(12, width-3)
	left := min(18, max(9, w/3))
	right := max(1, w-left-7)
	edge := func(s string) string { return paint(fg("border"), s) }
	row := func(key, description string) {
		keys := strings.Split(ansi.Wrap(symbolHints(key), left, ""), "\n")
		values := strings.Split(ansi.Wrap(description, right, ""), "\n")
		for i := 0; i < max(len(keys), len(values)); i++ {
			k, v := "", ""
			if i < len(keys) {
				k = keys[i]
			}
			if i < len(values) {
				v = values[i]
			}
			lines = append(lines, edge("│ ")+paint(fg("text").Bold(true), k+strings.Repeat(" ", max(0, left-ansi.StringWidth(k))))+edge(" │ ")+paint(fg("muted"), v+strings.Repeat(" ", max(0, right-ansi.StringWidth(v))))+edge(" │"))
		}
	}
	for _, section := range sections {
		if len(section.rows) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, paint(fg("section"), section.title), "")
		lines = append(lines, edge("┌"+strings.Repeat("─", left+2)+"┬"+strings.Repeat("─", right+2)+"┐"))
		row("Key", "Action")
		lines = append(lines, edge("├"+strings.Repeat("─", left+2)+"┼"+strings.Repeat("─", right+2)+"┤"))
		for _, entry := range section.rows {
			row(entry.key, entry.description)
		}
		lines = append(lines, edge("└"+strings.Repeat("─", left+2)+"┴"+strings.Repeat("─", right+2)+"┘"))
	}
	return strings.Join(lines, "\n")
}
func (m harborModel) contextHelp() []helpSection {
	navigation := helpSection{"Navigation", []helpRow{{"Tab / Shift+Tab", "Move between fields, list and navigation"}, {"↑↓", "Select a row or scroll text"}, {"Enter", "Apply current field or selected row; use shown action keys"}, {"Esc", "Clear search, leave input, then return"}, {"PgUp/PgDn", "Scroll long documents"}}}
	actions := []uiAction{}
	parent := m
	parent.screen = m.previous
	if p, ok := parent.additionalPage(); ok {
		actions = p.Actions
	}
	if m.previous == "confirm" || m.previous == "confirm-evidence" {
		actions = parent.reviewActions()
	}
	if len(actions) == 0 {
		actions = append(m.sessionActions(false), m.sessionActions(true)...)
		actions = append(actions, uiAction{Key: ",", Label: "Settings"}, uiAction{Key: "m", Label: "Connections"}, uiAction{Key: "a", Label: "Accounts"}, uiAction{Key: "p", Label: "Profiles"})
	}
	current := helpSection{title: "Page actions"}
	seen := map[string]bool{}
	for _, a := range actions {
		if a.Key == "" || seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		description := a.Label
		if a.Disabled != "" {
			description += " · " + a.Disabled
		}
		current.rows = append(current.rows, helpRow{a.Key, description})
	}
	sections := []helpSection{navigation, current, {"Input and terminal", []helpRow{{"/", "Search; ordinary characters belong to text input"}, {"^C", "Close Harbor"}, {"^Z", "Suspend on supported terminals; resume refreshes runtime"}}}}
	if strings.HasPrefix(m.previous, "new-") {
		sections = append(sections, helpSection{"Creation", []helpRow{{"^S", "Only Create saves and launches a session"}, {"Esc", "Field edits and connection Apply update the draft only"}}})
	}
	return sections
}
func settingsHelp() []helpSection {
	return []helpSection{
		{"Navigation", []helpRow{{"Tab / Shift+Tab", "Move focus"}, {"↑↓", "Choose a field"}, {"Enter", "Edit field; accept draft value"}, {"Esc", "Cancel edit or return without saving"}, {"/", "Search settings"}, {"PgUp/PgDn", "Scroll help and details"}}},
		{"Changes", []helpRow{{"^S", "Save all changes"}, {"^R", "Restore defaults in the draft"}, {"i", "Inspect the current message"}, {"?", "Open keyboard help"}}},
		{"Workspace", []helpRow{{"prefix", "Ctrl+B; shortcuts affect this workspace only"}, {"prefix+s", "Open native Herdr settings"}, {"^Z", "Suspend; resume retains the draft"}, {"^C", "Close Harbor"}}},
	}
}

// Structured evidence retains its existing label/value styles. Plain prose is
// secondary, while explicit label: value rows keep the same left/right hierarchy.
func documentLine(line string) string {
	if strings.Contains(line, "\x1b[") {
		return line
	}
	if label, value, ok := strings.Cut(line, ": "); ok && ansi.StringWidth(label) < 26 {
		return paint(fg("text"), label+": ") + paint(fg("muted"), value)
	}
	if label, value, ok := strings.Cut(line, "  "); ok && strings.TrimSpace(label) != "" && ansi.StringWidth(label) < 26 {
		return paint(fg("text"), label+"  ") + paint(fg("muted"), value)
	}
	if strings.HasPrefix(line, "Review connection · ") {
		return paint(fg("section"), line)
	}
	if strings.HasPrefix(line, "⚠ ") {
		return paint(fg("warn"), line)
	}
	return paint(fg("muted"), line)
}
