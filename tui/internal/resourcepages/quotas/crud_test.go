package quotas

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	rpgenerated "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTableSuccessExactColumns(t *testing.T) {
	page, _ := newTestPage(t)
	view := page.View()
	for _, col := range []string{"ID", "NAME", "RPM", "MAX", "FOREGROUND", "BACKGROUND", "NEXT"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %q:\n%s", col, view)
		}
	}
	if page.State() != resourcepage.StateSuccess {
		t.Fatalf("state=%q want success", page.State())
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
					cmd.QuotaGroups = nil
				})
				p.Sync()
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
				p.Sync()
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

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                  "quota-new",
		"name":                "brand-new",
		"rpm":                 "120",
		"max_concurrency":     "4",
		"foreground_capacity": "2",
		"background_capacity": "1",
		"foreground_weight":   "9",
		"background_weight":   "1",
		"queue_timeout_ms":    "30000",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create save: %v", err)
	}
	found := false
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-new" && q.Name == "brand-new" && q.Rpm == 120 {
			found = true
		}
	}
	if !found {
		t.Fatalf("create did not mutate shared draft: %+v", draft.LocalCommand().QuotaGroups)
	}
	if !draft.DomainDirty(configdraft.DomainQuotas) {
		t.Fatal("expected DomainQuotas dirty after create")
	}

	page.SelectID("quota-new")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"name": "renamed-quota"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("edit save: %v", err)
	}
	found = false
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-new" && q.Name == "renamed-quota" {
			found = true
		}
	}
	if !found {
		t.Fatal("edit did not mutate draft")
	}

	page.SelectID("quota-new")
	if blocked, _ := page.TryDelete(); blocked {
		t.Fatal("unreferenced quota should open confirm dialog")
	}
	page.ConfirmDelete()
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-new" {
			t.Fatal("delete did not remove quota from draft")
		}
	}
}

func TestValidationFailureKeepsDraftAndSurfacesPath(t *testing.T) {
	page, draft := newTestPage(t)
	before := len(draft.LocalCommand().QuotaGroups)

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                  "quota-bad",
		"name":                "",
		"rpm":                 "120",
		"max_concurrency":     "4",
		"foreground_capacity": "2",
		"background_capacity": "1",
		"foreground_weight":   "9",
		"background_weight":   "1",
		"queue_timeout_ms":    "30000",
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
	if len(draft.LocalCommand().QuotaGroups) != before {
		t.Fatal("draft mutated on validation failure")
	}
}

func TestEditorIDEditableOnCreateReadOnlyOnEdit(t *testing.T) {
	page, _ := newTestPage(t)
	page.BeginCreate()
	if !page.EditorIDEditable() {
		t.Fatal("id should be editable on create")
	}
	page.CancelOverlay()

	page.SelectID("quota-default")
	page.BeginEdit()
	if page.EditorIDEditable() {
		t.Fatal("id should be read-only on edit")
	}
	desc, ok := resourcepage.Lookup(rpgenerated.ResourceQuotaGroup)
	if !ok {
		t.Fatal("missing quota_group descriptor")
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

func TestDefaultWeightsFromOpenAPI(t *testing.T) {
	page, draft := newTestPage(t)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                  "quota-weights",
		"name":                "weights",
		"rpm":                 "60",
		"max_concurrency":     "2",
		"foreground_capacity": "1",
		"background_capacity": "1",
		"queue_timeout_ms":    "30000",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-weights" {
			if q.ForegroundWeight != 9 || q.BackgroundWeight != 1 {
				t.Fatalf("defaults = fg %d bg %d", q.ForegroundWeight, q.BackgroundWeight)
			}
			return
		}
	}
	t.Fatal("quota-weights not found")
}

func TestDisconnectedSuppressesPublish(t *testing.T) {
	page, draft := newTestPage(t)
	draft.SetDisconnected(true)
	page.Sync()
	page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if page.LastIntent() == resourcepage.IntentPublish {
		t.Fatal("publish must be suppressed when disconnected")
	}
}

func TestEditorEnterAdvancesCtrlSSaves(t *testing.T) {
	page, draft := newTestPage(t)
	page.BeginCreate()
	if page.Editor().cursor != 0 {
		t.Fatalf("initial cursor=%d want 0", page.Editor().cursor)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if page.Editor().cursor == 0 {
		t.Fatal("enter should advance field cursor, not save")
	}
	before := len(draft.LocalCommand().QuotaGroups)
	page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if len(draft.LocalCommand().QuotaGroups) != before {
		t.Fatal("ctrl+s on incomplete form should not create quota without required fields")
	}

	page.SetEditorValues(map[string]string{
		"id":                  "quota-enter",
		"name":                "enter-test",
		"rpm":                 "60",
		"max_concurrency":     "2",
		"foreground_capacity": "1",
		"background_capacity": "1",
		"queue_timeout_ms":    "30000",
	})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(draft.LocalCommand().QuotaGroups) != before {
		t.Fatal("enter must not save complete form")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	found := false
	for _, q := range draft.LocalCommand().QuotaGroups {
		if q.Id == "quota-enter" {
			found = true
		}
	}
	if !found {
		t.Fatal("ctrl+s should save editor")
	}
}

func TestValidationCancelClearsForcedState(t *testing.T) {
	page, _ := newTestPage(t)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"id": "bad", "name": ""})
	_ = page.SaveEditor()
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("state=%q want validation_error", page.State())
	}
	page.CancelOverlay()
	page.Sync()
	if page.State() == resourcepage.StateValidationError {
		t.Fatal("cancel overlay should clear forced validation state")
	}
}

func TestRowsWithoutRuntimeShowCapacitiesAndDashNext(t *testing.T) {
	page, _ := newTestPage(t)
	page.SetRuntime(nil)
	page.Sync()
	view := page.View()
	if !strings.Contains(view, "quota-default") {
		t.Fatalf("missing row:\n%s", view)
	}
	if strings.Contains(view, "2026") {
		t.Fatalf("expected '-' NEXT without runtime, got timestamp in:\n%s", view)
	}
}
