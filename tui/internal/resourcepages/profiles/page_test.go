package profiles_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/profiles"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func fixtureDraft(t *testing.T) *configdraft.Draft {
	t.Helper()
	snap := configdraft.FixtureSnapshot()
	route := "route-a"
	proj := "proj-a"
	root := "/tmp/native-root"
	snap.MutableConfig.Routes = []generated.RouteConfig{{
		Id:                  generated.ConfigID(route),
		Name:                "Route A",
		BackendSetId:        "bs-a",
		ModelPolicyId:       "mp-a",
		IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:         1,
		MaxRequestBodyBytes: 1024,
		RequestDeadlineMs:   2000,
		RetryDeadlineMs:     1000,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 5000,
	}}
	snap.MutableConfig.ModelProjections = []generated.ModelProjectionConfig{{
		Id:            generated.ConfigID(proj),
		Name:          "Projection A",
		LogicalModels: []string{"gpt"},
	}}
	snap.MutableConfig.CompatibilityTransforms = []generated.CompatibilityTransformConfig{{
		Id:      "xf-a",
		Name:    "XF A",
		Scope:   generated.Route,
		ScopeId: generated.ConfigID(route),
	}}
	snap.MutableConfig.ClientProfiles = []generated.MutableClientProfile{{
		Id:                        "prof-a",
		Name:                      "Profile A",
		Launcher:                  generated.MutableClientProfileLauncherCodex,
		Arguments:                 []string{"--foo"},
		Environment:               []generated.EnvironmentVariableConfig{{Name: "FOO", Value: "1"}},
		DefaultRouteId:            generated.ConfigID(route),
		ModelProjectionId:         generated.ConfigID(proj),
		CompatibilityTransformIds: []generated.ConfigID{"xf-a"},
		NativeConfigRoot:          &root,
	}}
	return configdraft.Load(snap)
}

