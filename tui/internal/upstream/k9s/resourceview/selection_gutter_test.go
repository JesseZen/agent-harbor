package resourceview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestSelectedRowIsContinuousWithGlyphRules(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	TokenSelected = "#2E4A5E"
	TokenRuleIdle = "#3B4261"
	TokenRuleSelected = "#7AA2F7"

	table := New("Credentials", []Column{
		{Title: "ID", MinWidth: 8, Priority: 0},
		{Title: "NAME", MinWidth: 6, Priority: 1},
		{Title: "SECRET", MinWidth: 8, Priority: 2},
	})
	table.SetScope("all")
	table.SetRows([]Row{
		{ID: "a", Cells: []string{"credential_openai", "OpenAI", "Configured"}},
		{ID: "b", Cells: []string{"credential_anthropic", "Anthropic", "Configured"}},
	})
	table.SetSize(80, 10)

	view := table.View()
	selectedBG := trueColorSignature(TokenSelected, true)
	idleRule := trueColorSignature(TokenRuleIdle, false)
	selRule := trueColorSignature(TokenRuleSelected, false)
	if selectedBG == "" || !strings.Contains(view, selectedBG) {
		t.Fatalf("missing selected band %q:\n%s", selectedBG, view)
	}
	if !strings.Contains(view, "┊") {
		t.Fatalf("missing idle glyph rule ┊:\n%s", view)
	}
	if !strings.Contains(view, "│") {
		t.Fatalf("missing selected glyph rule │:\n%s", view)
	}
	if idleRule == "" || !strings.Contains(view, idleRule) {
		t.Fatalf("missing idle rule fg %q:\n%s", idleRule, view)
	}
	if selRule == "" || !strings.Contains(view, selRule) {
		t.Fatalf("missing selected rule fg %q:\n%s", selRule, view)
	}

	// Selected cells must not be separated by an unstyled single space gap
	// (the old Join(parts, " ") failure mode).
	if strings.Contains(view, selectedBG+"m \x1b[0m"+selectedBG) {
		t.Fatalf("selected row still has unstyled gap between cells:\n%s", view)
	}
}

func TestApplyThemeUpdatesTokens(t *testing.T) {
	prev := ThemeTokens{
		OnAccent: TokenOnAccent, Border: TokenBorder, Header: TokenHeader, Text: TokenText,
		Healthy: TokenHealthy, Warning: TokenWarning, Error: TokenError,
		Selected: TokenSelected, Accent: TokenAccent,
		RuleIdle: TokenRuleIdle, RuleSelected: TokenRuleSelected,
	}
	t.Cleanup(func() { ApplyTheme(prev) })

	ApplyTheme(ThemeTokens{
		Selected:     "#112233",
		Header:       "#445566",
		RuleIdle:     "#778899",
		RuleSelected: "#AABBCC",
		Accent:       "#DDEEFF",
	})
	if TokenSelected != "#112233" || TokenHeader != "#445566" {
		t.Fatalf("ApplyTheme did not stick: selected=%s header=%s", TokenSelected, TokenHeader)
	}
	if TokenRuleIdle != "#778899" || TokenRuleSelected != "#AABBCC" {
		t.Fatalf("rule tokens: idle=%s sel=%s", TokenRuleIdle, TokenRuleSelected)
	}
}
