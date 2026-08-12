package managedconfig

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/stretchr/testify/require"
)

func TestCreateAndProjectManagedConfiguration(t *testing.T) {
	cmd := generated.MutableConfigCommand{}
	_, quota, err := CreateLimitPolicy(&cmd, "Default", 60, 4)
	require.NoError(t, err)

	action := replaceAction("stage-1")
	upstream, targets, err := CreateUpstream(&cmd, UpstreamRequest{
		Name: "Main", BaseURL: "https://api.example.test", QuotaGroupID: string(quota.Id),
		Formats: []Format{FormatOpenAIResponses},
		SecretActions: map[generated.MutableCredentialCommandProvider]generated.CredentialSecretAction{
			generated.MutableCredentialCommandProviderOpenaiCompatible: action,
		},
	})
	require.NoError(t, err)
	require.Equal(t, generated.ConfigID("upstream-main"), upstream.Id)

	rule, err := CreateTrafficRule(&cmd, TrafficRuleRequest{
		Name: "Codex Main", Launcher: "codex", Routing: RoutingSingle,
		PrimaryTarget: targets[FormatOpenAIResponses], ModelHandling: ModelPreserve,
	})
	require.NoError(t, err)
	require.Equal(t, generated.ConfigID("rule-codex-main"), rule.Id)
	require.Len(t, ProjectUpstreams(cmd), 1)
	require.False(t, ProjectUpstreams(cmd)[0].Custom)
	require.Len(t, ProjectLimitPolicies(cmd), 1)
	require.False(t, ProjectLimitPolicies(cmd)[0].Custom)
	require.Len(t, ProjectTrafficRules(cmd), 1)
	require.False(t, ProjectTrafficRules(cmd)[0].Custom)
	require.Len(t, *cmd.ManagedObjects, 3)
}

func TestDeleteObjectDeletesOnlyOwnedBundle(t *testing.T) {
	cmd := generated.MutableConfigCommand{}
	_, quota, err := CreateLimitPolicy(&cmd, "Shared", 32, 10)
	require.NoError(t, err)
	upstream, _, err := CreateUpstream(&cmd, UpstreamRequest{
		Name: "Main", BaseURL: "https://api.example.test", QuotaGroupID: string(quota.Id),
		Formats: []Format{FormatOpenAIResponses},
		SecretActions: map[generated.MutableCredentialCommandProvider]generated.CredentialSecretAction{
			generated.MutableCredentialCommandProviderOpenaiCompatible: replaceAction("stage-1"),
		},
	})
	require.NoError(t, err)
	require.True(t, DeleteObject(&cmd, string(upstream.Id)))
	require.Empty(t, cmd.Endpoints)
	require.Empty(t, cmd.Credentials)
	require.Empty(t, cmd.Targets)
	require.Len(t, cmd.QuotaGroups, 1, "shared limit policy must survive upstream deletion")
	require.Len(t, Objects(cmd), 1)
}

func replaceAction(stage string) generated.CredentialSecretAction {
	var action generated.CredentialSecretAction
	_ = action.FromCredentialSecretAction1(generated.CredentialSecretAction1{
		Mode: generated.CredentialSecretActionReplace, StageId: generated.SecretStageID(stage),
	})
	return action
}
