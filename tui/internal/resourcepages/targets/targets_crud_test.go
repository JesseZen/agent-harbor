package targets

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFactoryDefaultsToUpstreamsAndDocumentsAdvancedResources(t *testing.T) {
	draft := seedDraft(t)
	page := New(Deps{
		Draft:       draft,
		StageClient: &fakeStageHTTP{createFn: okCreate("s")},
		TargetStatus: StaticTargetStatusProvider{
			"tgt-1": {Health: string(generated.Healthy), Eligible: true},
		},
		Scope: "all",
	})
	if page.Kind() != KindUpstreams {
		t.Fatalf("default kind=%q want upstreams", page.Kind())
	}
	view := page.View()
	for _, label := range []string{"Upstreams", "Limit Policies"} {
		if !strings.Contains(view, label) {
			t.Fatalf("strip missing %s:\n%s", label, view)
		}
	}
	// User order: Upstreams (default) | Limit Policies. Advanced resources live in Spotlight.
	upstreamsIdx := strings.Index(view, "Upstreams")
	limitsIdx := strings.Index(view, "Limit Policies")
	if !(upstreamsIdx >= 0 && limitsIdx > upstreamsIdx) {
		t.Fatalf("strip order wrong: upstreams=%d limits=%d\n%s", upstreamsIdx, limitsIdx, view)
	}
}

func TestTargetsExactColumnsAndStatusProjection(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	view := page.View()
	for _, col := range []string{"ID", "NAME", "HEALTH", "ELIGIBLE", "ADAPTER", "ENDPOINT", "CREDENTIAL", "QUOTA"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %q:\n%s", col, view)
		}
	}
	plain := view
	if !strings.Contains(plain, "healthy") {
		t.Fatalf("HEALTH should come from TargetStatus provider:\n%s", plain)
	}
	if !strings.Contains(plain, "true") {
		t.Fatalf("ELIGIBLE should come from TargetStatus provider:\n%s", plain)
	}
	if page.State() != resourcepage.StateSuccess {
		t.Fatalf("state=%q want success", page.State())
	}
}

func TestTargetsUIStateBanners(t *testing.T) {
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
			page.SetKind(KindTargets)
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

func TestTargetsCreateEditDeleteMutateDraft(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                         "tgt-new",
		"name":                       "brand-new-target",
		"adapter":                    "openai",
		"bridge":                     "openai_chat",
		"capabilities":               "chat",
		"credential_id":              "cred-1",
		"endpoint_id":                "ep-1",
		"quota_group_id":             "quota-1",
		"failure_threshold":          "2",
		"initial_backoff_ms":         "100",
		"jitter_percent":             "10",
		"max_backoff_ms":             "1000",
		"probe_timeout_ms":           "1000",
		"recovery_success_threshold": "1",
		"stable_probe_interval_ms":   "5000",
		"default_cooling_ms":         "1000",
		"max_cooling_ms":             "5000",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create save: %v", err)
	}
	cmd := draft.LocalCommand()
	found := false
	for _, tgt := range cmd.Targets {
		if string(tgt.Id) == "tgt-new" && tgt.Name == "brand-new-target" &&
			tgt.Adapter == generated.MutableTargetCommandAdapterOpenai &&
			string(tgt.EndpointId) == "ep-1" && string(tgt.CredentialId) == "cred-1" &&
			string(tgt.QuotaGroupId) == "quota-1" &&
			tgt.HealthPolicy.FailureThreshold == 2 &&
			tgt.ThrottlePolicy.DefaultCoolingMs == 1000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("create did not mutate targets: %+v", cmd.Targets)
	}
	if !draft.DomainDirty(configdraft.DomainTargets) {
		t.Fatal("expected DomainTargets dirty after create")
	}

	page.SelectID("tgt-new")
	page.BeginEdit()
	if page.EditorIDEditable() {
		t.Fatal("id should be read-only on edit")
	}
	page.SetEditorValues(map[string]string{"name": "renamed-tgt", "bridge": "openai_responses"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("edit save: %v", err)
	}
	cmd = draft.LocalCommand()
	found = false
	for _, tgt := range cmd.Targets {
		if string(tgt.Id) == "tgt-new" && tgt.Name == "renamed-tgt" &&
			tgt.Bridge == generated.MutableTargetCommandBridgeOpenaiResponses {
			found = true
		}
	}
	if !found {
		t.Fatalf("edit did not mutate draft: %+v", cmd.Targets)
	}

	// Unreferenced target can delete (confirm path).
	page.SelectID("tgt-new")
	page = confirmTryDelete(t, page)
	for _, tgt := range draft.LocalCommand().Targets {
		if string(tgt.Id) == "tgt-new" {
			t.Fatal("delete did not remove target from draft")
		}
	}
}

func TestTargetsDeleteBlockedByBackendSetRef(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.BackendSets = []generated.BackendSetConfig{{
			Id:   "bs-1",
			Name: "primary",
			Candidates: []generated.BackendCandidate{{
				TargetId: "tgt-1",
				Priority: 1,
				Weight:   1,
			}},
		}}
	})
	before := len(draft.LocalCommand().Targets)

	page.SetKind(KindTargets)
	page.SelectID("tgt-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected delete blocked by BackendSet.candidates.target_id")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "backend_sets[") || !strings.Contains(joined, "target_id") {
		t.Fatalf("expected backend_sets target_id path, got %v", paths)
	}
	view := page.View()
	if !strings.Contains(view, "target_id") {
		t.Fatalf("dependency dialog missing path:\n%s", view)
	}
	if len(draft.LocalCommand().Targets) != before {
		t.Fatal("blocked delete must not mutate draft")
	}
}

