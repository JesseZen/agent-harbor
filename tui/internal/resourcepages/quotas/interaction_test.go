package quotas

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFilterSortMouseKeyboardParity(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.QuotaGroups = append(cmd.QuotaGroups, sampleQuota("quota-extra"))
	})
	page.Sync()
	page.SetSize(120, 30)
	_ = page.View()

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range []rune("quota-default") {
		page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ids := page.VisibleIDs()
	if len(ids) != 1 || ids[0] != "quota-default" {
		t.Fatalf("filter visible=%v want [quota-default]", ids)
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.SelectID("quota-default")

	cases := []struct {
		key  tea.KeyMsg
		want resourcepage.Intent
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, resourcepage.IntentCreate},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, resourcepage.IntentEdit},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, resourcepage.IntentDelete},
		{tea.KeyMsg{Type: tea.KeyEnter}, resourcepage.IntentDetails},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}}, resourcepage.IntentCommands},
	}
	for _, tc := range cases {
		page.CancelOverlay()
		page.Update(tc.key)
		if page.LastIntent() != tc.want {
			t.Fatalf("key %v => intent=%q want %q", tc.key, page.LastIntent(), tc.want)
		}
	}

	page.CancelOverlay()
	_ = page.View()
	newHit := page.Host().Table().FooterActionHit(resourceview.ActionCreate)
	if newHit.Kind != 0 {
		y := newHit.Y + page.OverlayLines()
		page.Update(tea.MouseMsg{X: newHit.X, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if page.LastIntent() != resourcepage.IntentCreate {
			t.Fatalf("footer new => intent=%q", page.LastIntent())
		}
	}

	page.CancelOverlay()
	rowY := page.TableHeaderY() + page.OverlayLines() + 1
	click := tea.MouseMsg{X: 2, Y: rowY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	page.Update(click)
	page.Update(click)
	if page.LastIntent() != resourcepage.IntentDetails || !page.ShowingDetails() {
		t.Fatalf("double-click details failed; intent=%q showing=%v", page.LastIntent(), page.ShowingDetails())
	}
}

func TestSortChangesVisibleOrder(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.QuotaGroups = append(cmd.QuotaGroups, sampleQuota("quota-z"), sampleQuota("quota-a"))
	})
	page.Sync()
	before := append([]string(nil), page.VisibleIDs()...)
	if len(before) < 2 {
		t.Fatalf("need multiple rows, got %v", before)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	after := page.VisibleIDs()
	if len(after) != len(before) {
		t.Fatalf("sort changed row count: before=%v after=%v", before, after)
	}
	changed := false
	for i := range before {
		if before[i] != after[i] {
			changed = true
			break
		}
	}
	if !changed {
		page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		after = page.VisibleIDs()
		for i := range before {
			if before[i] != after[i] {
				changed = true
				break
			}
		}
	}
	if !changed {
		t.Fatalf("sort did not change visible order: before=%v after=%v", before, after)
	}
}

func TestKeyboardBlockedDeleteShowsDialog(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().QuotaGroups)
	page.SelectID("quota-default")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	view := page.View()
	if !strings.Contains(view, "Delete blocked") {
		t.Fatalf("expected blocked dialog:\n%s", view)
	}
	if !strings.Contains(view, "targets[target-1].quota_group_id") {
		t.Fatalf("dependency dialog missing path:\n%s", view)
	}
	if !strings.Contains(view, "1 inbound reference") {
		t.Fatalf("dependency dialog missing inbound count:\n%s", view)
	}
	if len(draft.LocalCommand().QuotaGroups) != before {
		t.Fatal("blocked delete via keyboard must not mutate draft")
	}
}

func TestResizeKeepsSelection(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.QuotaGroups = append(cmd.QuotaGroups, sampleQuota("quota-b"), sampleQuota("quota-c"))
	})
	page.Sync()
	page.SetSize(120, 30)
	page.Update(tea.KeyMsg{Type: tea.KeyDown})
	selected := page.SelectedID()

	page.SetSize(70, 30)
	_ = page.View()
	if page.SelectedID() != selected {
		t.Fatalf("selection lost after resize: got %q want %q", page.SelectedID(), selected)
	}
}

