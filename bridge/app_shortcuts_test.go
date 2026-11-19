package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestShortcutFooterStaysAtBottom(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{40, 10}, {45, 12}, {60, 20}, {80, 24}, {100, 30}, {160, 45}} {
		for _, screen := range []string{"sessions", "sessions-more", "accounts", "profile-list", "profile-default", "rename", "confirm", "new-form", "new-name", "new-cli", "new-connection", "magpie", "magpie-bind"} {
			m, _ := formFixture()
			m.width, m.height = size[0], size[1]
			m.screen = screen
			m.formActive = strings.HasPrefix(screen, "new-")
			m.action = "stop"
			m.notice = "A message must not replace the shortcuts."
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) != m.height || strings.TrimSpace(lines[len(lines)-1]) == "" {
				t.Fatalf("%s %v: footer is not on the last row", screen, size)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%s %v: row exceeds width", screen, size)
				}
			}
		}
	}
}

func TestShortcutsKeepTextInputAndDirectActionsSeparate(t *testing.T) {
	m, f := formFixture()
	m.formFocus = formName
	n, _ := m.Update(uiKey("enter"))
	m = n.(harborModel)
	n, _ = m.Update(uiKey("r"))
	m = n.(harborModel)
	if m.screen != "new-name" || m.input.Value() != m.draft.Name+"r" {
		t.Fatal("ordinary shortcut characters escaped the name field")
	}
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = n.(harborModel)
	n, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if n.(harborModel).screen != "busy" || cmd == nil || len(f.Sessions) != 0 {
		t.Fatal("Ctrl+S did not submit the draft through the existing create flow")
	}
}

func TestProfileDefaultKeyboardUsesVisibleFocus(t *testing.T) {
	f := &fakeApplicationService{ProfileRecords: []profile{{ID: "p", Kind: profileIsolated, Clients: []profileClientHome{{Client: "codex", ID: "env"}}}}}
	m := newHarborModel(f)
	m.profiles = f.ProfileRecords
	m.screen = "profile-default"
	m.profileInstances = []magpieInstance{{ID: "other", Name: "Other"}}
	selected, _ := m.Update(uiKey("j"))
	n, c := selected.Update(uiKey("enter"))
	n = settleApp(n, c)
	if n.(harborModel).screen != "profile-list" || f.ProfileRecords[0].DefaultInstanceID != "other" {
		t.Fatal("Enter saved a different instance from the visible focus")
	}
}

func TestLongDocumentScrollReachesEndAboveShortcuts(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.width, m.height, m.screen = 45, 12, "error"
	m.notice = strings.Repeat("long diagnostic line\n", 100)
	for i := 0; i < 30; i++ {
		next, _, _ := m.secondaryPageInput(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(harborModel)
	}
	if !strings.Contains(cleanUI(m.View()), "trying again.") {
		t.Fatal("fixed shortcut bar hid the final document line")
	}
}
