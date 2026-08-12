package routes

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

const (
	routingSingle   = "single"
	routingFailover = "failover"
	routingBalance  = "load_balance"
	modelPreserve   = "preserve"
	modelOverride   = "override"
	modelMap        = "map"
	wildcardModel   = "*"
)

type simpleRuleClientTemplate struct {
	launcher generated.MutableClientProfileLauncher
	ingress  generated.RouteConfigIngressProtocol
}

var simpleRuleClientTemplates = map[string]simpleRuleClientTemplate{
	"codex": {
		launcher: generated.MutableClientProfileLauncherCodex,
		ingress:  generated.RouteConfigIngressProtocolOpenaiResponses,
	},
	"claude": {
		launcher: generated.MutableClientProfileLauncherClaude,
		ingress:  generated.RouteConfigIngressProtocolAnthropicMessages,
	},
}

func simpleRuleLauncherValues() []string { return []string{"codex", "claude"} }

func newTrafficRuleCreateEditor() editorState {
	return editorState{mode: editorCreate, kind: KindTrafficRules, values: map[string]string{
		"name": "", "launcher": "codex", "model_strategy": modelPreserve, "routing": routingSingle,
		"primary_target_id": "", "backup_target_id": "",
	}}
}

func compatibleCreateTargets(draft *configdraft.Draft, launcher string) []string {
	template, ok := simpleRuleClientTemplates[strings.TrimSpace(launcher)]
	if !ok || draft == nil {
		return nil
	}
	cmd := draft.LocalCommand()
	managedTargets := managedUpstreamTargetIDs(cmd)
	route := generated.RouteConfig{IngressProtocol: template.ingress}
	backend := generated.BackendSetConfig{}
	ids := make([]string, 0, len(cmd.Targets))
	for _, target := range cmd.Targets {
		if !managedTargets[string(target.Id)] {
			continue
		}
		if targetCompatibleWithRoute(route, backend, target) {
			ids = append(ids, string(target.Id))
		}
	}
	sort.Strings(ids)
	return ids
}

func managedUpstreamTargetIDs(cmd generated.MutableConfigCommand) map[string]bool {
	ids := map[string]bool{}
	for _, object := range managedconfig.ObjectsByKind(cmd, generated.ManagedObjectKindUpstream) {
		for _, id := range managedconfig.Members(object, generated.ManagedResourceRefKindTarget) {
			ids[id] = true
		}
	}
	return ids
}

type trafficRuleRestorePreview struct {
	Name       string
	Launcher   string
	TargetID   string
	TargetName string
}

