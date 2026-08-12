package targets

import "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"

// Kind identifies a secondary resource inside the Targets owner tab.
type Kind string

const (
	KindUpstreams     Kind = "upstreams"
	KindLimitPolicies Kind = "limit_policies"
	KindTargets       Kind = "targets"
	KindEndpoints     Kind = "endpoints"
	KindCredentials   Kind = "credentials"
)

// KindOrder is the secondary strip order; first entry is the default.
var KindOrder = []Kind{
	KindUpstreams,
	KindLimitPolicies,
	KindTargets,
	KindEndpoints,
	KindCredentials,
}

func (k Kind) Label() string {
	switch k {
	case KindUpstreams:
		return "Upstreams"
	case KindLimitPolicies:
		return "Limit Policies"
	case KindTargets:
		return "Targets"
	case KindEndpoints:
		return "Endpoints"
	case KindCredentials:
		return "Credentials"
	default:
		return string(k)
	}
}

func (k Kind) Title() string {
	switch k {
	case KindUpstreams:
		return "Upstreams"
	case KindLimitPolicies:
		return "Limit Policies"
	case KindTargets:
		return "Targets"
	case KindEndpoints:
		return "Endpoints"
	case KindCredentials:
		return "Credentials"
	default:
		return string(k)
	}
}

func (k Kind) DescriptorKind() generated.ResourceKind {
	switch k {
	case KindUpstreams:
		return generated.ResourceTarget
	case KindLimitPolicies:
		return generated.ResourceQuotaGroup
	case KindTargets:
		return generated.ResourceTarget
	case KindEndpoints:
		return generated.ResourceEndpoint
	case KindCredentials:
		return generated.ResourceCredential
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
