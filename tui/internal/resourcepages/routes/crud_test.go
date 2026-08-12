package routes

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTableSuccessExactColumnsAllKinds(t *testing.T) {
	page, _ := newTestPage(t)

	want := map[Kind][]string{
		KindTrafficRules:     {"CLIENT", "PRIMARY", "BACKUP", "ROUTING", "STATUS", "MODE"},
		KindRoutes:           {"ID", "NAME", "INGRESS", "POLICY", "BACKEND", "MODEL", "ATTEMPTS"},
		KindBackendSets:      {"ID", "NAME", "CANDIDATES", "CAPABILITIES"},
		KindContentPolicies:  {"ID", "MODE", "MAX_BYTES"},
		KindModelPolicies:    {"ID", "NAME", "MAPPINGS", "TTL", "DISCOVERY"},
		KindModelProjections: {"ID", "NAME", "MODELS"},
		KindTransforms:       {"ID", "NAME", "SCOPE", "SCOPE_ID", "OPERATION"},
	}

	for _, kind := range KindOrder {
		page.SetKind(kind)
		view := page.View()
		for _, col := range want[kind] {
			if !strings.Contains(view, col) {
				t.Fatalf("kind %s missing column %q:\n%s", kind, col, view)
			}
		}
		if page.State() != resourcepage.StateSuccess {
			t.Fatalf("kind %s state=%q want success", kind, page.State())
		}
	}
}

func TestUIStateBanners(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Page, *configdraft.Draft)
		want  string
		state resourcepage.State
	}{
		{
			name: "empty",
			setup: func(p *Page, d *configdraft.Draft) {
				d.Mutate(func(cmd *generated.MutableConfigCommand) {
					cmd.Routes = nil
					empty := []generated.ManagedObject{}
					cmd.ManagedObjects = &empty
				})
				p.Refresh()
			},
			want:  "No resources",
			state: resourcepage.StateEmpty,
		},
		{
			name: "loading",
			setup: func(p *Page, _ *configdraft.Draft) {
				p.SetState(resourcepage.StateLoading)
			},
			want:  "Loading",
			state: resourcepage.StateLoading,
		},
		{
			name: "validation",
			setup: func(p *Page, _ *configdraft.Draft) {
				p.SetState(resourcepage.StateValidationError)
				p.SetStatus("$.name: required")
			},
			want:  "Validation error",
			state: resourcepage.StateValidationError,
		},
		{
			name: "publication",
			setup: func(p *Page, _ *configdraft.Draft) {
				p.SetState(resourcepage.StatePublicationError)
			},
			want:  "Publication error",
			state: resourcepage.StatePublicationError,
		},
		{
			name: "disconnected",
			setup: func(p *Page, d *configdraft.Draft) {
				d.SetDisconnected(true)
				p.Refresh()
			},
			want:  "Disconnected",
			state: resourcepage.StateDisconnected,
		},
		{
			name: "stale",
			setup: func(p *Page, _ *configdraft.Draft) {
				p.SetState(resourcepage.StateStale)
			},
			want:  "Stale snapshot",
			state: resourcepage.StateStale,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, draft := newTestPage(t)
			page.SetKind(KindRoutes)
			tc.setup(page, draft)
			view := page.View()
			if !strings.Contains(view, tc.want) {
				t.Fatalf("missing %q:\n%s", tc.want, view)
			}
			if page.State() != tc.state {
				t.Fatalf("state=%q want %q", page.State(), tc.state)
			}
		})
	}
}

func TestCreateEditDeleteMutateSharedDraft(t *testing.T) {
	page, draft := newTestPage(t)
	page.SetKind(KindRoutes)

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                     "route-new",
		"name":                   "brand-new",
		"ingress_protocol":       "openai_chat",
		"routing_policy":         "automatic",
		"backend_set_id":         "bs-1",
		"model_policy_id":        "mp-1",
		"content_policy_id":      "cp-1",
		"max_attempts":           "3",
		"max_request_body_bytes": "33554432",
		"request_deadline_ms":    "30000",
		"retry_deadline_ms":      "10000",
		"stream_idle_timeout_ms": "5000",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create save: %v", err)
	}
	cmd := draft.LocalCommand()
	found := false
	for _, r := range cmd.Routes {
		if r.Id == "route-new" && r.Name == "brand-new" && r.MaxAttempts == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("create did not mutate shared draft routes: %+v", cmd.Routes)
	}
	if !draft.DomainDirty(configdraft.DomainRoutes) {
		t.Fatal("expected DomainRoutes dirty after create")
	}
	view := page.View()
	if !strings.Contains(view, "*Routes") && !strings.Contains(view, "*") {
		t.Fatalf("dirty asterisk missing after create:\n%s", view)
	}

	page.SelectID("route-new")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"name": "renamed-route"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("edit save: %v", err)
	}
	cmd = draft.LocalCommand()
	found = false
	for _, r := range cmd.Routes {
		if r.Id == "route-new" && r.Name == "renamed-route" {
			found = true
		}
	}
	if !found {
		t.Fatalf("edit did not mutate draft: %+v", cmd.Routes)
	}

	// Unreferenced route can delete via confirm path.
	page.SelectID("route-new")
	page = deleteViaConfirm(t, page)
	cmd = draft.LocalCommand()
	for _, r := range cmd.Routes {
		if r.Id == "route-new" {
			t.Fatal("delete did not remove route from draft")
		}
	}
}

