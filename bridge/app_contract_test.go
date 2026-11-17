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

func TestReverseFocusStartsAtLastControl(t *testing.T) {
	if got := uiCycle("", []string{"first", "middle", "last"}, true); got != "last" {
		t.Fatal(got)
	}
	m, _ := formFixture()
	m.screen = "new-catalog"
	m.draft.Instance = "fixture"
	m.models = []catalogModel{{ID: "test"}}
	n, _, _ := m.connectionSelectorInput(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = n.(harborModel)
	if m.uiFocus != "selector-list" {
		t.Fatal(m.uiFocus)
	}
	n, _, _ = m.connectionSelectorInput(tea.KeyMsg{Type: tea.KeyTab})
	m = n.(harborModel)
	if m.uiFocus != "selector-search" {
		t.Fatal(m.uiFocus)
	}
}