func typeRunes(page *profiles.Page, text string) {
	for _, r := range text {
		page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func focusField(t *testing.T, page *profiles.Page, name string) {
	t.Helper()
	for i := 0; i < 32; i++ {
		if page.CurrentField() == name {
			return
		}
		page.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("could not focus field %q (stuck on %q)", name, page.CurrentField())
}

func TestNewPageFactoryListsProfileColumns(t *testing.T) {
	page := profiles.NewPage(fixtureDraft(t), profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	view := ansi.Strip(page.View())
	for _, col := range []string{"ID", "NAME", "LAUNCHER", "DEFAULT_ROUTE", "MODEL_PROJECTION"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %s:\n%s", col, view)
		}
	}
	if !strings.Contains(view, "prof-a") || !strings.Contains(view, "Profile A") {
		t.Fatalf("missing profile row:\n%s", view)
	}
}

func TestProfilesKeyboardCreateEditDeleteCRUD(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(140, 40)
	page.Refresh()

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !page.Editing() {
		t.Fatal("create should open editor")
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "Create Profile") {
		t.Fatalf("missing create editor:\n%s", view)
	}

	focusField(t, page, "id")
	typeRunes(page, "prof-b")
	focusField(t, page, "name")
	typeRunes(page, "Profile B")
	focusField(t, page, "launcher")
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // empty → codex
	if !strings.Contains(ansi.Strip(page.View()), "codex") {
		t.Fatalf("first launcher cycle want codex:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // codex → claude
	if !strings.Contains(ansi.Strip(page.View()), "claude") {
		t.Fatalf("second launcher cycle want claude:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // claude → opencode
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // opencode → pi
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // pi → grok
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // grok → codex
	if !strings.Contains(ansi.Strip(page.View()), "codex") {
		t.Fatalf("launcher should wrap from grok to codex:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // codex → claude
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // claude → opencode
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !page.SelectingReference() {
		t.Fatal("expected reference selector")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter}) // choose route-a
	focusField(t, page, "model_projection_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	focusField(t, page, "compatibility_transform_ids")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggle xf-a
	page.Update(tea.KeyMsg{Type: tea.KeyEnter}) // finish multi-select
	focusField(t, page, "native_config_root")
	typeRunes(page, "/tmp/b")

	page.Update(tea.KeyMsg{Type: tea.KeyF2})
	if page.Editing() {
		t.Fatalf("editor should close after f2 save; status view:\n%s", page.View())
	}
	if !draft.DomainDirty(configdraft.DomainProfiles) {
		t.Fatal("profiles domain should be dirty")
	}
	var created *generated.MutableClientProfile
	for i := range draft.LocalCommand().ClientProfiles {
		if draft.LocalCommand().ClientProfiles[i].Id == "prof-b" {
			created = &draft.LocalCommand().ClientProfiles[i]
		}
	}
	if created == nil {
		t.Fatal("prof-b missing after keyboard create")
	}
	if created.Launcher != generated.MutableClientProfileLauncherOpencode {
		t.Fatalf("launcher cycle result=%q, want opencode", created.Launcher)
	}
	if created.Name != "Profile B" || created.NativeConfigRoot == nil || *created.NativeConfigRoot != "/tmp/b" {
		t.Fatalf("create content mismatch: %#v", created)
	}
	if string(created.DefaultRouteId) != "route-a" || string(created.ModelProjectionId) != "proj-a" {
		t.Fatalf("refs not saved: %#v", created)
	}
	if len(created.CompatibilityTransformIds) != 1 || created.CompatibilityTransformIds[0] != "xf-a" {
		t.Fatalf("compatibility transforms not saved: %#v", created.CompatibilityTransformIds)
	}

	// Edit via keyboard
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "id")
	typeRunes(page, "x") // must be ignored (read-only)
	editorView := ansi.Strip(page.View())
	if strings.Contains(editorView, "prof-ax") || !strings.Contains(editorView, "prof-a  (read-only)") {
		t.Fatalf("id read-only failed:\n%s", editorView)
	}
	focusField(t, page, "name")
	// clear and retype
	for range "Profile A" {
		page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeRunes(page, "Profile A2")
	page.Update(tea.KeyMsg{Type: tea.KeyF2})
	found := false
	for _, profile := range draft.LocalCommand().ClientProfiles {
		if profile.Id == "prof-a" && profile.Name == "Profile A2" {
			found = true
		}
	}
	if !found {
		t.Fatal("edit did not update shared draft")
	}

	// Delete unreferenced
	page.Refresh()
	page.SelectID("prof-b")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !page.ConfirmingDelete() {
		t.Fatal("delete should ask confirmation when unblocked")
	}
	confirmView := ansi.Strip(page.View())
	if !strings.Contains(confirmView, "Resulting resource count: 1") {
		t.Fatalf("confirm missing exact resource count:\n%s", confirmView)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, profile := range draft.LocalCommand().ClientProfiles {
		if profile.Id == "prof-b" {
			t.Fatal("prof-b should be deleted from draft")
		}
	}
}

func TestProfileIDFollowsNameUntilCustomized(t *testing.T) {
	page := profiles.NewPage(fixtureDraft(t), profiles.Options{})
	page.SetSize(120, 30)
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	requireNoErr(t, page.SetEditorField("name", "My Profile"))
	focusField(t, page, "id")
	if !strings.Contains(ansi.Strip(page.View()), "my-profile") {
		t.Fatalf("generated ID missing:\n%s", page.View())
	}
	requireNoErr(t, page.SetEditorField("id", "custom-profile"))
	requireNoErr(t, page.SetEditorField("name", "Renamed Profile"))
	if !strings.Contains(ansi.Strip(page.View()), "custom-profile") {
		t.Fatalf("manual ID was overwritten:\n%s", page.View())
	}
}

func TestProfilesShowsAllGeneratedFieldsIncludingNativeRoot(t *testing.T) {
	page := profiles.NewPage(fixtureDraft(t), profiles.Options{})
	page.SetSize(140, 40)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(page.View())
	desc, ok := resourcepage.Lookup("client_profile")
	if !ok {
		t.Fatal("missing client_profile descriptor")
	}
	for _, field := range desc.Fields {
		if !strings.Contains(view, field.Name) {
			t.Fatalf("details missing generated field %s:\n%s", field.Name, view)
		}
	}
	if !strings.Contains(view, "/tmp/native-root") {
		t.Fatalf("native_config_root value missing:\n%s", view)
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	editorFields := page.EditorFieldNames()
	for _, field := range desc.Fields {
		if !slices.Contains(editorFields, field.Name) {
			t.Fatalf("editor descriptor coverage missing %s: %#v", field.Name, editorFields)
		}
	}
}

func TestProfilesReferenceSelectorAndInvalidReferences(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(140, 40)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	refView := ansi.Strip(page.View())
	if !strings.Contains(refView, "Traffic route") || !strings.Contains(refView, "route-a") {
		t.Fatalf("selector missing choices:\n%s", refView)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	typeRunes(page, "nope")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter}) // no match keeps selector open
	if !strings.Contains(ansi.Strip(page.View()), "No matching options") {
		t.Fatalf("filter should narrow to empty:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc}) // clear filter
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	typeRunes(page, "route")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter}) // choose
	if page.SelectingReference() {
		t.Fatal("selector should close after choose")
	}
	if !strings.Contains(ansi.Strip(page.View()), "route-a") {
		t.Fatalf("selector did not write field:\n%s", page.View())
	}

	before := draft.LocalCommand().ClientProfiles[0]
	requireNoErr(t, page.SetEditorField("default_route_id", "missing-route"))
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected invalid default_route_id")
	} else if !strings.Contains(err.Error(), "$.default_route_id") || !strings.Contains(err.Error(), "invalid reference") {
		t.Fatalf("default_route_id error identity=%v", err)
	}
	if draft.LocalCommand().ClientProfiles[0].DefaultRouteId != before.DefaultRouteId {
		t.Fatal("invalid save mutated default_route_id")
	}
	requireNoErr(t, page.SetEditorField("default_route_id", "route-a"))
	before = draft.LocalCommand().ClientProfiles[0]
	requireNoErr(t, page.SetEditorField("model_projection_id", "missing-proj"))
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected invalid model_projection_id")
	} else if !strings.Contains(err.Error(), "$.model_projection_id") || !strings.Contains(err.Error(), "invalid reference") {
		t.Fatalf("model_projection_id error identity=%v", err)
	}
	if draft.LocalCommand().ClientProfiles[0].ModelProjectionId != before.ModelProjectionId {
		t.Fatal("invalid save mutated model_projection_id")
	}
	requireNoErr(t, page.SetEditorField("model_projection_id", "proj-a"))
	before = draft.LocalCommand().ClientProfiles[0]
	requireNoErr(t, page.SetEditorField("compatibility_transform_ids", "missing-xf"))
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected invalid compatibility_transform_ids")
	} else if !strings.Contains(err.Error(), "$.compatibility_transform_ids") || !strings.Contains(err.Error(), "invalid reference") {
		t.Fatalf("compatibility_transform_ids error identity=%v", err)
	}
	gotXF := draft.LocalCommand().ClientProfiles[0].CompatibilityTransformIds
	if len(gotXF) != len(before.CompatibilityTransformIds) {
		t.Fatalf("invalid save mutated compatibility_transform_ids: before=%v after=%v", before.CompatibilityTransformIds, gotXF)
	}
	for i := range before.CompatibilityTransformIds {
		if gotXF[i] != before.CompatibilityTransformIds[i] {
			t.Fatalf("invalid save mutated compatibility_transform_ids: before=%v after=%v", before.CompatibilityTransformIds, gotXF)
		}
	}
}

func TestProfilesBlockedDeleteDraftAndExternalReferrers(t *testing.T) {
	draft := fixtureDraft(t)
	// Point a transform at the profile inside the shared draft.
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.CompatibilityTransforms[0].Scope = generated.ClientProfile
		cmd.CompatibilityTransforms[0].ScopeId = "prof-a"
	})
	page := profiles.NewPage(draft, profiles.Options{
		Referrers: func(profileID string) []profiles.Referrer {
			if profileID == "prof-a" {
				return []profiles.Referrer{{Path: "sessions[id=ses_1].client_profile_id"}}
			}
			return nil
		},
	})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if page.ConfirmingDelete() {
		t.Fatal("blocked delete must not enter confirm-delete")
	}
	if !page.DeleteBlocked() {
		t.Fatal("expected blocked delete dialog")
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "Cannot delete profile prof-a") {
		t.Fatalf("blocked delete must show exact profile id:\n%s", view)
	}
	if !strings.Contains(view, "compatibility_transforms[0].scope_id") {
		t.Fatalf("missing draft referrer:\n%s", view)
	}
	if !strings.Contains(view, "sessions[id=ses_1].client_profile_id") {
		t.Fatalf("missing external referrer:\n%s", view)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	for _, profile := range draft.LocalCommand().ClientProfiles {
		if profile.Id == "prof-a" {
			return
		}
	}
	t.Fatal("blocked delete must keep profile in draft")
}

func TestProfilesValidationConflictDisconnectedDuplicate(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	focusField(t, page, "id")
	typeRunes(page, "BAD ID")
	focusField(t, page, "name")
	typeRunes(page, "x")
	focusField(t, page, "launcher")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	focusField(t, page, "model_projection_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	beforeCount := len(draft.LocalCommand().ClientProfiles)
	page.Update(tea.KeyMsg{Type: tea.KeyF2})
	if page.State() != resourcepage.StateValidationError || !page.Editing() {
		t.Fatalf("state=%q editing=%v", page.State(), page.Editing())
	}
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "lowercase letters") {
		t.Fatalf("missing validation error in editor:\n%s", page.View())
	}
	if len(draft.LocalCommand().ClientProfiles) != beforeCount {
		t.Fatal("failed create must not append to draft")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if page.State() == resourcepage.StateValidationError {
		t.Fatal("validation error must clear after cancel")
	}

	// Duplicate id
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	requireNoErr(t, page.SetEditorField("id", "prof-a"))
	requireNoErr(t, page.SetEditorField("name", "dup"))
	requireNoErr(t, page.SetEditorField("launcher", "codex"))
	requireNoErr(t, page.SetEditorField("default_route_id", "route-a"))
	requireNoErr(t, page.SetEditorField("model_projection_id", "proj-a"))
	if err := page.SaveEditor(); err == nil || !strings.Contains(err.Error(), "duplicate_id") {
		t.Fatalf("expected duplicate_id, got %v", err)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Invalid env pattern + reserved names
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	requireNoErr(t, page.SetEditorField("environment", "foo=1"))
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected env name pattern failure")
	}
	requireNoErr(t, page.SetEditorField("environment", `["PATH=/bin"]`))
	if err := page.SaveEditor(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved env, got %v", err)
	}
	requireNoErr(t, page.SetEditorField("environment", `["FOO_SECRET=x"]`))
	if err := page.SaveEditor(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved suffix, got %v", err)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})

	draft2 := fixtureDraft(t)
	draft2.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles[0].Name = "local"
	})
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft2.BaseView()
	cur.MutableConfig.ClientProfiles = []generated.MutableClientProfile{{
		Id:                        "prof-a",
		Name:                      "remote",
		Launcher:                  generated.MutableClientProfileLauncherCodex,
		Arguments:                 []string{},
		Environment:               []generated.EnvironmentVariableConfig{},
		DefaultRouteId:            "route-a",
		ModelProjectionId:         "proj-a",
		CompatibilityTransformIds: []generated.ConfigID{},
	}}
	cur.ActiveGeneration = 9
	draft2.BeginConflict(cur)
	page2 := profiles.NewPage(draft2, profiles.Options{})
	page2.SetSize(120, 30)
	page2.Refresh()
	if !strings.Contains(strings.ToLower(ansi.Strip(page2.View())), "conflict") {
		t.Fatalf("missing conflict UI:\n%s", page2.View())
	}

	draft3 := fixtureDraft(t)
	draft3.SetDisconnected(true)
	page3 := profiles.NewPage(draft3, profiles.Options{})
	page3.SetSize(120, 30)
	page3.Refresh()
	if !strings.Contains(ansi.Strip(page3.View()), "Disconnected") {
		t.Fatal("missing disconnected state")
	}
	intent, consumed := page3.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatalf("list publish must be unavailable while disconnected (intent=%q consumed=%v)", intent, consumed)
	}
	page3.SelectID("prof-a")
	page3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	intent, consumed = page3.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatalf("editor publish must be unavailable while disconnected (intent=%q consumed=%v)", intent, consumed)
	}
}

func TestProfilesEditorAndRefSelectCtrlSSavesAndApplies(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "name")
	for range "Profile A" {
		page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeRunes(page, "unsaved-name")
	intent, consumed := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !consumed || intent != resourcepage.IntentPublish {
		t.Fatalf("editor ctrl+s => intent=%q consumed=%v", intent, consumed)
	}
	if draft.LocalCommand().ClientProfiles[0].Name != "unsaved-name" {
		t.Fatalf("ctrl+s must save to Draft; draft name=%q", draft.LocalCommand().ClientProfiles[0].Name)
	}
	if page.Editing() {
		t.Fatal("successful ctrl+s should close the editor")
	}

	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !page.SelectingReference() {
		t.Fatal("expected ref select")
	}
	intent, consumed = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !consumed || intent != resourcepage.IntentPublish {
		t.Fatalf("ref-select ctrl+s => intent=%q consumed=%v", intent, consumed)
	}
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "search:") {
		t.Fatalf("expected filteringRef mode:\n%s", page.View())
	}
	intent, consumed = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !consumed || intent != resourcepage.IntentPublish {
		t.Fatalf("filteringRef ctrl+s => intent=%q consumed=%v", intent, consumed)
	}
}

func TestProfilesJSONListRoundTripAndEditMiss(t *testing.T) {
	draft := fixtureDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles[0].Arguments = []string{"a;b", "c"}
		cmd.ClientProfiles[0].Environment = []generated.EnvironmentVariableConfig{
			{Name: "MY_DIRS", Value: "/bin;/usr/bin"},
		}
	})
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("round-trip save: %v", err)
	}
	got := draft.LocalCommand().ClientProfiles[0]
	if len(got.Arguments) != 2 || got.Arguments[0] != "a;b" {
		t.Fatalf("arguments corrupted: %#v", got.Arguments)
	}
	if len(got.Environment) != 1 || got.Environment[0].Value != "/bin;/usr/bin" {
		t.Fatalf("environment corrupted: %#v", got.Environment)
	}

	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles = nil
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected edit-miss error")
	}
	if page.State() != resourcepage.StateValidationError || !page.Editing() {
		t.Fatalf("edit-miss should keep editor+validation, state=%q editing=%v", page.State(), page.Editing())
	}
	if strings.Contains(strings.ToLower(ansi.Strip(page.View())), "no longer") == false {
		t.Fatalf("missing error banner:\n%s", page.View())
	}
}

