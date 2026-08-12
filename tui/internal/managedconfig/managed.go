package managedconfig

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

type Format string

const (
	FormatOpenAIResponses   Format = "openai_responses"
	FormatOpenAIChat        Format = "openai_chat"
	FormatAnthropicMessages Format = "anthropic_messages"
)

type Routing string

const (
	RoutingSingle   Routing = "single"
	RoutingFailover Routing = "failover"
	RoutingBalance  Routing = "load_balance"
)

type ModelHandling string

const (
	ModelPreserve ModelHandling = "preserve"
	ModelOverride ModelHandling = "override"
	ModelMap      ModelHandling = "map"
)

type ModelPair struct {
	Client  string
	Primary string
	Backup  string
}

type UpstreamRequest struct {
	Name                string
	BaseURL             string
	AllowPrivateNetwork bool
	Formats             []Format
	QuotaGroupID        string
	SecretActions       map[generated.MutableCredentialCommandProvider]generated.CredentialSecretAction
}

type TrafficRuleRequest struct {
	Name          string
	Launcher      string
	Routing       Routing
	PrimaryTarget string
	BackupTarget  string
	ModelHandling ModelHandling
	Override      ModelPair
	Mappings      []ModelPair
}

type Upstream struct {
	Object      generated.ManagedObject
	Endpoint    generated.EndpointConfig
	Credentials []generated.MutableCredentialCommand
	Targets     []generated.MutableTargetCommand
	Formats     []Format
	LimitPolicy *generated.ManagedObject
	Custom      bool
}

type LimitPolicy struct {
	Object generated.ManagedObject
	Quota  generated.QuotaGroupConfig
	Custom bool
}

type TrafficRule struct {
	Object     generated.ManagedObject
	Profile    generated.MutableClientProfile
	Route      generated.RouteConfig
	Backend    generated.BackendSetConfig
	Policy     generated.ModelPolicyConfig
	Projection generated.ModelProjectionConfig
	Custom     bool
}

func Objects(cmd generated.MutableConfigCommand) []generated.ManagedObject {
	if cmd.ManagedObjects == nil {
		return nil
	}
	return append([]generated.ManagedObject(nil), (*cmd.ManagedObjects)...)
}

func SetObjects(cmd *generated.MutableConfigCommand, objects []generated.ManagedObject) {
	if cmd == nil {
		return
	}
	copyObjects := append([]generated.ManagedObject(nil), objects...)
	cmd.ManagedObjects = &copyObjects
}

func AddObject(cmd *generated.MutableConfigCommand, object generated.ManagedObject) {
	objects := Objects(*cmd)
	objects = append(objects, object)
	SetObjects(cmd, objects)
}

func ReplaceObject(cmd *generated.MutableConfigCommand, object generated.ManagedObject) bool {
	objects := Objects(*cmd)
	for i := range objects {
		if objects[i].Id == object.Id {
			objects[i] = object
			SetObjects(cmd, objects)
			return true
		}
	}
	return false
}

func FindObject(cmd generated.MutableConfigCommand, id string) (generated.ManagedObject, bool) {
	for _, object := range Objects(cmd) {
		if string(object.Id) == id {
			return object, true
		}
	}
	return generated.ManagedObject{}, false
}

