package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCRUDAllKindsCoverage(t *testing.T) {
	page, draft := newTestPage(t)

	// Content policy create/edit/delete (unreferenced)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()
	_ = page.View()
	page.SetEditorValues(map[string]string{
		"id":                   "cp-new",
		"mode":                 "audit",
		"max_inspection_bytes": "2048",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("cp create: %v", err)
	}
	page.SelectID("cp-new")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"mode": "block"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("cp edit: %v", err)
	}
	page.SelectID("cp-new")
	page = deleteViaConfirm(t, page)
	if resourceExists(draft, KindContentPolicies, "cp-new") {
		t.Fatal("cp-new should be deleted via confirm")
	}

	// Backend set create with real candidates then delete
	page.SetKind(KindBackendSets)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                      "bs-new",
		"name":                    "bs-new-name",
		"required_capabilities":   "chat",
		"candidates[0].target_id": "tgt-a",
		"candidates[0].priority":  "1",
		"candidates[0].weight":    "1",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("bs create: %v", err)
	}
	bs, ok := findBackendSet(draft.LocalCommand(), "bs-new")
	if !ok || len(bs.Candidates) != 1 || bs.Candidates[0].TargetId != "tgt-a" {
		t.Fatalf("bs candidates not saved: ok=%v %+v", ok, bs.Candidates)
	}
	page.SelectID("bs-new")
	page = deleteViaConfirm(t, page)
	if resourceExists(draft, KindBackendSets, "bs-new") {
		t.Fatal("bs-new should be deleted via confirm")
	}

	// Model policy create/delete
	page.SetKind(KindModelPolicies)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                         "mp-new",
		"name":                       "mp-new-name",
		"catalog_ttl_ms":             "1000",
		"discovery_timeout_ms":       "1000",
		"mappings[0].logical_model":  "logic",
		"mappings[0].physical_model": "phys",
		"mappings[0].target_id":      "tgt-a",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("mp create: %v", err)
	}
	page.SelectID("mp-new")
	page.BeginEdit()
	_ = page.View()
	page.SetEditorValues(map[string]string{"name": "mp-renamed"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("mp edit: %v", err)
	}
	mp, ok := findModelPolicy(draft.LocalCommand(), "mp-new")
	if !ok || mp.Name != "mp-renamed" {
		t.Fatalf("mp rename not persisted: ok=%v name=%q", ok, mp.Name)
	}
	page.SelectID("mp-new")
	page = deleteViaConfirm(t, page)
	if resourceExists(draft, KindModelPolicies, "mp-new") {
		t.Fatal("mp-new should be deleted via confirm")
	}

	// Model projection create/edit/delete
	page.SetKind(KindModelProjections)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"id": "proj-new", "name": "proj-new", "logical_models": "a,b"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("proj create: %v", err)
	}
	proj, ok := findModelProjection(draft.LocalCommand(), "proj-new")
	if !ok || len(proj.LogicalModels) != 2 {
		t.Fatalf("logical_models not saved: %+v", proj.LogicalModels)
	}
	page.SelectID("proj-new")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"name": "proj-renamed"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("proj edit: %v", err)
	}
	proj, ok = findModelProjection(draft.LocalCommand(), "proj-new")
	if !ok || proj.Name != "proj-renamed" {
		t.Fatalf("proj rename not persisted: ok=%v name=%q", ok, proj.Name)
	}
	page.SelectID("proj-new")
	page = deleteViaConfirm(t, page)
	if resourceExists(draft, KindModelProjections, "proj-new") {
		t.Fatal("proj-new should be deleted via confirm")
	}

	// Transform create/edit/delete
	page.SetKind(KindTransforms)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                          "xf-new",
		"name":                        "xf-new",
		"scope":                       "route",
		"scope_id":                    "route-1",
		"operation":                   "rename_model",
		"operation.source_model":      "src",
		"operation.destination_model": "dst",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("xf create: %v", err)
	}
	xf, ok := findTransform(draft.LocalCommand(), "xf-new")
	if !ok || xf.Operation.RenameModel == nil || xf.Operation.RenameModel.SourceModel != "src" {
		t.Fatalf("operation not saved: %+v", xf.Operation)
	}
	page.SelectID("xf-new")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"name": "xf-renamed"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("xf edit: %v", err)
	}
	xf, ok = findTransform(draft.LocalCommand(), "xf-new")
	if !ok || xf.Name != "xf-renamed" {
		t.Fatalf("xf rename not persisted: ok=%v name=%q", ok, xf.Name)
	}
	page.SelectID("xf-new")
	page = deleteViaConfirm(t, page)
	if resourceExists(draft, KindTransforms, "xf-new") {
		t.Fatal("xf-new should be deleted via confirm")
	}
}