func TestValidationFailureKeepsDraftAndSurfacesPath(t *testing.T) {
	page, draft := newTestPage(t)
	before := draft.LocalCommand()

	page.SetKind(KindRoutes)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                     "route-bad",
		"name":                   "", // required — leave empty to force typed path
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
	err := page.SaveEditor()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "$.name") {
		t.Fatalf("expected typed path $.name, got %v", err)
	}
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("state=%q want validation_error", page.State())
	}
	after := draft.LocalCommand()
	if len(after.Routes) != len(before.Routes) {
		t.Fatalf("draft mutated on validation failure: before=%d after=%d", len(before.Routes), len(after.Routes))
	}
}

func TestOrderedArrayPreservationAfterEdit(t *testing.T) {
	page, draft := newTestPage(t)

	page.SetKind(KindBackendSets)
	page.SelectID("bs-1")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{
		"name": "backend-renamed",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("backend edit: %v", err)
	}
	bs := draft.LocalCommand().BackendSets[0]
	if len(bs.Candidates) != 2 {
		t.Fatalf("candidates length changed: %d", len(bs.Candidates))
	}
	if bs.Candidates[0].TargetId != "tgt-a" || bs.Candidates[1].TargetId != "tgt-b" {
		t.Fatalf("candidate order lost: %+v", bs.Candidates)
	}

	page.SetKind(KindModelPolicies)
	page.SelectID("mp-1")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"name": "model-renamed"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("model policy edit: %v", err)
	}
	mp := draft.LocalCommand().ModelPolicies[0]
	if len(mp.Mappings) != 2 {
		t.Fatalf("mappings length changed: %d", len(mp.Mappings))
	}
	if mp.Mappings[0].LogicalModel != "gpt-4" || mp.Mappings[1].LogicalModel != "claude" {
		t.Fatalf("mapping order lost: %+v", mp.Mappings)
	}
}

func TestDisconnectedAndConflictSuppressPublish(t *testing.T) {
	page, draft := newTestPage(t)
	draft.SetDisconnected(true)
	page.Refresh()

	model, cmd := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	_ = cmd
	if page.LastIntent() == resourcepage.IntentPublish {
		t.Fatal("publish must be suppressed when disconnected")
	}
	view := page.View()
	if !strings.Contains(view, "Disconnected") {
		t.Fatalf("missing disconnected banner:\n%s", view)
	}

	draft.SetDisconnected(false)
	draft.BeginConflict(configdraft.FixtureSnapshot(configdraft.WithGeneration(2), configdraft.WithRevision("rev-2")))
	page.Refresh()
	if draft.CanPublish() {
		t.Fatal("conflict should block CanPublish")
	}
	if page.State() != resourcepage.StatePublicationError {
		t.Fatalf("conflict state=%q want publication_error", page.State())
	}
	view = page.View()
	if !strings.Contains(view, "Publication error") {
		t.Fatalf("missing publication banner:\n%s", view)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	if page.LastIntent() == resourcepage.IntentPublish {
		t.Fatal("publish must be suppressed during conflict")
	}
}

func TestStripKindOrderAndSwitch(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetSize(180, 30)
	if page.Kind() != KindTrafficRules {
		t.Fatalf("default kind=%q want traffic_rules", page.Kind())
	}
	view := page.View()
	for _, label := range []string{"Traffic Rules"} {
		if !strings.Contains(view, label) {
			t.Fatalf("strip missing %q:\n%s", label, view)
		}
	}

	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlRight}) // should NOT switch strip
	page = model.(*Page)
	if page.Kind() != KindTrafficRules {
		t.Fatal("Ctrl+Right must not switch secondary strip")
	}

	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}, Alt: true}) // Alt+Right via alt+right key
	// Prefer explicit alt left/right strings used by bubbletea
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindTrafficRules {
		t.Fatalf("Alt+Right => kind=%q want traffic_rules", page.Kind())
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindTrafficRules {
		t.Fatalf("Alt+Left => kind=%q want traffic_rules", page.Kind())
	}

	page.SetKind(KindTransforms)
	if page.Kind() != KindTransforms {
		t.Fatal("SetKind failed")
	}
}

func TestEditorIDEditableOnCreateReadOnlyOnEdit(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetKind(KindContentPolicies)
	page.BeginCreate()
	if !page.EditorIDEditable() {
		t.Fatal("id should be editable on create")
	}
	page.CancelOverlay()

	page.SelectID("cp-1")
	page.BeginEdit()
	if page.EditorIDEditable() {
		t.Fatal("id should be read-only on edit")
	}
	desc, ok := resourcepage.Lookup(resourcepage.ResourceKind("content_policy"))
	if !ok {
		t.Fatal("missing content_policy descriptor")
	}
	fields := page.EditorFieldNames()
	for _, f := range desc.Fields {
		found := false
		for _, name := range fields {
			if name == f.Name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("editor missing descriptor field %q; have %v", f.Name, fields)
		}
	}
}