func TestProfilesSelectIDWorksBothDirections(t *testing.T) {
	draft := fixtureDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles = append(cmd.ClientProfiles, generated.MutableClientProfile{
			Id:                        "prof-b",
			Name:                      "B",
			Launcher:                  generated.MutableClientProfileLauncherClaude,
			Arguments:                 []string{},
			Environment:               []generated.EnvironmentVariableConfig{},
			DefaultRouteId:            "route-a",
			ModelProjectionId:         "proj-a",
			CompatibilityTransformIds: []generated.ConfigID{},
		})
	})
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-b")
	if page.Inner().SelectedID() != "prof-b" {
		t.Fatalf("selected=%q", page.Inner().SelectedID())
	}
	page.SelectID("prof-a")
	if page.Inner().SelectedID() != "prof-a" {
		t.Fatalf("selected=%q after upward select", page.Inner().SelectedID())
	}

	// Active filter must not trap SelectID.
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	typeRunes(page, "zzzz-no-match")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.SelectID("prof-b")
	if page.Inner().SelectedID() != "prof-b" {
		t.Fatalf("SelectID under filter selected %q", page.Inner().SelectedID())
	}
}

func TestProfilesStatusClearsOnDisconnectAndConflictResolve(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 9
	cur.ConfigRevision = "rev-conflict"
	draft.BeginConflict(cur)
	page.Refresh()
	if page.OverlayLines() < 2 {
		t.Fatalf("conflict overlay=%d", page.OverlayLines())
	}
	draft.SetDisconnected(true)
	page.Refresh()
	if page.OverlayLines() != 1 {
		t.Fatalf("disconnect overlay=%d, want 1 (stale status cleared)", page.OverlayLines())
	}

	draft2 := fixtureDraft(t)
	page2 := profiles.NewPage(draft2, profiles.Options{})
	page2.SetSize(120, 30)
	page2.Refresh()
	page2.SelectID("prof-a")
	page2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) // editor (renders status)
	draft2.BeginConflict(cur)
	page2.Refresh()
	if page2.OverlayLines() < 2 {
		t.Fatalf("editor conflict overlay=%d", page2.OverlayLines())
	}
	draft2.AcceptCurrent()
	page2.Refresh()
	if page2.State() == resourcepage.StateValidationError || page2.State() == resourcepage.StateStale {
		t.Fatalf("resolved conflict must clear stale/validation: state=%q view=%s", page2.State(), page2.View())
	}
	if page2.OverlayLines() != 0 {
		t.Fatalf("resolved conflict overlay=%d", page2.OverlayLines())
	}
	if strings.Contains(strings.ToLower(ansi.Strip(page2.View())), "conflict") {
		t.Fatalf("ghost conflict text remains:\n%s", page2.View())
	}
}

