package detailpane

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestViewRendersBoxSectionsAndFooter(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	resourceview.TokenBorder = "#54D6E8"
	resourceview.TokenHeader = "#63E6E2"
	resourceview.TokenText = "#D8DEE9"
	resourceview.TokenRuleIdle = "#3B4261"
	resourceview.TokenAccent = "#7AA2F7"
	resourceview.TokenHealthy = "#8BD600"

	raw := Model{
		Title: "Target · Anthropic reserve",
		Summary: []Row{
			{Label: "id", Value: "target_anthropic"},
			{Label: "adapter", Value: "anthropic"},
		},
		Sections: []Section{
			{
				Title: "Identity",
				Rows: []Row{
					{Label: "name", Value: "Anthropic reserve"},
					{Label: "bridge", Value: "anthropic_messages"},
				},
			},
			{
				Title: "Binding",
				Rows: []Row{
					{Label: "credential_id", Value: "credential_anthropic"},
				},
			},
		},
		Width:  60,
		Height: 20,
	}.View()
	view := ansi.Strip(raw)

	for _, want := range []string{
		"┌─", "┐", "└", "│",
		"Target · Anthropic reserve",
		"▸ Identity", "▸ Binding",
		"name", "Anthropic reserve",
		"credential_id", "credential_anthropic",
		"esc close",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "name: Anthropic") {
		t.Fatalf("must not use raw key: value dump style:\n%s", view)
	}
	accent := extractColor(lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenAccent)).Render("x"))
	header := extractColor(lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenHeader)).Render("x"))
	if accent == "" || header == "" || !strings.Contains(raw, accent) || !strings.Contains(raw, header) {
		t.Fatalf("expected accent+header colors in styled view (accent=%q header=%q):\n%s", accent, header, raw)
	}
}

func extractColor(styled string) string {
	for _, part := range strings.Split(styled, "\x1b[") {
		if strings.HasPrefix(part, "38;2;") {
			if i := strings.IndexByte(part, 'm'); i > 0 {
				return part[:i]
			}
		}
	}
	return ""
}

func TestViewKeepsFooterWhenClamped(t *testing.T) {
	view := ansi.Strip(Model{
		Title: "Tiny",
		Sections: []Section{{
			Title: "Fields",
			Rows: []Row{
				{Label: "a", Value: "1"},
				{Label: "b", Value: "2"},
				{Label: "c", Value: "3"},
				{Label: "d", Value: "4"},
				{Label: "e", Value: "5"},
			},
		}},
		Width:  40,
		Height: 6,
	}.View())
	if !strings.Contains(view, "esc close") {
		t.Fatalf("footer missing when clamped:\n%s", view)
	}
	if !strings.Contains(view, "┌─") || !strings.Contains(view, "└") {
		t.Fatalf("box chrome missing:\n%s", view)
	}
}

func TestRowsFromKeysKeepsPlaceholders(t *testing.T) {
	rows := RowsFromKeys([]string{"a", "b", "c"}, map[string]string{"a": "1", "c": "3"})
	if len(rows) != 3 || rows[0].Value != "1" || rows[1].Value != "-" || rows[2].Value != "3" {
		t.Fatalf("rows=%#v", rows)
	}
}
