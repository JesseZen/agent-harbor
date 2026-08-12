package targets

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
)

func TestSimpleUpstreamCreateBuildsResourceBundle(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-upstream")}
	page, draft, _ := newTestPage(t, fake)
	page.SetKind(KindUpstreams)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"name":        "Reserve API",
		"launcher":    upstreamClientCodex,
		"api_formats": upstreamAPIResponses,
		"base_url":    "https://reserve.example.com/v1",
	})
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatalf("paste API key: %v", err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save upstream: %v", err)
	}

	cmd := draft.LocalCommand()
	if len(cmd.Targets) != 2 || len(cmd.Endpoints) != 2 || len(cmd.Credentials) != 3 || len(cmd.QuotaGroups) != 2 {
		t.Fatalf("resource bundle counts: targets=%d endpoints=%d credentials=%d quotas=%d",
			len(cmd.Targets), len(cmd.Endpoints), len(cmd.Credentials), len(cmd.QuotaGroups))
	}
	target, ok := findTarget(cmd, "target-reserve-api-openai-responses")
	if !ok {
		t.Fatal("generated target missing")
	}
	if target.Bridge != generated.MutableTargetCommandBridgeOpenaiResponses ||
		target.Adapter != generated.MutableTargetCommandAdapterOpenaiCompatible ||
		target.EndpointId != "endpoint-reserve-api" ||
		target.CredentialId != "credential-reserve-api-openai" ||
		target.QuotaGroupId != "quota-reserve-api-limits" {
		t.Fatalf("generated target=%#v", target)
	}
	credential, ok := findCredential(cmd, "credential-reserve-api-openai")
	if !ok {
		t.Fatal("generated credential missing")
	}
	replace, err := credential.SecretAction.AsCredentialSecretAction1()
	if err != nil || string(replace.StageId) != "stage-upstream" {
		t.Fatalf("credential replace=%#v err=%v", credential.SecretAction, err)
	}
	if page.TokenBufferLen() != 0 {
		t.Fatal("API key buffer was not zeroed")
	}
	if got := len(managedconfig.ProjectUpstreams(cmd)); got != 1 {
		t.Fatalf("managed upstreams=%d want 1", got)
	}
	if got := len(managedconfig.ProjectLimitPolicies(cmd)); got != 1 {
		t.Fatalf("managed limit policies=%d want 1", got)
	}
	rules := managedconfig.ProjectTrafficRules(cmd)
	if len(rules) != 1 || rules[0].Profile.Launcher != generated.MutableClientProfileLauncherCodex ||
		len(rules[0].Backend.Candidates) != 1 || rules[0].Backend.Candidates[0].TargetId != target.Id {
		t.Fatalf("first save did not create its traffic rule atomically: %#v", rules)
	}
}

func TestSimpleUpstreamMultiProviderFormatsStageKeySeparately(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-multi")}
	page, draft, _ := newTestPage(t, fake)
	page.SetKind(KindUpstreams)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"name":        "Multi API",
		"launcher":    upstreamClientCodex,
		"api_formats": upstreamAPIResponses + "," + upstreamAPIAnthropic,
		"base_url":    "https://multi.example.com/v1",
	})
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatal(err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatal(err)
	}
	if fake.createCalls != 2 {
		t.Fatalf("secret stage calls=%d want one per provider family", fake.createCalls)
	}
	upstreams := managedconfig.ProjectUpstreams(draft.LocalCommand())
	if len(upstreams) != 1 || len(upstreams[0].Credentials) != 2 || len(upstreams[0].Targets) != 2 {
		t.Fatalf("multi-format upstream=%#v", upstreams)
	}
}