func TestOverlayKeyboardAndStripClick(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetSize(120, 30)

	// Strip click must change kind away from Routes.
	_ = page.View()
	model, _ := page.Update(tea.MouseMsg{X: 20, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if page.Kind() == KindRoutes {
		t.Fatal("strip click should change kind away from Routes")
	}

	page.SetKind(KindRoutes)
	page.BeginCreate()
	_ = page.View()
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyTab})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.overlay != overlayNone {
		t.Fatal("esc should close editor")
	}

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                     "route-ov",
		"name":                   "route-ov",
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
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)

	page.SelectID("route-ov")
	page.OpenDetails()
	_ = page.View()
	if !page.ShowingDetails() {
		t.Fatal("expected details overlay")
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)

	page.SelectID("route-1")
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayDeps {
		t.Fatalf("blocked delete should open deps overlay, got %v", page.overlay)
	}
	_ = page.View()
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)

	// Unblocked delete via keyboard opens confirm, then enter applies.
	page.SelectID("route-ov")
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("unblocked delete should confirm, got %v", page.overlay)
	}
	_ = page.View()
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if resourceExists(page.draft, KindRoutes, "route-ov") {
		t.Fatal("confirm delete should remove route-ov")
	}
}

func TestHelpersAndReferenceOptions(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{{Id: "tgt-a", Name: "A"}, {Id: "tgt-b", Name: "B"}}
	})

	if Kind("").Label() != "" {
		t.Fatalf("empty kind label=%q", Kind("").Label())
	}
	if KindRoutes.Title() != "Routes" {
		t.Fatalf("routes title=%q", KindRoutes.Title())
	}
	if KindRoutes.DescriptorKind() == "" {
		t.Fatal("routes descriptor kind empty")
	}
	if kindIndex(Kind("nope")) != 0 {
		t.Fatal("unknown kind index should be 0")
	}
	if len(columnsFor(KindRoutes)) == 0 {
		t.Fatal("routes columns empty")
	}
	if len(rowsFor(draft, KindRoutes)) == 0 {
		t.Fatal("routes rows empty")
	}
	if refs := InboundRefs(draft, KindRoutes, "route-1"); len(refs) == 0 {
		t.Fatal("expected inbound refs for route-1")
	}

	if operationLabel(generated.CompatibilityTransformOperation{
		DropHeader: &generated.HeaderNameTransform{HeaderName: "x"},
	}) != "drop_header" {
		t.Fatal("drop_header label")
	}
	if operationLabel(generated.CompatibilityTransformOperation{
		SetHeader: &generated.SetHeaderTransform{HeaderName: "x", HeaderValue: "y"},
	}) != "set_header" {
		t.Fatal("set_header label")
	}
	if operationLabel(generated.CompatibilityTransformOperation{
		NormalizeStreamEvent: &generated.NormalizeStreamEventTransform{SourceEvent: "a", DestinationEvent: "b"},
	}) != "normalize_stream_event" {
		t.Fatal("normalize label")
	}
	if operationLabel(generated.CompatibilityTransformOperation{}) != "" {
		t.Fatal("empty operation label")
	}

	values := map[string]string{"scope": "route"}
	if opts := referenceOptions(draft, "backend_set_id", values); !containsString(opts, "bs-1") {
		t.Fatalf("backend_set_id opts=%v", opts)
	}
	if opts := referenceOptions(draft, "model_policy_id", values); !containsString(opts, "mp-1") {
		t.Fatalf("model_policy_id opts=%v", opts)
	}
	if opts := referenceOptions(draft, "content_policy_id", values); !containsString(opts, "cp-1") {
		t.Fatalf("content_policy_id opts=%v", opts)
	}
	if opts := referenceOptions(draft, "scope_id", values); !containsString(opts, "route-1") {
		t.Fatalf("scope_id route opts=%v", opts)
	}
	if opts := referenceOptions(draft, "other", values); len(opts) != 0 {
		t.Fatalf("unknown field opts=%v", opts)
	}
	if opts := referenceOptions(draft, "candidates[0].target_id", values); !containsString(opts, "tgt-a") {
		t.Fatalf("nested target_id opts=%v", opts)
	}

	if countFor(draft, KindRoutes) < 1 {
		t.Fatal("countFor routes")
	}
	for _, kind := range []Kind{KindRoutes, KindBackendSets, KindContentPolicies, KindModelPolicies, KindModelProjections, KindTransforms} {
		id := map[Kind]string{
			KindRoutes: "route-1", KindBackendSets: "bs-1", KindContentPolicies: "cp-1",
			KindModelPolicies: "mp-1", KindModelProjections: "proj-1", KindTransforms: "xf-1",
		}[kind]
		if !resourceExists(draft, kind, id) {
			t.Fatalf("expected %s %s to exist", kind, id)
		}
	}
	if resourceExists(draft, Kind(""), "x") {
		t.Fatal("unknown kind should not exist")
	}

	page.SetState(resourcepage.StateStale)
	if page.BannerOffset() < 1 {
		t.Fatalf("stale banner offset=%d", page.BannerOffset())
	}
	page.forceState = false
	page.Refresh()

	draft.SetDisconnected(true)
	page.Refresh()
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	if page.LastIntent() == resourcepage.IntentPublish {
		t.Fatal("disconnected publish suppressed")
	}

	draft.SetDisconnected(false)
	page.Refresh()
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                     "route-1",
		"name":                   "dup",
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
		t.Fatal("expected duplicate id error")
	}
	view := page.View()
	if !strings.Contains(view, "already exists") && page.State() != resourcepage.StateValidationError {
		t.Fatalf("expected validation presentation:\n%s", view)
	}
}

