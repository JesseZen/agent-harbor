package routes

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func simpleTrafficDraft(t *testing.T) *configdraft.Draft {
	t.Helper()
	draft := seedDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes[0].ContentPolicyId = nil
		cmd.ClientProfiles[0].CompatibilityTransformIds = []generated.ConfigID{}
		cmd.ModelProjections[0].LogicalModels = []string{"gpt-4"}
		cmd.ModelPolicies[0].Mappings = []generated.ModelMapping{{
			LogicalModel: "gpt-4", PhysicalModel: "gpt-4o", TargetId: "tgt-a",
		}}
		cmd.BackendSets[0].Candidates = []generated.BackendCandidate{{
			TargetId: "tgt-a", Priority: 0, Weight: 1,
		}}
		cmd.Targets = []generated.MutableTargetCommand{
			simpleTrafficTarget("tgt-a", "Primary"),
			simpleTrafficTarget("tgt-c", "Reserve"),
			simpleTrafficTarget("tgt-conflict", "Conflict"),
		}
		cmd.Endpoints = []generated.EndpointConfig{
			{Id: "ep-tgt-a", Name: "Primary", BaseUrl: "https://primary.example.com", Http2Enabled: true, MaxIdleConnections: 10, IdleConnectionTimeoutMs: 30000},
			{Id: "ep-tgt-c", Name: "Reserve", BaseUrl: "https://reserve.example.com", Http2Enabled: true, MaxIdleConnections: 10, IdleConnectionTimeoutMs: 30000},
			{Id: "ep-tgt-conflict", Name: "Conflict", BaseUrl: "https://conflict.example.com", Http2Enabled: true, MaxIdleConnections: 10, IdleConnectionTimeoutMs: 30000},
		}
		cmd.ModelPolicies[0].Mappings = append(cmd.ModelPolicies[0].Mappings, generated.ModelMapping{
			LogicalModel: "gpt-4", PhysicalModel: "gpt-4o", TargetId: "tgt-c",
		}, generated.ModelMapping{
			LogicalModel: "gpt-4", PhysicalModel: "different-model", TargetId: "tgt-conflict",
		})
		attachManagedTrafficFixture(cmd, []string{"tgt-a", "tgt-c", "tgt-conflict"})
	})
	return draft
}

func attachManagedTrafficFixture(cmd *generated.MutableConfigCommand, targetIDs []string) {
	attachManagedUpstreamFixture(cmd, targetIDs)
	objects := managedObjectsCopy(cmd)
	objects = filterManagedObjectsByKind(objects, generated.ManagedObjectKindTrafficRule)
	objects = append(objects,
		generated.ManagedObject{Id: "rule-test", Name: "profile-route-1", Kind: generated.ManagedObjectKindTrafficRule, Members: []generated.ManagedResourceRef{
			{Kind: generated.ManagedResourceRefKindClientProfile, Id: "profile-1"},
			{Kind: generated.ManagedResourceRefKindRoute, Id: "route-1"},
			{Kind: generated.ManagedResourceRefKindBackendSet, Id: "bs-1"},
			{Kind: generated.ManagedResourceRefKindModelPolicy, Id: "mp-1"},
			{Kind: generated.ManagedResourceRefKindModelProjection, Id: "proj-1"},
		}},
	)
	cmd.ManagedObjects = &objects
}

func attachManagedUpstreamFixture(cmd *generated.MutableConfigCommand, targetIDs []string) {
	objects := managedObjectsCopy(cmd)
	objects = filterManagedObjectsByKind(objects, generated.ManagedObjectKindUpstream)
	members := make([]generated.ManagedResourceRef, 0, len(targetIDs))
	for _, id := range targetIDs {
		members = append(members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindTarget, Id: generated.ConfigID(id)})
	}
	objects = append(objects, generated.ManagedObject{
		Id: "upstream-test", Name: "Test upstreams", Kind: generated.ManagedObjectKindUpstream, Members: members,
	})
	cmd.ManagedObjects = &objects
}

