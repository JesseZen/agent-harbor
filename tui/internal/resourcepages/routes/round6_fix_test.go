package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDeleteAlreadyRemovedStatusIncludesID(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = append(cmd.ContentPolicies, sampleContentPolicy("cp-orphan"))
	})
	page.Refresh()
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-orphan")
	page.beginDeleteIntent()
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = filterContentPolicies(cmd.ContentPolicies, "cp-orphan")
	})
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if !strings.Contains(page.status, "cp-orphan") {
		t.Fatalf("status=%q want resource id cp-orphan", page.status)
	}
	if !strings.Contains(page.status, "already removed") {
		t.Fatalf("status=%q want already removed", page.status)
	}
}

func TestConfirmDeleteViewShowsLiveInboundDeps(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = append(cmd.ContentPolicies, sampleContentPolicy("cp-inb"))
	})
	page.Refresh()
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-inb")
	page.beginDeleteIntent()
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm", page.overlay)
	}
	view := page.View()
	if !strings.Contains(view, "Dependent changes: none") {
		t.Fatalf("want none before inbound:\n%s", view)
	}
	if !strings.Contains(view, "Remaining after delete: 1") {
		// fixture has cp-1 + cp-inb = 2 → remaining 1
		t.Fatalf("want remaining 1 before inbound:\n%s", view)
	}

	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Routes {
			if string(cmd.Routes[i].Id) == "route-1" {
				id := generated.ConfigID("cp-inb")
				cmd.Routes[i].ContentPolicyId = &id
			}
		}
	})
	view = page.View()
	if strings.Contains(view, "Dependent changes: none") {
		t.Fatalf("confirm must show live inbound, not none:\n%s", view)
	}
	if !strings.Contains(view, "content_policy_id") {
		t.Fatalf("want inbound path painted:\n%s", view)
	}
	// Delete will not apply → remaining stays live count (2)
	if !strings.Contains(view, "Remaining after delete: 2") {
		t.Fatalf("want remaining = live count when blocked:\n%s", view)
	}
}

func TestConfirmDeleteRechecksTransformRouteScope(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles = nil
	})
	page.Refresh()
	page.SetKind(KindRoutes)
	page.SelectID("route-1")
	page.beginDeleteIntent()
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm", page.overlay)
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.CompatibilityTransforms = append(cmd.CompatibilityTransforms, generated.CompatibilityTransformConfig{
			Id:      "xf-race",
			Name:    "race",
			Scope:   generated.Route,
			ScopeId: "route-1",
		})
	})
	before := len(draft.LocalCommand().Routes)
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayDeps {
		t.Fatalf("overlay=%v want deps after transform scope appears", page.overlay)
	}
	joined := strings.Join(page.depsPaths, "\n")
	if !strings.Contains(joined, "scope_id") {
		t.Fatalf("want scope_id path, got %v", page.depsPaths)
	}
	if len(draft.LocalCommand().Routes) != before {
		t.Fatal("blocked confirm must not delete route")
	}
}

func TestLogicalModelsMouseSelectsWithoutCommittingFilter(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetSize(120, 30)
	page.SetKind(KindModelProjections)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "logical_models")
	page.editor.values["logical_models"] = "alpha,beta"
	page.editor.refFilter = "gamma"
	page.editor.refIndex = 1 // start on beta so a hit must change selection
	page.editor.selectorOpen = true

	contentH := page.height - stripHeight()
	layout := page.editor.formLayout(page.width, contentH, page.draft)
	clickY := -1
	for line, option := range layout.OptionLines {
		if option == 0 {
			clickY = stripHeight() + line
			break
		}
	}
	if clickY < 0 {
		t.Fatal("token optionIdx 0 missing from layout")
	}
	model, _ := page.Update(tea.MouseMsg{
		X: 4, Y: clickY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	page = model.(*Page)
	if page.editor.values["logical_models"] != "alpha,beta" {
		t.Fatalf("mouse must not commit filter; got %q", page.editor.values["logical_models"])
	}
	if page.editor.refIndex != 0 {
		t.Fatalf("refIndex=%d want 0 after clicking alpha (was 1)", page.editor.refIndex)
	}
	if page.editor.refFilter != "gamma" {
		t.Fatalf("refFilter=%q want gamma preserved", page.editor.refFilter)
	}
}

func TestMouseOpensRouteSelectorAndChoosesOption(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetSize(120, 30)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "backend_set_id")

	contentH := page.height - stripHeight()
	layout := page.editor.formLayout(page.width, contentH, page.draft)
	fieldY, ok := layout.FieldLines["backend_set_id"]
	if !ok {
		t.Fatal("backend_set_id missing from field hit map")
	}
	model, _ := page.Update(tea.MouseMsg{X: 40, Y: stripHeight() + fieldY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if !page.editor.selectorOpen {
		t.Fatal("clicking the route selector should open it")
	}

	layout = page.editor.formLayout(page.width, contentH, page.draft)
	optionY := -1
	for line, option := range layout.OptionLines {
		if option == 0 {
			optionY = stripHeight() + line
			break
		}
	}
	if optionY < 0 {
		t.Fatal("open route selector has no clickable option")
	}
	model, _ = page.Update(tea.MouseMsg{X: 40, Y: optionY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if page.editor.values["backend_set_id"] == "" || page.editor.selectorOpen {
		t.Fatalf("mouse selection value=%q open=%v", page.editor.values["backend_set_id"], page.editor.selectorOpen)
	}
}
