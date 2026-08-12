package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecret_ReplaceConflictWhenGenerationChanges(t *testing.T) {
	base := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1))
	d := Load(base)
	d.SetCredentialReplace("cred-1", "stage-local")

	current := FixtureSnapshot(
		WithGeneration(2),
		WithManagedCredential("cred-1", "Primary", "openai", 2),
	)
	d.BeginConflict(current)
	conflicts := d.Reapply()
	require.NotEmpty(t, conflicts)
	assert.Contains(t, conflicts[0].Path, "secret_action")
}

func TestSecret_ReplaceSurvivesWhenGenerationUnchanged(t *testing.T) {
	base := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1))
	d := Load(base)
	d.SetCredentialReplace("cred-1", "stage-local")

	current := FixtureSnapshot(
		WithGeneration(2),
		WithManagedCredential("cred-1", "Primary", "openai", 1),
	)
	d.BeginConflict(current)
	conflicts := d.Reapply()
	assert.Empty(t, conflicts)

	action, err := d.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	require.NoError(t, err)
	assert.Equal(t, generated.CredentialSecretActionReplace, action.Mode)
	assert.Equal(t, generated.SecretStageID("stage-local"), action.StageId)
}

func TestSecret_StageLossReplaceRequired(t *testing.T) {
	base := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1))
	d := Load(base)
	d.SetCredentialReplace("cred-1", "stage-lost")
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Credentials[0].Name = "Renamed"
	})

	d.MarkReplaceRequired("cred-1")
	_, err := d.Command()
	require.Error(t, err)
	assert.False(t, d.CanPublish())

	cmd := d.LocalCommand()
	require.Len(t, cmd.Credentials, 1)
	assert.Equal(t, "Renamed", cmd.Credentials[0].Name)
	assert.Equal(t, generated.MutableCredentialCommandProvider("openai"), cmd.Credentials[0].Provider)
}

func TestSecret_ExternalRefConflict(t *testing.T) {
	refBase := generated.ExternalSecretRef{
		File:       &generated.FileSecretLocator{Path: "/a"},
		Exportable: false,
	}
	refLocal := generated.ExternalSecretRef{
		File:       &generated.FileSecretLocator{Path: "/local"},
		Exportable: false,
	}
	refRemote := generated.ExternalSecretRef{
		File:       &generated.FileSecretLocator{Path: "/remote"},
		Exportable: false,
	}

	base := FixtureSnapshot(WithExternalCredential("cred-1", "Ext", "openai", refBase))
	d := Load(base)

	var localAction generated.CredentialSecretAction
	_ = localAction.FromCredentialSecretAction2(generated.CredentialSecretAction2{
		Mode: generated.CredentialSecretActionExternalRef,
		Ref:  refLocal,
	})
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Credentials[0].SecretAction = localAction
	})

	current := FixtureSnapshot(
		WithGeneration(2),
		WithExternalCredential("cred-1", "Ext", "openai", refRemote),
	)
	d.BeginConflict(current)
	conflicts := d.Reapply()
	require.NotEmpty(t, conflicts)
}

func TestSecret_PreserveUsesBaseBinding(t *testing.T) {
	base := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1))
	cmd := ViewToCommand(base.MutableConfig)
	action, err := cmd.Credentials[0].SecretAction.AsCredentialSecretAction0()
	require.NoError(t, err)
	assert.Equal(t, generated.CredentialSecretActionPreserve, action.Mode)
}

func TestViewToCommand_NeverEmitsGenerations(t *testing.T) {
	gen := 5
	snap := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", gen))
	snap.MutableConfig.Targets = []generated.TargetConfig{
		{
			Id:           "target-1",
			Name:         "T1",
			Generation:   99,
			Adapter:      "openai",
			Bridge:       "none",
			CredentialId: "cred-1",
			EndpointId:   "ep-1",
			QuotaGroupId: "qg-1",
		},
	}
	cmd := ViewToCommand(snap.MutableConfig)
	require.Len(t, cmd.Targets, 1)
	// MutableTargetCommand has no Generation field
	assert.Equal(t, generated.ConfigID("target-1"), cmd.Targets[0].Id)
}