func filterManagedObjectsByKind(objects []generated.ManagedObject, remove generated.ManagedObjectKind) []generated.ManagedObject {
	out := objects[:0]
	for _, object := range objects {
		if object.Kind != remove {
			out = append(out, object)
		}
	}
	return out
}

func managedObjectsCopy(cmd *generated.MutableConfigCommand) []generated.ManagedObject {
	if cmd.ManagedObjects == nil {
		return nil
	}
	return append([]generated.ManagedObject(nil), (*cmd.ManagedObjects)...)
}

func simpleTrafficTarget(id, name string) generated.MutableTargetCommand {
	return generated.MutableTargetCommand{
		Id: id, Name: name, EndpointId: generated.ConfigID("ep-" + id),
		Adapter: generated.MutableTargetCommandAdapterOpenaiCompatible,
		Bridge:  generated.MutableTargetCommandBridgeOpenaiChat,
		Capabilities: []generated.MutableTargetCommandCapabilities{
			generated.MutableTargetCommandCapabilitiesChat,
			generated.MutableTargetCommandCapabilitiesStreaming,
		},
	}
}

func simpleResponsesTarget(id, name string) generated.MutableTargetCommand {
	return generated.MutableTargetCommand{
		Id: id, Name: name, EndpointId: generated.ConfigID("ep-" + id),
		Adapter: generated.MutableTargetCommandAdapterOpenaiCompatible,
		Bridge:  generated.MutableTargetCommandBridgeOpenaiResponses,
		Capabilities: []generated.MutableTargetCommandCapabilities{
			generated.MutableTargetCommandCapabilitiesResponses,
			generated.MutableTargetCommandCapabilitiesStreaming,
		},
	}
}

func TestCreateSimpleTrafficRuleBuildsRunnableBundle(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets, simpleResponsesTarget("target-primary", "Primary"))
		attachManagedUpstreamFixture(cmd, []string{"target-primary"})
	})
	page := New(draft)
	page.BeginCreate()
	if page.overlay != overlayEditor || page.editor.mode != editorCreate {
		t.Fatal("simple create did not open its editor")
	}
	page.SetEditorValues(map[string]string{
		"name": "Default Codex", "launcher": "codex",
		"routing": routingSingle, "primary_target_id": "target-primary",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save simple traffic rule: %v", err)
	}

	cmd := draft.LocalCommand()
	if len(cmd.BackendSets) != 1 || len(cmd.ModelPolicies) != 1 || len(cmd.ModelProjections) != 1 || len(cmd.Routes) != 1 || len(cmd.ClientProfiles) != 1 {
		t.Fatalf("generated counts: backends=%d policies=%d projections=%d routes=%d profiles=%d",
			len(cmd.BackendSets), len(cmd.ModelPolicies), len(cmd.ModelProjections), len(cmd.Routes), len(cmd.ClientProfiles))
	}
	if got := cmd.Routes[0].IngressProtocol; got != generated.RouteConfigIngressProtocolOpenaiResponses {
		t.Fatalf("ingress=%q want openai_responses", got)
	}
	if got := cmd.ClientProfiles[0].Launcher; got != generated.MutableClientProfileLauncherCodex {
		t.Fatalf("launcher=%q want codex", got)
	}
	if len(cmd.ModelPolicies[0].Mappings) != 1 || cmd.ModelPolicies[0].Mappings[0].LogicalModel != wildcardModel || cmd.ModelPolicies[0].Mappings[0].PhysicalModel != wildcardModel {
		t.Fatalf("preserve mappings=%#v", cmd.ModelPolicies[0].Mappings)
	}
	if len(cmd.ModelProjections[0].LogicalModels) != 0 {
		t.Fatalf("preserve projection=%#v", cmd.ModelProjections[0].LogicalModels)
	}
	rules := trafficRules(cmd, nil)
	if len(rules) != 1 || !rules[0].Editable || rules[0].Mode != "Managed" {
		t.Fatalf("generated simple rule=%#v", rules)
	}
}

