package targets

import (
	"fmt"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
)

type upstreamDeleteUse struct {
	RuleName string
	Primary  bool
	Ingress  generated.RouteConfigIngressProtocol
}

type upstreamDeleteOption struct {
	ObjectID string
	Name     string
	Targets  map[managedconfig.Format]string
}

type upstreamDeletePlan struct {
	Name           string
	Uses           []upstreamDeleteUse
	Options        []upstreamDeleteOption
	CustomBlockers []string
}

func planUpstreamDelete(cmd generated.MutableConfigCommand, objectID string) upstreamDeletePlan {
	plan := upstreamDeletePlan{Name: objectID}
	object, ok := managedconfig.FindObject(cmd, objectID)
	if !ok || object.Kind != generated.ManagedObjectKindUpstream {
		return plan
	}
	plan.Name = object.Name
	ownedTargets := stringSet(managedconfig.Members(object, generated.ManagedResourceRefKindTarget))

	for _, rule := range managedconfig.ProjectTrafficRules(cmd) {
		usesRule := false
		for index, candidate := range rule.Backend.Candidates {
			if !ownedTargets[string(candidate.TargetId)] {
				continue
			}
			usesRule = true
			plan.Uses = append(plan.Uses, upstreamDeleteUse{
				RuleName: rule.Object.Name,
				Primary:  index == 0,
				Ingress:  rule.Route.IngressProtocol,
			})
		}
		if usesRule && rule.Custom {
			plan.CustomBlockers = append(plan.CustomBlockers,
				fmt.Sprintf("Traffic rule %q has custom internal configuration", rule.Object.Name))
		}
	}

	if !plan.needsReplacement() || len(plan.CustomBlockers) > 0 {
		return plan
	}
	for _, upstream := range managedconfig.ProjectUpstreams(cmd) {
		if upstream.Object.Id == object.Id || upstream.Custom {
			continue
		}
		option := upstreamDeleteOption{ObjectID: string(upstream.Object.Id), Name: upstream.Object.Name, Targets: map[managedconfig.Format]string{}}
		for _, target := range upstream.Targets {
			if format, ok := managedconfig.FormatForTarget(target); ok {
				option.Targets[format] = string(target.Id)
			}
		}
		if plan.optionCompatible(option) {
			plan.Options = append(plan.Options, option)
		}
	}
	return plan
}

func (plan upstreamDeletePlan) needsReplacement() bool {
	for _, use := range plan.Uses {
		if use.Primary {
			return true
		}
	}
	return false
}

func (plan upstreamDeletePlan) optionCompatible(option upstreamDeleteOption) bool {
	for _, use := range plan.Uses {
		if use.Primary && replacementTargetForIngress(option.Targets, use.Ingress) == "" {
			return false
		}
	}
	return true
}

func replacementTargetForIngress(targets map[managedconfig.Format]string, ingress generated.RouteConfigIngressProtocol) string {
	switch ingress {
	case generated.RouteConfigIngressProtocolOpenaiResponses:
		return targets[managedconfig.FormatOpenAIResponses]
	case generated.RouteConfigIngressProtocolAnthropicMessages:
		if id := targets[managedconfig.FormatAnthropicMessages]; id != "" {
			return id
		}
		return targets[managedconfig.FormatOpenAIChat]
	default:
		return ""
	}
}