func TestDeletePrimaryUpstreamReplacesItAndKeepsRuleRunnable(t *testing.T) {
	page, draft, primaryID, backupID := managedFailoverFixture(t)
	page.SelectID(primaryID)
	page.beginDeleteIntent()
	if page.overlay != overlayUpstreamDelete || len(page.upstreamDelete.Options) != 1 {
		t.Fatalf("delete overlay=%v plan=%#v", page.overlay, page.upstreamDelete)
	}
	page.updateOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := draft.LocalCommand()
	if _, ok := managedconfig.FindObject(cmd, primaryID); ok {
		t.Fatal("primary upstream still exists")
	}
	if _, ok := managedconfig.FindObject(cmd, backupID); !ok {
		t.Fatal("replacement upstream was deleted")
	}
	rules := managedconfig.ProjectTrafficRules(cmd)
	if len(rules) != 1 || len(rules[0].Backend.Candidates) != 1 ||
		string(rules[0].Backend.Candidates[0].TargetId) != "target-backup-openai-responses" {
		t.Fatalf("rewired rule=%#v", rules)
	}
	if len(rules[0].Policy.Mappings) != 1 || string(rules[0].Policy.Mappings[0].TargetId) != "target-backup-openai-responses" {
		t.Fatalf("rewired mappings=%#v", rules[0].Policy.Mappings)
	}
}

func TestDeleteBackupUpstreamRemovesItFromFailover(t *testing.T) {
	page, draft, _, backupID := managedFailoverFixture(t)
	page.SelectID(backupID)
	page.beginDeleteIntent()
	if page.overlay != overlayUpstreamDelete || page.upstreamDelete.needsReplacement() {
		t.Fatalf("backup delete plan=%#v overlay=%v", page.upstreamDelete, page.overlay)
	}
	page.updateOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	rules := managedconfig.ProjectTrafficRules(draft.LocalCommand())
	if len(rules) != 1 || len(rules[0].Backend.Candidates) != 1 ||
		string(rules[0].Backend.Candidates[0].TargetId) != "target-primary-openai-responses" {
		t.Fatalf("rule after backup delete=%#v", rules)
	}
}

func TestUsedUpstreamFormatChangeShowsImpactBeforeMutation(t *testing.T) {
	page, draft, primaryID, _ := managedFailoverFixture(t)
	before := draft.LocalCommand()
	page.SelectID(primaryID)
	page.BeginEdit()
	page.SetEditorValues(map[string]string{
		"api_formats": upstreamAPIResponses + "," + upstreamAPIAnthropic,
	})
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatal(err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatal(err)
	}
	if page.overlay != overlayMigrationConfirm || !reflect.DeepEqual(before.Targets, draft.LocalCommand().Targets) {
		t.Fatalf("format change skipped impact confirmation: overlay=%v", page.overlay)
	}
	if len(page.migrationImpact) != 1 || page.migrationImpact[0] != "Coding" {
		t.Fatalf("impact=%#v", page.migrationImpact)
	}
}

func TestIncompatibleFormatMigrationSelectsReplacementUpstream(t *testing.T) {
	page, draft, primaryID, _ := managedFailoverFixture(t)
	page.SelectID(primaryID)
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"api_formats": upstreamAPIAnthropic})
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatal(err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatal(err)
	}
	if page.overlay != overlayMigrationConfirm || len(page.migrationOptions) != 1 {
		t.Fatalf("migration replacement options=%#v overlay=%v blocked=%q", page.migrationOptions, page.overlay, page.migrationBlocked)
	}
	page.updateOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if page.overlay != overlayNone || page.lastIntent != resourcepage.IntentPublish {
		t.Fatalf("confirmed migration overlay=%v intent=%v status=%q", page.overlay, page.lastIntent, page.statusExtra)
	}
	rules := managedconfig.ProjectTrafficRules(draft.LocalCommand())
	if len(rules) != 1 || len(rules[0].Backend.Candidates) != 1 ||
		string(rules[0].Backend.Candidates[0].TargetId) != "target-backup-openai-responses" {
		t.Fatalf("migrated rule=%#v", rules)
	}
}

