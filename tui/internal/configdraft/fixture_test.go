package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixtureSnapshot_MinimalDefaults(t *testing.T) {
	snap := FixtureSnapshot()
	assert.Equal(t, int64(1), snap.ActiveGeneration)
	assert.Equal(t, "rev-1", snap.ConfigRevision)
	assert.Equal(t, generated.InstanceID("inst-1"), snap.InstanceId)
	assert.Equal(t, generated.MutableInstanceConfigLogLevelSimple, snap.MutableConfig.Instance.LogLevel)
	assert.Empty(t, snap.MutableConfig.Routes)
	assert.Empty(t, snap.MutableConfig.Credentials)
	assert.Empty(t, snap.MutableConfig.Targets)
}

func TestFixtureSnapshot_WithOptions(t *testing.T) {
	ref := generated.ExternalSecretRef{
		Env:        &generated.EnvSecretLocator{Name: "API_KEY"},
		Exportable: true,
	}
	snap := FixtureSnapshot(
		WithGeneration(99),
		WithRevision("custom-rev"),
		WithInstanceID("custom-inst"),
		WithManagedCredential("cred-1", "Test", "anthropic", 7),
		WithExternalCredential("cred-2", "Ext", "openai", ref),
	)
	assert.Equal(t, int64(99), snap.ActiveGeneration)
	assert.Equal(t, "custom-rev", snap.ConfigRevision)
	require.Len(t, snap.MutableConfig.Credentials, 2)

	managed, err := snap.MutableConfig.Credentials[0].SecretBinding.AsCredentialSecretBinding0()
	require.NoError(t, err)
	assert.Equal(t, generated.CredentialSecretBindingManaged, managed.Mode)

	external, err := snap.MutableConfig.Credentials[1].SecretBinding.AsCredentialSecretBinding1()
	require.NoError(t, err)
	assert.Equal(t, ref, external.Ref)
}

func TestTargetToCommand_StripsGeneration(t *testing.T) {
	target := generated.TargetConfig{
		Id:           "target-1",
		Name:         "My Target",
		Generation:   42,
		Adapter:      "openai",
		Bridge:       "none",
		CredentialId: "cred-1",
		EndpointId:   "ep-1",
		QuotaGroupId: "qg-1",
	}
	cmd := TargetToCommand(target)
	assert.Equal(t, generated.ConfigID("target-1"), cmd.Id)
	assert.Equal(t, "My Target", cmd.Name)
}