func ObjectsByKind(cmd generated.MutableConfigCommand, kind generated.ManagedObjectKind) []generated.ManagedObject {
	var out []generated.ManagedObject
	for _, object := range Objects(cmd) {
		if object.Kind == kind {
			out = append(out, object)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func Members(object generated.ManagedObject, kind generated.ManagedResourceRefKind) []string {
	var ids []string
	for _, member := range object.Members {
		if member.Kind == kind {
			ids = append(ids, string(member.Id))
		}
	}
	return ids
}

func OwnerOf(cmd generated.MutableConfigCommand, kind generated.ManagedResourceRefKind, id string) (generated.ManagedObject, bool) {
	for _, object := range Objects(cmd) {
		for _, member := range object.Members {
			if member.Kind == kind && string(member.Id) == id {
				return object, true
			}
		}
	}
	return generated.ManagedObject{}, false
}

func ProjectUpstreams(cmd generated.MutableConfigCommand) []Upstream {
	objects := ObjectsByKind(cmd, generated.ManagedObjectKindUpstream)
	out := make([]Upstream, 0, len(objects))
	for _, object := range objects {
		upstream := Upstream{Object: object}
		endpointIDs := Members(object, generated.ManagedResourceRefKindEndpoint)
		endpointSet := stringSet(endpointIDs)
		credentialIDs := stringSet(Members(object, generated.ManagedResourceRefKindCredential))
		targetIDs := stringSet(Members(object, generated.ManagedResourceRefKindTarget))
		if len(endpointIDs) != 1 {
			upstream.Custom = true
		} else if endpoint, ok := findEndpoint(cmd, endpointIDs[0]); ok {
			upstream.Endpoint = endpoint
		} else {
			upstream.Custom = true
		}
		for _, credential := range cmd.Credentials {
			if credentialIDs[string(credential.Id)] {
				upstream.Credentials = append(upstream.Credentials, credential)
			}
		}
		for _, target := range cmd.Targets {
			if !targetIDs[string(target.Id)] {
				continue
			}
			upstream.Targets = append(upstream.Targets, target)
			format, ok := FormatForTarget(target)
			if !ok || !credentialIDs[string(target.CredentialId)] || string(target.EndpointId) != first(endpointIDs) {
				upstream.Custom = true
			} else {
				upstream.Formats = append(upstream.Formats, format)
			}
			if owner, ok := OwnerOf(cmd, generated.ManagedResourceRefKindQuotaGroup, string(target.QuotaGroupId)); ok && owner.Kind == generated.ManagedObjectKindLimitPolicy {
				candidate := owner
				if upstream.LimitPolicy == nil {
					upstream.LimitPolicy = &candidate
				} else if upstream.LimitPolicy.Id != candidate.Id {
					upstream.Custom = true
				}
			} else {
				upstream.Custom = true
			}
		}
		if len(upstream.Credentials) != len(credentialIDs) || len(upstream.Targets) != len(targetIDs) || len(upstream.Targets) == 0 {
			upstream.Custom = true
		}
		for _, target := range cmd.Targets {
			id := string(target.Id)
			if targetIDs[id] {
				continue
			}
			if endpointSet[string(target.EndpointId)] || credentialIDs[string(target.CredentialId)] {
				upstream.Custom = true
			}
		}
		upstream.Formats = uniqueFormats(upstream.Formats)
		out = append(out, upstream)
	}
	return out
}

func ProjectLimitPolicies(cmd generated.MutableConfigCommand) []LimitPolicy {
	objects := ObjectsByKind(cmd, generated.ManagedObjectKindLimitPolicy)
	out := make([]LimitPolicy, 0, len(objects))
	for _, object := range objects {
		policy := LimitPolicy{Object: object}
		ids := Members(object, generated.ManagedResourceRefKindQuotaGroup)
		if len(ids) != 1 {
			policy.Custom = true
		} else if quota, ok := findQuota(cmd, ids[0]); ok {
			policy.Quota = quota
		} else {
			policy.Custom = true
		}
		out = append(out, policy)
	}
	return out
}

func ProjectTrafficRules(cmd generated.MutableConfigCommand) []TrafficRule {
	objects := ObjectsByKind(cmd, generated.ManagedObjectKindTrafficRule)
	out := make([]TrafficRule, 0, len(objects))
	for _, object := range objects {
		rule := TrafficRule{Object: object}
		profileIDs := Members(object, generated.ManagedResourceRefKindClientProfile)
		routeIDs := Members(object, generated.ManagedResourceRefKindRoute)
		backendIDs := Members(object, generated.ManagedResourceRefKindBackendSet)
		policyIDs := Members(object, generated.ManagedResourceRefKindModelPolicy)
		projectionIDs := Members(object, generated.ManagedResourceRefKindModelProjection)
		if len(profileIDs) != 1 || len(routeIDs) != 1 || len(backendIDs) != 1 || len(policyIDs) != 1 || len(projectionIDs) != 1 {
			rule.Custom = true
		} else {
			rule.Profile, _ = findProfile(cmd, profileIDs[0])
			rule.Route, _ = findRoute(cmd, routeIDs[0])
			rule.Backend, _ = findBackend(cmd, backendIDs[0])
			rule.Policy, _ = findPolicy(cmd, policyIDs[0])
			rule.Projection, _ = findProjection(cmd, projectionIDs[0])
			if string(rule.Profile.DefaultRouteId) != routeIDs[0] || string(rule.Profile.ModelProjectionId) != projectionIDs[0] ||
				string(rule.Route.BackendSetId) != backendIDs[0] || string(rule.Route.ModelPolicyId) != policyIDs[0] ||
				(rule.Profile.Launcher != generated.MutableClientProfileLauncherCodex && rule.Profile.Launcher != generated.MutableClientProfileLauncherClaude) {
				rule.Custom = true
			}
		}
		out = append(out, rule)
	}
	return out
}

func CreateLimitPolicy(cmd *generated.MutableConfigCommand, name string, rpm, maxConcurrency int) (generated.ManagedObject, generated.QuotaGroupConfig, error) {
	if cmd == nil || strings.TrimSpace(name) == "" || rpm < 1 || maxConcurrency < 1 {
		return generated.ManagedObject{}, generated.QuotaGroupConfig{}, fmt.Errorf("invalid limit policy")
	}
	base := uniqueBase(*cmd, name)
	quota := generated.QuotaGroupConfig{
		Id: generated.ConfigID("quota-" + base), Name: strings.TrimSpace(name), Rpm: rpm, MaxConcurrency: maxConcurrency,
		ForegroundCapacity: 16, BackgroundCapacity: 4, QueueTimeoutMs: 30000, ForegroundWeight: 9, BackgroundWeight: 1,
	}
	object := generated.ManagedObject{
		Id: generated.ConfigID("limit-" + base), Name: strings.TrimSpace(name), Kind: generated.ManagedObjectKindLimitPolicy,
		Members: []generated.ManagedResourceRef{{Kind: generated.ManagedResourceRefKindQuotaGroup, Id: quota.Id}},
	}
	cmd.QuotaGroups = append(cmd.QuotaGroups, quota)
	AddObject(cmd, object)
	return object, quota, nil
}

func CreateUpstream(cmd *generated.MutableConfigCommand, req UpstreamRequest) (generated.ManagedObject, map[Format]string, error) {
	if cmd == nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.BaseURL) == "" || len(req.Formats) == 0 {
		return generated.ManagedObject{}, nil, fmt.Errorf("invalid upstream")
	}
	if _, ok := findQuota(*cmd, req.QuotaGroupID); !ok {
		return generated.ManagedObject{}, nil, fmt.Errorf("limit policy not found")
	}
	base := uniqueBase(*cmd, req.Name)
	endpoint := generated.EndpointConfig{
		Id: generated.ConfigID("endpoint-" + base), Name: strings.TrimSpace(req.Name), BaseUrl: strings.TrimSpace(req.BaseURL),
		AllowPrivateNetwork: req.AllowPrivateNetwork, Http2Enabled: true, MaxIdleConnections: 20, IdleConnectionTimeoutMs: 30000,
	}
	members := []generated.ManagedResourceRef{{Kind: generated.ManagedResourceRefKindEndpoint, Id: endpoint.Id}}
	cmd.Endpoints = append(cmd.Endpoints, endpoint)

	providers := requiredProviders(req.Formats)
	credentialIDs := map[generated.MutableCredentialCommandProvider]generated.ConfigID{}
	for _, provider := range providers {
		action, ok := req.SecretActions[provider]
		if !ok {
			return generated.ManagedObject{}, nil, fmt.Errorf("API key required for %s", provider)
		}
		credential := generated.MutableCredentialCommand{
			Id:   generated.ConfigID(fmt.Sprintf("credential-%s-%s", base, providerSuffix(provider))),
			Name: fmt.Sprintf("%s %s API key", strings.TrimSpace(req.Name), providerLabel(provider)), Provider: provider, SecretAction: action,
		}
		cmd.Credentials = append(cmd.Credentials, credential)
		credentialIDs[provider] = credential.Id
		members = append(members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindCredential, Id: credential.Id})
	}

	targetIDs := map[Format]string{}
	for _, format := range uniqueFormats(req.Formats) {
		preset, err := presetForFormat(format)
		if err != nil {
			return generated.ManagedObject{}, nil, err
		}
		target := generated.MutableTargetCommand{
			Id:   generated.ConfigID(fmt.Sprintf("target-%s-%s", base, formatSuffix(format))),
			Name: fmt.Sprintf("%s · %s", strings.TrimSpace(req.Name), FormatLabel(format)), EndpointId: endpoint.Id,
			CredentialId: credentialIDs[preset.provider], QuotaGroupId: generated.ConfigID(req.QuotaGroupID),
			Adapter: preset.adapter, Bridge: preset.bridge, Capabilities: preset.capabilities,
			HealthPolicy:   generated.HealthPolicyConfig{FailureThreshold: 3, RecoverySuccessThreshold: 2, InitialBackoffMs: 1000, MaxBackoffMs: 60000, JitterPercent: 10, ProbeTimeoutMs: 10000, StableProbeIntervalMs: 60000},
			ThrottlePolicy: generated.ThrottlePolicyConfig{DefaultCoolingMs: 1000, MaxCoolingMs: 60000},
		}
		cmd.Targets = append(cmd.Targets, target)
		targetIDs[format] = string(target.Id)
		members = append(members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindTarget, Id: target.Id})
	}
	object := generated.ManagedObject{Id: generated.ConfigID("upstream-" + base), Name: strings.TrimSpace(req.Name), Kind: generated.ManagedObjectKindUpstream, Members: members}
	AddObject(cmd, object)
	return object, targetIDs, nil
}