func previewRestoreTrafficRule(cmd generated.MutableConfigCommand, objectID string) (trafficRuleRestorePreview, error) {
	object, ok := managedconfig.FindObject(cmd, objectID)
	if !ok || object.Kind != generated.ManagedObjectKindTrafficRule {
		return trafficRuleRestorePreview{}, fmt.Errorf("managed traffic rule not found")
	}
	profileIDs := managedconfig.Members(object, generated.ManagedResourceRefKindClientProfile)
	routeIDs := managedconfig.Members(object, generated.ManagedResourceRefKindRoute)
	backendIDs := managedconfig.Members(object, generated.ManagedResourceRefKindBackendSet)
	policyIDs := managedconfig.Members(object, generated.ManagedResourceRefKindModelPolicy)
	projectionIDs := managedconfig.Members(object, generated.ManagedResourceRefKindModelProjection)
	if len(profileIDs) != 1 || len(routeIDs) != 1 || len(backendIDs) != 1 || len(policyIDs) != 1 || len(projectionIDs) != 1 {
		return trafficRuleRestorePreview{}, fmt.Errorf("the managed object no longer has one complete rule bundle")
	}
	profile, ok := findTrafficProfile(cmd, profileIDs[0])
	if !ok {
		return trafficRuleRestorePreview{}, fmt.Errorf("the CLI profile is missing")
	}
	template, ok := simpleRuleClientTemplates[string(profile.Launcher)]
	if !ok {
		return trafficRuleRestorePreview{}, fmt.Errorf("only Codex and Claude rules can be restored")
	}
	backend, _ := findBackendSet(cmd, backendIDs[0])
	managedTargets := managedUpstreamTargetIDs(cmd)
	choose := func(ids []generated.BackendCandidate) (generated.MutableTargetCommand, bool) {
		for _, candidate := range ids {
			target, exists := findRouteTarget(cmd, string(candidate.TargetId))
			if exists && managedTargets[string(target.Id)] && targetCompatibleWithRoute(generated.RouteConfig{IngressProtocol: template.ingress}, generated.BackendSetConfig{}, target) {
				return target, true
			}
		}
		return generated.MutableTargetCommand{}, false
	}
	target, ok := choose(backend.Candidates)
	if !ok {
		all := make([]generated.BackendCandidate, 0, len(cmd.Targets))
		for _, candidate := range cmd.Targets {
			all = append(all, generated.BackendCandidate{TargetId: candidate.Id})
		}
		target, ok = choose(all)
	}
	if !ok {
		return trafficRuleRestorePreview{}, fmt.Errorf("no compatible managed upstream is available")
	}
	return trafficRuleRestorePreview{
		Name: object.Name, Launcher: string(profile.Launcher), TargetID: string(target.Id), TargetName: targetDisplayName(cmd, string(target.Id)),
	}, nil
}

func restoreTrafficRuleSimple(draft *configdraft.Draft, objectID string) error {
	working := draft.LocalCommand()
	preview, err := previewRestoreTrafficRule(working, objectID)
	if err != nil {
		return err
	}
	object, _ := managedconfig.FindObject(working, objectID)
	profileID := managedconfig.Members(object, generated.ManagedResourceRefKindClientProfile)[0]
	routeID := managedconfig.Members(object, generated.ManagedResourceRefKindRoute)[0]
	backendID := managedconfig.Members(object, generated.ManagedResourceRefKindBackendSet)[0]
	policyID := managedconfig.Members(object, generated.ManagedResourceRefKindModelPolicy)[0]
	projectionID := managedconfig.Members(object, generated.ManagedResourceRefKindModelProjection)[0]

	templateCommand := generated.MutableConfigCommand{Targets: append([]generated.MutableTargetCommand(nil), working.Targets...)}
	created, err := managedconfig.CreateTrafficRule(&templateCommand, managedconfig.TrafficRuleRequest{
		Name: preview.Name, Launcher: preview.Launcher, Routing: managedconfig.RoutingSingle,
		PrimaryTarget: preview.TargetID, ModelHandling: managedconfig.ModelPreserve,
	})
	if err != nil {
		return err
	}
	templateRule := managedconfig.ProjectTrafficRules(templateCommand)
	if len(templateRule) != 1 || templateRule[0].Object.Id != created.Id {
		return fmt.Errorf("failed to construct the simple template")
	}
	template := templateRule[0]
	template.Profile.Id = generated.ConfigID(profileID)
	template.Profile.DefaultRouteId = generated.ConfigID(routeID)
	template.Profile.ModelProjectionId = generated.ConfigID(projectionID)
	template.Route.Id = generated.ConfigID(routeID)
	template.Route.BackendSetId = generated.ConfigID(backendID)
	template.Route.ModelPolicyId = generated.ConfigID(policyID)
	template.Backend.Id = generated.ConfigID(backendID)
	template.Policy.Id = generated.ConfigID(policyID)
	template.Projection.Id = generated.ConfigID(projectionID)
	replaceTrafficProfile(&working, profileID, template.Profile)
	replaceTrafficRoute(&working, routeID, template.Route)
	replaceTrafficBackend(&working, backendID, template.Backend)
	replaceTrafficPolicy(&working, policyID, template.Policy)
	replaceTrafficProjection(&working, projectionID, template.Projection)
	draft.Mutate(func(command *generated.MutableConfigCommand) { *command = working })
	return nil
}