func managedFailoverFixture(t *testing.T) (*Page, *configdraft.Draft, string, string) {
	t.Helper()
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	var primaryID, backupID string
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		*cmd = generated.MutableConfigCommand{}
		_, quota, err := managedconfig.CreateLimitPolicy(cmd, "Shared", 60, 4)
		if err != nil {
			t.Fatal(err)
		}
		action, err := buildReplaceAction("stage-primary")
		if err != nil {
			t.Fatal(err)
		}
		primary, primaryTargets, err := managedconfig.CreateUpstream(cmd, managedconfig.UpstreamRequest{
			Name: "Primary", BaseURL: "https://primary.example.com", QuotaGroupID: string(quota.Id),
			Formats: []managedconfig.Format{managedconfig.FormatOpenAIResponses},
			SecretActions: map[generated.MutableCredentialCommandProvider]generated.CredentialSecretAction{
				generated.MutableCredentialCommandProviderOpenaiCompatible: action,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		action, err = buildReplaceAction("stage-backup")
		if err != nil {
			t.Fatal(err)
		}
		backup, backupTargets, err := managedconfig.CreateUpstream(cmd, managedconfig.UpstreamRequest{
			Name: "Backup", BaseURL: "https://backup.example.com", QuotaGroupID: string(quota.Id),
			Formats: []managedconfig.Format{managedconfig.FormatOpenAIResponses},
			SecretActions: map[generated.MutableCredentialCommandProvider]generated.CredentialSecretAction{
				generated.MutableCredentialCommandProviderOpenaiCompatible: action,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = managedconfig.CreateTrafficRule(cmd, managedconfig.TrafficRuleRequest{
			Name: "Coding", Launcher: "codex", Routing: managedconfig.RoutingFailover,
			PrimaryTarget: primaryTargets[managedconfig.FormatOpenAIResponses],
			BackupTarget:  backupTargets[managedconfig.FormatOpenAIResponses], ModelHandling: managedconfig.ModelPreserve,
		})
		if err != nil {
			t.Fatal(err)
		}
		primaryID, backupID = string(primary.Id), string(backup.Id)
	})
	fake := &fakeStageHTTP{createFn: okCreate("stage-migration")}
	page := New(Deps{Draft: draft, StageClient: fake, Scope: "all"})
	page.SetSize(120, 30)
	page.SetKind(KindUpstreams)
	return page, draft, primaryID, backupID
}

func TestSimpleUpstreamPrivateEndpointRequiresExplicitOptIn(t *testing.T) {
	values := map[string]string{
		"name": "Local API", "client": upstreamClientCodex, "api_type": upstreamAPIResponses,
		"base_url": "http://localhost:3344", "allow_private_network": "false",
	}
	if err := validateUpstreamValues(values, true, true); err == nil || !strings.Contains(err.Error(), "allow_private_network") {
		t.Fatalf("private endpoint validation = %v", err)
	}
	values["allow_private_network"] = "true"
	if err := validateUpstreamValues(values, true, true); err != nil {
		t.Fatalf("explicit private endpoint opt-in: %v", err)
	}
}

func TestSimpleUpstreamPrivateEndpointPersistsOptIn(t *testing.T) {
	_, draft, _ := newTestPage(t, &fakeStageHTTP{})
	values := map[string]string{
		"name": "Local API", "client": upstreamClientCodex, "api_type": upstreamAPIResponses,
		"base_url": "http://127.0.0.1:3344", "allow_private_network": "true",
	}
	if err := createSimpleUpstream(draft, values, "stage-local"); err != nil {
		t.Fatalf("create local upstream: %v", err)
	}
	endpoint, ok := findEndpoint(draft.LocalCommand(), "endpoint-local-api")
	if !ok || !endpoint.AllowPrivateNetwork {
		t.Fatalf("private endpoint opt-in not persisted: %#v", endpoint)
	}
}

func TestSimpleUpstreamEditPreservesProtocolAndOptionalSecret(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-new-key")}
	page, draft, _ := newTestPage(t, fake)
	makeFixtureUpstreamSimple(draft)
	page.SetKind(KindUpstreams)
	page.Refresh()
	page.SelectID("tgt-1")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{"name": "Primary renamed", "base_url": "https://new.example.com/v1"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	cmd := draft.LocalCommand()
	endpoint, _ := findEndpoint(cmd, "ep-1")
	credential, _ := findCredential(cmd, "cred-1")
	if endpoint.Name != "Primary renamed" || endpoint.BaseUrl != "https://new.example.com/v1" ||
		credential.Name != "Primary renamed API key" {
		t.Fatalf("metadata edit endpoint=%#v credential=%#v", endpoint, credential)
	}
	if _, err := credential.SecretAction.AsCredentialSecretAction0(); err != nil {
		t.Fatalf("blank API key must preserve secret: %v", err)
	}

	page.SelectID("tgt-1")
	page.BeginEdit()
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatalf("paste replacement: %v", err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("replace key: %v", err)
	}
	credential, _ = findCredential(draft.LocalCommand(), "cred-1")
	replace, err := credential.SecretAction.AsCredentialSecretAction1()
	if err != nil || string(replace.StageId) != "stage-new-key" {
		t.Fatalf("replacement=%#v err=%v", credential.SecretAction, err)
	}
}

func TestSimpleUpstreamProtocolChangeCreatesNewBinding(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("stage-protocol")})
	makeFixtureUpstreamSimple(draft)
	before := draft.LocalCommand()
	page.SetKind(KindUpstreams)
	page.Refresh()
	page.SelectID("tgt-1")
	page.BeginEdit()
	page.SetEditorValues(map[string]string{
		"name": "Codex binding", "base_url": "https://codex.example.com/v1",
		"api_formats": upstreamAPIResponses,
	})
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatalf("paste protocol key: %v", err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create protocol binding: %v", err)
	}
	after := draft.LocalCommand()
	if len(after.Targets) != len(before.Targets) || len(after.Endpoints) != len(before.Endpoints) ||
		len(after.Credentials) != len(before.Credentials) || len(after.QuotaGroups) != len(before.QuotaGroups) {
		t.Fatalf("protocol binding resource counts before=%#v after=%#v", before, after)
	}
	if !reflect.DeepEqual(before.Routes, after.Routes) || !reflect.DeepEqual(before.ModelPolicies, after.ModelPolicies) {
		t.Fatal("protocol migration changed unrelated route/model references")
	}
	created := after.Targets[len(after.Targets)-1]
	if created.Bridge != generated.MutableTargetCommandBridgeOpenaiResponses || created.Adapter != generated.MutableTargetCommandAdapterOpenaiCompatible {
		t.Fatalf("created protocol binding=%#v", created)
	}
}

func TestSimpleUpstreamProtocolPresets(t *testing.T) {
	tests := []struct {
		client, api string
		provider    generated.MutableCredentialCommandProvider
		adapter     generated.MutableTargetCommandAdapter
		bridge      generated.MutableTargetCommandBridge
		capability  generated.MutableTargetCommandCapabilities
	}{
		{upstreamClientClaude, upstreamAPIAnthropic, generated.MutableCredentialCommandProviderAnthropic, generated.MutableTargetCommandAdapterAnthropic, generated.MutableTargetCommandBridgeAnthropicMessages, generated.MutableTargetCommandCapabilitiesMessages},
		{upstreamClientClaude, upstreamAPIChat, generated.MutableCredentialCommandProviderOpenaiCompatible, generated.MutableTargetCommandAdapterOpenaiCompatible, generated.MutableTargetCommandBridgeAnthropicToOpenai, generated.MutableTargetCommandCapabilitiesChat},
		{upstreamClientCodex, upstreamAPIResponses, generated.MutableCredentialCommandProviderOpenaiCompatible, generated.MutableTargetCommandAdapterOpenaiCompatible, generated.MutableTargetCommandBridgeOpenaiResponses, generated.MutableTargetCommandCapabilitiesResponses},
		{upstreamClientCodex, upstreamAPIAnthropic, generated.MutableCredentialCommandProviderAnthropic, generated.MutableTargetCommandAdapterAnthropic, generated.MutableTargetCommandBridgeOpenaiToAnthropic, generated.MutableTargetCommandCapabilitiesMessages},
	}
	for _, test := range tests {
		preset, err := upstreamPresetFor(test.client, test.api)
		if err != nil {
			t.Fatalf("preset %s/%s: %v", test.client, test.api, err)
		}
		if preset.provider != test.provider || preset.adapter != test.adapter || preset.bridge != test.bridge || !slicesContainsCapability(preset.capabilities, test.capability) {
			t.Fatalf("preset %s/%s=%#v", test.client, test.api, preset)
		}
	}
}

func TestSimpleUpstreamUniqueIDsAndDefaults(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets, generated.MutableTargetCommand{Id: "target-reserve"})
	})
	values := map[string]string{"name": "Reserve", "client": upstreamClientCodex, "api_type": upstreamAPIResponses, "base_url": "https://reserve.example.com"}
	if err := createSimpleUpstream(draft, values, "stage-1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	cmd := draft.LocalCommand()
	target, ok := findTarget(cmd, "target-reserve-2-openai-responses")
	if !ok {
		t.Fatalf("collision suffix not applied: %#v", cmd.Targets)
	}
	endpoint, _ := findEndpoint(cmd, "endpoint-reserve-2")
	quota := cmd.QuotaGroups[len(cmd.QuotaGroups)-1]
	if !endpoint.Http2Enabled || endpoint.MaxIdleConnections != 20 || endpoint.IdleConnectionTimeoutMs != 30000 ||
		target.HealthPolicy.FailureThreshold != 3 || target.HealthPolicy.ProbeTimeoutMs != 10000 ||
		target.ThrottlePolicy.DefaultCoolingMs != 1000 || quota.Rpm != 60 || quota.MaxConcurrency != 4 {
		t.Fatalf("defaults endpoint=%#v target=%#v quota=%#v", endpoint, target, quota)
	}
}

func TestSimpleUpstreamEditOnlyChangesOwnedBundle(t *testing.T) {
	_, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("unused")})
	makeFixtureUpstreamSimple(draft)
	before := draft.LocalCommand()
	upstream, _ := findSimpleUpstream(draft, "tgt-1", nil)
	if err := editSimpleUpstream(draft, upstream, map[string]string{"name": "Renamed", "base_url": "https://renamed.example.com"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	after := draft.LocalCommand()
	if !reflect.DeepEqual(before.Routes, after.Routes) || !reflect.DeepEqual(before.ModelPolicies, after.ModelPolicies) || !reflect.DeepEqual(before.QuotaGroups, after.QuotaGroups) {
		t.Fatal("simple upstream edit changed routes, models, or quotas")
	}
}

func TestSimpleUpstreamSharedResourcesAreReadOnly(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("unused")})
	makeFixtureUpstreamSimple(draft)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		copyTarget := cmd.Targets[0]
		copyTarget.Id = "tgt-shared"
		copyTarget.Name = "shared"
		cmd.Targets = append(cmd.Targets, copyTarget)
	})
	page.SetKind(KindUpstreams)
	page.Refresh()

	upstream, ok := findSimpleUpstream(draft, "tgt-1", nil)
	if !ok || upstream.Editable || upstream.Mode != "Custom" {
		t.Fatalf("shared upstream=%#v ok=%v", upstream, ok)
	}
	page.SelectID("tgt-1")
	page.BeginEdit()
	if page.overlay == overlayEditor || page.Kind() != KindUpstreams || !strings.Contains(page.statusExtra, "press R") {
		t.Fatalf("custom upstream must remain in the friendly view: kind=%v overlay=%v selected=%q status=%q", page.Kind(), page.overlay, page.SelectedID(), page.statusExtra)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if page.overlay != overlayNone || !strings.Contains(page.statusExtra, "Cannot restore") {
		t.Fatalf("shared upstream restore must be blocked: overlay=%v status=%q", page.overlay, page.statusExtra)
	}
}

