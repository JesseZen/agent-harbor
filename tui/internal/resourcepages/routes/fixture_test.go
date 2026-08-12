package routes

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func ptr[T any](v T) *T { return &v }

func sampleRoute(id string) generated.RouteConfig {
	return generated.RouteConfig{
		Id:                  generated.ConfigID(id),
		Name:                "route-" + id,
		IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		BackendSetId:        "bs-1",
		ModelPolicyId:       "mp-1",
		ContentPolicyId:     ptr(generated.ConfigID("cp-1")),
		MaxAttempts:         2,
		MaxRequestBodyBytes: 33554432,
		RequestDeadlineMs:   30000,
		RetryDeadlineMs:     10000,
		StreamIdleTimeoutMs: 5000,
	}
}

func sampleBackendSet(id string) generated.BackendSetConfig {
	caps := []generated.BackendSetConfigRequiredCapabilities{
		generated.BackendSetConfigRequiredCapabilitiesChat,
		generated.BackendSetConfigRequiredCapabilitiesStreaming,
	}
	return generated.BackendSetConfig{
		Id:   generated.ConfigID(id),
		Name: "backend-" + id,
		Candidates: []generated.BackendCandidate{
			{Priority: 1, TargetId: "tgt-a", Weight: 10},
			{Priority: 2, TargetId: "tgt-b", Weight: 5},
		},
		RequiredCapabilities: &caps,
	}
}

func sampleContentPolicy(id string) generated.ContentPolicyConfig {
	mode := generated.ContentPolicyConfigModeRedact
	maxBytes := int64(1048576)
	return generated.ContentPolicyConfig{
		Id:                 generated.ConfigID(id),
		Mode:               &mode,
		MaxInspectionBytes: &maxBytes,
	}
}

func sampleModelPolicy(id string) generated.ModelPolicyConfig {
	return generated.ModelPolicyConfig{
		Id:                 generated.ConfigID(id),
		Name:               "model-policy-" + id,
		CatalogTtlMs:       60000,
		DiscoveryTimeoutMs: 5000,
		Mappings: []generated.ModelMapping{
			{LogicalModel: "gpt-4", PhysicalModel: "gpt-4o", TargetId: "tgt-a"},
			{LogicalModel: "claude", PhysicalModel: "claude-3", TargetId: "tgt-b"},
		},
	}
}

func sampleModelProjection(id string) generated.ModelProjectionConfig {
	return generated.ModelProjectionConfig{
		Id:            generated.ConfigID(id),
		Name:          "projection-" + id,
		LogicalModels: []string{"gpt-4", "claude"},
	}
}

func sampleTransform(id string) generated.CompatibilityTransformConfig {
	return generated.CompatibilityTransformConfig{
		Id:      generated.ConfigID(id),
		Name:    "transform-" + id,
		Scope:   generated.ClientProfile,
		ScopeId: "profile-1",
		Operation: generated.CompatibilityTransformOperation{
			RenameModel: &generated.RenameModelTransform{
				SourceModel:      "old",
				DestinationModel: "new",
			},
		},
	}
}

func sampleProfile(id, routeID, projectionID, transformID string) generated.MutableClientProfile {
	return generated.MutableClientProfile{
		Id:                        generated.ConfigID(id),
		Name:                      "profile-" + id,
		Launcher:                  generated.MutableClientProfileLauncherCodex,
		DefaultRouteId:            generated.ConfigID(routeID),
		ModelProjectionId:         generated.ConfigID(projectionID),
		CompatibilityTransformIds: []generated.ConfigID{generated.ConfigID(transformID)},
		Arguments:                 []string{},
		Environment:               []generated.EnvironmentVariableConfig{},
	}
}

func seedDraft(t *testing.T) *configdraft.Draft {
	t.Helper()
	snap := configdraft.FixtureSnapshot(configdraft.WithRoute(sampleRoute("route-1")))
	draft := configdraft.Load(snap)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{
			{Id: "tgt-a", Name: "A"},
			{Id: "tgt-b", Name: "B"},
		}
		cmd.BackendSets = []generated.BackendSetConfig{sampleBackendSet("bs-1")}
		cmd.ContentPolicies = []generated.ContentPolicyConfig{sampleContentPolicy("cp-1")}
		cmd.ModelPolicies = []generated.ModelPolicyConfig{sampleModelPolicy("mp-1")}
		cmd.ModelProjections = []generated.ModelProjectionConfig{sampleModelProjection("proj-1")}
		cmd.CompatibilityTransforms = []generated.CompatibilityTransformConfig{sampleTransform("xf-1")}
		cmd.ClientProfiles = []generated.MutableClientProfile{
			sampleProfile("profile-1", "route-1", "proj-1", "xf-1"),
		}
		objects := []generated.ManagedObject{{
			Id: "rule-fixture", Name: "Fixture rule", Kind: generated.ManagedObjectKindTrafficRule,
			Members: []generated.ManagedResourceRef{
				{Kind: generated.ManagedResourceRefKindClientProfile, Id: "profile-1"},
			},
		}}
		cmd.ManagedObjects = &objects
	})
	return draft
}

func newTestPage(t *testing.T) (*Page, *configdraft.Draft) {
	t.Helper()
	draft := seedDraft(t)
	page := New(draft)
	page.SetSize(120, 30)
	_ = page.Init()
	model, _ := page.Update(nil)
	page = model.(*Page)
	return page, draft
}

// deleteViaConfirm drives the production delete path: d → confirm → Enter.
func deleteViaConfirm(t *testing.T, page *Page) *Page {
	t.Helper()
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v want confirm delete", page.overlay)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model.(*Page)
}
