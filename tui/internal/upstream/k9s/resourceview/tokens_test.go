package resourceview

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestViewUsesApprovedDesignTokens(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	table := New("Targets", []Column{
		{Title: "ID", MinWidth: 4, Priority: 0},
		{Title: "HEALTH", MinWidth: 8, Priority: 1},
		{Title: "STATE", MinWidth: 8, Priority: 2},
		{Title: "SYNC", MinWidth: 8, Priority: 3},
	})
	table.SetScope("default")
	table.SetRows([]Row{
		{ID: "a", Cells: []string{"a", "healthy", "error", "degraded"}},
		{ID: "b", Cells: []string{"b", "healthy", "ready", "synced"}},
	})
	table.SetSize(80, 10)

	view := table.View()
	for _, token := range []struct {
		name string
		hex  string
		bg   bool
	}{
		{"border", TokenBorder, false},
		{"header", TokenHeader, false},
		{"headerBg", TokenHeaderBg, true},
		{"text", TokenText, false},
		{"healthy", TokenHealthy, false},
		{"warning", TokenWarning, false},
		{"error", TokenError, false},
		{"onAccent", TokenOnAccent, false},
		{"selected", TokenSelected, true},
		{"accent", TokenAccent, true},
		{"ruleIdle", TokenRuleIdle, false},
		{"ruleSelected", TokenRuleSelected, false},
	} {
		if !strings.Contains(view, trueColorSignature(token.hex, token.bg)) {
			t.Fatalf("view missing %s token color %s:\n%s", token.name, token.hex, view)
		}
	}
	if strings.Contains(view, "#bb9af7") {
		t.Fatalf("view must not contain deprecated purple token:\n%s", view)
	}
}

var trueColorSeq = regexp.MustCompile(`(?:38|48);2;\d+;\d+;\d+`)

func trueColorSignature(hex string, background bool) string {
	// Derive the sequence lipgloss/termenv actually emits (some hex values are
	// quantized by one channel when converted through colorful).
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	style := lipgloss.NewStyle()
	if background {
		style = style.Background(lipgloss.Color(hex))
	} else {
		style = style.Foreground(lipgloss.Color(hex))
	}
	rendered := style.Render("x")
	match := trueColorSeq.FindString(rendered)
	if match == "" {
		return ""
	}
	if background && !strings.HasPrefix(match, "48;") {
		return ""
	}
	if !background && !strings.HasPrefix(match, "38;") {
		return ""
	}
	return match
}
