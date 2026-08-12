package configdraft

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecret_PreserveAdoptsCurrentBindingWhenLocalChangedNonSecret(t *testing.T) {
	refRemote := generated.ExternalSecretRef{
		File:       &generated.FileSecretLocator{Path: "/remote/key"},
		Exportable: false,
	}
	base := FixtureSnapshot(WithManagedCredential("cred-1", "Primary", "openai", 1))
	d := Load(base)
	d.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Credentials[0].Name = "Renamed Locally"
	})

	current := FixtureSnapshot(
		WithGeneration(2),
		WithExternalCredential("cred-1", "Primary", "openai", refRemote),
	)
	d.BeginConflict(current)
	conflicts := d.Reapply()
	assert.Empty(t, conflicts)

	cmd, err := d.Command()
	require.NoError(t, err)
	require.Len(t, cmd.Credentials, 1)
	assert.Equal(t, "Renamed Locally", cmd.Credentials[0].Name)

	action, err := cmd.Credentials[0].SecretAction.AsCredentialSecretAction2()
	require.NoError(t, err)
	assert.Equal(t, generated.CredentialSecretActionExternalRef, action.Mode)
	assert.Equal(t, refRemote, action.Ref)
}