func TestEmptySimpleTrafficRulesOpenCreateEditorFromKeyboard(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	page := New(draft)
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	page = model.(*Page)
	if page.overlay != overlayEditor || page.editor.mode != editorCreate {
		t.Fatal("n did not open the simple traffic rule creator")
	}
	view := strings.ToLower(page.View())
	for _, field := range []string{"name", "cli", "model handling", "primary upstream"} {
		if !strings.Contains(view, field) {
			t.Fatalf("create view missing %q:\n%s", field, view)
		}
	}
}

func TestCreateSimpleTrafficRuleSupportsFailoverAndUniqueIDs(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets,
			simpleResponsesTarget("target-primary", "Primary"),
			simpleResponsesTarget("target-backup", "Backup"),
		)
		attachManagedUpstreamFixture(cmd, []string{"target-primary", "target-backup"})
	})
	values := map[string]string{
		"name": "Coding", "launcher": "codex", "model_strategy": modelPreserve,
		"routing": routingFailover, "primary_target_id": "target-primary", "backup_target_id": "target-backup",
	}
	if err := createSimpleTrafficRule(draft, values); err != nil {
		t.Fatal(err)
	}
	if err := createSimpleTrafficRule(draft, values); err != nil {
		t.Fatal(err)
	}
	cmd := draft.LocalCommand()
	if cmd.Routes[0].Id == cmd.Routes[1].Id || cmd.ClientProfiles[0].Id == cmd.ClientProfiles[1].Id {
		t.Fatalf("generated IDs collided: routes=%#v profiles=%#v", cmd.Routes, cmd.ClientProfiles)
	}
	if candidates := cmd.BackendSets[0].Candidates; len(candidates) != 2 || candidates[0].Priority != 0 || candidates[1].Priority != 1 {
		t.Fatalf("failover candidates=%#v", candidates)
	}
	if got := len(cmd.ModelPolicies[0].Mappings); got != 2 {
		t.Fatalf("mapping count=%d want 2", got)
	}
}

func TestCreateSimpleTrafficRuleOverridesModelsPerTarget(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets,
			simpleResponsesTarget("target-primary", "Primary"),
			simpleResponsesTarget("target-backup", "Backup"),
		)
		attachManagedUpstreamFixture(cmd, []string{"target-primary", "target-backup"})
	})
	err := createSimpleTrafficRule(draft, map[string]string{
		"name": "Override", "launcher": "codex", "routing": routingFailover,
		"primary_target_id": "target-primary", "backup_target_id": "target-backup",
		"model_strategy": modelOverride, "primary_upstream_model": "model-a", "backup_upstream_model": "model-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	mappings := draft.LocalCommand().ModelPolicies[0].Mappings
	if len(mappings) != 2 || mappings[0].LogicalModel != wildcardModel || mappings[0].PhysicalModel != "model-a" || mappings[1].PhysicalModel != "model-b" {
		t.Fatalf("override mappings=%#v", mappings)
	}
}

func TestCreateSimpleTrafficRuleMapsClientModelsPerTarget(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets,
			simpleResponsesTarget("target-primary", "Primary"),
			simpleResponsesTarget("target-backup", "Backup"),
		)
		attachManagedUpstreamFixture(cmd, []string{"target-primary", "target-backup"})
	})
	err := createSimpleTrafficRule(draft, map[string]string{
		"name": "Mapped", "launcher": "codex", "routing": routingFailover,
		"primary_target_id": "target-primary", "backup_target_id": "target-backup", "model_strategy": modelMap,
		"model_mappings[0].client_model": "opus", "model_mappings[0].primary_model": "model-a", "model_mappings[0].backup_model": "backup-a",
		"model_mappings[1].client_model": "sonnet", "model_mappings[1].primary_model": "model-b", "model_mappings[1].backup_model": "backup-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := draft.LocalCommand()
	if got := cmd.ModelProjections[0].LogicalModels; !reflect.DeepEqual(got, []string{"opus", "sonnet"}) {
		t.Fatalf("projection=%#v", got)
	}
	if len(cmd.ModelPolicies[0].Mappings) != 4 {
		t.Fatalf("mappings=%#v", cmd.ModelPolicies[0].Mappings)
	}
	rules := trafficRules(cmd, nil)
	if len(rules) != 1 || !rules[0].Editable {
		t.Fatalf("mapped rule should remain simple: %#v", rules)
	}
}

func TestMapEditorUsesPairRowsAndDiscoveredModelSelector(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets, simpleResponsesTarget("target-primary", "Primary"))
		attachManagedUpstreamFixture(cmd, []string{"target-primary"})
	})
	discover := func(_ context.Context, targetID string) ([]string, error) {
		if targetID != "target-primary" {
			t.Fatalf("targetID=%q", targetID)
		}
		return []string{"model-a", "model-b"}, nil
	}
	page := New(draft, Options{DiscoverModels: discover})
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"model_strategy": modelMap, "primary_target_id": "target-primary",
		"model_mappings[0].client_model": "claude-opus", "model_mappings[0].primary_model": "",
	})
	cmd := page.ensureEditorModelCatalogs()
	if cmd == nil {
		t.Fatal("expected model discovery command")
	}
	page.Update(cmd())
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "model_mappings[0].primary_model")
	page.editor.selectorOpen = true
	view := strings.ToLower(page.View())
	for _, text := range []string{"map 1 / cli model", "map 1 / primary", "model-a", "model-b"} {
		if !strings.Contains(view, text) {
			t.Fatalf("map editor missing %q:\n%s", text, view)
		}
	}
	if !page.editor.addArrayItem() || !slices.Contains(page.EditorFieldNames(), "model_mappings[1].client_model") {
		t.Fatalf("ctrl+n model row fields=%#v", page.EditorFieldNames())
	}
}

