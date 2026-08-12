package targets

import (
	"strconv"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

func rowsFor(draft *configdraft.Draft, kind Kind, status TargetStatusProvider) []resourceview.Row {
	cmd := draft.LocalCommand()
	switch kind {
	case KindUpstreams:
		upstreams := aggregateUpstreams(cmd, status)
		rows := make([]resourceview.Row, 0, len(upstreams))
		for _, u := range upstreams {
			rowID := u.ObjectID
			if rowID == "" {
				rowID = u.TargetID
			}
			rows = append(rows, resourceview.Row{ID: rowID, Cells: []string{
				u.Name, u.APIType, u.URL, u.LimitPolicyID, u.Health, u.Mode,
			}})
		}
		return rows
	case KindLimitPolicies:
		policies := managedconfig.ProjectLimitPolicies(cmd)
		rows := make([]resourceview.Row, 0, len(policies))
		for _, policy := range policies {
			mode := "Managed"
			if policy.Custom {
				mode = "Custom"
			}
			usedBy := 0
			for _, target := range cmd.Targets {
				if target.QuotaGroupId == policy.Quota.Id {
					usedBy++
				}
			}
			rows = append(rows, resourceview.Row{ID: string(policy.Object.Id), Cells: []string{
				policy.Object.Name, strconv.Itoa(policy.Quota.Rpm), strconv.Itoa(policy.Quota.MaxConcurrency), strconv.Itoa(usedBy), mode,
			}})
		}
		return rows
	case KindTargets:
		rows := make([]resourceview.Row, 0, len(cmd.Targets))
		for _, t := range cmd.Targets {
			health, eligible := "", ""
			if status != nil {
				if snap, ok := status.Lookup(string(t.Id)); ok {
					health = snap.Health
					eligible = strconv.FormatBool(snap.Eligible)
				}
			}
			rows = append(rows, resourceview.Row{
				ID: string(t.Id),
				Cells: []string{
					string(t.Id),
					t.Name,
					health,
					eligible,
					string(t.Adapter),
					string(t.EndpointId),
					string(t.CredentialId),
					string(t.QuotaGroupId),
				},
			})
		}
		return rows
	case KindEndpoints:
		rows := make([]resourceview.Row, 0, len(cmd.Endpoints))
		for _, e := range cmd.Endpoints {
			proxy := ""
			if e.ProxyUrl != nil {
				proxy = *e.ProxyUrl
			}
			http2 := strconv.FormatBool(e.Http2Enabled)
			private := strconv.FormatBool(e.AllowPrivateNetwork)
			rows = append(rows, resourceview.Row{
				ID: string(e.Id),
				Cells: []string{
					string(e.Id),
					e.Name,
					e.BaseUrl,
					proxy,
					http2,
					private,
				},
			})
		}
		return rows
	case KindCredentials:
		rows := make([]resourceview.Row, 0, len(cmd.Credentials))
		replace := map[string]struct{}{}
		for _, id := range draft.ReplaceRequiredIDs() {
			replace[id] = struct{}{}
		}
		base := draft.BaseView()
		genByID := map[string]string{}
		for _, cv := range base.Credentials {
			if cv.Generation != nil {
				genByID[string(cv.Id)] = strconv.Itoa(*cv.Generation)
			}
		}
		for _, c := range cmd.Credentials {
			gen := genByID[string(c.Id)]
			if gen == "" {
				gen = "-"
			}
			secret := secretSummary(c, replace)
			rows = append(rows, resourceview.Row{
				ID: string(c.Id),
				Cells: []string{
					string(c.Id),
					c.Name,
					string(c.Provider),
					gen,
					secret,
				},
			})
		}
		return rows
	default:
		return nil
	}
}

func secretSummary(c generated.MutableCredentialCommand, replace map[string]struct{}) string {
	if _, ok := replace[string(c.Id)]; ok {
		return "Replace required"
	}
	if preserve, err := c.SecretAction.AsCredentialSecretAction0(); err == nil && preserve.Mode == generated.CredentialSecretActionPreserve {
		return "Configured"
	}
	if replaceAction, err := c.SecretAction.AsCredentialSecretAction1(); err == nil && replaceAction.Mode == generated.CredentialSecretActionReplace {
		return "Configured"
	}
	if external, err := c.SecretAction.AsCredentialSecretAction2(); err == nil && external.Mode == generated.CredentialSecretActionExternalRef {
		return externalRefSummary(external.Ref)
	}
	return "Configured"
}

func externalRefSummary(ref generated.ExternalSecretRef) string {
	switch {
	case ref.Env != nil:
		return "env"
	case ref.File != nil:
		return "file"
	case ref.Keychain != nil:
		return "keychain"
	default:
		return "external"
	}
}

func countFor(draft *configdraft.Draft, kind Kind) int {
	return len(rowsFor(draft, kind, nil))
}

func findCredential(cmd generated.MutableConfigCommand, id string) (generated.MutableCredentialCommand, bool) {
	for _, c := range cmd.Credentials {
		if string(c.Id) == id {
			return c, true
		}
	}
	return generated.MutableCredentialCommand{}, false
}

func findTarget(cmd generated.MutableConfigCommand, id string) (generated.MutableTargetCommand, bool) {
	for _, t := range cmd.Targets {
		if string(t.Id) == id {
			return t, true
		}
	}
	return generated.MutableTargetCommand{}, false
}

func findEndpoint(cmd generated.MutableConfigCommand, id string) (generated.EndpointConfig, bool) {
	for _, e := range cmd.Endpoints {
		if string(e.Id) == id {
			return e, true
		}
	}
	return generated.EndpointConfig{}, false
}

func resourceExists(draft *configdraft.Draft, kind Kind, id string) bool {
	cmd := draft.LocalCommand()
	switch kind {
	case KindUpstreams:
		object, ok := managedconfig.FindObject(cmd, id)
		return ok && object.Kind == generated.ManagedObjectKindUpstream
	case KindLimitPolicies:
		object, ok := managedconfig.FindObject(cmd, id)
		return ok && object.Kind == generated.ManagedObjectKindLimitPolicy
	case KindCredentials:
		_, ok := findCredential(cmd, id)
		return ok
	case KindTargets:
		_, ok := findTarget(cmd, id)
		return ok
	case KindEndpoints:
		_, ok := findEndpoint(cmd, id)
		return ok
	default:
		return false
	}
}

func credentialGeneration(draft *configdraft.Draft, id string) string {
	for _, cv := range draft.BaseView().Credentials {
		if string(cv.Id) == id && cv.Generation != nil {
			return strconv.Itoa(*cv.Generation)
		}
	}
	return "-"
}