func TestProfilesEditorRebasesAfterAcceptCurrent(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "name")
	for range "Profile A" {
		page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	typeRunes(page, "stale-local")

	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.MutableConfig.ClientProfiles = []generated.MutableClientProfile{{
		Id:                        "prof-a",
		Name:                      "remote-accepted",
		Launcher:                  generated.MutableClientProfileLauncherCodex,
		Arguments:                 []string{},
		Environment:               []generated.EnvironmentVariableConfig{},
		DefaultRouteId:            "route-a",
		ModelProjectionId:         "proj-a",
		CompatibilityTransformIds: []generated.ConfigID{},
	}}
	cur.ActiveGeneration = 11
	cur.ConfigRevision = "rev-11"
	draft.BeginConflict(cur)
	page.Refresh() // sticky conflict status while editor open
	draft.AcceptCurrent()
	page.Refresh()
	if !page.Editing() {
		t.Fatal("editor should remain open after rebase")
	}
	if page.OverlayLines() != 0 {
		t.Fatalf("rebase must clear editor overlay, got %d", page.OverlayLines())
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "remote-accepted") {
		t.Fatalf("editor fields not rebased:\n%s", view)
	}
	if strings.Contains(strings.ToLower(view), "conflict") {
		t.Fatalf("sticky conflict status after rebase:\n%s", view)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyF2})
	if draft.LocalCommand().ClientProfiles[0].Name != "remote-accepted" {
		t.Fatalf("F2 after rebase wrote stale name %q", draft.LocalCommand().ClientProfiles[0].Name)
	}
}

