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
	m = selectorKey(m, "esc")
	if m.draft != m.formSaved {
		t.Fatal(m.draft)
	}
}

func TestTicket06CompactDisclosureAndPinnedActions(t *testing.T) {
	for _, size := range [][2]int{{46, 12}, {57, 21}, {105, 33}} {
		m, _ := formFixture()
		m.width, m.height = size[0], size[1]
		m.screen = "new-catalog"
		m.draft.Instance = "fixture"
		m.models = []catalogModel{{ID: "group/test"}}
		var document strings.Builder
		for scroll := 0; scroll < 20; scroll++ {
			m.scroll = scroll
			p, _ := m.connectionSelectorPage()
			for _, line := range p.Lines {
				// The rightmost cell is viewport chrome, not part of the disclosure text.
				line = strings.TrimSuffix(strings.TrimSuffix(cleanUI(line), "█"), "│")
				document.WriteString(line + "\n")
			}
			document.WriteByte('\n')
			for _, id := range []string{"selector-apply", "selector-cancel"} {
				found := false
				for _, r := range p.Hits {
					if r.ID == id && r.Y+r.H <= m.height {
						found = true
					}
				}
				if !found {
					t.Fatalf("%v scroll%d: %s hidden", size, scroll, id)
				}
			}
		}
		text := strings.Join(strings.Fields(cleanUI(document.String())), " ")
		for _, want := range []string{"Auxiliary requests may send conversation parts to other providers.", "Groups may switch members; an ID does not prove the destination."} {
			if !strings.Contains(text, want) {
				t.Fatalf("%v disclosure truncated: %s", size, text)
			}
		}
	}
}
func TestTicket06SwitchInstanceRequiresOwnAcknowledgement(t *testing.T) {
	m, f := formFixture()
	m.width = 100
	m.height = 30
	m.formSaved = m.draft
	m.screen = "new-catalog"
	m.draft.Connection = "magpie"
	m.draft.Instance = "second"
	m.models = []catalogModel{{ID: "group/fixture"}}
	f.Acknowledged = map[string]bool{"first": true}
	m = selectorKey(m, "enter")
	if !f.Acknowledged["second"] || m.screen != "new-form" {
		t.Fatal(f.Calls)
	}
}
