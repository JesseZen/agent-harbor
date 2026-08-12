package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	descgen "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEmptyCatalogGhostRefFailsValidate(t *testing.T) {
	_, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.BackendSets = nil
	})
	err := validateValues(KindRoutes, map[string]string{
		"id": "r", "name": "n",
		"ingress_protocol": "openai_chat", "routing_policy": "automatic",
		"backend_set_id": "ghost-bs", "model_policy_id": "mp-1",
		"max_attempts": "2", "max_request_body_bytes": "33554432",
		"request_deadline_ms": "30000", "retry_deadline_ms": "10000",
		"stream_idle_timeout_ms": "5000",
	}, true, draft)
	if err == nil || !strings.Contains(err.Error(), "unknown reference") {
		t.Fatalf("empty catalog + ghost ref must fail, got %v", err)
	}
}

func TestUniqueItemsRejectsDuplicates(t *testing.T) {
	err := validateValues(KindModelProjections, map[string]string{
		"id": "p", "name": "n", "logical_models": "a,a",
	}, true, nil)
	if err == nil || !strings.Contains(err.Error(), "logical_models") {
		t.Fatalf("expected uniqueItems error, got %v", err)
	}
	err = validateValues(KindBackendSets, map[string]string{
		"id": "b", "name": "n",
		"required_capabilities":   "chat,chat",
		"candidates[0].target_id": "tgt-a",
		"candidates[0].priority":  "1",
		"candidates[0].weight":    "1",
	}, true, nil)
	if err == nil || !strings.Contains(err.Error(), "required_capabilities") {
		t.Fatalf("expected uniqueItems error for capabilities, got %v", err)
	}
}

func TestNoInventedArraySeeds(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindBackendSets)
	page.BeginCreate()
	if got := page.editor.values["candidates[0].priority"]; got != "" {
		t.Fatalf("create must not invent priority=%q", got)
	}
	if got := page.editor.values["candidates[0].weight"]; got != "" {
		t.Fatalf("create must not invent weight=%q", got)
	}
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "candidates[0].target_id")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	page = model.(*Page)
	if got := page.editor.values["candidates[1].priority"]; got != "" {
		t.Fatalf("ctrl+n must not invent priority=%q", got)
	}
	if got := page.editor.values["candidates[1].weight"]; got != "" {
		t.Fatalf("ctrl+n must not invent weight=%q", got)
	}
}

func TestEmptyWeightBlocksSave(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().BackendSets)
	page.SetKind(KindBackendSets)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "bs-empty-w", "name": "bs-empty-w",
		"candidates[0].target_id": "tgt-a",
		"candidates[0].priority":  "1",
		"candidates[0].weight":    "",
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("empty weight must block Save")
	}
	if len(draft.LocalCommand().BackendSets) != before {
		t.Fatal("failed save must not mutate draft")
	}
}

func TestEditorOptionMouseAppliesCorrectIndex(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.BackendSets = append(cmd.BackendSets,
			sampleBackendSet("bs-2"),
			sampleBackendSet("bs-3"),
		)
	})
	page.SetKind(KindRoutes)
	page.SetSize(120, 40)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "backend_set_id")
	page.editor.selectorOpen = true
	_ = page.View()

	layout := page.editor.formLayout(page.width, page.height-stripHeight(), page.draft)
	if len(layout.OptionLines) == 0 {
		t.Fatal("expected option rows in layout")
	}
	// Click second option (index 1) — should be bs-2 after sorted? order is catalog order.
	opts := page.editor.filteredRefs(page.draft)
	if len(opts) < 2 {
		t.Fatalf("need ≥2 options, got %v", opts)
	}
	want := opts[1]
	clickY := -1
	for line, option := range layout.OptionLines {
		if option == 1 {
			clickY = stripHeight() + line
			break
		}
	}
	if clickY < 0 {
		t.Fatal("optionIdx 1 missing from layout")
	}
	model, _ := page.Update(tea.MouseMsg{
		X: 4, Y: clickY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	page = model.(*Page)
	if page.editor.values["backend_set_id"] != want {
		t.Fatalf("mouse applied %q want %q", page.editor.values["backend_set_id"], want)
	}
}

func TestConfirmFooterMouseHitsPaintedFooter(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetSize(120, 30)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "cp-footer", "mode": "audit", "max_inspection_bytes": "2048",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create: %v", err)
	}
	page.SelectID("cp-footer")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v", page.overlay)
	}
	view := page.View()
	lines := strings.Split(view, "\n")
	footerLine := -1
	for i, line := range lines {
		if strings.Contains(line, "enter confirm") {
			footerLine = i
			break
		}
	}
	if footerLine < 0 {
		t.Fatalf("painted footer missing:\n%s", view)
	}
	// Footer must be last viewport row (padded).
	if footerLine != page.height-1 {
		t.Fatalf("painted footer at Y=%d want last row %d", footerLine, page.height-1)
	}
	model, _ = page.Update(tea.MouseMsg{
		X: 10, Y: footerLine, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	page = model.(*Page)
	if resourceExists(draft, KindContentPolicies, "cp-footer") {
		t.Fatal("click on painted confirm footer should delete")
	}
}

func TestKeyboardE2ECreateRoutesAndBackendSets(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{
			{Id: "tgt-a", Name: "A"},
		}
	})

	// Routes: n → fill required → Ctrl+S
	page.SetKind(KindRoutes)
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	page = model.(*Page)
	if page.overlay != overlayEditor {
		t.Fatal("n should open create editor")
	}
	fillField := func(name, value string) {
		t.Helper()
		page.editor.cursor = indexOfField(page.EditorFieldNames(), name)
		page.editor.values[name] = ""
		page.editor.refFilter = ""
		if page.editor.focusedIsSelector(page.draft) {
			page.editor.refFilter = value
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
			page = model.(*Page)
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
			page = model.(*Page)
			return
		}
		for _, r := range value {
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			page = model.(*Page)
		}
	}
	fillField("id", "route-kbd")
	fillField("name", "route-kbd")
	fillField("ingress_protocol", "openai_chat")
	fillField("routing_policy", "automatic")
	fillField("backend_set_id", "bs-1")
	fillField("model_policy_id", "mp-1")
	fillField("request_deadline_ms", "30000")
	fillField("retry_deadline_ms", "10000")
	fillField("stream_idle_timeout_ms", "5000")
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	if !resourceExists(draft, KindRoutes, "route-kbd") {
		t.Fatalf("keyboard create route failed; err=%q overlay=%v", page.editor.err, page.overlay)
	}

	// BackendSets keyboard create
	page.SetKind(KindBackendSets)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	page = model.(*Page)
	fillField("id", "bs-kbd")
	fillField("name", "bs-kbd")
	fillField("candidates[0].target_id", "tgt-a")
	fillField("candidates[0].priority", "1")
	fillField("candidates[0].weight", "2")
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	if !resourceExists(draft, KindBackendSets, "bs-kbd") {
		t.Fatalf("keyboard create backend set failed; err=%q", page.editor.err)
	}
}

