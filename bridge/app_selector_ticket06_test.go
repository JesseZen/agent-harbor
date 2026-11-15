package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func selectorKey(m harborModel, k string) harborModel {
	n, _, _ := m.connectionSelectorInput(uiKey(k))
	return n.(harborModel)
}
func TestTicket06CancelAndStaleCatalog(t *testing.T) {
	m, _ := formFixture()
	m.width = 100
	m.height = 30
	m.draft.Name = "keep"
	m.formSaved = m.draft
	m.screen = "new-catalog"
	m.draft.Connection = "magpie"
	m.draft.Instance = "other"
	m.draft.Model = "group"
	m.generation = 9
	m = selectorKey(m, "esc")
	if m.draft != m.formSaved || m.screen != "new-form" {
		t.Fatal(m.draft)
	}
	next, _ := m.Update(appCatalog{generation: 9, instance: "other", models: []catalogModel{{ID: "stale"}}})
	m = next.(harborModel)
	if len(m.models) != 0 || m.screen != "new-form" {
		t.Fatal("stale result applied")
	}
}
func TestTicket06ExplicitApplyAndCreate(t *testing.T) {
	m, f := formFixture()
	m.width = 100
	m.height = 30
	m.formSaved = m.draft
	m.screen = "new-catalog"
	m.draft.Connection = "magpie"
	m.draft.Instance = "fixture"
	m.models = []catalogModel{{ID: "provider-model", OwnedBy: "synthetic"}, {ID: "routing-group", MagpieLabel: "Group"}}
	m = creationClick(t, m, "selector-model:routing-group")
	if m.draft.Model != "" {
		t.Fatal("row click committed")
	}
	m = creationClick(t, m, "selector-apply")
	if m.screen != "new-form" || m.draft.Model != "routing-group" || !f.Acknowledged["fixture"] {
		t.Fatal(m.draft, f.Calls)
	}
	if strings.Contains(strings.Join(f.Calls, "|"), "create") {
		t.Fatal("selector launched")
	}
	m.formFocus = 5
	n, c := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if n.(harborModel).screen != "busy" || c == nil {
		t.Fatal("Create not immediate", n.(harborModel).notice)
	}
	c()
	if len(f.Sessions) != 1 {
		t.Fatal(f.Calls)
	}
}
func TestTicket06SearchAndAppliedMarker(t *testing.T) {
	m, _ := formFixture()
	m.width = 100
	m.height = 30
	m.formSaved = sessionDraft{Connection: "magpie", Instance: "one", Model: "model"}
	m.draft = m.formSaved
	m.screen = "new-catalog"
	m.models = []catalogModel{{ID: "model"}, {ID: "group"}}
	m.catalogCursor = 1
	p, _ := m.connectionSelectorPage()
	_ = p
	m = selectorKey(m, "/")
	m = selectorKey(m, "g")
	if len(m.filteredModels()) != 1 || m.filteredModels()[0].ID != "group" {
		t.Fatal(m.catalogFilter)
	}
	m = selectorKey(m, "esc")
	m = selectorKey(m, "esc")
	if m.draft != m.formSaved {
		t.Fatal(m.draft)
	}
}
