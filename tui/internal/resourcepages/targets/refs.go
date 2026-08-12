package targets

import (
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
)

// InboundRefs returns every inbound reference path for id under kind.
func InboundRefs(draft *configdraft.Draft, kind Kind, id string) []string {
	cmd := draft.LocalCommand()
	switch kind {
	case KindUpstreams:
		object, ok := managedconfig.FindObject(cmd, id)
		if !ok {
			return nil
		}
		var paths []string
		for _, targetID := range managedconfig.Members(object, generated.ManagedResourceRefKindTarget) {
			paths = append(paths, targetInbound(cmd, targetID)...)
		}
		return paths
	case KindLimitPolicies:
		object, ok := managedconfig.FindObject(cmd, id)
		if !ok {
			return nil
		}
		var paths []string
		for _, quotaID := range managedconfig.Members(object, generated.ManagedResourceRefKindQuotaGroup) {
			for i, target := range cmd.Targets {
				if string(target.QuotaGroupId) == quotaID {
					paths = append(paths, fmt.Sprintf("$.targets[%d].quota_group_id", i))
				}
			}
		}
		return paths
	case KindCredentials:
		return credentialInbound(cmd, id)
	case KindEndpoints:
		return endpointInbound(cmd, id)
	case KindTargets:
		return targetInbound(cmd, id)
	default:
		return nil
	}
}

func credentialInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, t := range cmd.Targets {
		if string(t.CredentialId) == id {
			paths = append(paths, fmt.Sprintf("$.targets[%d].credential_id", i))
		}
	}
	return paths
}

func endpointInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, t := range cmd.Targets {
		if string(t.EndpointId) == id {
			paths = append(paths, fmt.Sprintf("$.targets[%d].endpoint_id", i))
		}
	}
	return paths
}

func targetInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, bs := range cmd.BackendSets {
		for j, c := range bs.Candidates {
			if string(c.TargetId) == id {
				paths = append(paths, fmt.Sprintf("$.backend_sets[%d].candidates[%d].target_id", i, j))
			}
		}
	}
	for i, mp := range cmd.ModelPolicies {
		for j, m := range mp.Mappings {
			if string(m.TargetId) == id {
				paths = append(paths, fmt.Sprintf("$.model_policies[%d].mappings[%d].target_id", i, j))
			}
		}
	}
	return paths
}