func TestTargetsValidationFailureKeepsDraft(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	before := len(draft.LocalCommand().Targets)

	page.SetKind(KindTargets)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                         "tgt-bad",
		"name":                       "",
		"adapter":                    "openai",
		"bridge":                     "openai_chat",
		"capabilities":               "chat",
		"credential_id":              "cred-1",
		"endpoint_id":                "ep-1",
		"quota_group_id":             "quota-1",
		"failure_threshold":          "2",
		"initial_backoff_ms":         "100",
		"jitter_percent":             "10",
		"max_backoff_ms":             "1000",
		"probe_timeout_ms":           "1000",
		"recovery_success_threshold": "1",
		"stable_probe_interval_ms":   "5000",
		"default_cooling_ms":         "1000",
		"max_cooling_ms":             "5000",
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
	if len(draft.LocalCommand().Targets) != before {
		t.Fatal("draft mutated on validation failure")
	}
}

func TestTargetsEditorDescriptorFieldsAndNoGeneration(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	page.BeginCreate()
	if !page.EditorIDEditable() {
		t.Fatal("id should be editable on create")
	}
	desc, ok := resourcepage.Lookup(KindTargets.DescriptorKind())
	if !ok {
		t.Fatal("missing target descriptor")
	}
	fields := page.EditorFieldNames()
	for _, f := range desc.Fields {
		if len(f.Children) > 0 {
			for _, child := range f.Children {
				found := false
				for _, name := range fields {
					if name == child.Name {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("editor missing nested field %q; have %v", child.Name, fields)
				}
			}
			continue
		}
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
	for _, name := range fields {
		if name == "generation" || strings.Contains(name, "generation") {
			t.Fatalf("generation must not appear in target editor fields: %v", fields)
		}
	}
}

func TestTargetsDetailsShowsSchemaFields(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	page.SelectID("tgt-1")
	page.OpenDetails()
	view := page.View()
	desc, ok := resourcepage.Lookup(KindTargets.DescriptorKind())
	if !ok {
		t.Fatal("missing target descriptor")
	}
	for _, f := range desc.Fields {
		if len(f.Children) > 0 {
			for _, child := range f.Children {
				if !strings.Contains(view, child.Name) {
					t.Fatalf("details missing nested field %q:\n%s", child.Name, view)
				}
			}
			continue
		}
		if !strings.Contains(view, f.Name) {
			t.Fatalf("details missing descriptor field %q:\n%s", f.Name, view)
		}
	}
	if strings.Contains(view, "generation:") {
		t.Fatalf("details must not emit generation into command/schema pane:\n%s", view)
	}
}

func TestTargetsReferenceSelectorsCycleDraftIDs(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"credential_id": "cred-1"})
	for i, name := range page.EditorFieldNames() {
		if name == "credential_id" {
			page.editor.cursor = i
			break
		}
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	got := page.editor.values["credential_id"]
	if got != "cred-ext" {
		t.Fatalf("credential_id selector should choose cred-ext, got %q", got)
	}
}

func TestTargetEditorsSearchSelectorsAndSpaceToggles(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindUpstreams)
	page.BeginCreate()
	for i, name := range page.EditorFieldNames() {
		if name == "launcher" {
			page.editor.cursor = i
			break
		}
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cdx")})
	page = model.(*Page)
	if got := page.editor.filteredSelectorValues(); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("fuzzy launcher results=%v", got)
	}
	if !strings.Contains(page.editor.render(90, 30), i18n.T("form.select.search", map[string]string{"query": "cdx"})) {
		t.Fatal("selector should render the active search query")
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.editor.values["launcher"] != "codex" || page.editor.selectorOpen {
		t.Fatalf("launcher=%q open=%v", page.editor.values["launcher"], page.editor.selectorOpen)
	}

	page.CancelOverlay()
	page.SetKind(KindEndpoints)
	page.BeginCreate()
	for i, name := range page.EditorFieldNames() {
		if name == "http2_enabled" {
			page.editor.cursor = i
			break
		}
	}
	before := page.editor.values["http2_enabled"]
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeySpace})
	page = model.(*Page)
	if page.editor.values["http2_enabled"] == before {
		t.Fatal("Space should toggle boolean fields")
	}
}

