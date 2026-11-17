package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestFlowCancelRestoresFirstEnterTarget(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "one", Name: "One", CLI: "codex", Mode: "local"}}}
	m.screen, m.action, m.uiFocus = "confirm", "stop", "review:esc"
	n, _ := m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.screen != "sessions" || m.uiFocus != "list" {
		t.Fatal(m.screen, m.uiFocus)
	}
	_, cmd := m.Update(uiKey("enter"))
	if cmd == nil {
		t.Fatal("first Enter failed to inspect target")
	}
}
func TestFlowAccountsScrollAndCaller(t *testing.T) {
	f := &fakeApplicationService{}
	p, err := f.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
	if err != nil {
		t.Fatal(err)
	}
	b := bindingFor(p)
	m := newHarborModel(f)
	m.accountProfile = &b
	m.activeProfile = &b
	m.profiles = []profile{p}
	m.screen = "accounts-result"
	m.height = 12
	m.accountResult = strings.Repeat("long status\n", 80)
	for i := 0; i < 200; i++ {
		n, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = n.(harborModel)
	}
	end := m.scroll
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = n.(harborModel)
	if m.scroll != end-1 {
		t.Fatal("overscroll swallowed Up", end, m.scroll)
	}
	m.screen = "accounts"
	m.scroll = 0
	n, _ = m.Update(uiKey("p"))
	m = n.(harborModel)
	n, _ = m.Update(uiKey("enter"))
	m = n.(harborModel)
	if m.screen != "accounts" || m.accountProfile.ProfileID != b.ProfileID {
		t.Fatal("lost caller", m.screen)
	}
}
func TestFlowMoreAndNotificationBoundary(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "one", Name: "One"}}}
	m.notice = "Magpie action completed"
	n, _ := m.Update(uiKey("."))
	m = n.(harborModel)
	if m.uiFocus != "action:i" || m.notice != "" {
		t.Fatal(m.uiFocus, m.notice)
	}
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = n.(harborModel)
	if m.uiFocus != "action:e" {
		t.Fatal(m.uiFocus)
	}
	n, _ = m.Update(uiKey("enter"))
	m = n.(harborModel)
	if m.screen != "rename" {
		t.Fatal(m.screen)
	}
}
func TestFlowSearchEscLayers(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.searchFocus = "sessions"
	m.filter = "unmatched"
	m.input.SetValue(m.filter)
	n, _ := m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.filter != "" || m.searchFocus != "sessions" {
		t.Fatal("first escape must clear")
	}
	n, _ = m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.searchFocus != "" || m.screen != "sessions" {
		t.Fatal("second escape must leave input")
	}
}
