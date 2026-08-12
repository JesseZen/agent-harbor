package quotas

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func sampleQuota(id string) generated.QuotaGroupConfig {
	return generated.QuotaGroupConfig{
		Id:                 generated.ConfigID(id),
		Name:               "quota-" + id,
		MaxConcurrency:     8,
		Rpm:                240,
		ForegroundCapacity: 4,
		BackgroundCapacity: 2,
		ForegroundWeight:   9,
		BackgroundWeight:   1,
		QueueTimeoutMs:     30000,
	}
}

func sampleTarget(id, quotaID string) generated.MutableTargetCommand {
	return generated.MutableTargetCommand{
		Id:           generated.ConfigID(id),
		Name:         "target-" + id,
		Adapter:      generated.MutableTargetCommandAdapterOpenai,
		Bridge:       generated.MutableTargetCommandBridgeOpenaiChat,
		CredentialId: "cred-1",
		EndpointId:   "ep-1",
		QuotaGroupId: generated.ConfigID(quotaID),
		Capabilities: []generated.MutableTargetCommandCapabilities{
			generated.MutableTargetCommandCapabilitiesChat,
		},
		HealthPolicy: generated.HealthPolicyConfig{
			StableProbeIntervalMs: 1000,
			ProbeTimeoutMs:        500,
			FailureThreshold:      3,
		},
		ThrottlePolicy: generated.ThrottlePolicyConfig{
			DefaultCoolingMs: 1000,
			MaxCoolingMs:     60000,
		},
	}
}

func seedDraft(t *testing.T) *configdraft.Draft {
	t.Helper()
	snap := configdraft.FixtureSnapshot()
	snap.MutableConfig.QuotaGroups = []generated.QuotaGroupConfig{
		sampleQuota("quota-default"),
	}
	draft := configdraft.Load(snap)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = []generated.MutableTargetCommand{
			sampleTarget("target-1", "quota-default"),
		}
	})
	return draft
}

func sampleRuntime() []backend.QuotaGroup {
	base := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	return []backend.QuotaGroup{
		{
			ID:              "quota-default",
			Name:            "quota-quota-default",
			RPM:             240,
			MaxConcurrency:  8,
			ForegroundDepth: 2,
			BackgroundDepth: 1,
			NextPermitAt:    base.Add(3 * time.Second),
		},
	}
}

func newTestPage(t *testing.T) (*Page, *configdraft.Draft) {
	t.Helper()
	draft := seedDraft(t)
	page := NewPage(draft)
	page.SetSize(120, 30)
	page.SetRuntime(sampleRuntime())
	page.Sync()
	return page, draft
}
