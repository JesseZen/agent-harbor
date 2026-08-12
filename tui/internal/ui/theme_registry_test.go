package ui_test

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/ui"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

func TestListThemesIncludesBuiltins(t *testing.T) {
	names := ui.ListThemes()
	need := map[string]bool{"tokyonight": false, "nord": false, "dracula": false, "catppuccin": false}
	for _, name := range names {
		if _, ok := need[name]; ok {
			need[name] = true
		}
	}
	for name, ok := range need {
		if !ok {
			t.Fatalf("missing theme %q in %v", name, names)
		}
	}
}

func TestInitThemeChangesAccent(t *testing.T) {
	ui.InitTheme("tokyonight")
	base := string(ui.ColorAccent)
	ui.InitTheme("nord")
	if string(ui.ColorAccent) == base {
		t.Fatalf("expected nord accent to differ from tokyonight (%q)", base)
	}
	ui.InitTheme("dark")
	if ui.GetCurrentThemeName() != "tokyonight" {
		t.Fatalf("dark alias = %q", ui.GetCurrentThemeName())
	}
}

func TestInitThemeSyncsResourceviewTokens(t *testing.T) {
	ui.InitTheme("nord")
	if resourceview.TokenAccent != string(ui.ColorAccent) {
		t.Fatalf("TokenAccent=%q want %q", resourceview.TokenAccent, ui.ColorAccent)
	}
	if resourceview.TokenHeaderBg != string(ui.ColorSurface) {
		t.Fatalf("TokenHeaderBg=%q want Surface %q", resourceview.TokenHeaderBg, ui.ColorSurface)
	}
	if resourceview.TokenSelected == string(ui.ColorSurface) {
		t.Fatalf("TokenSelected should be Accent-tinted, got plain Surface %q", resourceview.TokenSelected)
	}
	if resourceview.TokenRuleSelected != string(ui.ColorAccent) {
		t.Fatalf("TokenRuleSelected=%q want Accent %q", resourceview.TokenRuleSelected, ui.ColorAccent)
	}
	if resourceview.TokenHeader != string(ui.ColorCyan) {
		t.Fatalf("TokenHeader=%q want Cyan %q", resourceview.TokenHeader, ui.ColorCyan)
	}
	ui.InitTheme("tokyonight")
}
