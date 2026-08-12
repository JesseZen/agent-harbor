package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDraft_CanPublishFalseWhileInConflictBeforeReapply(t *testing.T) {
	route := generated.RouteConfig{
		Id: "route-1", Name: "Base", BackendSetId: "bs-1",
		IngressProtocol: generated.RouteConfigIngressProtocolOpenaiChat,
		MaxAttempts:     1, MaxRequestBodyBytes: 1024, ModelPolicyId: "mp-1",
		RequestDeadlineMs: 1000, RetryDeadlineMs: 500,
		RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
		StreamIdleTimeoutMs: 30000,
	}
	d := Load(FixtureSnapshot(WithRoute(route)))
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Routes[0].MaxAttempts = 2
	})
	assert.True(t, d.CanPublish())

	current := FixtureSnapshot(WithGeneration(2), WithRevision("rev-2"), WithRoute(route))
	current.MutableConfig.Routes[0].MaxAttempts = 3
	d.BeginConflict(current)

	assert.True(t, d.InConflict())
	assert.Empty(t, d.Conflicts())
	assert.False(t, d.CanPublish(), "publish must stay blocked from BeginConflict until conflict clears")
}

func TestDraft_ReapplySuccessSyncsGenerationAndRevision(t *testing.T) {
	base := FixtureSnapshot(WithGeneration(1), WithRevision("rev-1"))
	d := Load(base)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})

	current := FixtureSnapshot(WithGeneration(5), WithRevision("rev-5"))
	d.BeginConflict(current)
	conflicts := d.Reapply()
	require.Empty(t, conflicts)
	assert.Equal(t, int64(5), d.Generation())
	assert.Equal(t, "rev-5", d.Revision())
	assert.False(t, d.InConflict())
}

func TestDraft_AcceptCurrentSyncsGenerationRevisionAndClearsConflict(t *testing.T) {
	base := FixtureSnapshot(WithGeneration(1), WithRevision("rev-1"))
	d := Load(base)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})

	current := FixtureSnapshot(WithGeneration(9), WithRevision("rev-9"))
	current.MutableConfig.Instance.LogLevel = generated.MutableInstanceConfigLogLevelSimple
	d.BeginConflict(current)
	d.Reapply()

	d.AcceptCurrent()
	assert.Equal(t, int64(9), d.Generation())
	assert.Equal(t, "rev-9", d.Revision())
	assert.False(t, d.InConflict())
	assert.Empty(t, d.Conflicts())
}

func TestDraft_ReplaceRequiredIDs(t *testing.T) {
	d := Load(FixtureSnapshot(
		WithManagedCredential("cred-a", "A", "openai", 1),
		WithManagedCredential("cred-b", "B", "openai", 1),
	))
	d.MarkReplaceRequired("cred-b")

	ids := d.ReplaceRequiredIDs()
	require.Len(t, ids, 1)
	assert.Equal(t, "cred-b", ids[0])
}