func TestProfilesRebaseClearsValidationStatus(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "name")
	for range "Profile A" {
		page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	page.Update(tea.KeyMsg{Type: tea.KeyF2}) // validation error: name required
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("precondition: want validation error, got %q view=%s", page.State(), page.View())
	}
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "required") {
		t.Fatalf("precondition: missing validation status:\n%s", page.View())
	}

	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.MutableConfig.ClientProfiles = []generated.MutableClientProfile{{
		Id:                        "prof-a",
		Name:                      "remote-valid",
		Launcher:                  generated.MutableClientProfileLauncherCodex,
		Arguments:                 []string{},
		Environment:               []generated.EnvironmentVariableConfig{},
		DefaultRouteId:            "route-a",
		ModelProjectionId:         "proj-a",
		CompatibilityTransformIds: []generated.ConfigID{},
	}}
	cur.ActiveGeneration = 16
	cur.ConfigRevision = "rev-16"
	draft.BeginConflict(cur)
	draft.AcceptCurrent()
	page.Refresh()
	if !page.Editing() {
		t.Fatal("editor should remain open after rebase")
	}
	if page.State() == resourcepage.StateValidationError {
		t.Fatalf("rebase must clear sticky validation: state=%q view=%s", page.State(), page.View())
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "remote-valid") {
		t.Fatalf("fields not rebased:\n%s", view)
	}
	if strings.Contains(strings.ToLower(view), "error:") {
		t.Fatalf("sticky validation status survived rebase:\n%s", view)
	}
}