func CreateTrafficRule(cmd *generated.MutableConfigCommand, req TrafficRuleRequest) (generated.ManagedObject, error) {
	if cmd == nil || strings.TrimSpace(req.Name) == "" {
		return generated.ManagedObject{}, fmt.Errorf("invalid traffic rule")
	}
	launcher := generated.MutableClientProfileLauncher(strings.TrimSpace(req.Launcher))
	var ingress generated.RouteConfigIngressProtocol
	switch launcher {
	case generated.MutableClientProfileLauncherCodex:
		ingress = generated.RouteConfigIngressProtocolOpenaiResponses
	case generated.MutableClientProfileLauncherClaude:
		ingress = generated.RouteConfigIngressProtocolAnthropicMessages
	default:
		return generated.ManagedObject{}, fmt.Errorf("supported launchers are codex and claude")
	}
	if _, ok := findTarget(*cmd, req.PrimaryTarget); !ok {
		return generated.ManagedObject{}, fmt.Errorf("primary upstream not found")
	}
	candidates := []generated.BackendCandidate{{TargetId: generated.ConfigID(req.PrimaryTarget), Priority: 0, Weight: 1}}
	targets := []string{req.PrimaryTarget}
	if req.Routing == RoutingFailover || req.Routing == RoutingBalance {
		if req.BackupTarget == "" || req.BackupTarget == req.PrimaryTarget {
			return generated.ManagedObject{}, fmt.Errorf("distinct backup upstream required")
		}
		if _, ok := findTarget(*cmd, req.BackupTarget); !ok {
			return generated.ManagedObject{}, fmt.Errorf("backup upstream not found")
		}
		priority := 1
		if req.Routing == RoutingBalance {
			priority = 0
		}
		candidates = append(candidates, generated.BackendCandidate{TargetId: generated.ConfigID(req.BackupTarget), Priority: priority, Weight: 1})
		targets = append(targets, req.BackupTarget)
	} else if req.Routing != RoutingSingle {
		return generated.ManagedObject{}, fmt.Errorf("invalid routing mode")
	}

	logicalModels := []string{}
	mappings := []generated.ModelMapping{}
	addMapping := func(targetID, logical, physical string) {
		mappings = append(mappings, generated.ModelMapping{TargetId: generated.ConfigID(targetID), LogicalModel: logical, PhysicalModel: physical})
	}
	switch req.ModelHandling {
	case ModelPreserve:
		for _, targetID := range targets {
			addMapping(targetID, "*", "*")
		}
	case ModelOverride:
		if strings.TrimSpace(req.Override.Primary) == "" {
			return generated.ManagedObject{}, fmt.Errorf("primary model required")
		}
		addMapping(targets[0], "*", strings.TrimSpace(req.Override.Primary))
		if len(targets) > 1 {
			if strings.TrimSpace(req.Override.Backup) == "" {
				return generated.ManagedObject{}, fmt.Errorf("backup model required")
			}
			addMapping(targets[1], "*", strings.TrimSpace(req.Override.Backup))
		}
	case ModelMap:
		if len(req.Mappings) == 0 {
			return generated.ManagedObject{}, fmt.Errorf("at least one model mapping required")
		}
		seen := map[string]bool{}
		for _, pair := range req.Mappings {
			logical := strings.TrimSpace(pair.Client)
			if logical == "" || logical == "*" || seen[logical] || strings.TrimSpace(pair.Primary) == "" {
				return generated.ManagedObject{}, fmt.Errorf("invalid model mapping")
			}
			seen[logical] = true
			logicalModels = append(logicalModels, logical)
			addMapping(targets[0], logical, strings.TrimSpace(pair.Primary))
			if len(targets) > 1 {
				if strings.TrimSpace(pair.Backup) == "" {
					return generated.ManagedObject{}, fmt.Errorf("backup model required")
				}
				addMapping(targets[1], logical, strings.TrimSpace(pair.Backup))
			}
		}
	default:
		return generated.ManagedObject{}, fmt.Errorf("invalid model handling")
	}

	base := uniqueBase(*cmd, req.Name)
	backendID := generated.ConfigID("backend-" + base)
	policyID := generated.ConfigID("policy-" + base)
	projectionID := generated.ConfigID("projection-" + base)
	routeID := generated.ConfigID("route-" + base)
	profileID := generated.ConfigID("profile-" + base)
	name := strings.TrimSpace(req.Name)
	cmd.BackendSets = append(cmd.BackendSets, generated.BackendSetConfig{Id: backendID, Name: name + " backends", Candidates: candidates})
	cmd.ModelPolicies = append(cmd.ModelPolicies, generated.ModelPolicyConfig{Id: policyID, Name: name + " models", CatalogTtlMs: 60000, DiscoveryTimeoutMs: 5000, Mappings: mappings})
	cmd.ModelProjections = append(cmd.ModelProjections, generated.ModelProjectionConfig{Id: projectionID, Name: name + " models", LogicalModels: logicalModels})
	cmd.Routes = append(cmd.Routes, generated.RouteConfig{
		Id: routeID, Name: name, IngressProtocol: ingress, RoutingPolicy: generated.RouteConfigRoutingPolicyAutomatic,
		BackendSetId: backendID, ModelPolicyId: policyID, RequestDeadlineMs: 30000, RetryDeadlineMs: 10000,
		StreamIdleTimeoutMs: 5000, MaxAttempts: 2, MaxRequestBodyBytes: 33554432,
	})
	cmd.ClientProfiles = append(cmd.ClientProfiles, generated.MutableClientProfile{
		Id: profileID, Name: name, Launcher: launcher, DefaultRouteId: routeID, ModelProjectionId: projectionID,
		Arguments: []string{}, Environment: []generated.EnvironmentVariableConfig{}, CompatibilityTransformIds: []generated.ConfigID{},
	})
	object := generated.ManagedObject{
		Id: generated.ConfigID("rule-" + base), Name: name, Kind: generated.ManagedObjectKindTrafficRule,
		Members: []generated.ManagedResourceRef{
			{Kind: generated.ManagedResourceRefKindBackendSet, Id: backendID},
			{Kind: generated.ManagedResourceRefKindModelPolicy, Id: policyID},
			{Kind: generated.ManagedResourceRefKindModelProjection, Id: projectionID},
			{Kind: generated.ManagedResourceRefKindRoute, Id: routeID},
			{Kind: generated.ManagedResourceRefKindClientProfile, Id: profileID},
		},
	}
	AddObject(cmd, object)
	return object, nil
}

