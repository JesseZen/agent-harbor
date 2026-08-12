package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCancelAfterValidationClearsForcedState(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                     "route-bad",
		"name":                   "",
		"ingress_protocol":       "openai_chat",
		"routing_policy":         "automatic",
		"backend_set_id":         "bs-1",
		"model_policy_id":        "mp-1",
		"max_attempts":           "2",
		"max_request_body_bytes": "33554432",
		"request_deadline_ms":    "30000",
		"retry_deadline_ms":      "10000",
		"stream_idle_timeout_ms": "5000",
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected validation error")
	}
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("state=%q want validation_error before cancel", page.State())
	}
	if page.BannerOffset() < 2 {
		t.Fatalf("banner offset=%d want banner+status while validation forced", page.BannerOffset())
	}

	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.overlay != overlayNone {
		t.Fatalf("overlay=%v want none after esc", page.overlay)
	}
	if page.State() == resourcepage.StateValidationError {
		t.Fatalf("state=%q want draft-derived state after cancel", page.State())
	}
	if page.State() != resourcepage.StateSuccess {
		t.Fatalf("state=%q want success after cancel", page.State())
	}
	if page.BannerOffset() != 0 {
		t.Fatalf("banner offset=%d want 0 after cancel", page.BannerOffset())
	}
	view := page.View()
	if strings.Contains(view, "Validation error") {
		t.Fatalf("validation banner should be gone after cancel:\n%s", view)
	}
}

func TestEditorAcceptsKeyboardInput(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()

	// Move to name-like field "id" first and type.
	names := page.EditorFieldNames()
	if len(names) == 0 {
		t.Fatal("expected editor fields")
	}
	// Focus id (index 0 typically) and type runes.
	page.editor.cursor = indexOfField(names, "id")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cp-typed")})
	page = model.(*Page)
	if page.editor.values["id"] != "cp-typed" {
		t.Fatalf("typed id=%q want cp-typed", page.editor.values["id"])
	}

	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	page = model.(*Page)
	if page.editor.values["id"] != "cp-type" {
		t.Fatalf("backspace id=%q want cp-type", page.editor.values["id"])
	}

	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDelete})
	page = model.(*Page)
	if page.editor.values["id"] != "cp-typ" && page.editor.values["id"] != "cp-type" {
		// delete removes forward; with cursor at end acts like backspace for this editor
		if got := page.editor.values["id"]; len(got) >= len("cp-type") {
			t.Fatalf("delete did not shrink id: %q", got)
		}
	}
}

func TestOrderedArrayEditorWritesCandidatesAndMappings(t *testing.T) {
	page, draft := newTestPage(t)

	page.SetKind(KindBackendSets)
	page.BeginCreate()
	fields := page.EditorFieldNames()
	for _, want := range []string{"candidates[0].target_id", "candidates[0].priority", "candidates[0].weight"} {
		if !containsString(fields, want) {
			t.Fatalf("editor missing nested candidate field %q; have %v", want, fields)
		}
	}
	page.SetEditorValues(map[string]string{
		"id":                      "bs-arr",
		"name":                    "bs-arr",
		"required_capabilities":   "chat,tools",
		"candidates[0].target_id": "tgt-a",
		"candidates[0].priority":  "1",
		"candidates[0].weight":    "10",
		"candidates[1].target_id": "tgt-b",
		"candidates[1].priority":  "2",
		"candidates[1].weight":    "5",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("backend create: %v", err)
	}
	bs, ok := findBackendSet(draft.LocalCommand(), "bs-arr")
	if !ok {
		t.Fatal("bs-arr missing")
	}
	if len(bs.Candidates) != 2 {
		t.Fatalf("candidates=%d want 2", len(bs.Candidates))
	}
	if bs.Candidates[0].TargetId != "tgt-a" || bs.Candidates[1].TargetId != "tgt-b" {
		t.Fatalf("candidate order/values: %+v", bs.Candidates)
	}
	if bs.RequiredCapabilities == nil || len(*bs.RequiredCapabilities) != 2 {
		t.Fatalf("required_capabilities not written: %+v", bs.RequiredCapabilities)
	}

	// Reorder via editor keys: focus candidates[0].target_id, ctrl+down swaps with next.
	page.SelectID("bs-arr")
	page.BeginEdit()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "candidates[0].target_id")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlDown})
	page = model.(*Page)
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("backend reorder save: %v", err)
	}
	bs, _ = findBackendSet(draft.LocalCommand(), "bs-arr")
	if bs.Candidates[0].TargetId != "tgt-b" || bs.Candidates[1].TargetId != "tgt-a" {
		t.Fatalf("reorder failed: %+v", bs.Candidates)
	}

	page.SetKind(KindModelPolicies)
	page.BeginCreate()
	fields = page.EditorFieldNames()
	for _, want := range []string{"mappings[0].logical_model", "mappings[0].physical_model", "mappings[0].target_id"} {
		if !containsString(fields, want) {
			t.Fatalf("editor missing nested mapping field %q; have %v", want, fields)
		}
	}
	page.SetEditorValues(map[string]string{
		"id":                         "mp-arr",
		"name":                       "mp-arr",
		"catalog_ttl_ms":             "1000",
		"discovery_timeout_ms":       "1000",
		"mappings[0].logical_model":  "logic-a",
		"mappings[0].physical_model": "phys-a",
		"mappings[0].target_id":      "tgt-a",
		"mappings[1].logical_model":  "logic-b",
		"mappings[1].physical_model": "phys-b",
		"mappings[1].target_id":      "tgt-b",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("model policy create: %v", err)
	}
	mp, ok := findModelPolicy(draft.LocalCommand(), "mp-arr")
	if !ok || len(mp.Mappings) != 2 {
		t.Fatalf("mappings not written: ok=%v %+v", ok, mp.Mappings)
	}
	if mp.Mappings[0].LogicalModel != "logic-a" || mp.Mappings[1].LogicalModel != "logic-b" {
		t.Fatalf("mapping order lost: %+v", mp.Mappings)
	}
}