func TestPageLevelInvalidSaveNonRouteKinds(t *testing.T) {
	cases := []struct {
		kind Kind
		vals map[string]string
		path string
	}{
		{
			kind: KindBackendSets,
			vals: map[string]string{
				"id": "bs-bad", "name": "",
				"candidates[0].target_id": "tgt-a",
				"candidates[0].priority":  "1",
				"candidates[0].weight":    "1",
			},
			path: "$.name",
		},
		{
			kind: KindContentPolicies,
			vals: map[string]string{"id": "cp-bad", "mode": "nope", "max_inspection_bytes": "1048576"},
			path: "mode",
		},
		{
			kind: KindModelProjections,
			vals: map[string]string{"id": "proj-bad", "name": "n", "logical_models": ""},
			path: "logical_models",
		},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			page, draft := newTestPage(t)
			before := countFor(draft, tc.kind)
			page.SetKind(tc.kind)
			page.BeginCreate()
			page.SetEditorValues(tc.vals)
			err := page.SaveEditor()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("err=%v want path containing %q", err, tc.path)
			}
			if page.State() != resourcepage.StateValidationError {
				t.Fatalf("state=%q", page.State())
			}
			if countFor(draft, tc.kind) != before {
				t.Fatal("draft must be unchanged")
			}
		})
	}
}

func TestMappingsCtrlNMiddleRemoveRenumberAndSave(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{
			{Id: "tgt-a", Name: "A"}, {Id: "tgt-b", Name: "B"}, {Id: "tgt-c", Name: "C"},
		}
	})
	page.SetKind(KindModelPolicies)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "mp-map", "name": "mp-map",
		"catalog_ttl_ms": "1000", "discovery_timeout_ms": "1000",
		"mappings[0].logical_model":  "m0",
		"mappings[0].physical_model": "p0",
		"mappings[0].target_id":      "tgt-a",
	})
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "mappings[0].logical_model")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	page = model.(*Page)
	page.SetEditorValues(map[string]string{
		"mappings[1].logical_model":  "m1",
		"mappings[1].physical_model": "p1",
		"mappings[1].target_id":      "tgt-b",
		"mappings[2].logical_model":  "m2",
		"mappings[2].physical_model": "p2",
		"mappings[2].target_id":      "tgt-c",
	})
	// Remove middle row (index 1)
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "mappings[1].logical_model")
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	page = model.(*Page)
	if containsString(page.EditorFieldNames(), "mappings[2].logical_model") {
		t.Fatal("after middle remove should renumber to 2 rows")
	}
	if page.editor.values["mappings[1].logical_model"] != "m2" {
		t.Fatalf("renumber failed: got mappings[1]=%q want m2", page.editor.values["mappings[1].logical_model"])
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save after renumber: %v", err)
	}
	mp, ok := findModelPolicy(draft.LocalCommand(), "mp-map")
	if !ok || len(mp.Mappings) != 2 {
		t.Fatalf("saved mappings=%v", mp.Mappings)
	}
	if mp.Mappings[0].LogicalModel != "m0" || mp.Mappings[1].LogicalModel != "m2" {
		t.Fatalf("order=%+v", mp.Mappings)
	}
}