func renderRestoreTrafficRule(cmd generated.MutableConfigCommand, objectID string, width, height int) string {
	preview, err := previewRestoreTrafficRule(cmd, objectID)
	if err != nil {
		return detailpane.Model{Title: "Restore Simple Structure", Sections: []detailpane.Section{{Title: "Blocked", Rows: []detailpane.Row{{Label: "reason", Value: err.Error()}}}}, Hints: []string{"esc cancel"}, Width: width, Height: height}.View()
	}
	return detailpane.Model{
		Title: "Restore Simple Structure · " + preview.Name,
		Sections: []detailpane.Section{{Title: "Impact preview", Rows: []detailpane.Row{
			{Label: "CLI", Value: preview.Launcher}, {Label: "upstream", Value: preview.TargetName},
			{Label: "routing", Value: "Single upstream"}, {Label: "models", Value: "Preserve client model"},
			{Label: "advanced", Value: "Reset route, backend, model policy and projection defaults"},
		}}},
		Hints: []string{"enter restore and apply  esc cancel"}, Width: width, Height: height,
	}.View()
}

func replaceTrafficProfile(cmd *generated.MutableConfigCommand, id string, value generated.MutableClientProfile) {
	for index := range cmd.ClientProfiles {
		if string(cmd.ClientProfiles[index].Id) == id {
			cmd.ClientProfiles[index] = value
			return
		}
	}
}
func replaceTrafficRoute(cmd *generated.MutableConfigCommand, id string, value generated.RouteConfig) {
	for index := range cmd.Routes {
		if string(cmd.Routes[index].Id) == id {
			cmd.Routes[index] = value
			return
		}
	}
}
func replaceTrafficBackend(cmd *generated.MutableConfigCommand, id string, value generated.BackendSetConfig) {
	for index := range cmd.BackendSets {
		if string(cmd.BackendSets[index].Id) == id {
			cmd.BackendSets[index] = value
			return
		}
	}
}
func replaceTrafficPolicy(cmd *generated.MutableConfigCommand, id string, value generated.ModelPolicyConfig) {
	for index := range cmd.ModelPolicies {
		if string(cmd.ModelPolicies[index].Id) == id {
			cmd.ModelPolicies[index] = value
			return
		}
	}
}
func replaceTrafficProjection(cmd *generated.MutableConfigCommand, id string, value generated.ModelProjectionConfig) {
	for index := range cmd.ModelProjections {
		if string(cmd.ModelProjections[index].Id) == id {
			cmd.ModelProjections[index] = value
			return
		}
	}
}

var nonSimpleRuleID = regexp.MustCompile(`[^a-z0-9_-]+`)

func simpleRuleIDBase(name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = nonSimpleRuleID.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-_")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "rule-" + base
	}
	if len(base) > 40 {
		base = strings.Trim(base[:40], "-_")
	}
	return base
}

type simpleRuleIDs struct {
	backend, policy, projection, route, profile string
}

func uniqueSimpleRuleIDs(cmd generated.MutableConfigCommand, name string) simpleRuleIDs {
	used := allConfigIDs(cmd)
	base := simpleRuleIDBase(name)
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		ids := simpleRuleIDs{
			backend: "backend-" + candidate, policy: "policy-" + candidate,
			projection: "projection-" + candidate, route: "route-" + candidate,
			profile: "profile-" + candidate,
		}
		if !used[ids.backend] && !used[ids.policy] && !used[ids.projection] && !used[ids.route] && !used[ids.profile] {
			return ids
		}
	}
}