func TestCustomUpstreamRestoreKeepsConnectionAndReturnsToSimpleMode(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("unused")})
	makeFixtureUpstreamSimple(draft)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		extra := cmd.Endpoints[0]
		extra.Id = "endpoint-extra"
		extra.Name = "obsolete connection"
		cmd.Endpoints = append(cmd.Endpoints, extra)
		objects := managedconfig.Objects(*cmd)
		for index := range objects {
			if objects[index].Kind == generated.ManagedObjectKindUpstream {
				objects[index].Members = append(objects[index].Members, generated.ManagedResourceRef{
					Kind: generated.ManagedResourceRefKindEndpoint, Id: extra.Id,
				})
			}
		}
		managedconfig.SetObjects(cmd, objects)
	})
	projected := managedconfig.ProjectUpstreams(draft.LocalCommand())
	if len(projected) != 1 || !projected[0].Custom {
		t.Fatalf("fixture must start custom: %#v", projected)
	}

	page.SetKind(KindUpstreams)
	page.Refresh()
	page.SelectID(string(projected[0].Object.Id))
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if page.overlay != overlayRestoreUpstream {
		t.Fatalf("restore overlay=%v status=%q", page.overlay, page.statusExtra)
	}
	page.Update(tea.MouseMsg{X: 8, Y: page.height - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if page.overlay != overlayNone || page.lastIntent != resourcepage.IntentPublish {
		t.Fatalf("mouse restore overlay=%v intent=%v status=%q", page.overlay, page.lastIntent, page.statusExtra)
	}
	cmd := draft.LocalCommand()
	if _, ok := findEndpoint(cmd, "endpoint-extra"); ok {
		t.Fatal("extra endpoint was not removed")
	}
	projected = managedconfig.ProjectUpstreams(cmd)
	if len(projected) != 1 || projected[0].Custom || projected[0].Endpoint.BaseUrl != "https://api.example.com" {
		t.Fatalf("restored upstream=%#v", projected)
	}
}

func TestDisconnectedUpstreamDoesNotOpenEditor(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("unused")})
	makeFixtureUpstreamSimple(draft)
	page.SetKind(KindUpstreams)
	page.Refresh()
	page.SelectID("tgt-1")
	draft.SetDisconnected(true)
	page.BeginEdit()
	if page.overlay == overlayEditor {
		t.Fatal("disconnected upstream opened editor")
	}
}

