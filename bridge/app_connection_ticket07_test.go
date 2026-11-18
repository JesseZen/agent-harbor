package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func connectionFixture() harborModel {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "magpie"
	m.width = 100
	m.height = 30
	m.reports = []instanceReport{
		{magpieInstance: magpieInstance{ID: managedInstanceID, Kind: "managed", Name: "Harbor gateway"}, Health: instanceVerified},
		{magpieInstance: magpieInstance{ID: "external-test", Kind: "external", Name: "External fixture"}, Health: instanceVerified},
	}
	m.instanceCursor = 1
	return m
}
func TestConnectionTicket07TargetsAndCancel(t *testing.T) {
	for _, key := range []string{"s", "x", "w"} {
		m := connectionFixture()
		n, _, _ := m.connectionPageInput(uiKey(key))
		got := n.(harborModel)
		if got.instanceID != managedInstanceID || got.screen != "magpie-confirm" {
			t.Fatalf("%s target: %+v", key, got)
		}
		n, c, _ := got.connectionPageInput(uiKey("esc"))
		got = n.(harborModel)
		if c != nil || got.screen != "magpie" || got.instanceCursor != 1 {
			t.Fatal("cancel lost selection or executed")
		}
	}
	m := connectionFixture()
	n, _, _ := m.connectionPageInput(uiKey("enter"))
	if n.(harborModel).instanceID != "external-test" {
		t.Fatal("Enter did not target the selected instance")
	}
	n, cmd, _ := m.connectionPageInput(uiKey("u"))
	if n.(harborModel).screen != "magpie" || cmd != nil {
		t.Fatal("retired default shortcut still acts")
	}
}
func TestConnectionTicket07OfflineBusyAndAsync(t *testing.T) {
	m := connectionFixture()
	m.reports[1].Health = "offline"
	n, c, _ := m.connectionPageInput(uiKey("enter"))
	got := n.(harborModel)
	if got.screen != "magpie" || c != nil || got.instanceCursor != 1 {
		t.Fatal("offline manage was submitted")
	}
	m.screen = "magpie-busy"
	for _, key := range []string{"esc", "enter", "s", "x"} {
		n, c, _ := m.connectionPageInput(uiKey(key))
		if n.(harborModel).screen != m.screen || c != nil {
			t.Fatal("busy accepted input")
		}
	}
	if _, _, ok := m.connectionPageInput(appMagpieDone{}); ok {
		t.Fatal("swallowed async service event")
	}
	if _, _, ok := m.connectionPageInput(tea.WindowSizeMsg{Width: 60, Height: 20}); ok {
		t.Fatal("swallowed resize")
	}
}
func TestConnectionTicket07KeyboardReachabilityAndSearch(t *testing.T) {
	for _, size := range [][2]int{{60, 14}, {100, 24}, {160, 39}} {
		m := connectionFixture()
		m.width = size[0]
		m.height = size[1]
		for _, key := range []string{"s", "x", "w", "enter"} {
			direct := m
			n, _, _ := direct.connectionPageInput(uiKey(key))
			if n.(harborModel).screen != "magpie-confirm" {
				t.Fatalf("direct %s did not open confirmation", key)
			}
		}

	}
	m := connectionFixture()
	n, _, _ := m.connectionPageInput(uiKey("/"))
	m = n.(harborModel)
	n, _, _ = m.connectionPageInput(uiKey("s"))
	m = n.(harborModel)
	if m.screen != "magpie" || m.instanceFilter != "s" {
		t.Fatal("search leaked action")
	}
}
func TestConnectionTicket07FailureAndConfirmationGuard(t *testing.T) {
	m := connectionFixture()
	n, _, _ := m.connectionPageInput(uiKey("s"))
	m = n.(harborModel)
	p, _ := m.connectionPage()
	for _, r := range p.Hits {
		if r.ID == "conn:enter" {
			n, c, _ := m.connectionPageInput(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if c != nil || n.(harborModel).screen != "magpie-confirm" {
				t.Fatal("click leaked into confirmation")
			}
		}
	}
	n, c, _ := m.connectionPageInput(uiKey("enter"))
	if c != nil || n.(harborModel).screen != "magpie-confirm" {
		t.Fatal("repeated Enter leaked")
	}
	m = connectionFixture()
	m.screen = "magpie-busy"
	n, c = m.Update(appMagpieDone{err: fmt.Errorf("fixture failure; retry refresh")})
	got := n.(harborModel)
	if c == nil || got.instanceCursor != 1 || !strings.Contains(got.notice, "fixture failure") {
		t.Fatal("failure lost target or feedback")
	}
}

func TestConnectionTextOwnsGlobalCharacters(t *testing.T) {
	for _, mode := range []string{"search", "bind"} {
		m := connectionFixture()
		if mode == "search" {
			n, _, _ := m.connectionPageInput(uiKey("/"))
			m = n.(harborModel)
		} else {
			n, _, _ := m.connectionPageInput(uiKey("b"))
			m = n.(harborModel)
			m.input.SetValue("")
		}
		for _, key := range []string{"q", "?", "s", "x"} {
			n, _, _ := m.connectionPageInput(uiKey(key))
			m = n.(harborModel)
		}
		if m.input.Value() != "q?sx" || (mode == "bind" && m.screen != "magpie-bind") || (mode == "search" && m.screen != "magpie") {
			t.Fatalf("%s leaked text: screen=%s input=%q", mode, m.screen, m.input.Value())
		}
	}
}

func TestConnectionConfirmAlwaysShowsActions(t *testing.T) {
	for _, size := range [][2]int{{46, 12}, {57, 21}, {105, 33}} {
		m := connectionFixture()
		m.width, m.height = size[0], size[1]
		m.screen = "magpie-confirm"
		m.action = "start"
		for _, scroll := range []int{0, 10000} {
			m.scroll = scroll
			p, _ := m.connectionPage()
			for _, id := range []string{"conn:esc", "conn:enter"} {
				found := false
				for _, r := range p.Hits {
					if r.ID == id && r.Y+r.H <= m.height {
						found = true
					}
				}
				if !found {
					t.Fatalf("%v scroll%d: action %s hidden", size, scroll, id)
				}
			}
		}
	}
}