func allConfigIDs(cmd generated.MutableConfigCommand) map[string]bool {
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

func createSimpleTrafficRule(draft *configdraft.Draft, values map[string]string) error {
	if draft == nil {
		return fmt.Errorf("$.traffic_rule: configuration unavailable")
	}
	name := strings.TrimSpace(values["name"])
	if name == "" {
		return fmt.Errorf("$.name: required")
	}
	template, ok := simpleRuleClientTemplates[strings.TrimSpace(values["launcher"])]
	if !ok {
		return fmt.Errorf("$.launcher: supported simple launchers are codex and claude")
	}
	allowed := map[string]bool{}
	for _, id := range compatibleCreateTargets(draft, string(template.launcher)) {
		allowed[id] = true
	}
	primary := strings.TrimSpace(values["primary_target_id"])
	if primary == "" || !allowed[primary] {
		return fmt.Errorf("$.primary_target_id: compatible upstream required")
	}
	mode := strings.TrimSpace(values["routing"])
	candidates := []generated.BackendCandidate{{TargetId: generated.ConfigID(primary), Priority: 0, Weight: 1}}
	targetIDs := []string{primary}
	switch mode {
	case routingSingle:
	case routingFailover, routingBalance:
		backup := strings.TrimSpace(values["backup_target_id"])
		if backup == "" || backup == primary || !allowed[backup] {
			return fmt.Errorf("$.backup_target_id: distinct compatible upstream required")
		}
		priority := 1
		if mode == routingBalance {
			priority = 0
		}
		candidates = append(candidates, generated.BackendCandidate{TargetId: generated.ConfigID(backup), Priority: priority, Weight: 1})
		targetIDs = append(targetIDs, backup)
	default:
		return fmt.Errorf("$.routing: invalid")
	}

	projectionModels := []string{}
	mappings := []generated.ModelMapping{}
	appendMapping := func(targetID, logical, physical string) {
		mappings = append(mappings, generated.ModelMapping{LogicalModel: logical, PhysicalModel: physical, TargetId: generated.ConfigID(targetID)})
	}
	switch strings.TrimSpace(values["model_strategy"]) {
	case modelPreserve:
		for _, targetID := range targetIDs {
			appendMapping(targetID, wildcardModel, wildcardModel)
		}
	case modelOverride:
		primaryModel := strings.TrimSpace(values["primary_upstream_model"])
		if primaryModel == "" {
			return fmt.Errorf("$.primary_upstream_model: select or enter an upstream model")
		}
		appendMapping(primary, wildcardModel, primaryModel)
		if len(targetIDs) == 2 {
			backupModel := strings.TrimSpace(values["backup_upstream_model"])
			if backupModel == "" {
				return fmt.Errorf("$.backup_upstream_model: select or enter an upstream model")
			}
			appendMapping(targetIDs[1], wildcardModel, backupModel)
		}
	case modelMap:
		count := arrayLenFromValues(values, "model_mappings")
		if count == 0 {
			return fmt.Errorf("$.model_mappings: add at least one model mapping")
		}
		seenModels := map[string]bool{}
		for index := 0; index < count; index++ {
			prefix := fmt.Sprintf("model_mappings[%d]", index)
			logical := strings.TrimSpace(values[prefix+".client_model"])
			primaryModel := strings.TrimSpace(values[prefix+".primary_model"])
			if logical == "" || logical == wildcardModel {
				return fmt.Errorf("$.%s.client_model: enter a client model", prefix)
			}
			if seenModels[logical] {
				return fmt.Errorf("$.%s.client_model: duplicate client model %q", prefix, logical)
			}
			if primaryModel == "" {
				return fmt.Errorf("$.%s.primary_model: select or enter an upstream model", prefix)
			}
			seenModels[logical] = true
			projectionModels = append(projectionModels, logical)
			appendMapping(primary, logical, primaryModel)
			if len(targetIDs) == 2 {
				backupModel := strings.TrimSpace(values[prefix+".backup_model"])
				if backupModel == "" {
					return fmt.Errorf("$.%s.backup_model: select or enter an upstream model", prefix)
				}
				appendMapping(targetIDs[1], logical, backupModel)
			}
		}
	default:
		return fmt.Errorf("$.model_strategy: choose preserve, override, or map")
	}

	request := managedconfig.TrafficRuleRequest{
		Name: name, Launcher: strings.TrimSpace(values["launcher"]), Routing: managedconfig.Routing(mode),
		PrimaryTarget: primary, ModelHandling: managedconfig.ModelHandling(strings.TrimSpace(values["model_strategy"])),
	}
	if len(targetIDs) == 2 {
		request.BackupTarget = targetIDs[1]
	}
	if request.ModelHandling == managedconfig.ModelOverride {
		request.Override = managedconfig.ModelPair{
			Primary: strings.TrimSpace(values["primary_upstream_model"]),
			Backup:  strings.TrimSpace(values["backup_upstream_model"]),
		}
	}
	if request.ModelHandling == managedconfig.ModelMap {
		for index := 0; index < arrayLenFromValues(values, "model_mappings"); index++ {
			prefix := fmt.Sprintf("model_mappings[%d]", index)
			request.Mappings = append(request.Mappings, managedconfig.ModelPair{
				Client:  strings.TrimSpace(values[prefix+".client_model"]),
				Primary: strings.TrimSpace(values[prefix+".primary_model"]),
				Backup:  strings.TrimSpace(values[prefix+".backup_model"]),
			})
		}
	}
	var createErr error
	draft.Mutate(func(config *generated.MutableConfigCommand) {
		_, createErr = managedconfig.CreateTrafficRule(config, request)
	})
	return createErr
}

type RouteRuntimeStatus struct {
	EligibleTargetIDs []string
}

type RouteStatusProvider interface {
	Lookup(id string) (RouteRuntimeStatus, bool)
}

type StaticRouteStatusProvider map[string]RouteRuntimeStatus

func (provider StaticRouteStatusProvider) Lookup(id string) (RouteRuntimeStatus, bool) {
	value, ok := provider[id]
	return value, ok
}

type trafficRule struct {
	ObjectID   string
	ProfileID  string
	ClientName string
	RouteID    string
	BackendID  string
	PrimaryID  string
	BackupID   string
	Primary    string
	Backup     string
	Routing    string
	Status     string
	Mode       string
	Editable   bool
}

func trafficRules(cmd generated.MutableConfigCommand, status RouteStatusProvider) []trafficRule {
	managedRules := managedconfig.ProjectTrafficRules(cmd)
	rules := make([]trafficRule, 0, len(managedRules))
	for _, managed := range managedRules {
		primary, backup, routing := summarizeCandidates(managed.Backend.Candidates)
		rule := trafficRule{
			ObjectID: string(managed.Object.Id), ProfileID: string(managed.Profile.Id), ClientName: managed.Object.Name,
			RouteID: string(managed.Route.Id), BackendID: string(managed.Backend.Id), PrimaryID: primary, BackupID: backup,
			Primary: targetDisplayName(cmd, primary), Backup: targetDisplayName(cmd, backup), Routing: routing,
			Mode: "Managed", Editable: !managed.Custom && routeIsSimple(cmd, managed.Profile, managed.Route, managed.Backend),
		}
		if managed.Custom || !rule.Editable {
			rule.Mode = "Custom"
		}
		if rule.Backup == "" {
			rule.Backup = "—"
		}
		if status != nil {
			if snapshot, ok := status.Lookup(rule.RouteID); ok {
				rule.Status = fmt.Sprintf("%d/%d eligible", len(snapshot.EligibleTargetIDs), len(managed.Backend.Candidates))
			}
		}
		if rule.Status == "" {
			rule.Status = "—"
		}
		rules = append(rules, rule)
	}
	return rules
}

func findTrafficRule(cmd generated.MutableConfigCommand, profileID string, status RouteStatusProvider) (trafficRule, bool) {
	for _, rule := range trafficRules(cmd, status) {
		if rule.ProfileID == profileID || rule.ObjectID == profileID {
			return rule, true
		}
	}
	return trafficRule{}, false
}

func routeIsSimple(cmd generated.MutableConfigCommand, profile generated.MutableClientProfile, route generated.RouteConfig, backend generated.BackendSetConfig) bool {
	if len(backend.Candidates) < 1 || len(backend.Candidates) > 2 {
		return false
	}
	backendUses := 0
	for _, candidateRoute := range cmd.Routes {
		if candidateRoute.BackendSetId == backend.Id {
			backendUses++
		}
	}
	if backendUses != 1 {
		return false
	}
	profileUses := 0
	for _, candidateProfile := range cmd.ClientProfiles {
		if candidateProfile.DefaultRouteId == route.Id {
			profileUses++
		}
	}
	if profileUses != 1 {
		return false
	}
	if route.ContentPolicyId != nil || len(profile.CompatibilityTransformIds) > 0 {
		return false
	}
	if _, _, routing := summarizeCandidates(backend.Candidates); strings.HasPrefix(routing, "Custom") {
		return false
	}
	for _, candidate := range backend.Candidates {
		target, ok := findRouteTarget(cmd, string(candidate.TargetId))
		if !ok || !targetCompatibleWithRoute(route, backend, target) {
			return false
		}
	}
	policy, ok := findModelPolicy(cmd, string(route.ModelPolicyId))
	if !ok || !modelMappingsSymmetric(policy, backend.Candidates) {
		return false
	}
	projection, ok := findModelProjection(cmd, string(profile.ModelProjectionId))
	if !ok {
		return false
	}
	for _, candidate := range backend.Candidates {
		if !mappingsCoverProjection(mappingsForTarget(policy, string(candidate.TargetId)), projection.LogicalModels) {
			return false
		}
	}
	return true
}

func summarizeCandidates(candidates []generated.BackendCandidate) (string, string, string) {
	if len(candidates) == 1 && candidates[0].Priority == 0 && candidates[0].Weight == 1 {
		return string(candidates[0].TargetId), "", "Single"
	}
	if len(candidates) != 2 || candidates[0].Weight != 1 || candidates[1].Weight != 1 {
		primary, backup := "", ""
		if len(candidates) > 0 {
			primary = string(candidates[0].TargetId)
		}
		if len(candidates) > 1 {
			backup = string(candidates[1].TargetId)
		}
		return primary, backup, fmt.Sprintf("Custom (%d)", len(candidates))
	}
	sorted := append([]generated.BackendCandidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })
	if sorted[0].Priority == 0 && sorted[1].Priority == 1 {
		return string(sorted[0].TargetId), string(sorted[1].TargetId), "Failover"
	}
	if sorted[0].Priority == 0 && sorted[1].Priority == 0 {
		return string(sorted[0].TargetId), string(sorted[1].TargetId), "Load balance"
	}
	primary, backup := "", ""
	if len(candidates) > 0 {
		primary = string(candidates[0].TargetId)
	}
	if len(candidates) > 1 {
		backup = string(candidates[1].TargetId)
	}
	return primary, backup, fmt.Sprintf("Custom (%d)", len(candidates))
}