func DeleteObject(cmd *generated.MutableConfigCommand, objectID string) bool {
	if cmd == nil {
		return false
	}
	object, ok := FindObject(*cmd, objectID)
	if !ok {
		return false
	}
	memberSet := map[generated.ManagedResourceRefKind]map[string]bool{}
	for _, member := range object.Members {
		if memberSet[member.Kind] == nil {
			memberSet[member.Kind] = map[string]bool{}
		}
		memberSet[member.Kind][string(member.Id)] = true
	}
	cmd.Endpoints = filter(cmd.Endpoints, func(v generated.EndpointConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindEndpoint][string(v.Id)]
	})
	cmd.Credentials = filter(cmd.Credentials, func(v generated.MutableCredentialCommand) bool {
		return !memberSet[generated.ManagedResourceRefKindCredential][string(v.Id)]
	})
	cmd.QuotaGroups = filter(cmd.QuotaGroups, func(v generated.QuotaGroupConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindQuotaGroup][string(v.Id)]
	})
	cmd.Targets = filter(cmd.Targets, func(v generated.MutableTargetCommand) bool {
		return !memberSet[generated.ManagedResourceRefKindTarget][string(v.Id)]
	})
	cmd.ClientProfiles = filter(cmd.ClientProfiles, func(v generated.MutableClientProfile) bool {
		return !memberSet[generated.ManagedResourceRefKindClientProfile][string(v.Id)]
	})
	cmd.Routes = filter(cmd.Routes, func(v generated.RouteConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindRoute][string(v.Id)]
	})
	cmd.BackendSets = filter(cmd.BackendSets, func(v generated.BackendSetConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindBackendSet][string(v.Id)]
	})
	cmd.ModelPolicies = filter(cmd.ModelPolicies, func(v generated.ModelPolicyConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindModelPolicy][string(v.Id)]
	})
	cmd.ModelProjections = filter(cmd.ModelProjections, func(v generated.ModelProjectionConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindModelProjection][string(v.Id)]
	})
	cmd.ContentPolicies = filter(cmd.ContentPolicies, func(v generated.ContentPolicyConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindContentPolicy][string(v.Id)]
	})
	cmd.CompatibilityTransforms = filter(cmd.CompatibilityTransforms, func(v generated.CompatibilityTransformConfig) bool {
		return !memberSet[generated.ManagedResourceRefKindCompatibilityTransform][string(v.Id)]
	})
	objects := filter(Objects(*cmd), func(v generated.ManagedObject) bool { return string(v.Id) != objectID })
	SetObjects(cmd, objects)
	return true
}

