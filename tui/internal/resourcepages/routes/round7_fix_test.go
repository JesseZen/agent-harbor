package routes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDeleteBlockedFooterAndRemainingVisible(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetSize(120, 30)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = append(cmd.ContentPolicies, sampleContentPolicy("cp-flood"))
	})
	page.Refresh()
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-flood")
	page.beginDeleteIntent()
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm", page.overlay)
	}

	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cp := generated.ConfigID("cp-flood")
		for i := 0; i < 40; i++ {
			r := sampleRoute(fmt.Sprintf("route-mid-%02d", i))
			r.ContentPolicyId = &cp
			cmd.Routes = append(cmd.Routes, r)
		}
	})
	view := page.View()
	if !strings.Contains(view, "Remaining after delete:") {
		t.Fatalf("remaining must stay visible with inbound flood:\n%s", view)
	}
	if !strings.Contains(view, "enter view blockers") {
		t.Fatalf("blocked confirm footer must not say enter confirm:\n%s", view)
	}
	if strings.Contains(view, "enter confirm") {
		t.Fatalf("misleading enter confirm footer with live deps:\n%s", view)
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	view = page.View() // View must not clamp scroll back to 0 via focusLine=0
	if page.overlayScroll != 1 {
		t.Fatalf("after View, overlayScroll=%d want 1 (focusLine clamp?)", page.overlayScroll)
	}
	if strings.Contains(view, "Delete Content Policies cp-flood?") {
		t.Fatalf("title should leave viewport after scroll:\n%s", view)
	}
	if !strings.Contains(view, "Remaining after delete:") {
		t.Fatalf("remaining still on line 1 should remain visible at scroll=1:\n%s", view)
	}
}