func TestFixedTargetSelectsCycleWithLeftRightAndWrap(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindUpstreams)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"launcher": "codex", "api_formats": "openai_responses"})
	for i, name := range page.EditorFieldNames() {
		if name == "launcher" {
			page.editor.cursor = i
			break
		}
	}

	page.Update(tea.KeyMsg{Type: tea.KeyRight})
	if page.editor.values["launcher"] != "claude" || page.editor.values["api_formats"] != "openai_responses" {
		t.Fatalf("right cycle should change only the CLI: values=%v", page.editor.values)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if page.editor.values["launcher"] != "codex" {
		t.Fatalf("left cycle should wrap to codex, got %q", page.editor.values["launcher"])
	}
}

func TestMouseOpensTargetSelectorAndChoosesOption(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetSize(120, 30)
	page.SetKind(KindUpstreams)
	page.BeginCreate()
	for i, name := range page.EditorFieldNames() {
		if name == "launcher" {
			page.editor.cursor = i
			break
		}
	}

	contentH := page.height - stripHeight()
	layout := page.editor.formLayout(page.width, contentH)
	fieldY, ok := layout.FieldLines["launcher"]
	if !ok {
		t.Fatal("launcher missing from field hit map")
	}
	model, _ := page.Update(tea.MouseMsg{X: 40, Y: stripHeight() + fieldY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if !page.editor.selectorOpen {
		t.Fatal("clicking the target selector should open it")
	}

	layout = page.editor.formLayout(page.width, contentH)
	optionY := -1
	for line, option := range layout.OptionLines {
		if option == 1 {
			optionY = stripHeight() + line
			break
		}
	}
	if optionY < 0 {
		t.Fatal("open target selector has no second clickable option")
	}
	model, _ = page.Update(tea.MouseMsg{X: 40, Y: optionY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if page.editor.values["launcher"] != "claude" || page.editor.selectorOpen {
		t.Fatalf("mouse selection launcher=%q open=%v", page.editor.values["launcher"], page.editor.selectorOpen)
	}
}

func TestTargetsKeyboardAndMouseIntents(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	page.SetSize(120, 30)
	_ = page.View()
	page.SelectID("tgt-1")

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

func TestTargetsEditorSelectorsAndCapabilities(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"adapter":        "openai",
		"bridge":         "openai_chat",
		"capabilities":   "",
		"endpoint_id":    "ep-1",
		"quota_group_id": "quota-1",
	})
	for _, field := range []string{"adapter", "bridge", "capabilities", "endpoint_id", "quota_group_id"} {
		for i, name := range page.EditorFieldNames() {
			if name == field {
				page.editor.cursor = i
				break
			}
		}
		model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
		page = model.(*Page)
		if field == "capabilities" {
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeySpace})
			page = model.(*Page)
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
			page = model.(*Page)
		} else {
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
			page = model.(*Page)
			model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
			page = model.(*Page)
		}
	}
	if page.editor.values["adapter"] == "openai" {
		t.Fatal("adapter selector should cycle")
	}
	if page.editor.values["capabilities"] == "" {
		t.Fatal("capabilities selector should seed a value")
	}
	// Cycle capabilities again with a filled value.
	for i, name := range page.EditorFieldNames() {
		if name == "capabilities" {
			page.editor.cursor = i
			break
		}
	}
	before := page.editor.values["capabilities"]
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeySpace})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.editor.values["capabilities"] == before {
		t.Fatalf("capabilities should change on cycle, still %q", before)
	}
}

