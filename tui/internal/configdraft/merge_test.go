package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerge_UnrelatedEditsCoalesce(t *testing.T) {
	base := FixtureSnapshot()
	d := Load(base)

	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})

	current := FixtureSnapshot(WithGeneration(2))
	current.MutableConfig.Routes = []generated.RouteConfig{
		{
			Id:                  "route-1",
			Name:                "Remote Route",
			BackendSetId:        "bs-1",
			IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
			MaxAttempts:         1,
			MaxRequestBodyBytes: 1024,
			ModelPolicyId:       "mp-1",
			RequestDeadlineMs:   1000,
			RetryDeadlineMs:     500,
			RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
			StreamIdleTimeoutMs: 30000,
		},
	}

	d.BeginConflict(current)
	conflicts := d.Reapply()
	assert.Empty(t, conflicts)
	assert.Equal(t, generated.MutableInstanceConfigLogLevelDetail, d.LocalCommand().Instance.LogLevel)
	require.Len(t, d.LocalCommand().Routes, 1)
	assert.Equal(t, "Remote Route", d.LocalCommand().Routes[0].Name)
	assert.False(t, d.InConflict())
}

func TestMerge_ExplicitConflictSameField(t *testing.T) {
	routeBase := generated.RouteConfig{
		Id:                  "route-1",
		Name:                "Base Route",
		BackendSetId:        "bs-1",
		IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:         1,
		MaxRequestBodyBytes: 1024,
		ModelPolicyId:       "mp-1",
		RequestDeadlineMs:   1000,
		RetryDeadlineMs:     500,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 30000,
	}
	base := FixtureSnapshot(WithRoute(routeBase))
	d := Load(base)

	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes[0].MaxAttempts = 2
	})

	current := FixtureSnapshot(WithGeneration(2), WithRoute(routeBase))
	current.MutableConfig.Routes[0].MaxAttempts = 3

	d.BeginConflict(current)
	conflicts := d.Reapply()
	require.NotEmpty(t, conflicts)
	assert.True(t, d.InConflict())
	assert.Contains(t, conflicts[0].Path, "max_attempts")
}

func TestMerge_NoSilentAutoRebase(t *testing.T) {
	routeBase := generated.RouteConfig{
		Id:                  "route-1",
		Name:                "Base Route",
		BackendSetId:        "bs-1",
		IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:         1,
		MaxRequestBodyBytes: 1024,
		ModelPolicyId:       "mp-1",
		RequestDeadlineMs:   1000,
		RetryDeadlineMs:     500,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 30000,
	}
	base := FixtureSnapshot(WithRoute(routeBase))
	d := Load(base)

	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes[0].MaxAttempts = 2
	})

	current := FixtureSnapshot(WithGeneration(2), WithRoute(routeBase))
	current.MutableConfig.Routes[0].MaxAttempts = 3

	d.BeginConflict(current)
	first := d.Reapply()
	require.NotEmpty(t, first)

	second := d.Reapply()
	require.NotEmpty(t, second)
	assert.Equal(t, 2, d.LocalCommand().Routes[0].MaxAttempts)
}

func TestMerge_CollectionByID(t *testing.T) {
	routeBase := generated.RouteConfig{
		Id:                  "route-1",
		Name:                "Base Route",
		BackendSetId:        "bs-1",
		IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:         1,
		MaxRequestBodyBytes: 1024,
		ModelPolicyId:       "mp-1",
		RequestDeadlineMs:   1000,
		RetryDeadlineMs:     500,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 30000,
	}
	base := FixtureSnapshot(WithRoute(routeBase))
	d := Load(base)

	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes[0].Name = "Local Edit"
	})

	current := FixtureSnapshot(WithGeneration(2), WithRoute(routeBase))
	current.MutableConfig.Routes[0].MaxAttempts = 5

	d.BeginConflict(current)
	conflicts := d.Reapply()
	assert.Empty(t, conflicts)
	require.Len(t, d.LocalCommand().Routes, 1)
	assert.Equal(t, "Local Edit", d.LocalCommand().Routes[0].Name)
	assert.Equal(t, 5, d.LocalCommand().Routes[0].MaxAttempts)
}

func TestMerge_DeleteVsModifyConflict(t *testing.T) {
	routeBase := generated.RouteConfig{
		Id:                  "route-1",
		Name:                "Base Route",
		BackendSetId:        "bs-1",
		IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:         1,
		MaxRequestBodyBytes: 1024,
		ModelPolicyId:       "mp-1",
		RequestDeadlineMs:   1000,
		RetryDeadlineMs:     500,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 30000,
	}
	base := FixtureSnapshot(WithRoute(routeBase))
	d := Load(base)

	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes = nil
	})

	current := FixtureSnapshot(WithGeneration(2), WithRoute(routeBase))
	current.MutableConfig.Routes[0].Name = "Remote Edit"

	d.BeginConflict(current)
	conflicts := d.Reapply()
	require.NotEmpty(t, conflicts)
	assert.Contains(t, conflicts[0].Reason, "deleted locally but modified remotely")
}

func TestMerge_AcceptCurrent(t *testing.T) {
	base := FixtureSnapshot()
	d := Load(base)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})

	current := FixtureSnapshot(WithGeneration(2))
	current.MutableConfig.Instance.LogLevel = generated.MutableInstanceConfigLogLevelSimple
	d.BeginConflict(current)
	d.Reapply()
	d.AcceptCurrent()

	assert.False(t, d.IsDirty())
	assert.Equal(t, generated.MutableInstanceConfigLogLevelSimple, d.LocalCommand().Instance.LogLevel)
}
