package routes

import (
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

// InboundRefs returns every inbound reference path for id under kind.
func InboundRefs(draft *configdraft.Draft, kind Kind, id string) []string {
	cmd := draft.LocalCommand()
	switch kind {
	case KindRoutes:
		return routeInbound(cmd, id)
	case KindBackendSets:
		return backendSetInbound(cmd, id)
	case KindContentPolicies:
		return contentPolicyInbound(cmd, id)
	case KindModelPolicies:
		return modelPolicyInbound(cmd, id)
	case KindModelProjections:
		return modelProjectionInbound(cmd, id)
	case KindTransforms:
		return transformInbound(cmd, id)
	default:
		return nil
	}
}

func routeInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, p := range cmd.ClientProfiles {
		if string(p.DefaultRouteId) == id {
			paths = append(paths, fmt.Sprintf("$.client_profiles[%d].default_route_id", i))
		}
	}
	for i, xf := range cmd.CompatibilityTransforms {
		if xf.Scope == generated.Route && string(xf.ScopeId) == id {
			paths = append(paths, fmt.Sprintf("$.compatibility_transforms[%d].scope_id", i))
		}
	}
	return paths
}

func backendSetInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, r := range cmd.Routes {
		if string(r.BackendSetId) == id {
			paths = append(paths, fmt.Sprintf("$.routes[%d].backend_set_id", i))
		}
	}
	return paths
}

func contentPolicyInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, r := range cmd.Routes {
		if r.ContentPolicyId != nil && string(*r.ContentPolicyId) == id {
			paths = append(paths, fmt.Sprintf("$.routes[%d].content_policy_id", i))
		}
	}
	return paths
}

func modelPolicyInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, r := range cmd.Routes {
		if string(r.ModelPolicyId) == id {
			paths = append(paths, fmt.Sprintf("$.routes[%d].model_policy_id", i))
		}
	}
	return paths
}

func modelProjectionInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, p := range cmd.ClientProfiles {
		if string(p.ModelProjectionId) == id {
			paths = append(paths, fmt.Sprintf("$.client_profiles[%d].model_projection_id", i))
		}
	}
	return paths
}

func transformInbound(cmd generated.MutableConfigCommand, id string) []string {
	var paths []string
	for i, p := range cmd.ClientProfiles {
		for j, tid := range p.CompatibilityTransformIds {
			if string(tid) == id {
				paths = append(paths, fmt.Sprintf("$.client_profiles[%d].compatibility_transform_ids[%d]", i, j))
			}
		}
	}
	return paths
}