func targetDisplayName(cmd generated.MutableConfigCommand, targetID string) string {
	if targetID == "" {
		return ""
	}
	target, ok := findRouteTarget(cmd, targetID)
	if !ok {
		return targetID
	}
	for _, endpoint := range cmd.Endpoints {
		if endpoint.Id == target.EndpointId && endpoint.Name != "" {
			return endpoint.Name
		}
	}
	if target.Name != "" {
		return target.Name
	}
	return targetID
}

func findRouteTarget(cmd generated.MutableConfigCommand, id string) (generated.MutableTargetCommand, bool) {
	for _, target := range cmd.Targets {
		if string(target.Id) == id {
			return target, true
		}
	}
	return generated.MutableTargetCommand{}, false
}

func targetCompatibleWithRoute(route generated.RouteConfig, backend generated.BackendSetConfig, target generated.MutableTargetCommand) bool {
	capability := ""
	openAIAdapter := target.Adapter == generated.MutableTargetCommandAdapterOpenai ||
		target.Adapter == generated.MutableTargetCommandAdapterOpenaiCompatible ||
		target.Adapter == generated.MutableTargetCommandAdapterSub2apiOpenai
	switch {
	case route.IngressProtocol == generated.RouteConfigIngressProtocolAnthropicMessages &&
		target.Bridge == generated.MutableTargetCommandBridgeAnthropicMessages && target.Adapter == generated.MutableTargetCommandAdapterAnthropic:
		capability = "messages"
	case route.IngressProtocol == generated.RouteConfigIngressProtocolAnthropicMessages &&
		target.Bridge == generated.MutableTargetCommandBridgeAnthropicToOpenai && openAIAdapter:
		capability = "chat"
	case route.IngressProtocol == generated.RouteConfigIngressProtocolOpenaiResponses &&
		target.Bridge == generated.MutableTargetCommandBridgeOpenaiResponses && openAIAdapter:
		capability = "responses"
	case route.IngressProtocol == generated.RouteConfigIngressProtocolOpenaiChat &&
		target.Bridge == generated.MutableTargetCommandBridgeOpenaiChat && openAIAdapter:
		capability = "chat"
	case route.IngressProtocol == generated.RouteConfigIngressProtocolOpenaiChat &&
		target.Bridge == generated.MutableTargetCommandBridgeOpenaiToAnthropic && target.Adapter == generated.MutableTargetCommandAdapterAnthropic:
		capability = "messages"
	default:
		return false
	}
	caps := map[string]bool{}
	for _, item := range target.Capabilities {
		caps[string(item)] = true
	}
	if !caps[capability] {
		return false
	}
	if backend.RequiredCapabilities != nil {
		for _, item := range *backend.RequiredCapabilities {
			if !caps[string(item)] {
				return false
			}
		}
	}
	return true
}

