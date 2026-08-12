// Package detailpane renders themed, read-only inspect overlays for resource pages.
package detailpane

import (
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Row is one label/value pair inside a section or summary strip.
type Row struct {
	Label string
	Value string
}

// Section groups related rows under a heading.
type Section struct {
	Title string
	Rows  []Row
}

// Model is the input for a bordered detail pane.
type Model struct {
	Title    string
	Summary  []Row
	Sections []Section
	Hints    []string // default: esc close
	Width    int
	Height   int
	Scroll   int
}

// RowsFromKeys builds rows for every key; missing/empty values render as "-".
// Labels stay as field ids here; View localizes them via FieldLabel.
func RowsFromKeys(keys []string, values map[string]string) []Row {
	rows := make([]Row, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if value == "" {
			value = "-"
		}
		rows = append(rows, Row{Label: key, Value: value})
	}
	return rows
}

func localizeRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = Row{Label: FieldLabel(row.Label), Value: row.Value}
	}
	return out
}

// View renders the pane and discards the clamped scroll.
func (m Model) View() string {
	out, _ := m.ViewScroll()
	return out
}

// ViewScroll renders the pane and returns the clamped scroll offset.
func (m Model) ViewScroll() (string, int) {
	width := max(20, m.Width)
	height := max(4, m.Height)
	inner := max(1, width-2)

	border := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenBorder))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(resourceview.TokenHeader))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(resourceview.TokenAccent))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenBorder))
	leaderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenRuleIdle))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenText))
	summaryLabel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(resourceview.TokenAccent))
	summaryValue := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(resourceview.TokenHeader))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenAccent))

	hints := localizeHints(m.Hints)

	body := make([]string, 0, 32)
	if len(m.Summary) > 0 {
		body = append(body, renderSummary(localizeRows(m.Summary), inner, summaryLabel, summaryValue))
		body = append(body, "")
	}
	for _, section := range m.Sections {
		if section.Title != "" {
			marker := sectionStyle.Render("▸ ")
			heading := sectionStyle.Render(truncate(localizeHeading(section.Title), max(1, inner-2)))
			body = append(body, marker+heading)
		}
		rows := localizeRows(section.Rows)
		labelWidth := maxLabelWidth(rows, 28)
		for _, row := range rows {
			body = append(body, renderRow(row, inner, labelWidth, labelStyle, leaderStyle, valueStyle))
		}
		body = append(body, "")
	}
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}

	footerPlain := strings.Join(hints, "  ")
	// Chrome: top + separator + bottom hint = 3 lines. Footer is last row for mouse hits.
	bodyBudget := max(0, height-3)
	scroll := m.Scroll
	if scroll < 0 {
		scroll = 0
	}
	maxScroll := max(0, len(body)-bodyBudget)
	if scroll > maxScroll {
		scroll = maxScroll
	}
	visible := []string{}
	if bodyBudget > 0 && len(body) > 0 {
		end := min(len(body), scroll+bodyBudget)
		visible = body[scroll:end]
	}
	for len(visible) < bodyBudget {
		visible = append(visible, "")
	}

	title := strings.TrimSpace(m.Title)
	if title == "" {
		title = "Details"
	}
	lines := make([]string, 0, height)
	lines = append(lines, renderTop(title, width, border, titleStyle))
	for _, line := range visible {
		lines = append(lines, border.Render("│")+padInner(line, inner)+border.Render("│"))
	}
	lines = append(lines, border.Render("├"+strings.Repeat("─", inner)+"┤"))
	lines = append(lines, renderBottomHint(footerPlain, width, border, hintStyle))
	return strings.Join(lines, "\n"), scroll
}

func renderTop(title string, width int, border, titleStyle lipgloss.Style) string {
	prefix := "┌─ "
	suffix := "┐"
	gap := " "
	budget := max(0, width-ansi.StringWidth(prefix)-ansi.StringWidth(gap)-ansi.StringWidth(suffix))
	renderedTitle := titleStyle.Render(truncate(title, budget))
	fill := max(0, width-ansi.StringWidth(prefix)-ansi.StringWidth(ansi.Strip(renderedTitle))-ansi.StringWidth(gap)-ansi.StringWidth(suffix))
	return border.Render(prefix) + renderedTitle + border.Render(gap+strings.Repeat("─", fill)+suffix)
}

func renderBottomHint(hint string, width int, border, hintStyle lipgloss.Style) string {
	prefix := "└─ "
	suffix := "┘"
	gap := " "
	budget := max(0, width-ansi.StringWidth(prefix)-ansi.StringWidth(gap)-ansi.StringWidth(suffix))
	rendered := hintStyle.Render(truncate(hint, budget))
	fill := max(0, width-ansi.StringWidth(prefix)-ansi.StringWidth(ansi.Strip(rendered))-ansi.StringWidth(gap)-ansi.StringWidth(suffix))
	return border.Render(prefix) + rendered + border.Render(gap+strings.Repeat("─", fill)+suffix)
}

func renderSummary(rows []Row, inner int, labelStyle, valueStyle lipgloss.Style) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, labelStyle.Render(row.Label)+" "+valueStyle.Render(row.Value))
	}
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenRuleIdle)).Render("  ·  ")
	return truncate(strings.Join(parts, sep), inner)
}

func renderRow(row Row, inner, labelWidth int, labelStyle, leaderStyle, valueStyle lipgloss.Style) string {
	label := row.Label
	if ansi.StringWidth(label) > labelWidth {
		label = truncate(label, labelWidth)
	}
	pad := max(1, labelWidth-ansi.StringWidth(label)+2)
	leader := " " + strings.Repeat("·", pad) + " "
	prefix := labelStyle.Render(label) + leaderStyle.Render(leader)
	remain := max(1, inner-ansi.StringWidth(ansi.Strip(prefix)))
	value := truncate(row.Value, remain)
	return prefix + styledValue(value, valueStyle)
}

func styledValue(value string, base lipgloss.Style) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "-", "":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenRuleIdle)).Render(value)
	case "true", "configured", "success", "healthy", "active", "running", "allowed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenHealthy)).Render(value)
	case "false", "error", "failed", "invalid", "stale", "blocked", "denied":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenError)).Render(value)
	case "warning", "degraded", "unknown", "detecting", "launching":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenWarning)).Render(value)
	default:
		return base.Render(value)
	}
}

func maxLabelWidth(rows []Row, capWidth int) int {
	width := 0
	for _, row := range rows {
		w := ansi.StringWidth(row.Label)
		if w > width {
			width = w
		}
	}
	if width > capWidth {
		return capWidth
	}
	if width < 6 {
		return 6
	}
	return width
}

func padInner(line string, inner int) string {
	plain := ansi.Strip(line)
	pad := max(0, inner-ansi.StringWidth(plain))
	if ansi.StringWidth(plain) > inner {
		return truncate(line, inner) // already styled; best-effort
	}
	return line + strings.Repeat(" ", pad)
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(value, width, "…")
}