func TestFixedTrafficRuleSelectsCycleWithLeftRightAndWrap(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	page := New(draft)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "launcher")

	page.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := page.editor.values["launcher"]; got != "claude" {
		t.Fatalf("launcher right=%q want claude", got)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := page.editor.values["launcher"]; got != "codex" {
		t.Fatalf("launcher should wrap to codex, got %q", got)
	}

	page.editor.cursor = indexOfField(page.EditorFieldNames(), "model_strategy")
	page.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := page.editor.values["model_strategy"]; got != modelMap {
		t.Fatalf("model strategy left should wrap to map, got %q", got)
	}
	if !slices.Contains(page.EditorFieldNames(), "model_mappings[0].client_model") {
		t.Fatal("cycling to map should initialize mapping fields")
	}
}

func TestTypingInTrafficRuleSelectStartsSearchWithoutMutatingValue(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	page := New(draft)
	page.BeginCreate()
	page.editor.cursor = indexOfField(page.EditorFieldNames(), "launcher")

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cld")})
	if got := page.editor.values["launcher"]; got != "codex" {
		t.Fatalf("typing in select mutated value to %q", got)
	}
	if !page.editor.selectorOpen || page.editor.refFilter != "cld" {
		t.Fatalf("select search open=%v filter=%q", page.editor.selectorOpen, page.editor.refFilter)
	}
	if got := page.editor.filteredRefs(draft); len(got) != 1 || got[0] != "claude" {
		t.Fatalf("launcher fuzzy results=%v", got)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := page.editor.values["launcher"]; got != "claude" {
		t.Fatalf("searched launcher selection=%q want claude", got)
	}
}

func TestCreateSimpleTrafficRuleRejectsUnsupportedLauncherAndProtocol(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets, simpleTrafficTarget("target-chat", "Chat"))
		attachManagedUpstreamFixture(cmd, []string{"target-chat"})
	})
	base := map[string]string{
		"name": "Rule", "routing": routingSingle, "primary_target_id": "target-chat",
	}
	base["launcher"] = "opencode"
	if err := createSimpleTrafficRule(draft, base); err == nil || !strings.Contains(err.Error(), "supported simple launchers") {
		t.Fatalf("unsupported launcher error=%v", err)
	}
	base["launcher"] = "codex"
	if err := createSimpleTrafficRule(draft, base); err == nil || !strings.Contains(err.Error(), "compatible upstream") {
		t.Fatalf("incompatible protocol error=%v", err)
	}
}