func TestProfilesOpenEditorKeepsConflictBanner(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 17
	cur.ConfigRevision = "rev-17"
	draft.BeginConflict(cur)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !page.Editing() {
		t.Fatal("expected editor open during conflict")
	}
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "conflict") {
		t.Fatalf("opening editor must retain conflict banner:\n%s", page.View())
	}

	// create path also retains the banner
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !page.Editing() {
		t.Fatal("expected create editor open during conflict")
	}
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "conflict") {
		t.Fatalf("create editor must retain conflict banner:\n%s", page.View())
	}
}

func TestProfilesRefSelectShowsConflictAndRefreshKeepsValidation(t *testing.T) {
	draft := fixtureDraft(t)
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "name")
	for range "Profile A" {
		page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	page.Update(tea.KeyMsg{Type: tea.KeyF2}) // validation sticky
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("precondition: validation state=%q", page.State())
	}

	// Enter ref-select with sticky validation, then conflict+Refresh in modeRefSelect.
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !page.SelectingReference() {
		t.Fatal("expected ref select")
	}
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 18
	cur.ConfigRevision = "rev-18"
	draft.BeginConflict(cur)
	page.Refresh()
	refView := strings.ToLower(ansi.Strip(page.View()))
	if !strings.Contains(refView, "conflict") {
		t.Fatalf("ref-select conflict refresh missing conflict:\n%s", page.View())
	}
	if !strings.Contains(refView, "required") {
		t.Fatalf("ref-select conflict refresh must keep validation:\n%s", page.View())
	}

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "search:") {
		t.Fatalf("expected filteringRef:\n%s", page.View())
	}
	page.Refresh() // still modeRefSelect + filteringRef
	filterView := strings.ToLower(ansi.Strip(page.View()))
	if !strings.Contains(filterView, "conflict") || !strings.Contains(filterView, "required") {
		t.Fatalf("filteringRef conflict refresh must keep both notices:\n%s", page.View())
	}
}

