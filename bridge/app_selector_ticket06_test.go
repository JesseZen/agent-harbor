package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func selectorKey(m harborModel, k string) harborModel {
	n, c, _ := m.connectionSelectorInput(uiKey(k))
	return settleApp(n, c).(harborModel)
}

func TestNewMagpieModelStartsWithSearchOnDefaultInstance(t *testing.T) {
	m, f := formFixture()
	m.formFocus = formModel
	m.draft.Connection = "magpie"
	m.environment.DefaultInstance = "second"
	f.Reports = []instanceReport{
		{magpieInstance: magpieInstance{ID: "first"}, Health: instanceVerified},
		{magpieInstance: magpieInstance{ID: "second"}, Health: instanceVerified},
	}
	f.Models = []catalogModel{{ID: "provider/other"}, {ID: "group/one"}, {ID: "group/two"}}

	next, cmd := m.Update(uiKey("enter"))
	m = settleApp(next, cmd).(harborModel)
	if m.screen != "new-catalog" || m.uiFocus != "selector-search" || m.draft.Instance != "second" {
		t.Fatalf("expected focused model search for the default instance, got %s %s %q", m.screen, m.uiFocus, m.draft.Instance)
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = settleApp(next, cmd).(harborModel)
	if m.screen != "new-instance" || m.instanceRows()[m.instanceCursor].ID != "second" {
		t.Fatal("could not change instance from the focused model search")
	}
	next, cmd = m.Update(uiKey("enter"))
	m = settleApp(next, cmd).(harborModel)
	for _, key := range []string{"g", "r", "o", "u", "p", "/"} {
		next, cmd = m.Update(uiKey(key))
		m = settleApp(next, cmd).(harborModel)
	}
	rows := m.filteredModels()
	if m.catalogFilter != "group/" || len(rows) != 2 {
		t.Fatalf("typing did not filter the catalog: %q %+v", m.catalogFilter, rows)
	}
	for _, key := range []string{"down", "enter"} {
		next, cmd = m.Update(uiKey(key))
		m = settleApp(next, cmd).(harborModel)
	}
	if m.screen != "new-form" || m.draft.Model != rows[1].ID || !f.Acknowledged["second"] || len(f.Sessions) != 0 {
		t.Fatalf("search did not apply the explicitly selected model: %+v %v", m.draft, f.Calls)
	}
}

func TestNewMagpieModelAsksWhenDefaultInstanceIsUnavailable(t *testing.T) {
	m, f := formFixture()
	m.formFocus = formModel
	m.draft.Connection = "magpie"
	m.environment.DefaultInstance = "unavailable"
	f.Reports = []instanceReport{{magpieInstance: magpieInstance{ID: "other"}, Health: instanceVerified}}
	next, cmd := m.Update(uiKey("enter"))
	m = settleApp(next, cmd).(harborModel)
	if m.screen != "new-instance" || m.draft.Instance != "" || strings.Contains(strings.Join(f.Calls, "|"), "catalog") {
		t.Fatalf("unavailable default was silently substituted: %+v %v", m.draft, f.Calls)
	}
}

func TestFullAccessOptionsExplainFocusedField(t *testing.T) {
	m, _ := formFixture()
	m.width, m.height = 60, 24
	m.openCodexOptions(codexOptionEditor{returnScreen: "new-form", field: optionCompact, value: codexOptions{SandboxMode: "danger-full-access"}})
	if frame := strings.Join(strings.Fields(cleanUI(m.View())), " "); !strings.Contains(frame, "Token threshold") {
		t.Fatalf("auto compact help missing with Full access selected: %s", frame)
	}
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
	m.formFocus = formCreate
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