func applyUpstreamDelete(draft *configdraft.Draft, objectID, replacementObjectID string) error {
	working := draft.LocalCommand()
	object, ok := managedconfig.FindObject(working, objectID)
	if !ok || object.Kind != generated.ManagedObjectKindUpstream {
		return fmt.Errorf("upstream no longer exists")
	}
	deletedTargets := stringSet(managedconfig.Members(object, generated.ManagedResourceRefKindTarget))
	replacementTargets := map[managedconfig.Format]string{}
	if replacementObjectID != "" {
		replacement, ok := managedconfig.FindObject(working, replacementObjectID)
		if !ok || replacement.Kind != generated.ManagedObjectKindUpstream {
			return fmt.Errorf("replacement upstream no longer exists")
		}
		for _, id := range managedconfig.Members(replacement, generated.ManagedResourceRefKindTarget) {
			if target, ok := findTarget(working, id); ok {
				if format, ok := managedconfig.FormatForTarget(target); ok {
					replacementTargets[format] = id
				}
			}
		}
	}

	replaced := map[string]string{}
	dropped := map[string]bool{}
	for backendIndex := range working.BackendSets {
		backend := &working.BackendSets[backendIndex]
		ingress := ingressForBackend(working, backend.Id)
		balanced := candidatesBalanced(backend.Candidates)
		existing := map[string]bool{}
		for _, candidate := range backend.Candidates {
			if !deletedTargets[string(candidate.TargetId)] {
				existing[string(candidate.TargetId)] = true
			}
		}
		out := make([]generated.BackendCandidate, 0, len(backend.Candidates))
		for index, candidate := range backend.Candidates {
			oldID := string(candidate.TargetId)
			if !deletedTargets[oldID] {
				out = append(out, candidate)
				continue
			}
			if index > 0 {
				dropped[oldID] = true
				continue
			}
			replacementID := replacementTargetForIngress(replacementTargets, ingress)
			if replacementID == "" {
				return fmt.Errorf("choose a compatible replacement upstream")
			}
			if existing[replacementID] {
				dropped[oldID] = true
				continue
			}
			candidate.TargetId = generated.ConfigID(replacementID)
			replaced[oldID] = replacementID
			existing[replacementID] = true
			out = append(out, candidate)
		}
		if len(out) == 0 && len(backend.Candidates) > 0 {
			return fmt.Errorf("deleting this upstream would leave a traffic rule without a target")
		}
		for index := range out {
			if balanced {
				out[index].Priority = 0
			} else {
				out[index].Priority = index
			}
		}
		backend.Candidates = out
	}

	for policyIndex := range working.ModelPolicies {
		policy := &working.ModelPolicies[policyIndex]
		existing := map[string]bool{}
		for _, mapping := range policy.Mappings {
			if !deletedTargets[string(mapping.TargetId)] {
				existing[string(mapping.TargetId)+"\x00"+mapping.LogicalModel] = true
			}
		}
		out := make([]generated.ModelMapping, 0, len(policy.Mappings))
		for _, mapping := range policy.Mappings {
			oldID := string(mapping.TargetId)
			replacementID := replaced[oldID]
			if replacementID == "" {
				if dropped[oldID] || deletedTargets[oldID] {
					continue
				}
				out = append(out, mapping)
				continue
			}
			key := replacementID + "\x00" + mapping.LogicalModel
			if existing[key] {
				continue
			}
			mapping.TargetId = generated.ConfigID(replacementID)
			existing[key] = true
			out = append(out, mapping)
		}
		policy.Mappings = out
	}

	if !managedconfig.DeleteObject(&working, objectID) {
		return fmt.Errorf("upstream no longer exists")
	}
	draft.Mutate(func(command *generated.MutableConfigCommand) { *command = working })
	return nil
}

func ingressForBackend(cmd generated.MutableConfigCommand, backendID generated.ConfigID) generated.RouteConfigIngressProtocol {
	for _, route := range cmd.Routes {
		if route.BackendSetId == backendID {
			return route.IngressProtocol
		}
	}
	return ""
}