func TestProfilesListPublishGatedOnConflict(t *testing.T) {
	draft := fixtureDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles[0].Name = "local-dirty"
	})
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 8
	cur.ConfigRevision = "rev-8"
	draft.BeginConflict(cur)
	page.Refresh()
	if draft.CanPublish() {
		t.Fatal("precondition: conflict must disable CanPublish")
	}
	intent, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("list ctrl+s must not publish while CanPublish is false")
	}
}

func TestProfilesEditorAndRefSelectPublishGatedOnConflict(t *testing.T) {
	draft := fixtureDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles[0].Name = "local-dirty"
	})
	page := profiles.NewPage(draft, profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !page.Editing() {
		t.Fatal("precondition: must be in editor")
	}
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 14
	cur.ConfigRevision = "rev-14"
	draft.BeginConflict(cur)
	page.Refresh()
	if draft.CanPublish() {
		t.Fatal("precondition: conflict disables CanPublish")
	}
	intent, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("editor ctrl+s must not publish while CanPublish is false")
	}
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !page.SelectingReference() {
		t.Fatal("expected ref select")
	}
	intent, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("ref-select ctrl+s must not publish while CanPublish is false")
	}
	page.SelectID("prof-a")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	focusField(t, page, "default_route_id")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !strings.Contains(strings.ToLower(ansi.Strip(page.View())), "search:") {
		t.Fatalf("precondition: filteringRef not active:\n%s", page.View())
	}
	intent, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("filteringRef ctrl+s must not publish while CanPublish is false")
	}
}

func TestProfilesKeyboardMouseAndResponsiveGoldens(t *testing.T) {
	page := profiles.NewPage(fixtureDraft(t), profiles.Options{})
	page.SetSize(120, 30)
	page.Refresh()
	_ = page.View()

	hit := page.Inner().Table().FooterActionHit(resourceview.ActionEdit)
	page.Update(tea.MouseMsg{X: hit.X, Y: hit.Y + page.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !page.Editing() {
		t.Fatal("mouse edit path failed")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.Refresh()

	sizes := []struct{ w, h int }{{160, 45}, {120, 30}, {90, 30}, {70, 30}}
	for _, size := range sizes {
		page.SetSize(size.w, size.h)
		page.Refresh()
		got := ansi.Strip(page.View())
		if !strings.Contains(got, "ID") || !strings.Contains(got, "prof-a") {
			t.Fatalf("%dx%d lost identity:\n%s", size.w, size.h, got)
		}
		if strings.Contains(got, "/_") {
			t.Fatalf("%dx%d golden captured filter state:\n%s", size.w, size.h, got)
		}
		path := filepath.Join("testdata", "golden", "profiles_"+itoa(size.w)+"x"+itoa(size.h)+".ansi")
		assertOrUpdateGolden(t, path, got)
	}
}

func TestProfileFormResponsiveGoldens(t *testing.T) {
	page := profiles.NewPage(fixtureDraft(t), profiles.Options{})
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	requireNoErr(t, page.SetEditorField("name", "Coding"))
	for _, size := range []struct{ w, h int }{{160, 45}, {120, 30}, {90, 30}, {70, 30}} {
		page.SetSize(size.w, size.h)
		view := ansi.Strip(page.View())
		assertFormViewport(t, view, size.w)
		assertOrUpdateGolden(t, filepath.Join("testdata", "golden", "profile-form_"+itoa(size.w)+"x"+itoa(size.h)+".ansi"), view)
	}
}

func assertFormViewport(t *testing.T, view string, width int) {
	t.Helper()
	if !strings.Contains(view, "╭") || strings.Contains(view, "$.") {
		t.Fatalf("invalid form chrome:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > width {
			t.Fatalf("line wider than %d: %q", width, line)
		}
	}
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertOrUpdateGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run UPDATE_GOLDEN=1): %v\n--- got ---\n%s", path, err, got)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(got) {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