func TestTargetsDeleteBlockedByModelPolicyMapping(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ModelPolicies = []generated.ModelPolicyConfig{{
			Id:   "mp-1",
			Name: "map",
			Mappings: []generated.ModelMapping{{
				LogicalModel:  "frontier",
				PhysicalModel: "gpt",
				TargetId:      "tgt-1",
			}},
		}}
	})
	page.SetKind(KindTargets)
	page.SelectID("tgt-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected model_policies mapping inbound block")
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "model_policies[") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestTargetsStripAltNavigationFromDefault(t *testing.T) {
	draft := seedDraft(t)
	page := New(Deps{
		Draft:       draft,
		StageClient: &fakeStageHTTP{createFn: okCreate("s")},
		TargetStatus: StaticTargetStatusProvider{
			"tgt-1": {Health: string(generated.Healthy), Eligible: true},
		},
	})
	page.SetSize(120, 30)
	if page.Kind() != KindUpstreams {
		t.Fatalf("kind=%q", page.Kind())
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindLimitPolicies {
		t.Fatalf("alt-right kind=%q want limit_policies", page.Kind())
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindUpstreams {
		t.Fatalf("alt-right kind=%q want upstreams", page.Kind())
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindLimitPolicies {
		t.Fatalf("alt-left kind=%q want limit_policies", page.Kind())
	}
}

func validTargetEditorValues(id string) map[string]string {
	return map[string]string{
		"id":                         id,
		"name":                       "ok-target",
		"adapter":                    "openai",
		"bridge":                     "openai_chat",
		"capabilities":               "chat",
		"credential_id":              "cred-1",
		"endpoint_id":                "ep-1",
		"quota_group_id":             "quota-1",
		"failure_threshold":          "2",
		"initial_backoff_ms":         "100",
		"jitter_percent":             "10",
		"max_backoff_ms":             "1000",
		"probe_timeout_ms":           "1000",
		"recovery_success_threshold": "1",
		"stable_probe_interval_ms":   "5000",
		"default_cooling_ms":         "1000",
		"max_cooling_ms":             "5000",
	}
}

func TestTargetsEmptyReferenceSelectorConsumesEnter(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Credentials = nil
	})
	page.SetKind(KindTargets)
	page.BeginCreate()
	page.SetEditorValues(validTargetEditorValues("tgt-empty-ref"))
	// Non-empty value so a fallthrough Save would succeed today; Enter must not Save.
	page.SetEditorValues(map[string]string{"credential_id": "ghost-cred"})
	for i, name := range page.EditorFieldNames() {
		if name == "credential_id" {
			page.editor.cursor = i
			break
		}
	}
	if page.editor.applyFocusedSelector(draft) != true {
		t.Fatal("empty credential_id options must still return true (consume Enter)")
	}
	view := page.editor.render(90, 30)
	if !strings.Contains(view, i18n.T("form.select.none")) {
		t.Fatalf("expected empty selector hint, got:\n%s", view)
	}
	beforeTargets := len(draft.LocalCommand().Targets)
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayEditor {
		t.Fatalf("empty reference selector must consume Enter (not SaveEditor); overlay=%v", page.overlay)
	}
	if len(draft.LocalCommand().Targets) != beforeTargets {
		t.Fatal("Enter on empty credential selector must not mutate draft")
	}
}

func TestTargetsRejectUnknownCapabilities(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	before := len(draft.LocalCommand().Targets)
	page.SetKind(KindTargets)
	page.BeginCreate()
	values := validTargetEditorValues("tgt-bad-cap")
	values["capabilities"] = "chat,not_a_real_cap"
	page.SetEditorValues(values)
	err := page.SaveEditor()
	if err == nil {
		t.Fatal("expected unknown capability to fail Save")
	}
	if !strings.Contains(err.Error(), "$.capabilities") {
		t.Fatalf("expected typed path $.capabilities, got %v", err)
	}
	if len(draft.LocalCommand().Targets) != before {
		t.Fatal("invalid capabilities must not mutate draft")
	}
}

func TestTargetsRejectUnknownReferenceIDs(t *testing.T) {
	cases := []struct {
		field string
		value string
		path  string
	}{
		{"credential_id", "cred-missing", "$.credential_id"},
		{"endpoint_id", "ep-missing", "$.endpoint_id"},
		{"quota_group_id", "quota-missing", "$.quota_group_id"},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
			before := len(draft.LocalCommand().Targets)
			page.SetKind(KindTargets)
			page.BeginCreate()
			values := validTargetEditorValues("tgt-bad-ref-" + tc.field)
			values[tc.field] = tc.value
			page.SetEditorValues(values)
			err := page.SaveEditor()
			if err == nil {
				t.Fatalf("expected unknown %s to fail Save", tc.field)
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("expected typed path %s, got %v", tc.path, err)
			}
			if len(draft.LocalCommand().Targets) != before {
				t.Fatalf("unknown %s must not mutate draft", tc.field)
			}
		})
	}
}