func TestValidateEdgeCases(t *testing.T) {
	_, draft := newTestPage(t)
	err := validateValues(KindRoutes, map[string]string{
		"id":                     "r",
		"name":                   "n",
		"ingress_protocol":       "nope",
		"routing_policy":         "automatic",
		"backend_set_id":         "bs-1",
		"model_policy_id":        "mp-1",
		"max_attempts":           "2",
		"max_request_body_bytes": "33554432",
		"request_deadline_ms":    "30000",
		"retry_deadline_ms":      "10000",
		"stream_idle_timeout_ms": "5000",
	}, true, draft)
	if err == nil || !strings.Contains(err.Error(), "ingress_protocol") {
		t.Fatalf("expected enum error, got %v", err)
	}
	err = validateValues(KindRoutes, map[string]string{
		"id":                     "r",
		"name":                   "n",
		"ingress_protocol":       "openai_chat",
		"routing_policy":         "automatic",
		"backend_set_id":         "bs-1",
		"model_policy_id":        "mp-1",
		"max_attempts":           "not-int",
		"max_request_body_bytes": "33554432",
		"request_deadline_ms":    "30000",
		"retry_deadline_ms":      "10000",
		"stream_idle_timeout_ms": "5000",
	}, true, draft)
	if err == nil {
		t.Fatal("expected integer error")
	}
	err = validateValues(KindRoutes, map[string]string{
		"id":                     "r",
		"name":                   "n",
		"ingress_protocol":       "openai_chat",
		"routing_policy":         "automatic",
		"backend_set_id":         "bs-1",
		"model_policy_id":        "mp-1",
		"max_attempts":           "99",
		"max_request_body_bytes": "33554432",
		"request_deadline_ms":    "30000",
		"retry_deadline_ms":      "10000",
		"stream_idle_timeout_ms": "5000",
	}, true, draft)
	if err == nil {
		t.Fatal("expected max bound error")
	}
	err = validateValues(KindBackendSets, map[string]string{
		"id":   "b",
		"name": "n",
	}, true, draft)
	if err == nil || !strings.Contains(err.Error(), "candidates") {
		t.Fatalf("expected candidates required, got %v", err)
	}
}

func TestBuildHelpersDirect(t *testing.T) {
	cp, err := buildContentPolicy(map[string]string{"id": "c", "mode": "redact"}, generated.ContentPolicyConfig{}, true)
	if err != nil || cp.Id != "c" {
		t.Fatalf("buildContentPolicy: %#v %v", cp, err)
	}
	proj, err := buildModelProjection(map[string]string{"id": "p", "name": "n", "logical_models": "a,b"}, generated.ModelProjectionConfig{}, true)
	if err != nil || proj.Name != "n" || len(proj.LogicalModels) != 2 {
		t.Fatalf("buildModelProjection: %#v %v", proj, err)
	}
	xf, err := buildTransform(map[string]string{
		"id": "x", "name": "n", "scope": "target", "scope_id": "t1",
		"operation": "rename_model", "operation.source_model": "a", "operation.destination_model": "b",
	}, generated.CompatibilityTransformConfig{}, true)
	if err != nil || xf.Scope != generated.Target || xf.Operation.RenameModel == nil {
		t.Fatalf("buildTransform: %#v %v", xf, err)
	}
	_, _ = hitTestStrip(0, 1, 80)
	k, ok := hitTestStrip(2, 0, 160)
	if !ok || k != KindTrafficRules {
		t.Fatalf("strip hit traffic rules: %q %v", k, ok)
	}
}
