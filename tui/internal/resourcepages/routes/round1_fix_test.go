package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSelectorEnterNeverSaves(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().Routes)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                     "route-enter",
		"name":                   "route-enter",
		"ingress_protocol":       "openai_chat",
		"routing_policy":         "automatic",
		"backend_set_id":         "no-such-bs",
		"model_policy_id":        "mp-1",
		"max_attempts":           "2",
		"max_request_body_bytes": "33554432",
		"request_deadline_ms":    "30000",
		"retry_deadline_ms":      "10000",
		"stream_idle_timeout_ms": "5000",
	})
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "backend_set_id")
	page.editor.refFilter = "zzz-no-match"

	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayEditor {
		t.Fatal("selector Enter must keep editor open")
	}
	if len(draft.LocalCommand().Routes) != before {
		t.Fatal("selector Enter must not SaveEditor / mutate draft")
	}
}

func TestClearStickyStatusOnCancelAndSave(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "x", "name": "",
		"ingress_protocol": "openai_chat", "routing_policy": "automatic",
		"backend_set_id": "bs-1", "model_policy_id": "mp-1",
		"max_attempts": "2", "max_request_body_bytes": "33554432",
		"request_deadline_ms": "30000", "retry_deadline_ms": "10000",
		"stream_idle_timeout_ms": "5000",
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected validation error")
	}
	if page.status == "" {
		t.Fatal("expected sticky status after validation failure")
	}
	if page.BannerOffset() < 2 {
		t.Fatalf("BannerOffset=%d want banner+status", page.BannerOffset())
	}

	page.CancelOverlay()
	if page.status != "" {
		t.Fatalf("CancelOverlay should clear status, got %q", page.status)
	}
	if page.BannerOffset() != 0 {
		t.Fatalf("BannerOffset=%d want 0 after cancel", page.BannerOffset())
	}

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "route-ok", "name": "ok",
		"ingress_protocol": "openai_chat", "routing_policy": "automatic",
		"backend_set_id": "bs-1", "model_policy_id": "mp-1",
		"max_attempts": "2", "max_request_body_bytes": "33554432",
		"request_deadline_ms": "30000", "retry_deadline_ms": "10000",
		"stream_idle_timeout_ms": "5000",
	})
	page.SetStatus("stale-error")
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if page.status != "" {
		t.Fatalf("successful SaveEditor should clear status, got %q", page.status)
	}
}

func TestConfirmDeleteRechecksInboundRefs(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "cp-race", "mode": "audit", "max_inspection_bytes": "2048",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create: %v", err)
	}
	page.SelectID("cp-race")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm", page.overlay)
	}
	// Shared-draft mutation adds a reference while confirm is open.
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Routes {
			if string(cmd.Routes[i].Id) == "route-1" {
				id := generated.ConfigID("cp-race")
				cmd.Routes[i].ContentPolicyId = &id
			}
		}
	})
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayDeps {
		t.Fatalf("overlay=%v want deps after re-check", page.overlay)
	}
	if !resourceExists(draft, KindContentPolicies, "cp-race") {
		t.Fatal("blocked confirm must not delete")
	}
}

func TestRequiredCSVEmptyFails(t *testing.T) {
	err := validateValues(KindModelProjections, map[string]string{
		"id": "p", "name": "n", "logical_models": ",",
	}, true, nil)
	if err == nil || !strings.Contains(err.Error(), "logical_models") {
		t.Fatalf("expected logical_models required for %q, got %v", ",", err)
	}
}

func TestEnumFieldsUseSelector(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "ingress_protocol")
	if !page.editor.focusedIsSelector(page.draft) {
		t.Fatal("ingress_protocol enum should be a selector")
	}
	opts := page.editor.filteredRefs(page.draft)
	if !containsString(opts, "openai_chat") || !containsString(opts, "anthropic_messages") {
		t.Fatalf("enum options=%v", opts)
	}
}

func TestCreateTransformDefaultsEmptyBlockSave(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().CompatibilityTransforms)
	page.SetKind(KindTransforms)
	page.BeginCreate()
	if got := page.editor.values["operation"]; got != "" {
		t.Fatalf("create must not invent operation=%q", got)
	}
	if got := page.editor.values["operation.source_model"]; got != "" {
		t.Fatalf("create must not invent source_model=%q", got)
	}
	page.SetEditorValues(map[string]string{
		"id": "xf-empty", "name": "xf-empty", "scope": "route", "scope_id": "route-1",
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("empty operation must block Save")
	}
	if len(draft.LocalCommand().CompatibilityTransforms) != before {
		t.Fatal("failed save must not mutate draft")
	}
}

