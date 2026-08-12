package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
)

func TestDraft_DomainDirtyTracksSingleDomain(t *testing.T) {
	d := Load(FixtureSnapshot())
	assert.False(t, d.DomainDirty(DomainRoutes))

	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	assert.True(t, d.DomainDirty(DomainInstance))
	assert.False(t, d.DomainDirty(DomainRoutes))
}

func TestDraft_ReplaceRequiredBlocksPublish(t *testing.T) {
	d := Load(FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1)))
	d.MarkReplaceRequired("cred-1")
	assert.False(t, d.CanPublish())
	assert.True(t, d.IsDirty())
}

func TestDraft_ConflictTracking(t *testing.T) {
	route := generated.RouteConfig{
		Id: "route-1", Name: "Base", BackendSetId: "bs-1",
		IngressProtocol: generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:     1, MaxRequestBodyBytes: 1024, ModelPolicyId: "mp-1",
		RequestDeadlineMs: 1000, RetryDeadlineMs: 500,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 30000,
	}
	d := Load(FixtureSnapshot(WithRoute(route)))
	d.Mutate(func(cmd *generated.MutableConfigCommand) { cmd.Routes[0].MaxAttempts = 2 })

	current := FixtureSnapshot(WithGeneration(2), WithRoute(route))
	current.MutableConfig.Routes[0].MaxAttempts = 3
	d.BeginConflict(current)
	conflicts := d.Reapply()
	assert.NotEmpty(t, conflicts)
	assert.True(t, d.InConflict())
	assert.False(t, d.CanPublish())
}

func TestCanonicalCommandRoundTrip(t *testing.T) {
	snap := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 4))
	cmd := ViewToCommand(snap.MutableConfig)
	round := canonicalCommand(cmd)
	assert.True(t, jsonEqual(cmd, round))
}
