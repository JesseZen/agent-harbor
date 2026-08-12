package targets

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
)

func newLimitPolicyEditor(mode editorMode, id string, draft *configdraft.Draft) editorState {
	values := map[string]string{
		"name": "", "rpm": "60", "max_concurrency": "4", "foreground_capacity": "16",
		"background_capacity": "4", "queue_timeout_ms": "30000", "foreground_weight": "9", "background_weight": "1",
	}
	if mode == editorEdit && draft != nil {
		for _, policy := range managedconfig.ProjectLimitPolicies(draft.LocalCommand()) {
			if string(policy.Object.Id) != id {
				continue
			}
			values["name"] = policy.Object.Name
			values["rpm"] = strconv.Itoa(policy.Quota.Rpm)
			values["max_concurrency"] = strconv.Itoa(policy.Quota.MaxConcurrency)
			values["foreground_capacity"] = strconv.Itoa(policy.Quota.ForegroundCapacity)
			values["background_capacity"] = strconv.Itoa(policy.Quota.BackgroundCapacity)
			values["queue_timeout_ms"] = strconv.FormatInt(policy.Quota.QueueTimeoutMs, 10)
			values["foreground_weight"] = strconv.Itoa(policy.Quota.ForegroundWeight)
			values["background_weight"] = strconv.Itoa(policy.Quota.BackgroundWeight)
			break
		}
	}
	return editorState{mode: mode, kind: KindLimitPolicies, id: id, values: values, draft: draft}
}

func saveLimitPolicy(draft *configdraft.Draft, mode editorMode, id string, values map[string]string) error {
	name := strings.TrimSpace(values["name"])
	if name == "" {
		return fmt.Errorf("$.name: required")
	}
	rpm, err := positiveInt(values["rpm"])
	if err != nil {
		return fmt.Errorf("$.rpm: enter a positive requests-per-minute limit")
	}
	concurrency, err := positiveInt(values["max_concurrency"])
	if err != nil {
		return fmt.Errorf("$.max_concurrency: enter a positive concurrent-request limit")
	}
	if mode == editorCreate {
		working := draft.LocalCommand()
		if _, _, err := managedconfig.CreateLimitPolicy(&working, name, rpm, concurrency); err != nil {
			return err
		}
		draft.Mutate(func(cmd *generated.MutableConfigCommand) { *cmd = working })
		return nil
	}
	working := draft.LocalCommand()
	object, ok := managedconfig.FindObject(working, id)
	if !ok || object.Kind != generated.ManagedObjectKindLimitPolicy {
		return fmt.Errorf("$.limit_policy: managed policy not found")
	}
	quotaIDs := managedconfig.Members(object, generated.ManagedResourceRefKindQuotaGroup)
	if len(quotaIDs) != 1 {
		return fmt.Errorf("$.limit_policy: custom internal configuration is read-only")
	}
	found := false
	for i := range working.QuotaGroups {
		if string(working.QuotaGroups[i].Id) != quotaIDs[0] {
			continue
		}
		working.QuotaGroups[i].Name = name
		working.QuotaGroups[i].Rpm = rpm
		working.QuotaGroups[i].MaxConcurrency = concurrency
		working.QuotaGroups[i].ForegroundCapacity = intOr(values["foreground_capacity"], working.QuotaGroups[i].ForegroundCapacity)
		working.QuotaGroups[i].BackgroundCapacity = intOr(values["background_capacity"], working.QuotaGroups[i].BackgroundCapacity)
		working.QuotaGroups[i].QueueTimeoutMs = int64Or(values["queue_timeout_ms"], working.QuotaGroups[i].QueueTimeoutMs)
		working.QuotaGroups[i].ForegroundWeight = intOr(values["foreground_weight"], working.QuotaGroups[i].ForegroundWeight)
		working.QuotaGroups[i].BackgroundWeight = intOr(values["background_weight"], working.QuotaGroups[i].BackgroundWeight)
		found = true
		break
	}
	if !found {
		return fmt.Errorf("$.limit_policy: quota group not found")
	}
	object.Name = name
	managedconfig.ReplaceObject(&working, object)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) { *cmd = working })
	return nil
}

func positiveInt(value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("positive integer required")
	}
	return n, nil
}

func intOr(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func int64Or(value string, fallback int64) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