func makeFixtureUpstreamSimple(draft *configdraft.Draft) {
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets[0].Adapter = generated.MutableTargetCommandAdapterOpenaiCompatible
		cmd.Targets[0].Bridge = generated.MutableTargetCommandBridgeAnthropicToOpenai
		cmd.Targets[0].Capabilities = []generated.MutableTargetCommandCapabilities{
			generated.MutableTargetCommandCapabilitiesChat,
			generated.MutableTargetCommandCapabilitiesStreaming,
			generated.MutableTargetCommandCapabilitiesTools,
			generated.MutableTargetCommandCapabilitiesProbe,
		}
		cmd.Credentials[0].Provider = generated.MutableCredentialCommandProviderOpenaiCompatible
		objects := []generated.ManagedObject{
			{Id: "limit-fixture", Name: "Interactive", Kind: generated.ManagedObjectKindLimitPolicy, Members: []generated.ManagedResourceRef{
				{Kind: generated.ManagedResourceRefKindQuotaGroup, Id: "quota-1"},
			}},
			{Id: "upstream-fixture", Name: "Primary", Kind: generated.ManagedObjectKindUpstream, Members: []generated.ManagedResourceRef{
				{Kind: generated.ManagedResourceRefKindEndpoint, Id: "ep-1"},
				{Kind: generated.ManagedResourceRefKindCredential, Id: "cred-1"},
				{Kind: generated.ManagedResourceRefKindTarget, Id: "tgt-1"},
			}},
		}
		cmd.ManagedObjects = &objects
	})
}

func TestSimpleUpstreamRejectsInvalidURL(t *testing.T) {
	if err := validateUpstreamValues(map[string]string{
		"name": "Bad", "client": upstreamClientClaude, "api_type": upstreamAPIAnthropic, "base_url": "not-a-url",
	}, true, true); err == nil {
		t.Fatal("invalid URL accepted")
	}
}

func indexOfUpstreamField(fields []string, target string) int {
	for index, field := range fields {
		if field == target {
			return index
		}
	}
	return -1
}

func slicesContainsCapability(values []generated.MutableTargetCommandCapabilities, want generated.MutableTargetCommandCapabilities) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
