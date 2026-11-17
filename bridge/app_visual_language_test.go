package main

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSearchBoxSharesPlaceholderAndKeyboardCursor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	field := textinput.New()
	field.SetValue("abc")
	field.CursorStart()
	field.Focus()
	p := newUIPage(40, 12)
	p.search(field.Value(), "Search clients…", true, field)
	if !strings.Contains(ansi.Strip(strings.Join(p.Lines, "\n")), "⌕ abc") {
		t.Fatal(p.Lines)
	}
	if p.target(3, 1) != "search" {
		t.Fatal("search is not clickable")
	}
	field.SetValue("")
	p = newUIPage(40, 12)
	p.search("", "Search clients…", true, field)
	if !strings.Contains(ansi.Strip(strings.Join(p.Lines, "\n")), "Search clients…") {
		t.Fatal("placeholder missing")
	}
}
func TestDocumentRailTracksScrollAndAvoidsFooter(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	p := newUIPage(40, 12)
	for i := 0; i < 60; i++ {
		p.line("Document row")
	}
	p.shortcut(uiAction{ID: "back", Key: "esc", Label: "Back"}, false)
	top, bottom := p.finish(0), p.finish(1000)
	if !strings.Contains(ansi.Strip(top.Lines[0]), "█") || !strings.Contains(ansi.Strip(bottom.Lines[10]), "█") {
		t.Fatal("thumb did not reach both ends")
	}
	if strings.Contains(ansi.Strip(bottom.Lines[11]), "█") || !strings.Contains(ansi.Strip(bottom.Lines[11]), "⎋ Back") {
		t.Fatal("rail covered footer")
	}
	short := newUIPage(40, 12)
	short.line("Short document")
	if strings.Contains(short.finish(0).view(), "█") {
		t.Fatal("short document has a rail")
	}
}
func TestAccountsOverviewHasOneEmptyStateAndNoDecorativeProfileIcon(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "accounts"
	m.accountProfile = &profileBinding{ProfileID: "p"}
	m.accountSnapshot = &profileAccountResult{State: "signed-out"}
	content := ansi.Strip(strings.Join(m.accountContent().Lines, "\n"))
	if strings.Count(content, "No saved logins") != 1 || strings.Count(content, "🗂") != 1 {
		t.Fatal(content)
	}
	for _, old := range []string{"Switch between replies", "Status details available", "adds a login"} {
		if strings.Contains(content, old) {
			t.Fatal(content)
		}
	}
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if n.(harborModel).screen != "accounts-detail" {
		t.Fatal("details no longer reachable")
	}
}
