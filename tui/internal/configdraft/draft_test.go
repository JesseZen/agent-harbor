package configdraft

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewToCommand_EmptyCollectionsEncodeAsArrays(t *testing.T) {
	cmd := ViewToCommand(generated.MutableConfigView{})
	require.NotNil(t, cmd.BackendSets)
	require.NotNil(t, cmd.ClientProfiles)
	require.NotNil(t, cmd.CompatibilityTransforms)
	require.NotNil(t, cmd.ContentPolicies)
	require.NotNil(t, cmd.Credentials)
	require.NotNil(t, cmd.Endpoints)
	require.NotNil(t, cmd.ModelPolicies)
	require.NotNil(t, cmd.ModelProjections)
	require.NotNil(t, cmd.QuotaGroups)
	require.NotNil(t, cmd.Routes)
	require.NotNil(t, cmd.Targets)

	raw, err := json.Marshal(cmd)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), ":null", "empty resource collections must encode as []: %s", strings.TrimSpace(string(raw)))
}

func TestViewToCommand_PreservesManagedObjects(t *testing.T) {
	objects := []generated.ManagedObject{{
		Id: "upstream-main", Name: "Main", Kind: generated.ManagedObjectKindUpstream,
		Members: []generated.ManagedResourceRef{{Kind: generated.ManagedResourceRefKindEndpoint, Id: "endpoint-main"}},
	}}
	cmd := ViewToCommand(generated.MutableConfigView{ManagedObjects: &objects})
	require.NotNil(t, cmd.ManagedObjects)
	require.Equal(t, objects, *cmd.ManagedObjects)
	(*cmd.ManagedObjects)[0].Members[0].Id = "changed"
	require.Equal(t, generated.ConfigID("endpoint-main"), objects[0].Members[0].Id)
}

func TestViewToCommand_ManagedPreserve(t *testing.T) {
	snap := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 3))
	cmd := ViewToCommand(snap.MutableConfig)
	require.Len(t, cmd.Credentials, 1)

	action, err := cmd.Credentials[0].SecretAction.AsCredentialSecretAction0()
	require.NoError(t, err)
	assert.Equal(t, generated.CredentialSecretActionPreserve, action.Mode)
}

func TestViewToCommand_ExternalRef(t *testing.T) {
	ref := generated.ExternalSecretRef{
		File:       &generated.FileSecretLocator{Path: "/secrets/key"},
		Exportable: false,
	}
	snap := FixtureSnapshot(WithExternalCredential("cred-1", "External", "openai", ref))
	cmd := ViewToCommand(snap.MutableConfig)
	require.Len(t, cmd.Credentials, 1)

	action, err := cmd.Credentials[0].SecretAction.AsCredentialSecretAction2()
	require.NoError(t, err)
	assert.Equal(t, generated.CredentialSecretActionExternalRef, action.Mode)
	assert.Equal(t, ref, action.Ref)
}

func TestDraft_FreshLoadNotDirty(t *testing.T) {
	d := Load(FixtureSnapshot())
	assert.False(t, d.IsDirty(), "domains: %v", d.DirtyDomains())
}

func TestDraft_DiscardRestoresBase(t *testing.T) {
	snap := FixtureSnapshot()
	d := Load(snap)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	assert.True(t, d.IsDirty())
	assert.True(t, d.DomainDirty(DomainInstance))

	d.Discard()
	assert.False(t, d.IsDirty())
	assert.Equal(t, generated.MutableInstanceConfigLogLevelSimple, d.LocalCommand().Instance.LogLevel)
}

func TestDraft_DisconnectedDisablesPublish(t *testing.T) {
	snap := FixtureSnapshot()
	d := Load(snap)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	assert.True(t, d.CanPublish())

	d.SetDisconnected(true)
	assert.False(t, d.CanPublish())
	assert.True(t, d.IsDirty())
}

func TestDraft_SecretCleanupPendingDisablesPublish(t *testing.T) {
	snap := FixtureSnapshot()
	d := Load(snap)
	assert.True(t, d.CanPublish())

	d.SetMutationStatus(generated.ConfigMutationStatusSecretCleanupPending)
	assert.False(t, d.CanPublish())
}

func TestDraft_LoadMetadata(t *testing.T) {
	snap := FixtureSnapshot(
		WithGeneration(42),
		WithRevision("rev-test"),
		WithInstanceID("inst-test"),
	)
	d := Load(snap)
	assert.Equal(t, int64(42), d.Generation())
	assert.Equal(t, "rev-test", d.Revision())
	assert.Equal(t, "inst-test", d.InstanceID())
}
