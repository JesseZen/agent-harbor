package configdraft

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearCredentialSecretState_ClearsReplaceReqAndCredSecrets(t *testing.T) {
	d := Load(FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1)))
	d.SetCredentialReplace("cred-1", "stage-local")
	require.True(t, d.DomainDirty(DomainTargets))
	require.True(t, d.CanPublish())

	d.MarkReplaceRequired("cred-1")
	require.Contains(t, d.ReplaceRequiredIDs(), "cred-1")
	require.False(t, d.CanPublish())

	d.ClearCredentialSecretState("cred-1")
	assert.NotContains(t, d.ReplaceRequiredIDs(), "cred-1")
	assert.True(t, d.CanPublish(), "clearing replaceReq alone restores CanPublish when otherwise clean")

	// Sticky dirty from credSecrets alone: SetCredentialReplace then clear maps.
	d.SetCredentialReplace("cred-1", "stage-again")
	require.True(t, d.DomainDirty(DomainTargets))
	d.ClearCredentialSecretState("cred-1")
	// Local still has replace action wire — DomainTargets may still be dirty from
	// credential command diff; assert maps themselves are gone via CanPublish /
	// replaceReq and that a second clear is idempotent.
	d.ClearCredentialSecretState("cred-1")
	assert.Empty(t, d.ReplaceRequiredIDs())
}

func TestClearCredentialSecretState_RemovesCredSecretsStickyDirty(t *testing.T) {
	base := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1))
	d := Load(base)
	d.SetCredentialReplace("cred-1", "stage-sticky")
	require.True(t, d.DomainDirty(DomainTargets))

	// Simulate delete of a created credential: restore local credentials to base
	// while leaving secret maps populated (pre-fix ghost).
	d.local = canonicalCommand(ViewToCommand(d.baseView))
	require.True(t, d.DomainDirty(DomainTargets), "credSecrets alone must dirty DomainTargets")

	d.ClearCredentialSecretState("cred-1")
	assert.False(t, d.DomainDirty(DomainTargets), "clearing credSecrets removes sticky dirty")
	assert.True(t, d.CanPublish())
}
