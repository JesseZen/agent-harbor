package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFilterSortPagingAndMouseParity(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for _, id := range []string{"route-a", "route-b", "route-c"} {
			r := sampleRoute(id)
			cmd.Routes = append(cmd.Routes, r)
		}
	})
	page.Refresh()
	page.SetKind(KindRoutes)
	page.SetSize(120, 30)
	_ = page.View()

	// Filter via /
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	page = model.(*Page)
	for _, r := range []rune("route-a") {
		model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		page = model.(*Page)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	ids := page.VisibleIDs()
	if len(ids) != 1 || ids[0] != "route-a" {
		t.Fatalf("filter visible=%v want [route-a]", ids)
	}

	// Clear filter
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)

	beforeSort := append([]string(nil), page.VisibleIDs()...)
	if len(beforeSort) < 2 {
		t.Fatalf("need multiple rows for sort, got %v", beforeSort)
	}
	_ = page.View()
	hit := page.Table().HitTest(2, page.TableHeaderY())
	if hit.Kind == 0 {
		model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		page = model.(*Page)
	} else {
		model, _ = page.Update(tea.MouseMsg{X: hit.X, Y: hit.Y + page.StripHeight() + page.BannerOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		page = model.(*Page)
	}
	afterSort := page.VisibleIDs()
	view := page.View()
	orderChanged := len(afterSort) == len(beforeSort)
	sameOrder := true
	for i := range beforeSort {
		if beforeSort[i] != afterSort[i] {
			sameOrder = false
			break
		}
	}
	if !orderChanged || (sameOrder && !strings.Contains(view, "↑") && !strings.Contains(view, "↓") && !strings.Contains(view, "▼") && !strings.Contains(view, "▲")) {
		// Sort must flip order or show a sort marker.
		if sameOrder {
			t.Fatalf("sort did not change order or show marker; before=%v after=%v", beforeSort, afterSort)
		}
	}

	// Keyboard intents
	page.SelectID("route-1")
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
		model, _ = page.Update(tc.key)
		page = model.(*Page)
		if page.LastIntent() != tc.want {
			t.Fatalf("key %v => intent=%q want %q", tc.key, page.LastIntent(), tc.want)
		}
	}

	// Mouse: footer new — hard-fail if hit missing.
	page.CancelOverlay()
	_ = page.View()
	newHit := page.Table().FooterActionHit(resourceview.ActionCreate)
	if newHit.Kind == 0 {
		t.Fatal("footer create hit missing after View() warm-up")
	}
	y := newHit.Y + page.StripHeight() + page.BannerOffset()
	model, _ = page.Update(tea.MouseMsg{X: newHit.X, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if page.LastIntent() != resourcepage.IntentCreate {
		t.Fatalf("footer new => intent=%q want create", page.LastIntent())
	}

	// Wheel paging moves cursor
	page.CancelOverlay()
	page.SelectID("route-1")
	before := page.SelectedID()
	if len(page.VisibleIDs()) < 2 {
		t.Fatal("need multiple visible rows for wheel paging")
	}
	model, _ = page.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5})
	page = model.(*Page)
	if page.SelectedID() == before {
		t.Fatalf("wheel down should change selection; still %q", before)
	}

	// Double-click details
	page.CancelOverlay()
	_ = page.View()
	rowY := page.TableHeaderY() + 1
	click := tea.MouseMsg{X: 2, Y: rowY + page.StripHeight() + page.BannerOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	model, _ = page.Update(click)
	page = model.(*Page)
	model, _ = page.Update(click)
	page = model.(*Page)
	if page.LastIntent() != resourcepage.IntentDetails && !page.ShowingDetails() {
		t.Fatalf("double-click should open details; intent=%q details=%v", page.LastIntent(), page.ShowingDetails())
	}
}

func TestDetailsShowsContractFields(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.SelectID("route-1")
	page.OpenDetails()
	view := page.View()
	for _, field := range []string{"id", "name", "ingress_protocol", "backend_set_id", "model_policy_id", "max_attempts"} {
		if !strings.Contains(strings.ToLower(view), field) {
			t.Fatalf("details missing field %q:\n%s", field, view)
		}
	}
}

func TestDetailsArrowScrollsOneLinePerKey(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.SelectID("route-1")
	page.OpenDetails()
	if page.overlayScroll != 0 {
		t.Fatalf("overlayScroll=%d want 0", page.overlayScroll)
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	if page.overlayScroll != 1 {
		t.Fatalf("one KeyDown: overlayScroll=%d want 1 (double-handled String+Type?)", page.overlayScroll)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyUp})
	page = model.(*Page)
	if page.overlayScroll != 0 {
		t.Fatalf("one KeyUp: overlayScroll=%d want 0", page.overlayScroll)
	}
}

func TestKeyboardDeleteOpensDependencyDialog(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.SelectID("route-1")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	view := page.View()
	if !strings.Contains(view, "default_route_id") {
		t.Fatalf("delete dialog missing inbound refs:\n%s", view)
	}
}
