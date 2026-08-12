package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDeleteAlreadyRemovedShowsStatusNotFalseSuccess(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = append(cmd.ContentPolicies, sampleContentPolicy("cp-orphan"))
	})
	page.Refresh()
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-orphan")
	page.beginDeleteIntent()
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm", page.overlay)
	}

	// External shared-draft removal while confirm is open.
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = filterContentPolicies(cmd.ContentPolicies, "cp-orphan")
	})
	view := page.View()
	// Live count is 1 (cp-1 only); id already gone → remaining must be 1, not 0 (count-1).
	if !strings.Contains(view, "Remaining after delete: 1") {
		t.Fatalf("expected remaining = live count 1 when id already gone:\n%s", view)
	}

	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayNone {
		t.Fatalf("overlay=%v want none after confirm of missing id", page.overlay)
	}
	if !strings.Contains(page.status, "already removed") {
		t.Fatalf("status=%q want already removed", page.status)
	}
	if !strings.Contains(page.status, "cp-orphan") {
		t.Fatalf("status=%q want resource id cp-orphan", page.status)
	}
	// cp-1 still present; orphan already gone — no false mutation.
	if !resourceExists(draft, KindContentPolicies, "cp-1") {
		t.Fatal("unrelated policy must remain")
	}
}

func TestConfirmDeleteRemainingUsesLivePresence(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = append(cmd.ContentPolicies,
			sampleContentPolicy("cp-a"),
			sampleContentPolicy("cp-b"),
		)
	})
	page.Refresh()
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-a")
	page.beginDeleteIntent()
	view := page.View()
	// cp-1 + cp-a + cp-b = 3 → remaining after deleting cp-a = 2
	if !strings.Contains(view, "Remaining after delete: 2") {
		t.Fatalf("want remaining 2 with three policies:\n%s", view)
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = filterContentPolicies(cmd.ContentPolicies, "cp-a")
	})
	view = page.View()
	// After external remove: live count 2; remaining should stay 2 (not 1).
	if !strings.Contains(view, "Remaining after delete: 2") {
		t.Fatalf("after external remove of cp-a, remaining should be live count 2:\n%s", view)
	}
}

func TestDeleteRouteBlockedByTransformScope(t *testing.T) {
	page, draft := newTestPage(t)
	// Remove profile default_route binding so only transform scopes the route.
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles = nil
		cmd.CompatibilityTransforms = []generated.CompatibilityTransformConfig{{
			Id:      "xf-route-scope",
			Name:    "scoped",
			Scope:   generated.Route,
			ScopeId: "route-1",
		}}
	})
	page.Refresh()
	page.SetKind(KindRoutes)
	page.SelectID("route-1")
	before := len(draft.LocalCommand().Routes)
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by transform scope_id")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "compatibility_transforms") || !strings.Contains(joined, "scope_id") {
		t.Fatalf("expected transform scope_id path, got %v", paths)
	}
	if len(draft.LocalCommand().Routes) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}