func TargetForLauncher(upstream Upstream, launcher string) (string, bool) {
	want := FormatOpenAIResponses
	if launcher == "claude" {
		if hasFormat(upstream.Formats, FormatAnthropicMessages) {
			want = FormatAnthropicMessages
		} else {
			want = FormatOpenAIChat
		}
	}
	for _, target := range upstream.Targets {
		if format, ok := FormatForTarget(target); ok && format == want {
			return string(target.Id), true
		}
	}
	return "", false
}

func FormatForTarget(target generated.MutableTargetCommand) (Format, bool) {
	switch target.Bridge {
	case generated.MutableTargetCommandBridgeOpenaiResponses:
		return FormatOpenAIResponses, true
	case generated.MutableTargetCommandBridgeAnthropicToOpenai, generated.MutableTargetCommandBridgeOpenaiChat:
		return FormatOpenAIChat, true
	case generated.MutableTargetCommandBridgeAnthropicMessages:
		return FormatAnthropicMessages, true
	default:
		return "", false
	}
}

func FormatLabel(format Format) string {
	switch format {
	case FormatOpenAIResponses:
		return "OpenAI Responses"
	case FormatOpenAIChat:
		return "OpenAI Chat"
	case FormatAnthropicMessages:
		return "Anthropic Messages"
	default:
		return string(format)
	}
}

