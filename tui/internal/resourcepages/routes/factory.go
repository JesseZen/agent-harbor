package routes

import (
	"context"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
)

// New constructs the Routes owner-tab surface bound to the shared draft.
// Intended for TICKET-011 composition (do not register in app here).
func New(draft *configdraft.Draft, options ...Options) *Page {
	var status RouteStatusProvider
	var discover ModelDiscoverer
	var referrers func(string) []string
	if len(options) > 0 {
		status = options[0].Status
		discover = options[0].DiscoverModels
		referrers = options[0].TrafficRuleReferrers
	}
	page := &Page{
		draft:                draft,
		kind:                 KindTrafficRules,
		statusProvider:       status,
		discoverModels:       discover,
		trafficRuleReferrers: referrers,
		width:                120,
		height:               30,
	}
	page.rebuildTable()
	page.Refresh()
	return page
}

type Options struct {
	Status               RouteStatusProvider
	DiscoverModels       ModelDiscoverer
	TrafficRuleReferrers func(profileID string) []string
}

type ModelDiscoverer func(context.Context, string) ([]string, error)
