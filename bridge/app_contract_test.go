package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestContextHelpRestoresParentControlAfterMouseBack(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-catalog"
	m.width, m.height = 46, 12
	m.models = []catalogModel{{ID: "group/test"}}
	m.draft.Instance = "fixture"
	m.uiFocus = "selector-apply"
	p0, _ := m.additionalPage()
	m.scroll = min(1, p0.MaxScroll)
	expectedScroll := m.scroll
	n, _ := m.Update(uiKey("?"))
	m = n.(harborModel)
	if m.screen != "help" {
		t.Fatal("help did not open")
	}
	p, _ := m.secondaryPage()
	for _, r := range p.Hits {
		if r.ID == "secondary:esc" {
			n, _ = m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m = n.(harborModel)
			break
		}
	}
	if m.screen != "new-catalog" || m.uiFocus != "selector-apply" || m.scroll != expectedScroll {
		t.Fatalf("parent state lost: %s %s %d", m.screen, m.uiFocus, m.scroll)
	}
}

func TestSecondaryBackReturnsFocusToSessionList(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "detail"
	m.uiFocus = "action:i"
	p, _ := m.secondaryPage()
	for _, r := range p.Hits {
		if r.ID == "secondary:esc" {
			n, _ := m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m = n.(harborModel)
			break
		}
	}
	if m.screen != "sessions" || m.uiFocus != "list" {
		t.Fatalf("stale control retained: %s %s", m.screen, m.uiFocus)
	}
}

func TestTabLeavesSelectorFocusUnchanged(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-catalog"
	m.draft.Instance = "fixture"
	m.models = []catalogModel{{ID: "test"}}
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyShiftTab} {
		next, cmd := m.Update(tea.KeyMsg{Type: key})
		if cmd != nil || next.(harborModel).uiFocus != m.uiFocus {
			t.Fatal("Tab changed selector focus")
		}
	}
}

func TestMainTabNavigationAndTextOwnership(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	for _, step := range []struct{ key, screen string }{
		{"tab", "sessions"}, {"l", "magpie"}, {"right", "accounts"},
		{"l", "profile-list"}, {"right", "profile-list"},
	} {
		next, _ := m.Update(uiKey(step.key))
		m = next.(harborModel)
		if m.screen != step.screen {
			t.Fatalf("%s selected %s, want %s", step.key, m.screen, step.screen)
		}
	}
	if m.settings == nil {
		t.Fatal("right from Profiles did not open Settings")
	}
	next, _ := m.Update(uiKey("h"))
	m = next.(harborModel)
	if m.settings != nil || m.screen != "profile-list" {
		t.Fatal("h did not go back to Profiles")
	}
	for _, step := range []struct{ key, screen string }{
		{"left", "accounts"}, {"h", "magpie"}, {"left", "sessions"},
		{"/", "sessions"}, {"h", "sessions"}, {"l", "sessions"}, {"j", "sessions"}, {"k", "sessions"},
	} {
		next, _ := m.Update(uiKey(step.key))
		m = next.(harborModel)
		if m.screen != step.screen {
			t.Fatalf("%s left %s", step.key, step.screen)
		}
	}
	if m.filter != "hljk" {
		t.Fatal("vim keys escaped the search field")
	}
}

func TestNewFormRemembersOnlyItsProfileConnection(t *testing.T) {
	m, _ := formFixture()
	a := profileBinding{ProfileID: "A", Client: "codex"}
	b := profileBinding{ProfileID: "B", Client: "codex"}
	m.draft.Profile, m.draft.Connection = &a, "magpie"
	m.rememberConnection()
	for _, test := range []struct {
		binding        *profileBinding
		recorded, want string
	}{
		{&a, "local", "magpie"},
		{&b, "", "local"},
		{&b, "magpie", "magpie"},
	} {
		m.screen = "new-project-loading"
		next, _ := m.Update(appNewReady{generation: m.generation, environment: appEnvironment{
			Profile: test.binding, LastConnection: test.recorded,
		}})
		if draft := next.(harborModel).draft; draft.Connection != test.want || draft.Instance != "" || draft.Model != "" {
			t.Fatal(draft)
		}
	}
}