type targetPreset struct {
	provider     generated.MutableCredentialCommandProvider
	adapter      generated.MutableTargetCommandAdapter
	bridge       generated.MutableTargetCommandBridge
	capabilities []generated.MutableTargetCommandCapabilities
}

func presetForFormat(format Format) (targetPreset, error) {
	common := []generated.MutableTargetCommandCapabilities{
		generated.MutableTargetCommandCapabilitiesStreaming, generated.MutableTargetCommandCapabilitiesTools,
		generated.MutableTargetCommandCapabilitiesModelDiscovery, generated.MutableTargetCommandCapabilitiesProbe,
	}
	switch format {
	case FormatOpenAIResponses:
		return targetPreset{generated.MutableCredentialCommandProviderOpenaiCompatible, generated.MutableTargetCommandAdapterOpenaiCompatible, generated.MutableTargetCommandBridgeOpenaiResponses, append([]generated.MutableTargetCommandCapabilities{generated.MutableTargetCommandCapabilitiesResponses}, common...)}, nil
	case FormatOpenAIChat:
		return targetPreset{generated.MutableCredentialCommandProviderOpenaiCompatible, generated.MutableTargetCommandAdapterOpenaiCompatible, generated.MutableTargetCommandBridgeAnthropicToOpenai, append([]generated.MutableTargetCommandCapabilities{generated.MutableTargetCommandCapabilitiesChat}, common...)}, nil
	case FormatAnthropicMessages:
		return targetPreset{generated.MutableCredentialCommandProviderAnthropic, generated.MutableTargetCommandAdapterAnthropic, generated.MutableTargetCommandBridgeAnthropicMessages, append([]generated.MutableTargetCommandCapabilities{generated.MutableTargetCommandCapabilitiesMessages}, common...)}, nil
	default:
		return targetPreset{}, fmt.Errorf("unsupported API format %q", format)
	}
}

