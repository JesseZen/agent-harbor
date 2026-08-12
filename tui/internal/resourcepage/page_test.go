package resourcepage

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

func testSpec() Spec {
	return Spec{
		Title: "Targets",
		Scope: "default",
		Columns: []resourceview.Column{
			{Title: "ID", MinWidth: 8, Priority: 0},
			{Title: "HEALTH", MinWidth: 8, Priority: 1},
		},
		Actions: ActionSet{
			Create:  true,
			Edit:    true,
			Delete:  true,
			Publish: true,
			Details: true,
			Filter:  true,
			Mark:    true,
		},
		Domain: "targets",
	}
}

func TestPageDisconnectedSuppressesPublishAndShowsMessage(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 12)
	page.SetState(StateDisconnected)
	page.SetRows(FixtureRows(2))

	view := page.View()
	if !strings.Contains(view, "Disconnected") {
		t.Fatalf("missing disconnected banner:\n%s", view)
	}
	if !strings.Contains(view, "item-001") {
		t.Fatalf("table not retained while disconnected:\n%s", view)
	}

	intent, consumed := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if consumed || intent == IntentPublish {
		t.Fatalf("publish should be suppressed when disconnected: intent=%q consumed=%v", intent, consumed)
	}
}

func TestPageEmptyState(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 10)
	page.SetState(StateEmpty)
	page.SetRows(nil)

	view := page.View()
	if !strings.Contains(view, "No resources") {
		t.Fatalf("missing empty banner:\n%s", view)
	}
}

func TestPageFooterShowsAndHitsOnlySupportedActions(t *testing.T) {
	spec := testSpec()
	spec.Actions = ActionSet{Create: true, Filter: true}
	page := New(spec)
	page.SetSize(80, 10)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))

	view := page.View()
	// Key chips use horizontal padding, so the key/desc gap may be two spaces.
	for _, expected := range []string{"new", "filter", "column", "sort"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("supported footer action %q missing:\n%s", expected, view)
		}
	}
	for _, forbidden := range []string{"e edit", "d del", "^s pub", "space mark", "e  edit", "d  del", "^s  pub", "space  mark"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("unsupported footer action %q rendered:\n%s", forbidden, view)
		}
	}
	if hit := page.Table().FooterActionHit(resourceview.ActionCreate); hit.Kind != resourceview.HitFooterAction {
		t.Fatalf("supported create action has no hit region: %#v", hit)
	}
	for _, action := range []resourceview.Action{
		resourceview.ActionEdit,
		resourceview.ActionDelete,
		resourceview.ActionPublish,
	} {
		if hit := page.Table().FooterActionHit(action); hit.Kind != resourceview.HitNone {
			t.Fatalf("unsupported %s action retained hit region: %#v", action, hit)
		}
	}
}

func TestPageDisabledFilterAndMarkDoNotActivate(t *testing.T) {
	spec := testSpec()
	spec.Actions = ActionSet{}
	page := New(spec)
	page.SetSize(80, 10)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))
	_ = page.View()

	if intent, consumed := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}); consumed || intent != IntentNone {
		t.Fatalf("disabled filter consumed key: intent=%q consumed=%t", intent, consumed)
	}
	if page.Table().Filtering() {
		t.Fatal("disabled filter entered input mode")
	}
	if intent, consumed := page.Update(tea.KeyMsg{Type: tea.KeySpace}); consumed || intent != IntentNone {
		t.Fatalf("disabled mark consumed key: intent=%q consumed=%t", intent, consumed)
	}
	if marked := page.Table().MarkedIDs(); len(marked) != 0 {
		t.Fatalf("disabled mark changed selection: %v", marked)
	}
	if hit := page.Table().FooterFilterHit(); hit.Kind != resourceview.HitNone {
		t.Fatalf("disabled filter retained hit region: %#v", hit)
	}
}

func TestPageSuccessKeyboardIntents(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 12)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))

	cases := []struct {
		key  tea.KeyMsg
		want Intent
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, IntentCreate},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, IntentEdit},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, IntentDelete},
		{tea.KeyMsg{Type: tea.KeyEnter}, IntentDetails},
		{tea.KeyMsg{Type: tea.KeyCtrlS}, IntentPublish},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}}, IntentCommands},
	}

	for _, tc := range cases {
		intent, consumed := page.Update(tc.key)
		if !consumed || intent != tc.want {
			t.Fatalf("key %q => intent=%q consumed=%v, want %q true", tc.key, intent, consumed, tc.want)
		}
	}
}

func TestPageMouseDetailsViaTable(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 12)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))
	_ = page.View()

	click := tea.MouseMsg{X: 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	page.Update(click)
	intent, consumed := page.Update(click)
	if !consumed || intent != IntentDetails {
		t.Fatalf("double-click => intent=%q consumed=%v", intent, consumed)
	}
}

func TestPageResizeKeepsSelection(t *testing.T) {
	page := New(testSpec())
	page.SetRows(FixtureRows(4))
	page.SetSize(80, 12)
	page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page.Update(tea.KeyMsg{Type: tea.KeyDown})
	selected := page.SelectedID()

	page.SetSize(60, 8)
	_ = page.View()
	if page.SelectedID() != selected {
		t.Fatalf("selection lost after resize: got %q want %q", page.SelectedID(), selected)
	}
}

func TestPageDirtyMarksTitle(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 10)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))
	page.SetDirty(true)

	view := page.View()
	if !strings.Contains(view, "*Targets(default)") {
		t.Fatalf("dirty title missing asterisk:\n%s", view)
	}
}

func TestPageFilterIntent(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 10)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))
	_ = page.View()

	hit := page.table.FooterFilterHit()
	intent, consumed := page.Update(tea.MouseMsg{X: hit.X, Y: hit.Y + page.overlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !consumed || intent != IntentFilter {
		t.Fatalf("footer filter => intent=%q consumed=%v", intent, consumed)
	}
}
