package resourcepage

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPageAccessorsAndBanners(t *testing.T) {
	page := New(Spec{
		Title:   "Routes",
		Scope:   "all",
		Columns: []resourceview.Column{{Title: "ID", MinWidth: 8, Priority: 0}},
		Actions: ActionSet{Create: true, Edit: true, Delete: true, Publish: true, Details: true, Filter: true},
		Domain:  "routes",
	})
	if page.Table() == nil {
		t.Fatal("table is nil")
	}
	page.SetRows(FixtureRows(2))
	page.SetSize(40, 12)
	page.SetStatus("ready")
	page.SetDirty(true)
	if page.State() != StateLoading {
		t.Fatalf("state = %q", page.State())
	}

	for _, state := range []State{StateEmpty, StateLoading, StateValidationError, StatePublicationError, StateStale, StateDisconnected, StateSuccess} {
		page.SetState(state)
		view := page.View()
		if view == "" {
			t.Fatalf("empty view for state %s", state)
		}
		if state == StateDisconnected && !strings.Contains(view, "Disconnected") {
			t.Fatalf("missing disconnected banner:\n%s", view)
		}
	}
	if got := truncate("abcdef", 0); got != "" {
		t.Fatalf("truncate zero width = %q", got)
	}
	if got := truncate("abcdef", 1); got != "a" {
		t.Fatalf("truncate one = %q", got)
	}
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate ellipsis = %q", got)
	}
}

func TestPageIntentsAndLookup(t *testing.T) {
	page := New(Spec{
		Title:   "Routes",
		Scope:   "all",
		Columns: []resourceview.Column{{Title: "ID", MinWidth: 8, Priority: 0}},
		Actions: ActionSet{Create: true, Edit: true, Delete: true, Publish: true, Details: true, Filter: true},
	})
	page.SetRows(FixtureRows(1))
	page.SetState(StateSuccess)
	page.SetSize(60, 16)

	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}); !ok || intent != IntentCreate {
		t.Fatalf("create intent = %q ok=%v", intent, ok)
	}
	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}); !ok || intent != IntentEdit {
		t.Fatalf("edit intent = %q ok=%v", intent, ok)
	}
	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}); !ok || intent != IntentDelete {
		t.Fatalf("delete intent = %q ok=%v", intent, ok)
	}
	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyEnter}); !ok || intent != IntentDetails {
		t.Fatalf("details intent = %q ok=%v", intent, ok)
	}
	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS}); !ok || intent != IntentPublish {
		t.Fatalf("publish intent = %q ok=%v", intent, ok)
	}
	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}}); !ok || intent != IntentCommands {
		t.Fatalf("commands intent = %q ok=%v", intent, ok)
	}

	page.SetState(StateDisconnected)
	if intent, ok := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS}); ok || intent != IntentNone {
		t.Fatalf("disconnected publish = %q ok=%v", intent, ok)
	}

	descriptor, ok := Lookup(generated.ResourceRoute)
	if !ok || descriptor.SchemaName != "RouteConfig" || len(descriptor.Fields) == 0 {
		t.Fatalf("Lookup route = %#v ok=%v", descriptor, ok)
	}
	all := AllDescriptors()
	if len(all) < 12 {
		t.Fatalf("AllDescriptors len = %d", len(all))
	}
}