func requiredProviders(formats []Format) []generated.MutableCredentialCommandProvider {
	seen := map[generated.MutableCredentialCommandProvider]bool{}
	var out []generated.MutableCredentialCommandProvider
	for _, format := range uniqueFormats(formats) {
		preset, err := presetForFormat(format)
		if err == nil && !seen[preset.provider] {
			seen[preset.provider] = true
			out = append(out, preset.provider)
		}
	}
	return out
}

func providerSuffix(provider generated.MutableCredentialCommandProvider) string {
	if provider == generated.MutableCredentialCommandProviderAnthropic {
		return "anthropic"
	}
	return "openai"
}

func providerLabel(provider generated.MutableCredentialCommandProvider) string {
	if provider == generated.MutableCredentialCommandProviderAnthropic {
		return "Anthropic"
	}
	return "OpenAI"
}

func formatSuffix(format Format) string { return strings.ReplaceAll(string(format), "_", "-") }

func uniqueFormats(formats []Format) []Format {
	seen := map[Format]bool{}
	var out []Format
	for _, format := range formats {
		if !seen[format] {
			seen[format] = true
			out = append(out, format)
		}
	}
	return out
}

func hasFormat(formats []Format, want Format) bool {
	for _, format := range formats {
		if format == want {
			return true
		}
	}
	return false
}