func TestTrafficRuleModesPreserveModelMappings(t *testing.T) {
	draft := simpleTrafficDraft(t)
	rules := trafficRules(draft.LocalCommand(), StaticRouteStatusProvider{
		"route-1": {EligibleTargetIDs: []string{"tgt-a"}},
	})
	if len(rules) != 1 || !rules[0].Editable || rules[0].Routing != "Single" || rules[0].Status != "1/1 eligible" {
		t.Fatalf("simple rule=%#v", rules)
	}
	compatible := compatibleTrafficTargets(draft, "profile-1")
	if !slices.Contains(compatible, "tgt-a") || !slices.Contains(compatible, "tgt-c") {
		t.Fatalf("compatible targets=%v", compatible)
	}
	if !slices.Contains(compatible, "tgt-conflict") {
		t.Fatalf("protocol-compatible target should remain available for single mode: %v", compatible)
	}
	before := draft.LocalCommand()

	if err := applyTrafficRule(draft, "profile-1", map[string]string{
		"routing": routingFailover, "primary_target_id": "tgt-a", "backup_target_id": "tgt-c",
	}); err != nil {
		t.Fatalf("apply failover: %v", err)
	}
	cmd := draft.LocalCommand()
	candidates := cmd.BackendSets[0].Candidates
	if len(candidates) != 2 || candidates[0].Priority != 0 || candidates[1].Priority != 1 {
		t.Fatalf("failover candidates=%#v", candidates)
	}
	if !reflect.DeepEqual(before.ModelPolicies, cmd.ModelPolicies) || !reflect.DeepEqual(before.Routes, cmd.Routes) || !reflect.DeepEqual(before.ContentPolicies, cmd.ContentPolicies) || !reflect.DeepEqual(before.CompatibilityTransforms, cmd.CompatibilityTransforms) {
		t.Fatal("traffic rule edit changed advanced route configuration")
	}
	rules = trafficRules(cmd, nil)
	if len(rules) != 1 || !rules[0].Editable || rules[0].Routing != "Failover" {
		t.Fatalf("post-save rule=%#v", rules)
	}

	if err := applyTrafficRule(draft, "profile-1", map[string]string{
		"routing": routingBalance, "primary_target_id": "tgt-a", "backup_target_id": "tgt-c",
	}); err != nil {
		t.Fatalf("apply load balance: %v", err)
	}
	candidates = draft.LocalCommand().BackendSets[0].Candidates
	if candidates[0].Priority != 0 || candidates[1].Priority != 0 {
		t.Fatalf("load balance candidates=%#v", candidates)
	}
}

func TestTrafficRuleRejectsTargetWithoutExistingProjectionMappings(t *testing.T) {
	draft := simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for index := range cmd.ModelPolicies[0].Mappings {
			if cmd.ModelPolicies[0].Mappings[index].TargetId == "tgt-c" {
				cmd.ModelPolicies[0].Mappings = append(cmd.ModelPolicies[0].Mappings[:index], cmd.ModelPolicies[0].Mappings[index+1:]...)
				break
			}
		}
	})
	if got := compatibleTrafficTargets(draft, "profile-1"); slices.Contains(got, "tgt-c") {
		t.Fatalf("target without required model mappings exposed: %v", got)
	}
}

func TestTrafficRuleWithMissingCurrentProjectionMappingIsAdvanced(t *testing.T) {
	draft := simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ModelPolicies[0].Mappings = cmd.ModelPolicies[0].Mappings[1:]
	})
	rule, _ := findTrafficRule(draft.LocalCommand(), "profile-1", nil)
	if rule.Editable {
		t.Fatal("route missing a current projection mapping must be advanced")
	}
}

func TestAdvancedTrafficRuleIsReadOnly(t *testing.T) {
	draft := simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.BackendSets[0].Candidates[0].Weight = 2
	})
	rule, ok := findTrafficRule(draft.LocalCommand(), "profile-1", nil)
	if !ok || rule.Editable || rule.Mode != "Custom" {
		t.Fatalf("advanced rule=%#v ok=%v", rule, ok)
	}
	err := applyTrafficRule(draft, "profile-1", map[string]string{
		"routing": routingSingle, "primary_target_id": "tgt-a",
	})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("advanced mutation error=%v", err)
	}
}