func candidatesBalanced(candidates []generated.BackendCandidate) bool {
	if len(candidates) < 2 {
		return false
	}
	priority := candidates[0].Priority
	for _, candidate := range candidates[1:] {
		if candidate.Priority != priority {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func renderUpstreamDelete(plan upstreamDeletePlan, cursor, width, height int) string {
	rows := make([]detailpane.Row, 0, len(plan.Uses)+len(plan.Options)+2)
	for _, use := range plan.Uses {
		role := "backup"
		if use.Primary {
			role = "primary"
		}
		rows = append(rows, detailpane.Row{Label: "used by", Value: fmt.Sprintf("%s · %s", use.RuleName, role)})
	}
	if plan.needsReplacement() {
		for index, option := range plan.Options {
			marker := "  "
			if index == cursor {
				marker = "▶ "
			}
			rows = append(rows, detailpane.Row{Label: "replace with", Value: marker + option.Name})
		}
	} else {
		rows = append(rows, detailpane.Row{Label: "change", Value: "Remove it from backup lists"})
	}
	return detailpane.Model{
		Title:    fmt.Sprintf("Delete Upstream · %s", plan.Name),
		Sections: []detailpane.Section{{Title: "Traffic impact", Rows: rows}},
		Hints:    []string{"↑↓ choose replacement  enter apply and delete  esc cancel"},
		Width:    width, Height: height,
	}.View()
}

func (p *Page) beginMigrationConfirmation() bool {
	if p.migrationConfirm || p.editor.mode != editorEdit || p.editor.id == "" {
		return false
	}
	loaded := newUpstreamEditor(editorEdit, p.editor.id, p.draft, p.status)
	if loaded.values["api_formats"] == p.editor.values["api_formats"] {
		return false
	}
	current, ok := findSimpleUpstream(p.draft, p.editor.id, p.status)
	if !ok {
		return false
	}
	targets := stringSet(current.TargetIDs)
	var impact []string
	missingIngress := map[generated.RouteConfigIngressProtocol]bool{}
	desiredTargets := map[managedconfig.Format]string{}
	for _, format := range parseUpstreamFormats(p.editor.values) {
		desiredTargets[format] = "available"
	}
	for _, rule := range managedconfig.ProjectTrafficRules(p.draft.LocalCommand()) {
		for _, candidate := range rule.Backend.Candidates {
			if targets[string(candidate.TargetId)] {
				impact = append(impact, rule.Object.Name)
				if rule.Custom {
					p.migrationBlocked = fmt.Sprintf("Traffic rule %q has custom internal configuration", rule.Object.Name)
				}
				if replacementTargetForIngress(desiredTargets, rule.Route.IngressProtocol) == "" {
					missingIngress[rule.Route.IngressProtocol] = true
				}
				break
			}
		}
	}
	if len(impact) == 0 {
		return false
	}
	p.migrationImpact = impact
	p.migrationOptions = nil
	p.migrationCursor = 0
	if len(missingIngress) > 0 && p.migrationBlocked == "" {
		for _, upstream := range managedconfig.ProjectUpstreams(p.draft.LocalCommand()) {
			if string(upstream.Object.Id) == current.ObjectID || upstream.Custom {
				continue
			}
			option := upstreamDeleteOption{ObjectID: string(upstream.Object.Id), Name: upstream.Object.Name, Targets: map[managedconfig.Format]string{}}
			for _, target := range upstream.Targets {
				if format, ok := managedconfig.FormatForTarget(target); ok {
					option.Targets[format] = string(target.Id)
				}
			}
			compatible := true
			for ingress := range missingIngress {
				if replacementTargetForIngress(option.Targets, ingress) == "" {
					compatible = false
					break
				}
			}
			if compatible {
				p.migrationOptions = append(p.migrationOptions, option)
			}
		}
		if len(p.migrationOptions) == 0 {
			p.migrationBlocked = "Add a compatible upstream before removing a format used by traffic"
		}
	}
	p.overlay = overlayMigrationConfirm
	return true
}

func renderMigrationImpact(name string, rules []string, options []upstreamDeleteOption, cursor int, blocked string, width, height int) string {
	rows := make([]detailpane.Row, 0, len(rules)+len(options)+2)
	rows = append(rows, detailpane.Row{Label: "change", Value: "Update API formats and reconnect traffic atomically"})
	for _, rule := range rules {
		rows = append(rows, detailpane.Row{Label: "affected rule", Value: rule})
	}
	for index, option := range options {
		marker := "  "
		if index == cursor {
			marker = "▶ "
		}
		rows = append(rows, detailpane.Row{Label: "replacement", Value: marker + option.Name})
	}
	if blocked != "" {
		rows = append(rows, detailpane.Row{Label: "blocked", Value: blocked})
	}
	return detailpane.Model{
		Title:    fmt.Sprintf("Change API Formats · %s", name),
		Sections: []detailpane.Section{{Title: "Impact preview", Rows: rows}},
		Hints:    []string{"enter continue  esc review fields"},
		Width:    width, Height: height,
	}.View()
}

type upstreamRestorePreview struct {
	Name             string
	Formats          []managedconfig.Format
	DroppedTargets   int
	DroppedEndpoints int
	Object           generated.ManagedObject
	EndpointID       string
	QuotaID          string
	Targets          map[managedconfig.Format]string
	Credentials      map[generated.MutableCredentialCommandProvider]string
}

func previewRestoreUpstream(cmd generated.MutableConfigCommand, objectID string) (upstreamRestorePreview, error) {
	object, ok := managedconfig.FindObject(cmd, objectID)
	if !ok || object.Kind != generated.ManagedObjectKindUpstream {
		return upstreamRestorePreview{}, fmt.Errorf("managed upstream not found")
	}
	endpointIDs := managedconfig.Members(object, generated.ManagedResourceRefKindEndpoint)
	credentialIDs := managedconfig.Members(object, generated.ManagedResourceRefKindCredential)
	targetIDs := managedconfig.Members(object, generated.ManagedResourceRefKindTarget)
	if len(endpointIDs) == 0 || len(credentialIDs) == 0 || len(targetIDs) == 0 {
		return upstreamRestorePreview{}, fmt.Errorf("the managed object no longer has a complete connection bundle")
	}
	endpointSet := stringSet(endpointIDs)
	endpointUse := map[string]int{}
	ownedTargets := stringSet(targetIDs)
	for _, id := range endpointIDs {
		if _, ok := findEndpoint(cmd, id); !ok {
			return upstreamRestorePreview{}, fmt.Errorf("an endpoint is missing")
		}
	}
	for _, id := range targetIDs {
		target, ok := findTarget(cmd, id)
		if !ok {
			return upstreamRestorePreview{}, fmt.Errorf("a target is missing")
		}
		if !endpointSet[string(target.EndpointId)] {
			return upstreamRestorePreview{}, fmt.Errorf("a target points outside this upstream")
		}
		endpointUse[string(target.EndpointId)]++
	}
	endpointID := endpointIDs[0]
	for _, id := range endpointIDs[1:] {
		if endpointUse[id] > endpointUse[endpointID] {
			endpointID = id
		}
	}
	preview := upstreamRestorePreview{
		Name: object.Name, Object: object, EndpointID: endpointID, Targets: map[managedconfig.Format]string{},
		DroppedEndpoints: len(endpointIDs) - 1,
		Credentials:      map[generated.MutableCredentialCommandProvider]string{},
	}
	ownedCredentials := stringSet(credentialIDs)
	for _, target := range cmd.Targets {
		if ownedTargets[string(target.Id)] {
			continue
		}
		if endpointSet[string(target.EndpointId)] || ownedCredentials[string(target.CredentialId)] {
			return upstreamRestorePreview{}, fmt.Errorf("an unmanaged target shares this connection; detach it in Advanced Resources first")
		}
	}
	for _, id := range credentialIDs {
		credential, ok := findCredential(cmd, id)
		if !ok {
			return upstreamRestorePreview{}, fmt.Errorf("a credential is missing")
		}
		if preview.Credentials[credential.Provider] == "" {
			preview.Credentials[credential.Provider] = id
		}
	}
	for _, id := range targetIDs {
		target, ok := findTarget(cmd, id)
		if !ok {
			return upstreamRestorePreview{}, fmt.Errorf("a target is missing")
		}
		format, ok := managedconfig.FormatForTarget(target)
		if !ok {
			return upstreamRestorePreview{}, fmt.Errorf("a target uses an unsupported API format")
		}
		provider, _ := providerForFormat(format)
		if preview.Credentials[provider] == "" {
			return upstreamRestorePreview{}, fmt.Errorf("the %s credential is missing", managedconfig.FormatLabel(format))
		}
		if preview.QuotaID == "" {
			preview.QuotaID = string(target.QuotaGroupId)
		} else if preview.QuotaID != string(target.QuotaGroupId) {
			return upstreamRestorePreview{}, fmt.Errorf("targets use different limit policies")
		}
		if preview.Targets[format] == "" {
			preview.Targets[format] = id
			preview.Formats = append(preview.Formats, format)
		} else {
			preview.DroppedTargets++
		}
	}
	return preview, nil
}

func restoreUpstreamSimple(draft *configdraft.Draft, objectID string) error {
	working := draft.LocalCommand()
	preview, err := previewRestoreUpstream(working, objectID)
	if err != nil {
		return err
	}
	endpoint, _ := findEndpoint(working, preview.EndpointID)
	endpoint.Name = preview.Name
	endpoint.Http2Enabled = true
	endpoint.MaxIdleConnections = 20
	endpoint.IdleConnectionTimeoutMs = 30000
	for index := range working.Endpoints {
		if working.Endpoints[index].Id == endpoint.Id {
			working.Endpoints[index] = endpoint
		}
	}
	ownedEndpoints := stringSet(managedconfig.Members(preview.Object, generated.ManagedResourceRefKindEndpoint))
	endpoints := make([]generated.EndpointConfig, 0, len(working.Endpoints))
	for _, candidate := range working.Endpoints {
		if ownedEndpoints[string(candidate.Id)] && candidate.Id != endpoint.Id {
			continue
		}
		endpoints = append(endpoints, candidate)
	}
	working.Endpoints = endpoints

	keptTargets := map[string]generated.MutableTargetCommand{}
	for _, format := range preview.Formats {
		id := preview.Targets[format]
		provider, _ := providerForFormat(format)
		credentialID := preview.Credentials[provider]
		client := upstreamClientClaude
		if format == managedconfig.FormatOpenAIResponses {
			client = upstreamClientCodex
		}
		preset, err := upstreamPresetFor(client, string(format))
		if err != nil {
			return err
		}
		keptTargets[id] = defaultUpstreamTarget(id, preview.Name+" · "+managedconfig.FormatLabel(format), preview.EndpointID, credentialID, preview.QuotaID, preset)
	}
	ownedTargets := stringSet(managedconfig.Members(preview.Object, generated.ManagedResourceRefKindTarget))
	targets := make([]generated.MutableTargetCommand, 0, len(working.Targets))
	for _, target := range working.Targets {
		if !ownedTargets[string(target.Id)] {
			targets = append(targets, target)
			continue
		}
		if restored, keep := keptTargets[string(target.Id)]; keep {
			targets = append(targets, restored)
		}
	}
	working.Targets = targets

	object := preview.Object
	object.Members = []generated.ManagedResourceRef{{Kind: generated.ManagedResourceRefKindEndpoint, Id: endpoint.Id}}
	for _, id := range managedconfig.Members(preview.Object, generated.ManagedResourceRefKindCredential) {
		object.Members = append(object.Members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindCredential, Id: generated.ConfigID(id)})
	}
	for _, format := range preview.Formats {
		object.Members = append(object.Members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindTarget, Id: generated.ConfigID(preview.Targets[format])})
	}
	managedconfig.ReplaceObject(&working, object)
	draft.Mutate(func(command *generated.MutableConfigCommand) { *command = working })
	return nil
}

func renderRestoreUpstream(cmd generated.MutableConfigCommand, objectID string, width, height int) string {
	preview, err := previewRestoreUpstream(cmd, objectID)
	if err != nil {
		return detailpane.Model{Title: "Restore Simple Structure", Sections: []detailpane.Section{{Title: "Blocked", Rows: []detailpane.Row{{Label: "reason", Value: err.Error()}}}}, Hints: []string{"esc cancel"}, Width: width, Height: height}.View()
	}
	formats := make([]string, 0, len(preview.Formats))
	for _, format := range preview.Formats {
		formats = append(formats, managedconfig.FormatLabel(format))
	}
	rows := []detailpane.Row{
		{Label: "formats", Value: strings.Join(formats, ", ")},
		{Label: "connection", Value: "Keep URL, API keys and limit policy"},
		{Label: "advanced", Value: "Reset HTTP, health and throttle defaults"},
	}
	if preview.DroppedTargets > 0 {
		rows = append(rows, detailpane.Row{Label: "duplicates", Value: fmt.Sprintf("Remove %d duplicate target(s)", preview.DroppedTargets)})
	}
	if preview.DroppedEndpoints > 0 {
		rows = append(rows, detailpane.Row{Label: "endpoints", Value: fmt.Sprintf("Keep the most-used connection and remove %d extra endpoint(s)", preview.DroppedEndpoints)})
	}
	return detailpane.Model{Title: "Restore Simple Structure · " + preview.Name, Sections: []detailpane.Section{{Title: "Impact preview", Rows: rows}}, Hints: []string{"enter restore and apply  esc cancel"}, Width: width, Height: height}.View()
}