var nonID = regexp.MustCompile(`[^a-z0-9_-]+`)

func uniqueBase(cmd generated.MutableConfigCommand, value string) string {
	base := strings.ToLower(strings.TrimSpace(value))
	base = nonID.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-_")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "managed-" + base
	}
	if len(base) > 38 {
		base = strings.Trim(base[:38], "-_")
	}
	used := allIDs(cmd)
	for index := 1; ; index++ {
		candidate := base
		if index > 1 {
			candidate = fmt.Sprintf("%s-%d", base, index)
		}
		collision := false
		for _, prefix := range []string{"", "upstream-", "limit-", "rule-", "endpoint-", "credential-", "quota-", "target-", "backend-", "policy-", "projection-", "route-", "profile-"} {
			if used[prefix+candidate] {
				collision = true
				break
			}
		}
		if !collision {
			return candidate
		}
	}
}

func allIDs(cmd generated.MutableConfigCommand) map[string]bool {
	used := map[string]bool{}
	for _, object := range Objects(cmd) {
		used[string(object.Id)] = true
	}
	for _, v := range cmd.Endpoints {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.Credentials {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.QuotaGroups {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.Targets {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.ClientProfiles {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.Routes {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.BackendSets {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.ModelPolicies {
		used[string(v.Id)] = true
	}
	for _, v := range cmd.ModelProjections {
		used[string(v.Id)] = true
	}
	return used
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func filter[T any](values []T, keep func(T) bool) []T {
	out := values[:0]
	for _, value := range values {
		if keep(value) {
			out = append(out, value)
		}
	}
	return out
}

func findEndpoint(cmd generated.MutableConfigCommand, id string) (generated.EndpointConfig, bool) {
	for _, v := range cmd.Endpoints {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.EndpointConfig{}, false
}

func findQuota(cmd generated.MutableConfigCommand, id string) (generated.QuotaGroupConfig, bool) {
	for _, v := range cmd.QuotaGroups {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.QuotaGroupConfig{}, false
}

func findTarget(cmd generated.MutableConfigCommand, id string) (generated.MutableTargetCommand, bool) {
	for _, v := range cmd.Targets {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.MutableTargetCommand{}, false
}

func findProfile(cmd generated.MutableConfigCommand, id string) (generated.MutableClientProfile, bool) {
	for _, v := range cmd.ClientProfiles {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.MutableClientProfile{}, false
}

func findRoute(cmd generated.MutableConfigCommand, id string) (generated.RouteConfig, bool) {
	for _, v := range cmd.Routes {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.RouteConfig{}, false
}

func findBackend(cmd generated.MutableConfigCommand, id string) (generated.BackendSetConfig, bool) {
	for _, v := range cmd.BackendSets {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.BackendSetConfig{}, false
}

func findPolicy(cmd generated.MutableConfigCommand, id string) (generated.ModelPolicyConfig, bool) {
	for _, v := range cmd.ModelPolicies {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.ModelPolicyConfig{}, false
}

func findProjection(cmd generated.MutableConfigCommand, id string) (generated.ModelProjectionConfig, bool) {
	for _, v := range cmd.ModelProjections {
		if string(v.Id) == id {
			return v, true
		}
	}
	return generated.ModelProjectionConfig{}, false
}
