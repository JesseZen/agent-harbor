package targets

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEndpointsExactColumns(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindEndpoints)
	view := page.View()
	for _, col := range []string{"ID", "NAME", "BASE_URL", "PROXY", "HTTP2", "PRIVATE"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %q:\n%s", col, view)
		}
	}
	if page.State() != resourcepage.StateSuccess {
		t.Fatalf("state=%q want success", page.State())
	}
}

func TestEndpointsUIStateBanners(t *testing.T) {
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
					cmd.Endpoints = nil
					cmd.Targets = nil
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
			page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
			page.SetKind(KindEndpoints)
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

func TestEndpointsCreateEditDeleteMutateDraft(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindEndpoints)

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                         "ep-new",
		"name":                       "brand-new",
		"base_url":                   "https://new.example.com",
		"proxy_url":                  "http://proxy:8080",
		"http2_enabled":              "true",
		"allow_private_network":      "false",
		"idle_connection_timeout_ms": "30000",
		"max_idle_connections":       "10",
		"tls_server_name":            "new.example.com",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create save: %v", err)
	}
	cmd := draft.LocalCommand()
	found := false
	for _, e := range cmd.Endpoints {
		if string(e.Id) == "ep-new" && e.Name == "brand-new" && e.BaseUrl == "https://new.example.com" &&
			e.Http2Enabled && e.ProxyUrl != nil && *e.ProxyUrl == "http://proxy:8080" &&
			e.IdleConnectionTimeoutMs == 30000 && e.MaxIdleConnections == 10 {
			found = true
		}
	}
	if !found {
		t.Fatalf("create did not mutate endpoints: %+v", cmd.Endpoints)
	}
	if !draft.DomainDirty(configdraft.DomainTargets) {
		t.Fatal("expected DomainTargets dirty after create")
	}

	page.SelectID("ep-new")
	page.BeginEdit()
	if page.EditorIDEditable() {
		t.Fatal("id should be read-only on edit")
	}
	page.SetEditorValues(map[string]string{"name": "renamed-ep", "http2_enabled": "false"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("edit save: %v", err)
	}
	cmd = draft.LocalCommand()
	found = false
	for _, e := range cmd.Endpoints {
		if string(e.Id) == "ep-new" && e.Name == "renamed-ep" && !e.Http2Enabled {
			found = true
		}
	}
	if !found {
		t.Fatalf("edit did not mutate draft: %+v", cmd.Endpoints)
	}

	// Unreferenced endpoint can delete (confirm path).
	page.SelectID("ep-new")
	page = confirmTryDelete(t, page)
	for _, e := range draft.LocalCommand().Endpoints {
		if string(e.Id) == "ep-new" {
			t.Fatal("delete did not remove endpoint from draft")
		}
	}
}

func TestEndpointsDeleteBlockedByTargetRef(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	before := len(draft.LocalCommand().Endpoints)

	page.SetKind(KindEndpoints)
	page.SelectID("ep-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by Target.endpoint_id")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "targets[") || !strings.Contains(joined, "endpoint_id") {
		t.Fatalf("expected target endpoint_id path, got %v", paths)
	}
	view := page.View()
	if !strings.Contains(view, "endpoint_id") {
		t.Fatalf("dependency dialog missing path:\n%s", view)
	}
	if len(draft.LocalCommand().Endpoints) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestEndpointsValidationFailureKeepsDraft(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	before := len(draft.LocalCommand().Endpoints)

	page.SetKind(KindEndpoints)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                         "ep-bad",
		"name":                       "",
		"base_url":                   "https://bad.example.com",
		"http2_enabled":              "true",
		"allow_private_network":      "false",
		"idle_connection_timeout_ms": "30000",
		"max_idle_connections":       "10",
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
	if len(draft.LocalCommand().Endpoints) != before {
		t.Fatal("draft mutated on validation failure")
	}
}

func TestEndpointsEditorDescriptorFields(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindEndpoints)
	page.BeginCreate()
	if !page.EditorIDEditable() {
		t.Fatal("id should be editable on create")
	}
	desc, ok := resourcepage.Lookup(KindEndpoints.DescriptorKind())
	if !ok {
		t.Fatal("missing endpoint descriptor")
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

func TestEndpointsDetailsShowsSchemaFields(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindEndpoints)
	page.SelectID("ep-1")
	page.OpenDetails()
	view := page.View()
	desc, ok := resourcepage.Lookup(KindEndpoints.DescriptorKind())
	if !ok {
		t.Fatal("missing endpoint descriptor")
	}
	for _, f := range desc.Fields {
		if !strings.Contains(view, f.Name) {
			t.Fatalf("details missing descriptor field %q:\n%s", f.Name, view)
		}
	}
	for _, field := range []string{"idle_connection_timeout_ms", "max_idle_connections", "tls_server_name"} {
		if !strings.Contains(view, field) {
			t.Fatalf("details missing %s:\n%s", field, view)
		}
	}
}

func TestEndpointsKeyboardAndMouseIntents(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindEndpoints)
	page.SetSize(120, 30)
	_ = page.View()
	page.SelectID("ep-1")

	cases := []struct {
		key  tea.KeyMsg
		want resourcepage.Intent
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, resourcepage.IntentCreate},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, resourcepage.IntentEdit},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, resourcepage.IntentDelete},
		{tea.KeyMsg{Type: tea.KeyEnter}, resourcepage.IntentDetails},
	}
	for _, tc := range cases {
		page.CancelOverlay()
		model, _ := page.Update(tc.key)
		page = model.(*Page)
		if page.LastIntent() != tc.want {
			t.Fatalf("key %v => intent=%q want %q", tc.key, page.LastIntent(), tc.want)
		}
	}
}