func TestKeyboardCRUDViaUpdate(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().QuotaGroups)

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if page.LastIntent() != resourcepage.IntentCreate || page.Editor() == nil {
		t.Fatalf("create via n: intent=%q editor=%v", page.LastIntent(), page.Editor())
	}
	page.SetEditorValues(map[string]string{
		"id":                  "quota-kbd",
		"name":                "keyboard-create",
		"rpm":                 "120",
		"max_concurrency":     "4",
		"foreground_capacity": "2",
		"background_capacity": "1",
		"foreground_weight":   "9",
		"background_weight":   "1",
		"queue_timeout_ms":    "30000",
	})
	page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	found := false
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-kbd" && q.Name == "keyboard-create" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ctrl+s create did not mutate draft: %+v", draft.LocalCommand().QuotaGroups)
	}

	page.SelectID("quota-kbd")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if page.LastIntent() != resourcepage.IntentEdit || page.Editor() == nil {
		t.Fatalf("edit via e: intent=%q editor=%v", page.LastIntent(), page.Editor())
	}
	page.SetEditorValues(map[string]string{"name": "keyboard-edit"})
	page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	found = false
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-kbd" && q.Name == "keyboard-edit" {
			found = true
		}
	}
	if !found {
		t.Fatal("ctrl+s edit did not mutate draft")
	}

	page.SelectID("quota-kbd")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if page.LastIntent() != resourcepage.IntentDelete {
		t.Fatalf("delete via d: intent=%q", page.LastIntent())
	}
	if !strings.Contains(page.View(), "Confirm delete quota group quota-kbd") {
		t.Fatalf("expected confirm dialog:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-kbd" {
			t.Fatal("enter confirm should remove unreferenced quota")
		}
	}

	page.SelectID("quota-default")
	refBefore := len(draft.LocalCommand().QuotaGroups)
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !strings.Contains(page.View(), "Delete blocked") {
		t.Fatalf("expected blocked dialog:\n%s", page.View())
	}
	page.ConfirmDelete()
	if len(draft.LocalCommand().QuotaGroups) != refBefore {
		t.Fatal("ConfirmDelete on blocked overlay must not mutate draft")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(draft.LocalCommand().QuotaGroups) != refBefore {
		t.Fatal("enter on blocked dialog must not delete referenced quota")
	}
	if len(draft.LocalCommand().QuotaGroups) != before {
		t.Fatalf("draft length changed unexpectedly: before=%d now=%d", before, len(draft.LocalCommand().QuotaGroups))
	}
}

func TestFooterFilterMouseIntent(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetSize(120, 30)
	_ = page.View()
	filterHit := page.Host().Table().FooterFilterHit()
	if filterHit.Kind == 0 {
		t.Fatal("footer filter hit unavailable")
	}
	page.Update(tea.MouseMsg{
		X:      filterHit.X,
		Y:      filterHit.Y + page.OverlayLines(),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if page.LastIntent() != resourcepage.IntentFilter {
		t.Fatalf("footer filter click => intent=%q want filter", page.LastIntent())
	}
}

func TestMouseUnderDisconnectedBanner(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetSize(120, 30)
	draft.SetDisconnected(true)
	page.Sync()
	_ = page.View()
	if page.OverlayLines() == 0 {
		t.Fatal("expected disconnected banner overlay")
	}

	filterHit := page.Host().Table().FooterFilterHit()
	page.Update(tea.MouseMsg{
		X:      filterHit.X,
		Y:      filterHit.Y + page.OverlayLines(),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if page.LastIntent() != resourcepage.IntentFilter {
		t.Fatalf("footer filter under banner => intent=%q want filter", page.LastIntent())
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.SelectID("quota-default")
	rowY := page.TableHeaderY() + page.OverlayLines() + 1
	click := tea.MouseMsg{X: 2, Y: rowY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	page.Update(click)
	page.Update(click)
	if page.LastIntent() != resourcepage.IntentDetails || !page.ShowingDetails() {
		t.Fatalf("double-click under banner => intent=%q details=%v", page.LastIntent(), page.ShowingDetails())
	}
}

func TestDetailsShowsContractFields(t *testing.T) {
	page, _ := newTestPage(t)
	page.SelectID("quota-default")
	page.OpenDetails()
	view := strings.ToLower(page.View())
	for _, field := range []string{"id", "name", "rpm", "max_concurrency", "foreground_weight", "background_weight"} {
		if !strings.Contains(view, field) {
			t.Fatalf("details missing field %q:\n%s", field, view)
		}
	}
}