func modelMappingsSymmetric(policy generated.ModelPolicyConfig, candidates []generated.BackendCandidate) bool {
	if len(candidates) <= 1 {
		return true
	}
	byTarget := map[string]map[string]string{}
	for _, candidate := range candidates {
		byTarget[string(candidate.TargetId)] = map[string]string{}
	}
	for _, mapping := range policy.Mappings {
		if values, ok := byTarget[string(mapping.TargetId)]; ok {
			values[mapping.LogicalModel] = mapping.PhysicalModel
		}
	}
	var baseline map[string]string
	for _, candidate := range candidates {
		values := byTarget[string(candidate.TargetId)]
		if baseline == nil {
			baseline = values
			continue
		}
		if len(values) != len(baseline) {
			return false
		}
		for logical := range baseline {
			if strings.TrimSpace(values[logical]) == "" {
				return false
			}
		}
	}
	return true
}

func trafficRows(draft *configdraft.Draft, status RouteStatusProvider) []resourceview.Row {
	rows := []resourceview.Row{}
	for _, rule := range trafficRules(draft.LocalCommand(), status) {
		rowID := rule.ObjectID
		if rowID == "" {
			rowID = rule.ProfileID
		}
		rows = append(rows, resourceview.Row{ID: rowID, Cells: []string{
			rule.ClientName, rule.Primary, rule.Backup, rule.Routing, rule.Status, rule.Mode,
		}})
	}
	return rows
}

