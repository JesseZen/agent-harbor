package targets

import "github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"

func columnsFor(kind Kind) []resourceview.Column {
	switch kind {
	case KindUpstreams:
		return []resourceview.Column{
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "API FORMATS", MinWidth: 16, Priority: 1},
			{Title: "URL", MinWidth: 16, Priority: 3},
			{Title: "LIMIT POLICY", MinWidth: 14, Priority: 4},
			{Title: "HEALTH", MinWidth: 8, Priority: 4},
			{Title: "MODE", MinWidth: 8, Priority: 5},
		}
	case KindLimitPolicies:
		return []resourceview.Column{
			{Title: "NAME", MinWidth: 14, Priority: 0},
			{Title: "RPM", MinWidth: 8, Priority: 1},
			{Title: "MAX REQUESTS", MinWidth: 12, Priority: 2},
			{Title: "UPSTREAMS", MinWidth: 9, Priority: 3},
			{Title: "MODE", MinWidth: 8, Priority: 4},
		}
	case KindTargets:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "HEALTH", MinWidth: 8, Priority: 2},
			{Title: "ELIGIBLE", MinWidth: 8, Priority: 3},
			{Title: "ADAPTER", MinWidth: 10, Priority: 4},
			{Title: "ENDPOINT", MinWidth: 10, Priority: 5},
			{Title: "CREDENTIAL", MinWidth: 10, Priority: 6},
			{Title: "QUOTA", MinWidth: 8, Priority: 7},
		}
	case KindEndpoints:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "BASE_URL", MinWidth: 16, Priority: 2},
			{Title: "PROXY", MinWidth: 8, Priority: 3},
			{Title: "HTTP2", MinWidth: 6, Priority: 4},
			{Title: "PRIVATE", MinWidth: 8, Priority: 5},
		}
	case KindCredentials:
		return []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 0},
			{Title: "PROVIDER", MinWidth: 12, Priority: 2},
			{Title: "GENERATION", MinWidth: 10, Priority: 3},
			{Title: "SECRET", MinWidth: 12, Priority: 4},
		}
	default:
		return nil
	}
}