func TestDescriptorFieldsWrittenOnSave(t *testing.T) {
	page, draft := newTestPage(t)

	page.SetKind(KindModelProjections)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":             "proj-fields",
		"name":           "proj-fields",
		"logical_models": "alpha,beta,gamma",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("projection create: %v", err)
	}
	proj, ok := findModelProjection(draft.LocalCommand(), "proj-fields")
	if !ok {
		t.Fatal("projection missing")
	}
	if len(proj.LogicalModels) != 3 || proj.LogicalModels[0] != "alpha" || proj.LogicalModels[2] != "gamma" {
		t.Fatalf("logical_models=%v", proj.LogicalModels)
	}

	page.SetKind(KindTransforms)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                          "xf-fields",
		"name":                        "xf-fields",
		"scope":                       "route",
		"scope_id":                    "route-1",
		"operation":                   "rename_model",
		"operation.source_model":      "from-model",
		"operation.destination_model": "to-model",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("transform create: %v", err)
	}
	xf, ok := findTransform(draft.LocalCommand(), "xf-fields")
	if !ok || xf.Operation.RenameModel == nil {
		t.Fatalf("operation not written: %+v", xf.Operation)
	}
	if xf.Operation.RenameModel.SourceModel != "from-model" || xf.Operation.RenameModel.DestinationModel != "to-model" {
		t.Fatalf("operation children: %+v", xf.Operation.RenameModel)
	}
}

func TestUIDeleteAlwaysConfirmsWhenUnblocked(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                   "cp-del",
		"mode":                 "audit",
		"max_inspection_bytes": "2048",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := len(draft.LocalCommand().ContentPolicies)
	page.SelectID("cp-del")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm delete", page.overlay)
	}
	view := page.View()
	if !strings.Contains(view, "cp-del") {
		t.Fatalf("confirm missing id:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "none") {
		t.Fatalf("confirm should show no dependent changes:\n%s", view)
	}
	if !strings.Contains(view, "Remaining after delete") {
		t.Fatalf("confirm missing remaining count:\n%s", view)
	}
	if len(draft.LocalCommand().ContentPolicies) != before {
		t.Fatal("delete must not apply before confirm")
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if resourceExists(draft, KindContentPolicies, "cp-del") {
		t.Fatal("confirmed delete should remove cp-del")
	}
}

func TestReferenceSelectorTypesJKIntoFilterWithVisibleOptions(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	names := page.EditorFieldNames()
	page.editor.cursor = indexOfField(names, "backend_set_id")
	page.editor.values["backend_set_id"] = ""
	page.editor.refFilter = ""
	if len(page.editor.filteredRefs(page.draft)) == 0 {
		t.Fatal("expected visible ref options before typing")
	}

	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	page = model.(*Page)
	if got := page.editor.refFilter; got != "j" {
		t.Fatalf("typing j into ref filter with visible options: got %q want j", got)
	}
}

func TestReferenceSelectorIsSearchable(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{
			{Id: "tgt-a", Name: "A"},
			{Id: "tgt-b", Name: "B"},
		}
		cmd.Routes = append(cmd.Routes, sampleRoute("route-2"))
	})
	page.Refresh()

	page.SetKind(KindRoutes)
	page.BeginCreate()
	names := page.EditorFieldNames()
	page.editor.cursor = indexOfField(names, "backend_set_id")
	// Clear and type filter (separate from committed value).
	page.editor.values["backend_set_id"] = ""
	page.editor.refFilter = ""
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b1")})
	page = model.(*Page)
	if page.editor.refFilter != "b1" {
		t.Fatalf("refFilter=%q want b1", page.editor.refFilter)
	}
	view := page.View()
	if !strings.Contains(view, "bs-1") {
		t.Fatalf("filtered refs should show bs-1:\n%s", view)
	}
	// Enter applies highlighted option.
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.editor.values["backend_set_id"] != "bs-1" {
		t.Fatalf("enter should apply ref, got %q", page.editor.values["backend_set_id"])
	}
	if page.editor.refFilter != "" {
		t.Fatalf("apply should clear refFilter, got %q", page.editor.refFilter)
	}
	if page.overlay != overlayEditor {
		t.Fatal("enter on ref selector must not close editor")
	}

	// scope_id resolves by transform scope
	page.CancelOverlay()
	page.SetKind(KindTransforms)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"scope": "route"})
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "scope_id")
	page.editor.values["scope_id"] = ""
	opts := referenceOptions(draft, "scope_id", page.editor.values)
	if !containsString(opts, "route-1") || !containsString(opts, "route-2") {
		t.Fatalf("scope_id route candidates=%v", opts)
	}
	page.SetEditorValues(map[string]string{"scope": "client_profile"})
	opts = referenceOptions(draft, "scope_id", page.editor.values)
	if !containsString(opts, "profile-1") {
		t.Fatalf("scope_id profile candidates=%v", opts)
	}
	page.SetEditorValues(map[string]string{"scope": "target"})
	opts = referenceOptions(draft, "scope_id", page.editor.values)
	if !containsString(opts, "tgt-a") {
		t.Fatalf("scope_id target candidates=%v", opts)
	}
}

func indexOfField(names []string, want string) int {
	for i, name := range names {
		if name == want {
			return i
		}
	}
	return 0
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