func TestCapabilitiesMultiSelectToggle(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindBackendSets)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "required_capabilities")
	if !page.editor.focusedIsSelector(page.draft) {
		t.Fatal("required_capabilities should be multi-select selector")
	}
	opts := page.editor.filteredRefs(page.draft)
	if !containsString(opts, "chat") || !containsString(opts, "tools") {
		t.Fatalf("capability options from generated enum, got %v", opts)
	}
	page.editor.refIndex = indexOfField(opts, "chat")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	page = model.(*Page)
	if !strings.Contains(page.editor.values["required_capabilities"], "chat") {
		t.Fatalf("space should toggle chat in, got %q", page.editor.values["required_capabilities"])
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	page = model.(*Page)
	if strings.Contains(page.editor.values["required_capabilities"], "chat") {
		t.Fatalf("space again should toggle chat out, got %q", page.editor.values["required_capabilities"])
	}
}

func TestLogicalModelsAddRemoveTokens(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindModelProjections)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "logical_models")
	page.editor.refFilter = "alpha"
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	page = model.(*Page)
	if page.editor.values["logical_models"] != "alpha" {
		t.Fatalf("ctrl+n should add token, got %q", page.editor.values["logical_models"])
	}
	page.editor.refFilter = "beta"
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	page = model.(*Page)
	if page.editor.values["logical_models"] != "alpha,beta" {
		t.Fatalf("got %q", page.editor.values["logical_models"])
	}
	page.editor.refIndex = 0
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	page = model.(*Page)
	if page.editor.values["logical_models"] != "beta" {
		t.Fatalf("ctrl+x should remove selected token, got %q", page.editor.values["logical_models"])
	}
}

func TestIntegerRejectsNonDigitsAndShowsBounds(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "max_attempts")
	page.editor.values["max_attempts"] = "2"
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	page = model.(*Page)
	if page.editor.values["max_attempts"] != "2" {
		t.Fatalf("non-digit must be rejected, got %q", page.editor.values["max_attempts"])
	}
	view := page.View()
	if !strings.Contains(view, "min=") && !strings.Contains(view, "max=") && !strings.Contains(view, "required") {
		t.Fatalf("focused integer should show constraint chrome:\n%s", view)
	}
}

func TestEditLoadEmptyOperationDoesNotInvent(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.CompatibilityTransforms = []generated.CompatibilityTransformConfig{{
			Id: "xf-empty-op", Name: "xf-empty-op", Scope: generated.Route, ScopeId: "route-1",
		}}
	})
	page.SetKind(KindTransforms)
	page.SelectID("xf-empty-op")
	page.BeginEdit()
	if got := page.editor.values["operation"]; got != "" {
		t.Fatalf("edit-load empty op must not invent %q", got)
	}
}

func TestEditorFieldNamesCoverDescriptorKinds(t *testing.T) {
	page, _ := newTestPage(t)
	for _, kind := range KindOrder {
		if kind == KindTrafficRules {
			continue
		}
		page.SetKind(kind)
		page.BeginCreate()
		names := page.EditorFieldNames()
		if len(names) == 0 {
			t.Fatalf("%s: empty EditorFieldNames", kind)
		}
		desc, ok := resourcepage.Lookup(kind.DescriptorKind())
		if !ok {
			t.Fatalf("%s: missing descriptor", kind)
		}
		for _, field := range desc.Fields {
			if field.Kind == descgen.FieldKindArray && len(field.Children) > 0 {
				want := field.Name + "[0]." + field.Children[0].Name
				if !containsString(names, want) {
					t.Fatalf("%s: missing array child %q in %v", kind, want, names)
				}
				continue
			}
			if field.Name == "operation" {
				if !containsString(names, "operation") {
					t.Fatalf("%s: missing operation", kind)
				}
				continue
			}
			if !containsString(names, field.Name) {
				t.Fatalf("%s: missing field %q in %v", kind, field.Name, names)
			}
		}
	}
}

func TestRemoveArrayItemClampsCursor(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindBackendSets)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"candidates[0].target_id": "tgt-a",
		"candidates[0].priority":  "1",
		"candidates[0].weight":    "1",
		"candidates[1].target_id": "tgt-b",
		"candidates[1].priority":  "2",
		"candidates[1].weight":    "2",
	})
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "candidates[1].weight")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	page = model.(*Page)
	name := page.editor.focusedName()
	if name == "id" || name == "" {
		t.Fatalf("cursor jumped to %q; want clamped on remaining candidate field", name)
	}
	if !strings.HasPrefix(name, "candidates[") {
		t.Fatalf("cursor=%q want candidates[*]", name)
	}
}