func newTrafficRuleEditor(rule trafficRule, draft *configdraft.Draft) editorState {
	routing := routingSingle
	switch rule.Routing {
	case "Failover":
		routing = routingFailover
	case "Load balance":
		routing = routingBalance
	}
	return editorState{mode: editorEdit, kind: KindTrafficRules, id: rule.ProfileID, values: map[string]string{
		"profile_id": rule.ProfileID, "client": rule.ClientName, "routing": routing, "primary_target_id": rule.PrimaryID, "backup_target_id": rule.BackupID,
	}}
}

func compatibleTrafficTargets(draft *configdraft.Draft, profileID string) []string {
	cmd := draft.LocalCommand()
	managedTargets := managedUpstreamTargetIDs(cmd)
	rule, ok := findTrafficRule(cmd, profileID, nil)
	if !ok {
		return nil
	}
	route, ok := findRoute(cmd, rule.RouteID)
	if !ok {
		return nil
	}
	backend, ok := findBackendSet(cmd, rule.BackendID)
	if !ok {
		return nil
	}
	profile, ok := findTrafficProfile(cmd, profileID)
	if !ok {
		return nil
	}
	policy, ok := findModelPolicy(cmd, string(route.ModelPolicyId))
	if !ok {
		return nil
	}
	projection, ok := findModelProjection(cmd, string(profile.ModelProjectionId))
	if !ok {
		return nil
	}
	ids := []string{}
	for _, target := range cmd.Targets {
		if !managedTargets[string(target.Id)] {
			continue
		}
		if !targetCompatibleWithRoute(route, backend, target) {
			continue
		}
		targetID := string(target.Id)
		mappings := mappingsForTarget(policy, targetID)
		if mappingsCoverProjection(mappings, projection.LogicalModels) {
			ids = append(ids, targetID)
		}
	}
	return ids
}

