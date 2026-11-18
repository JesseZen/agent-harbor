package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func creationClick(t *testing.T, m harborModel, id string) harborModel {
	t.Helper()
	p, ok := m.newSessionPage()
	if !ok {
		t.Fatal(m.screen)
	}
	for _, r := range p.Hits {
		if r.ID == id {
			next, cmd, handled := m.newSessionPageInput(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if !handled {
				t.Fatal("unhandled")
			}
			return settleApp(next, cmd).(harborModel)
		}
	}
	t.Fatalf("missing %s", id)
	return m
}
func TestTicket04DynamicClientHits(t *testing.T) {
	for _, height := range []int{12, 20, 40} {
		m, _ := formFixture()
		m.width = 60
		m.height = height
		m.screen = "new-cli"
		m.clientCursor = 14
		for i := 0; i < 20; i++ {
			target := fmt.Sprintf("client-%02d", i)
			m.environment.Clients = append(m.environment.Clients, clientCapability{Target: target, Label: "Dynamic", Command: target, Available: true})
		}
		p, _ := m.newSessionPage()
		found := false
		for _, r := range p.Hits {
			if strings.HasPrefix(r.ID, "new-client:") {
				n := creationClick(t, m, r.ID)
				if n.screen == "new-cli" {
					n = creationClick(t, n, r.ID)
				}
				if n.draft.Client != strings.TrimPrefix(r.ID, "new-client:") {
					t.Fatalf("stale hit %s -> %s", r.ID, n.draft.Client)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("no visible clients")
		}
		m.input.SetValue("client-17")
		m.clientCursor = 0
		n := creationClick(t, m, "new-client:client-17")
		if n.screen == "new-cli" {
			n = creationClick(t, n, "new-client:client-17")
		}
		if n.draft.Client != "client-17" {
			t.Fatal(n.draft.Client)
		}
	}
}
func TestTicket04CancelAndReadonly(t *testing.T) {
	m, f := formFixture()
	m.width = 100
	m.height = 20
	m.draft.Client = "pi"
	m.draft.Name = "keep"
	m.formFocus = 3
	n := creationClick(t, m, "new-field:Model")
	if !strings.Contains(n.notice, "pi") || n.screen != "new-form" {
		t.Fatal(n.notice)
	}
	p, _ := n.newSessionPage()
	if strings.HasSuffix(strings.TrimSpace(p.Lines[4]), "›") {
		t.Fatal("readonly arrow")
	}
	n = creationClick(t, n, "new-field:Name")
	n = creationClick(t, n, "new-field:Name")
	n.input.SetValue("discard")
	n = creationClick(t, n, "new-back")
	if n.draft.Name != "keep" || n.formFocus != 4 {
		t.Fatal(n.draft)
	}
	n = creationClick(t, n, "new-field:Client")
	n = creationClick(t, n, "new-field:Client")
	n = creationClick(t, n, "new-back")
	if n.draft.Client != "pi" {
		t.Fatal(n.draft)
	}
	if len(f.Calls) > 0 {
		t.Fatal(f.Calls)
	}
	n = creationClick(t, n, "new-cancel")
	if n.screen != "sessions" {
		t.Fatal(n.screen)
	}
}
