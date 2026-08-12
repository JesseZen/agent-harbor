package routes

import "github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"

func columnsFor(kind Kind) []resourceview.Column {
	switch kind {
	case KindTrafficRules:
		return []resourceview.Column{
			{Title: "CLIENT", MinWidth: 12, Priority: 0},
			{Title: "PRIMARY", MinWidth: 12, Priority: 0},
			{Title: "BACKUP", MinWidth: 12, Priority: 2},
			{Title: "ROUTING", MinWidth: 14, Priority: 3},
			{Title: "STATUS", MinWidth: 10, Priority: 4},
			{Title: "MODE", MinWidth: 10, Priority: 5},
		}
	case KindRoutes:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "INGRESS", MinWidth: 14, Priority: 2},
			{Title: "POLICY", MinWidth: 10, Priority: 3},
			{Title: "BACKEND", MinWidth: 10, Priority: 4},
			{Title: "MODEL", MinWidth: 10, Priority: 5},
			{Title: "ATTEMPTS", MinWidth: 8, Priority: 6},
		}
	case KindBackendSets:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "CANDIDATES", MinWidth: 10, Priority: 2},
			{Title: "CAPABILITIES", MinWidth: 12, Priority: 3},
		}
	case KindContentPolicies:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "MODE", MinWidth: 8, Priority: 2},
			{Title: "MAX_BYTES", MinWidth: 10, Priority: 3},
		}
	case KindModelPolicies:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "MAPPINGS", MinWidth: 8, Priority: 2},
			{Title: "TTL", MinWidth: 8, Priority: 3},
			{Title: "DISCOVERY", MinWidth: 10, Priority: 4},
		}
	case KindModelProjections:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "MODELS", MinWidth: 8, Priority: 2},
		}
	case KindTransforms:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "SCOPE", MinWidth: 10, Priority: 2},
			{Title: "SCOPE_ID", MinWidth: 10, Priority: 3},
			{Title: "OPERATION", MinWidth: 12, Priority: 4},
		}
	default:
		return nil
	}
}