func mappingsCoverProjection(mappings map[string]string, logicalModels []string) bool {
	for _, logical := range logicalModels {
		if strings.TrimSpace(mappings[logical]) == "" && strings.TrimSpace(mappings[wildcardModel]) == "" {
			return false
		}
	}
	return true
}

func findTrafficProfile(cmd generated.MutableConfigCommand, id string) (generated.MutableClientProfile, bool) {
	for _, profile := range cmd.ClientProfiles {
		if string(profile.Id) == id {
			return profile, true
		}
	}
	return generated.MutableClientProfile{}, false
}

func mappingsForTarget(policy generated.ModelPolicyConfig, targetID string) map[string]string {
	mappings := map[string]string{}
	for _, mapping := range policy.Mappings {
		if string(mapping.TargetId) == targetID {
			mappings[mapping.LogicalModel] = mapping.PhysicalModel
		}
	}
	return mappings
}

func applyTrafficRule(draft *configdraft.Draft, profileID string, values map[string]string) error {
	cmd := draft.LocalCommand()
	rule, ok := findTrafficRule(cmd, profileID, nil)
	if !ok || !rule.Editable {
		return fmt.Errorf("$.traffic_rule: advanced configuration is read-only")
	}
	primary := strings.TrimSpace(values["primary_target_id"])
	backup := strings.TrimSpace(values["backup_target_id"])
	mode := strings.TrimSpace(values["routing"])
	allowed := map[string]bool{}
	for _, id := range compatibleTrafficTargets(draft, profileID) {
		allowed[id] = true
	}
	if primary == "" || !allowed[primary] {
		return fmt.Errorf("$.primary_target_id: incompatible")
	}
	candidates := []generated.BackendCandidate{{TargetId: generated.ConfigID(primary), Priority: 0, Weight: 1}}
	if mode != routingSingle {
		if backup == "" || backup == primary || !allowed[backup] {
			return fmt.Errorf("$.backup_target_id: distinct compatible target required")
		}
		priority := 1
		if mode == routingBalance {
			priority = 0
		} else if mode != routingFailover {
			return fmt.Errorf("$.routing: invalid")
		}
		candidates = append(candidates, generated.BackendCandidate{TargetId: generated.ConfigID(backup), Priority: priority, Weight: 1})
	}
	draft.Mutate(func(config *generated.MutableConfigCommand) {
		for i := range config.BackendSets {
			if string(config.BackendSets[i].Id) == rule.BackendID {
				config.BackendSets[i].Candidates = candidates
			}
		}
	})
	return nil
}
