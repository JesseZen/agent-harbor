package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func sessionsClick(t *testing.T, m harborModel, id string) harborModel {
	t.Helper()
	for _, r := range m.sessionsLayout().Hits {
		if r.ID == id {
			next, cmd := m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			for cmd != nil {
				next, cmd = next.Update(cmd())
			}
			return next.(harborModel)
		}
	}
	t.Fatal("missing", id)
	return m
}
func TestSessionsUISelectAndExplicitAction(t *testing.T) {
	m := searchFixture()
	f := m.service.(*fakeApplicationService)
	f.Sessions = append([]sessionView(nil), m.sessions...)
	m = sessionsClick(t, m, "session:1")
	if m.searchSelectedID() != "1" || len(f.Calls) != 0 {
		t.Fatal("single click acted", f.Calls)
	}
	m = sessionsClick(t, m, "action:i")
	if m.screen != "detail" || m.searchSelectedID() != "1" {
		t.Fatal("details target changed")
	}
	m = appKey(m, "esc").(harborModel)
	m = sessionsClick(t, m, "more")
	m = sessionsClick(t, m, "action:e")
	if m.screen != "rename" || m.reviewed.ID != "1" {
		t.Fatal("rename target changed")
	}
}
func TestSessionsUIPracticalInnerSizes(t *testing.T) {
	for _, sz := range [][2]int{{44, 12}, {58, 21}, {104, 32}, {60, 20}, {100, 30}, {160, 45}} {
		m := searchFixture()
		m.width, m.height = sz[0], sz[1]
		p := m.sessionsLayout()
		for _, line := range strings.Split(p.view(), "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal(sz, "overflow")
			}
		}
		for _, id := range []string{"search", "action:n", "more", "action:i"} {
			found := false
			for _, r := range p.Hits {
				if r.ID == id {
					found = true
				}
			}
			if !found {
				t.Fatal(sz, "clipped", id)
			}
		}
	}
}
func TestSessionsSearchButtonDoesNotTypeShortcut(t *testing.T) {
	m := searchFixture()
	f := m.service.(*fakeApplicationService)
	f.Sessions = append([]sessionView(nil), m.sessions...)
	m = keySearch(m, "/")
	m = keySearch(m, "Harbor")
	for _, hit := range m.sessionsLayout().Hits {
		if hit.ID == "action:i" {
			t.Fatal("global action shown while search owns input")
		}
	}
	m = keySearch(m, "enter")
	m = sessionsClick(t, m, "action:i")
	if m.screen != "detail" || m.filter != "Harbor" {
		t.Fatal(m.screen, m.filter)
	}
	m = appKey(m, "esc").(harborModel)
	m = sessionsClick(t, m, "nav:settings")
	if m.settings == nil {
		t.Fatal("settings navigation")
	}
}
