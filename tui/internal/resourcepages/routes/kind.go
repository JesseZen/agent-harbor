package routes

import "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"

// Kind identifies a secondary resource inside the Routes owner tab.
type Kind string

const (
	KindTrafficRules     Kind = "traffic_rules"
	KindRoutes           Kind = "routes"
	KindBackendSets      Kind = "backend_sets"
	KindContentPolicies  Kind = "content_policies"
	KindModelPolicies    Kind = "model_policies"
	KindModelProjections Kind = "model_projections"
	KindTransforms       Kind = "transforms"
)

// KindOrder is the secondary strip order; first entry is the default.
var KindOrder = []Kind{
	KindTrafficRules,
	KindRoutes,
	KindBackendSets,
	KindContentPolicies,
	KindModelPolicies,
	KindModelProjections,
	KindTransforms,
}

func (k Kind) Label() string {
	switch k {
	case KindTrafficRules:
		return "Traffic Rules"
	case KindRoutes:
		return "Routes"
	case KindBackendSets:
		return "Backend Sets"
	case KindContentPolicies:
		return "Content Policies"
	case KindModelPolicies:
		return "Model Policies"
	case KindModelProjections:
		return "Model Projections"
	case KindTransforms:
		return "Transforms"
	default:
		return string(k)
	}
}

func (k Kind) Title() string {
	switch k {
	case KindTrafficRules:
		return "TrafficRules"
	case KindRoutes:
		return "Routes"
	case KindBackendSets:
		return "BackendSets"
	case KindContentPolicies:
		return "ContentPolicies"
	case KindModelPolicies:
		return "ModelPolicies"
	case KindModelProjections:
		return "ModelProjections"
	case KindTransforms:
		return "Transforms"
	default:
		return string(k)
	}
}

func (k Kind) DescriptorKind() generated.ResourceKind {
	switch k {
	case KindTrafficRules:
		return generated.ResourceRoute
	case KindRoutes:
		return generated.ResourceRoute
	case KindBackendSets:
		return generated.ResourceBackendSet
	case KindContentPolicies:
		return generated.ResourceContentPolicy
	case KindModelPolicies:
		return generated.ResourceModelPolicy
	case KindModelProjections:
		return generated.ResourceModelProjection
	case KindTransforms:
		return generated.ResourceCompatibilityTransform
	default:
		return ""
	}
}

func kindIndex(k Kind) int {
	for i, candidate := range KindOrder {
		if candidate == k {
			return i
		}
	}
	return 0
}