func TestCustomTrafficRuleRestoreKeepsStableIDsAndReturnsToSimpleMode(t *testing.T) {
	draft := simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ClientProfiles[0].Launcher = generated.MutableClientProfileLauncherCodex
		cmd.Routes[0].IngressProtocol = generated.RouteConfigIngressProtocolOpenaiResponses
		cmd.Targets[0] = simpleResponsesTarget("tgt-a", "Primary")
		cmd.BackendSets[0].Candidates[0].Weight = 7
	})
	page := New(draft)
	page.SelectID("profile-1")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	page = model.(*Page)
	if page.overlay != overlayRestoreSimple {
		t.Fatalf("restore preview overlay=%v status=%q", page.overlay, page.status)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayNone || page.lastIntent != resourcepage.IntentPublish {
		t.Fatalf("restored overlay=%v intent=%v status=%q", page.overlay, page.lastIntent, page.status)
	}
	cmd := draft.LocalCommand()
	if string(cmd.ClientProfiles[0].Id) != "profile-1" || string(cmd.Routes[0].Id) != "route-1" ||
		string(cmd.BackendSets[0].Id) != "bs-1" || string(cmd.ModelPolicies[0].Id) != "mp-1" || string(cmd.ModelProjections[0].Id) != "proj-1" {
		t.Fatalf("restore changed stable IDs: profile=%s route=%s backend=%s policy=%s projection=%s",
			cmd.ClientProfiles[0].Id, cmd.Routes[0].Id, cmd.BackendSets[0].Id, cmd.ModelPolicies[0].Id, cmd.ModelProjections[0].Id)
	}
	rule, ok := findTrafficRule(cmd, "profile-1", nil)
	if !ok || !rule.Editable || rule.Routing != "Single" || len(cmd.ModelPolicies[0].Mappings) != 1 ||
		cmd.ModelPolicies[0].Mappings[0].LogicalModel != wildcardModel || cmd.ModelPolicies[0].Mappings[0].PhysicalModel != wildcardModel ||
		len(cmd.ModelProjections[0].LogicalModels) != 0 {
		t.Fatalf("restored rule=%#v ok=%v", rule, ok)
	}
}

func TestSharedBackendSetAndAdapterMismatchAreAdvanced(t *testing.T) {
	draft := simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		shared := cmd.Routes[0]
		shared.Id = "route-shared"
		cmd.Routes = append(cmd.Routes, shared)
	})
	rule, _ := findTrafficRule(draft.LocalCommand(), "profile-1", nil)
	if rule.Editable {
		t.Fatal("shared backend set must not be edited through one traffic rule")
	}

	draft = simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		shared := cmd.ClientProfiles[0]
		shared.Id = "profile-shared"
		cmd.ClientProfiles = append(cmd.ClientProfiles, shared)
	})
	rule, _ = findTrafficRule(draft.LocalCommand(), "profile-1", nil)
	if rule.Editable {
		t.Fatal("route shared by client profiles must not be edited through one traffic rule")
	}

	draft = simpleTrafficDraft(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets[0].Adapter = generated.MutableTargetCommandAdapterAnthropic
	})
	rule, _ = findTrafficRule(draft.LocalCommand(), "profile-1", nil)
	if rule.Editable {
		t.Fatal("bridge with incompatible adapter must be advanced")
	}
}

func TestDisconnectedSimplePagesDoNotOpenEditors(t *testing.T) {
	draft := simpleTrafficDraft(t)
	draft.SetDisconnected(true)
	page := New(draft)
	page.SelectID("profile-1")
	page.BeginEdit()
	if page.overlay == overlayEditor {
		t.Fatal("disconnected traffic rule opened editor")
	}
}

func TestTrafficRuleRejectsDuplicateBackup(t *testing.T) {
	draft := simpleTrafficDraft(t)
	err := applyTrafficRule(draft, "profile-1", map[string]string{
		"routing": routingFailover, "primary_target_id": "tgt-a", "backup_target_id": "tgt-a",
	})
	if err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("duplicate backup error=%v", err)
	}
}
