package formui

import "github.com/asheshgoplani/agent-deck/internal/coreclient/generated"

// UsedConfigIDs returns every resource ID in the mutable configuration. IDs
// share one namespace in Core, so generated IDs must be checked globally.
func UsedConfigIDs(cmd generated.MutableConfigCommand) map[string]bool {
	used := map[string]bool{}
	for _, item := range cmd.BackendSets {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.ClientProfiles {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.CompatibilityTransforms {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.ContentPolicies {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.Credentials {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.Endpoints {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.ModelPolicies {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.ModelProjections {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.QuotaGroups {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.Routes {
		used[string(item.Id)] = true
	}
	for _, item := range cmd.Targets {
		used[string(item.Id)] = true
	}
	return used
}
