package main

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"strings"
	"testing"
)

func TestFooterUsesOneFlowAndBrightKeysDimLabels(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	p := newUIPage(120, 24)
	p.shortcut(uiAction{ID: "save", Key: "Ctrl+S", Label: "Save changes"}, true)
	p.Hint = "Tab focus · Enter edit · ? help"
	bar := p.shortcutBar()
	plain := ansi.Strip(strings.Join(bar.Lines, "\n"))
	if strings.ContainsAny(plain, "›‹") || len(bar.Lines) != 1 || strings.Contains(plain, " · ") || !strings.Contains(plain, "^S Save changes") {
		t.Fatal(plain)
	}
	if keyLabel("ctrl+r") != "^R" {
		t.Fatal("control notation")
	}
	if bar.target(1, 0) != "save" {
		t.Fatal("shortcut hit moved")
	}
}
func TestHelpTablesWrapDescriptionsAndRetainScrollRail(t *testing.T) {
	for _, size := range [][2]int{{104, 32}, {58, 21}, {38, 17}} {
		body := renderHelp(size[0], settingsHelp())
		for _, line := range strings.Split(body, "\n") {
			if ansi.StringWidth(line) > size[0]-2 {
				t.Fatalf("help overflow %v: %s", size, line)
			}
		}
		p := secondaryDocument(size[0], size[1], "Settings help", body, []uiAction{{ID: "back", Key: "esc", Label: "Back"}}, "back", 0)
		if p.MaxScroll == 0 || !strings.Contains(ansi.Strip(p.view()), "█") {
			t.Fatal("missing help scroll rail", size)
		}
		if !strings.Contains(ansi.Strip(body), "^S") || !strings.Contains(ansi.Strip(body), "Key") {
			t.Fatal("missing help columns")
		}
	}
}

func TestNarrowFooterCapsHeightAndPreservesActionMetadata(t *testing.T) {
	p := newUIPage(25, 16)
	p.shortcuts([]uiAction{
		{ID: "first", Key: "a", Label: "First action"},
		{ID: "second", Key: "b", Label: "Second action to clip"},
		{ID: "third", Key: "c", Label: "Third action"},
	}, "")
	bar := p.shortcutBar()
	if len(bar.Lines) != 2 || !strings.HasSuffix(ansi.Strip(bar.Lines[1]), "...") {
		t.Fatal(bar.Lines)
	}
	if len(bar.Actions) != 3 || bar.target(1, 0) != "first" || len(bar.Hits) != 1 {
		t.Fatalf("actions = %v, hits = %v", bar.Actions, bar.Hits)
	}
}

func TestFooterColourSeparatesKeyFromDescription(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	old := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	p := newUIPage(100, 24)
	p.shortcut(uiAction{ID: "save", Key: "Ctrl+S", Label: "Save"}, false)
	p.Hint = "? help"
	output := strings.Join(p.shortcutBar().Lines, "\n")
	for _, pair := range [][2]string{{"^S", "Save"}, {"?", "help"}} {
		if !strings.Contains(output, paint(fg("text").Bold(true), pair[0])) || !strings.Contains(output, paint(fg("muted"), pair[1])) {
			t.Fatal("key and description lost their separate styles", output)
		}
	}
}

func TestTabSkipsFooterActionsOnExistingPages(t *testing.T) {
	for _, screen := range []string{"sessions", "accounts", "profile-list", "magpie"} {
		m := newHarborModel(&fakeApplicationService{})
		m.screen = screen
		for i := 0; i < 20; i++ {
			m = appKey(m, "tab").(harborModel)
			for _, prefix := range []string{"action:", "account:", "profile:", "conn:"} {
				if strings.HasPrefix(m.uiFocus, prefix) {
					t.Fatal(screen, m.uiFocus)
				}
			}
		}
	}
	settings := newSettingsModel()
	for i := 0; i < 20; i++ {
		n, _ := settings.Update(uiKey("tab"))
		settings = n.(settingsModel)
		if settings.cursor > 2 {
			t.Fatal("Settings footer acquired focus")
		}
	}
}