func TestApplyRefShowsFullOptionList(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "backend_set_id")
	page.editor.refFilter = "bs"
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.editor.values["backend_set_id"] != "bs-1" {
		t.Fatalf("apply got %q", page.editor.values["backend_set_id"])
	}
	if page.editor.refFilter != "" {
		t.Fatalf("apply should clear filter, got %q", page.editor.refFilter)
	}
	opts := page.editor.filteredRefs(page.draft)
	if len(opts) < 1 || !containsString(opts, "bs-1") {
		t.Fatalf("after apply should show full options, got %v", opts)
	}
}

func TestReferenceMustExistInOptions(t *testing.T) {
	_, draft := newTestPage(t)
	err := validateValues(KindRoutes, map[string]string{
		"id": "r", "name": "n",
		"ingress_protocol": "openai_chat", "routing_policy": "automatic",
		"backend_set_id": "missing-bs", "model_policy_id": "mp-1",
		"max_attempts": "2", "max_request_body_bytes": "33554432",
		"request_deadline_ms": "30000", "retry_deadline_ms": "10000",
		"stream_idle_timeout_ms": "5000",
	}, true, draft)
	if err == nil || !strings.Contains(err.Error(), "backend_set_id") {
		t.Fatalf("expected dangling ref error, got %v", err)
	}
}

func TestConflictShowsPublicationBannerAndSuppressesPublish(t *testing.T) {
	page, draft := newTestPage(t)
	draft.BeginConflict(configdraft.FixtureSnapshot(configdraft.WithGeneration(2), configdraft.WithRevision("rev-2")))
	page.Refresh()
	if page.State() != resourcepage.StatePublicationError {
		t.Fatalf("state=%q want publication_error", page.State())
	}
	view := page.View()
	if !strings.Contains(view, "Publication error") {
		t.Fatalf("missing publication banner:\n%s", view)
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	if page.LastIntent() == resourcepage.IntentPublish {
		t.Fatal("publish must be suppressed during conflict")
	}
}

func TestApplyEditMissingIDErrors(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-1")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"mode": "block"})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = nil
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected not-found error when ID disappears before apply")
	}
}

func TestArrayAddRemoveAndMoveRefKeys(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{
			{Id: "tgt-a", Name: "A"},
			{Id: "tgt-b", Name: "B"},
		}
	})
	page.SetKind(KindBackendSets)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "candidates[0].target_id")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	page = model.(*Page)
	fields := page.EditorFieldNames()
	if !containsString(fields, "candidates[1].target_id") {
		t.Fatalf("ctrl+n should add candidate row; fields=%v", fields)
	}
	page.editor.cursor = indexOfField(fields, "candidates[1].target_id")
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	page = model.(*Page)
	if containsString(page.EditorFieldNames(), "candidates[1].target_id") {
		t.Fatal("ctrl+x should remove candidate row")
	}

	page.editor.cursor = indexOfField(page.EditorFieldNames(), "candidates[0].target_id")
	page.editor.refFilter = ""
	if len(page.editor.filteredRefs(page.draft)) < 2 {
		t.Fatal("expected target options for moveRef")
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	if page.editor.refIndex != 1 {
		t.Fatalf("down on selector refIndex=%d want 1", page.editor.refIndex)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyUp})
	page = model.(*Page)
	if page.editor.refIndex != 0 {
		t.Fatalf("up on selector refIndex=%d want 0", page.editor.refIndex)
	}
}

func TestConfirmDeleteMouseHit(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetSize(120, 30)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "cp-mouse", "mode": "audit", "max_inspection_bytes": "2048",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create: %v", err)
	}
	page.SelectID("cp-mouse")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v", page.overlay)
	}
	// Click left half of footer → confirm.
	footerY := page.height - 1 // strip + content footer ≈ last row
	model, _ = page.Update(tea.MouseMsg{
		X: 10, Y: footerY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	page = model.(*Page)
	if resourceExists(draft, KindContentPolicies, "cp-mouse") {
		t.Fatal("mouse confirm should delete cp-mouse")
	}
}
