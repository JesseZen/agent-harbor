package targets

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

const (
	upstreamClientClaude = "claude"
	upstreamClientCodex  = "codex"
	upstreamAPIAnthropic = "anthropic_messages"
	upstreamAPIResponses = "openai_responses"
	upstreamAPIChat      = "openai_chat"
)

type simpleUpstream struct {
	ObjectID            string
	TargetID            string
	TargetIDs           []string
	EndpointID          string
	CredentialID        string
	CredentialIDs       []string
	QuotaID             string
	Name                string
	Client              string
	APIType             string
	APIFormats          []managedconfig.Format
	LimitPolicyID       string
	URL                 string
	Health              string
	Mode                string
	Editable            bool
	AllowPrivateNetwork bool
}

type upstreamPreset struct {
	provider     generated.MutableCredentialCommandProvider
	adapter      generated.MutableTargetCommandAdapter
	bridge       generated.MutableTargetCommandBridge
	capabilities []generated.MutableTargetCommandCapabilities
}

func aggregateUpstreams(cmd generated.MutableConfigCommand, status TargetStatusProvider) []simpleUpstream {
	managed := managedconfig.ProjectUpstreams(cmd)
	rows := make([]simpleUpstream, 0, len(managed))
	for _, item := range managed {
		row := simpleUpstream{
			ObjectID: string(item.Object.Id), Name: item.Object.Name, EndpointID: string(item.Endpoint.Id), URL: item.Endpoint.BaseUrl,
			AllowPrivateNetwork: item.Endpoint.AllowPrivateNetwork, APIFormats: item.Formats, Editable: !item.Custom, Mode: "Managed", Health: "—",
		}
		if item.Custom {
			row.Mode = "Custom"
		}
		if item.LimitPolicy != nil {
			row.LimitPolicyID = string(item.LimitPolicy.Id)
		}
		formatLabels := make([]string, 0, len(item.Formats))
		for _, format := range item.Formats {
			formatLabels = append(formatLabels, managedconfig.FormatLabel(format))
		}
		row.APIType = strings.Join(formatLabels, ", ")
		for _, credential := range item.Credentials {
			row.CredentialIDs = append(row.CredentialIDs, string(credential.Id))
		}
		if len(row.CredentialIDs) > 0 {
			row.CredentialID = row.CredentialIDs[0]
		}
		healthy := 0
		for _, target := range item.Targets {
			id := string(target.Id)
			row.TargetIDs = append(row.TargetIDs, id)
			if row.QuotaID == "" {
				row.QuotaID = string(target.QuotaGroupId)
			}
			if status != nil {
				if snapshot, ok := status.Lookup(id); ok && strings.EqualFold(snapshot.Health, "healthy") {
					healthy++
				}
			}
		}
		if len(row.TargetIDs) > 0 {
			row.TargetID = row.TargetIDs[0]
		}
		if status != nil && len(row.TargetIDs) > 0 {
			row.Health = fmt.Sprintf("%d/%d healthy", healthy, len(row.TargetIDs))
		}
		rows = append(rows, row)
	}
	return rows
}

func hasQuotaGroup(cmd generated.MutableConfigCommand, id string) bool {
	for _, quota := range cmd.QuotaGroups {
		if string(quota.Id) == id {
			return true
		}
	}
	return false
}

func findSimpleUpstream(draft *configdraft.Draft, targetID string, status TargetStatusProvider) (simpleUpstream, bool) {
	for _, upstream := range aggregateUpstreams(draft.LocalCommand(), status) {
		if upstream.TargetID == targetID || upstream.ObjectID == targetID {
			return upstream, true
		}
	}
	return simpleUpstream{}, false
}

func describeUpstreamProtocol(target generated.MutableTargetCommand) (string, string, bool) {
	switch {
	case target.Adapter == generated.MutableTargetCommandAdapterAnthropic && target.Bridge == generated.MutableTargetCommandBridgeAnthropicMessages:
		return "Claude", "Anthropic Messages", true
	case target.Adapter == generated.MutableTargetCommandAdapterOpenaiCompatible && target.Bridge == generated.MutableTargetCommandBridgeAnthropicToOpenai:
		return "Claude", "OpenAI Chat", true
	case target.Adapter == generated.MutableTargetCommandAdapterOpenaiCompatible && target.Bridge == generated.MutableTargetCommandBridgeOpenaiResponses:
		return "Codex", "OpenAI Responses", true
	case target.Adapter == generated.MutableTargetCommandAdapterAnthropic && target.Bridge == generated.MutableTargetCommandBridgeOpenaiToAnthropic:
		return "Codex", "Anthropic Messages", true
	default:
		return "—", "Custom", false
	}
}

func upstreamPresetFor(client, apiType string) (upstreamPreset, error) {
	baseOpenAI := upstreamPreset{
		provider: generated.MutableCredentialCommandProviderOpenaiCompatible,
		adapter:  generated.MutableTargetCommandAdapterOpenaiCompatible,
	}
	switch {
	case client == upstreamClientClaude && apiType == upstreamAPIAnthropic:
		return upstreamPreset{
			provider: generated.MutableCredentialCommandProviderAnthropic,
			adapter:  generated.MutableTargetCommandAdapterAnthropic, bridge: generated.MutableTargetCommandBridgeAnthropicMessages,
			capabilities: upstreamCapabilities(generated.MutableTargetCommandCapabilitiesMessages),
		}, nil
	case client == upstreamClientClaude && apiType == upstreamAPIChat:
		baseOpenAI.bridge = generated.MutableTargetCommandBridgeAnthropicToOpenai
		baseOpenAI.capabilities = upstreamCapabilities(generated.MutableTargetCommandCapabilitiesChat)
		return baseOpenAI, nil
	case client == upstreamClientCodex && apiType == upstreamAPIResponses:
		baseOpenAI.bridge = generated.MutableTargetCommandBridgeOpenaiResponses
		baseOpenAI.capabilities = upstreamCapabilities(generated.MutableTargetCommandCapabilitiesResponses)
		return baseOpenAI, nil
	case client == upstreamClientCodex && apiType == upstreamAPIChat:
		baseOpenAI.bridge = generated.MutableTargetCommandBridgeOpenaiChat
		baseOpenAI.capabilities = upstreamCapabilities(generated.MutableTargetCommandCapabilitiesChat)
		return baseOpenAI, nil
	case client == upstreamClientCodex && apiType == upstreamAPIAnthropic:
		return upstreamPreset{
			provider: generated.MutableCredentialCommandProviderAnthropic,
			adapter:  generated.MutableTargetCommandAdapterAnthropic, bridge: generated.MutableTargetCommandBridgeOpenaiToAnthropic,
			capabilities: upstreamCapabilities(generated.MutableTargetCommandCapabilitiesMessages),
		}, nil
	default:
		return upstreamPreset{}, fmt.Errorf("$.api_type: incompatible with selected client")
	}
}

func upstreamCapabilities(protocol generated.MutableTargetCommandCapabilities) []generated.MutableTargetCommandCapabilities {
	return []generated.MutableTargetCommandCapabilities{
		protocol,
		generated.MutableTargetCommandCapabilitiesStreaming,
		generated.MutableTargetCommandCapabilitiesTools,
		generated.MutableTargetCommandCapabilitiesModelDiscovery,
		generated.MutableTargetCommandCapabilitiesProbe,
	}
}

func newUpstreamEditor(mode editorMode, id string, draft *configdraft.Draft, status TargetStatusProvider) editorState {
	values := map[string]string{
		"name": "", "launcher": upstreamClientCodex, "api_formats": upstreamAPIResponses, "base_url": "", "limit_policy_id": "new", "allow_private_network": "false",
	}
	if mode == editorEdit {
		if upstream, ok := findSimpleUpstream(draft, id, status); ok {
			values["name"] = upstream.Name
			formats := make([]string, 0, len(upstream.APIFormats))
			for _, format := range upstream.APIFormats {
				formats = append(formats, string(format))
			}
			if len(formats) > 0 {
				values["api_formats"] = strings.Join(formats, ",")
			}
			if upstream.LimitPolicyID != "" {
				values["limit_policy_id"] = upstream.LimitPolicyID
			}
			values["base_url"] = upstream.URL
			values["allow_private_network"] = strconvBool(upstream.AllowPrivateNetwork)
		}
	}
	return editorState{mode: mode, kind: KindUpstreams, id: id, values: values, tokenBuf: secretBuffer(), draft: draft}
}

func secretBuffer() *secretinput.Buffer { return secretinput.New() }

var nonConfigID = regexp.MustCompile(`[^a-z0-9_-]+`)

func upstreamIDBase(name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = nonConfigID.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-_")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "upstream-" + base
	}
	if len(base) > 45 {
		base = strings.Trim(base[:45], "-_")
	}
	return base
}

func uniqueUpstreamSuffix(cmd generated.MutableConfigCommand, name string) string {
	base := upstreamIDBase(name)
	used := map[string]bool{}
	for _, target := range cmd.Targets {
		used[string(target.Id)] = true
	}
	for _, endpoint := range cmd.Endpoints {
		used[string(endpoint.Id)] = true
	}
	for _, credential := range cmd.Credentials {
		used[string(credential.Id)] = true
	}
	for _, quota := range cmd.QuotaGroups {
		used[string(quota.Id)] = true
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if !used["target-"+candidate] && !used["endpoint-"+candidate] && !used["credential-"+candidate] && !used["quota-"+candidate] {
			return candidate
		}
	}
}

func validateUpstreamValues(values map[string]string, creating bool, hasToken bool) error {
	if strings.TrimSpace(values["name"]) == "" {
		return fmt.Errorf("$.name: required")
	}
	if strings.TrimSpace(values["base_url"]) == "" {
		return fmt.Errorf("$.base_url: required")
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(values["base_url"]))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("$.base_url: valid http(s) URL required")
	}
	formats := parseUpstreamFormats(values)
	if len(formats) == 0 {
		return fmt.Errorf("$.api_formats: select at least one API format")
	}
	for _, format := range formats {
		switch format {
		case managedconfig.FormatOpenAIResponses, managedconfig.FormatOpenAIChat, managedconfig.FormatAnthropicMessages:
		default:
			return fmt.Errorf("$.api_formats: unsupported API format")
		}
	}
	if requiresPrivateNetworkOptIn(parsed.Hostname()) && !strings.EqualFold(strings.TrimSpace(values["allow_private_network"]), "true") {
		return fmt.Errorf("$.allow_private_network: enable for localhost or private network URLs")
	}
	if creating && !hasToken {
		return fmt.Errorf("$.api_key: required")
	}
	return nil
}

func parseUpstreamFormats(values map[string]string) []managedconfig.Format {
	raw := strings.TrimSpace(values["api_formats"])
	if raw == "" {
		// Compatibility for drafts/tests created by the previous form.
		raw = strings.TrimSpace(values["api_type"])
	}
	seen := map[managedconfig.Format]bool{}
	var formats []managedconfig.Format
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		format := managedconfig.Format(strings.TrimSpace(value))
		if format != "" && !seen[format] {
			seen[format] = true
			formats = append(formats, format)
		}
	}
	return formats
}

func requiresPrivateNetworkOptIn(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast())
}

func defaultUpstreamQuota(id, name string) generated.QuotaGroupConfig {
	return generated.QuotaGroupConfig{
		Id: generated.ConfigID(id), Name: name + " quota", Rpm: 60, MaxConcurrency: 4,
		ForegroundCapacity: 16, BackgroundCapacity: 4, QueueTimeoutMs: 30000,
		ForegroundWeight: 9, BackgroundWeight: 1,
	}
}

func defaultUpstreamTarget(id, name, endpointID, credentialID, quotaID string, preset upstreamPreset) generated.MutableTargetCommand {
	return generated.MutableTargetCommand{
		Id: generated.ConfigID(id), Name: name, EndpointId: generated.ConfigID(endpointID),
		CredentialId: generated.ConfigID(credentialID), QuotaGroupId: generated.ConfigID(quotaID),
		Adapter: preset.adapter, Bridge: preset.bridge, Capabilities: preset.capabilities,
		HealthPolicy: generated.HealthPolicyConfig{
			FailureThreshold: 3, RecoverySuccessThreshold: 2, InitialBackoffMs: 1000,
			MaxBackoffMs: 60000, JitterPercent: 10, ProbeTimeoutMs: 10000, StableProbeIntervalMs: 60000,
		},
		ThrottlePolicy: generated.ThrottlePolicyConfig{DefaultCoolingMs: 1000, MaxCoolingMs: 60000},
	}
}

func createSimpleUpstream(draft *configdraft.Draft, values map[string]string, stageID string) error {
	formats := parseUpstreamFormats(values)
	if len(formats) == 0 {
		return fmt.Errorf("$.api_formats: select at least one API format")
	}
	stages := map[generated.MutableCredentialCommandProvider]string{}
	for _, format := range formats {
		provider, err := providerForFormat(format)
		if err != nil {
			return err
		}
		stages[provider] = stageID
	}
	_, _, err := createManagedUpstream(draft, values, stages)
	return err
}

func createManagedUpstream(
	draft *configdraft.Draft,
	values map[string]string,
	stages map[generated.MutableCredentialCommandProvider]string,
) (managedconfig.Upstream, bool, error) {
	if draft == nil {
		return managedconfig.Upstream{}, false, fmt.Errorf("$.upstream: configuration unavailable")
	}
	formats := parseUpstreamFormats(values)
	actions := map[generated.MutableCredentialCommandProvider]generated.CredentialSecretAction{}
	for _, format := range formats {
		provider, err := providerForFormat(format)
		if err != nil {
			return managedconfig.Upstream{}, false, err
		}
		stageID := strings.TrimSpace(stages[provider])
		if stageID == "" {
			return managedconfig.Upstream{}, false, fmt.Errorf("$.api_key: required for %s", provider)
		}
		action, err := buildReplaceAction(stageID)
		if err != nil {
			return managedconfig.Upstream{}, false, err
		}
		actions[provider] = action
	}

	working := draft.LocalCommand()
	quotaID := ""
	limitID := strings.TrimSpace(values["limit_policy_id"])
	if limitID != "" && limitID != "new" {
		object, ok := managedconfig.FindObject(working, limitID)
		if !ok || object.Kind != generated.ManagedObjectKindLimitPolicy {
			return managedconfig.Upstream{}, false, fmt.Errorf("$.limit_policy_id: limit policy not found")
		}
		ids := managedconfig.Members(object, generated.ManagedResourceRefKindQuotaGroup)
		if len(ids) != 1 {
			return managedconfig.Upstream{}, false, fmt.Errorf("$.limit_policy_id: custom limit policy cannot be assigned here")
		}
		quotaID = ids[0]
	} else {
		_, quota, err := managedconfig.CreateLimitPolicy(&working, strings.TrimSpace(values["name"])+" limits", 60, 4)
		if err != nil {
			return managedconfig.Upstream{}, false, err
		}
		quotaID = string(quota.Id)
	}
	object, targets, err := managedconfig.CreateUpstream(&working, managedconfig.UpstreamRequest{
		Name: strings.TrimSpace(values["name"]), BaseURL: strings.TrimSpace(values["base_url"]),
		AllowPrivateNetwork: strings.EqualFold(values["allow_private_network"], "true"),
		Formats:             formats, QuotaGroupID: quotaID, SecretActions: actions,
	})
	if err != nil {
		return managedconfig.Upstream{}, false, err
	}
	firstSetup := len(managedconfig.ProjectTrafficRules(working)) == 0
	if firstSetup {
		launcher := strings.TrimSpace(values["launcher"])
		if launcher == "" {
			launcher = upstreamClientCodex
		}
		var targetID string
		if launcher == upstreamClientCodex {
			targetID = targets[managedconfig.FormatOpenAIResponses]
		} else if launcher == upstreamClientClaude {
			targetID = targets[managedconfig.FormatAnthropicMessages]
			if targetID == "" {
				targetID = targets[managedconfig.FormatOpenAIChat]
			}
		}
		if targetID == "" {
			return managedconfig.Upstream{}, false, fmt.Errorf("$.api_formats: select a format compatible with %s", launcher)
		}
		_, err = managedconfig.CreateTrafficRule(&working, managedconfig.TrafficRuleRequest{
			Name:     strings.TrimSpace(values["name"]),
			Launcher: launcher, Routing: managedconfig.RoutingSingle, PrimaryTarget: targetID, ModelHandling: managedconfig.ModelPreserve,
		})
		if err != nil {
			return managedconfig.Upstream{}, false, err
		}
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) { *cmd = working })
	for _, credentialID := range managedconfig.Members(object, generated.ManagedResourceRefKindCredential) {
		credential, ok := findCredential(working, credentialID)
		if !ok {
			continue
		}
		if stageID := stages[credential.Provider]; stageID != "" {
			draft.SetCredentialReplace(credentialID, stageID)
		}
	}
	for _, upstream := range managedconfig.ProjectUpstreams(draft.LocalCommand()) {
		if upstream.Object.Id == object.Id {
			return upstream, firstSetup, nil
		}
	}
	return managedconfig.Upstream{}, firstSetup, fmt.Errorf("$.upstream: failed to project saved upstream")
}

func providerForFormat(format managedconfig.Format) (generated.MutableCredentialCommandProvider, error) {
	switch format {
	case managedconfig.FormatOpenAIResponses, managedconfig.FormatOpenAIChat:
		return generated.MutableCredentialCommandProviderOpenaiCompatible, nil
	case managedconfig.FormatAnthropicMessages:
		return generated.MutableCredentialCommandProviderAnthropic, nil
	default:
		return "", fmt.Errorf("$.api_formats: unsupported API format %q", format)
	}
}

// createSimpleProtocolBinding leaves the old target and every reference to it
// untouched. A protocol edit therefore creates a new target binding instead of
// changing the meaning of an ID already used by routes or model policies.
//
// Credentials can only be reused when the provider is unchanged. Core makes a
// credential's provider immutable and a new credential ID cannot preserve an
// existing secret. Supplying a new key always creates an independent credential.
func createSimpleProtocolBinding(draft *configdraft.Draft, upstream simpleUpstream, values map[string]string, stageID string) (string, error) {
	preset, err := upstreamPresetFor(values["client"], values["api_type"])
	if err != nil {
		return "", err
	}
	cmd := draft.LocalCommand()
	oldCredential, ok := findCredential(cmd, upstream.CredentialID)
	if !ok {
		return "", fmt.Errorf("$.upstream: referenced credential not found")
	}
	if oldCredential.Provider != preset.provider && stageID == "" {
		return "", fmt.Errorf("$.api_key: required when the protocol changes provider")
	}

	name := strings.TrimSpace(values["name"])
	suffix := uniqueUpstreamSuffix(cmd, name)
	targetID, endpointID := "target-"+suffix, "endpoint-"+suffix
	quotaID := "quota-" + suffix
	credentialID := upstream.CredentialID
	var credential *generated.MutableCredentialCommand
	if stageID != "" {
		action, buildErr := buildReplaceAction(stageID)
		if buildErr != nil {
			return "", buildErr
		}
		credentialID = "credential-" + suffix
		credential = &generated.MutableCredentialCommand{
			Id: generated.ConfigID(credentialID), Name: name + " API key", Provider: preset.provider, SecretAction: action,
		}
	}

	draft.Mutate(func(config *generated.MutableConfigCommand) {
		config.Endpoints = append(config.Endpoints, generated.EndpointConfig{
			Id: generated.ConfigID(endpointID), Name: name, BaseUrl: strings.TrimSpace(values["base_url"]),
			AllowPrivateNetwork: strings.EqualFold(values["allow_private_network"], "true"), Http2Enabled: true, MaxIdleConnections: 20, IdleConnectionTimeoutMs: 30000,
		})
		if credential != nil {
			config.Credentials = append(config.Credentials, *credential)
		}
		config.QuotaGroups = append(config.QuotaGroups, defaultUpstreamQuota(quotaID, name))
		config.Targets = append(config.Targets, defaultUpstreamTarget(targetID, name, endpointID, credentialID, quotaID, preset))
	})
	if credential != nil {
		draft.SetCredentialReplace(credentialID, stageID)
		return credentialID, nil
	}
	return "", nil
}

func editSimpleUpstream(draft *configdraft.Draft, upstream simpleUpstream, values map[string]string) error {
	name := strings.TrimSpace(values["name"])
	foundEndpoint, foundTarget, foundCredential := false, false, false
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		if upstream.ObjectID != "" {
			if object, ok := managedconfig.FindObject(*cmd, upstream.ObjectID); ok {
				object.Name = name
				managedconfig.ReplaceObject(cmd, object)
			}
		}
		for i := range cmd.Endpoints {
			if string(cmd.Endpoints[i].Id) == upstream.EndpointID {
				cmd.Endpoints[i].Name = name
				cmd.Endpoints[i].BaseUrl = strings.TrimSpace(values["base_url"])
				cmd.Endpoints[i].AllowPrivateNetwork = strings.EqualFold(values["allow_private_network"], "true")
				foundEndpoint = true
			}
		}
		targetIDs := map[string]bool{upstream.TargetID: true}
		for _, id := range upstream.TargetIDs {
			targetIDs[id] = true
		}
		for i := range cmd.Targets {
			if targetIDs[string(cmd.Targets[i].Id)] {
				if format, ok := managedconfig.FormatForTarget(cmd.Targets[i]); ok {
					cmd.Targets[i].Name = name + " · " + managedconfig.FormatLabel(format)
				} else {
					cmd.Targets[i].Name = name
				}
				foundTarget = true
			}
		}
		credentialIDs := map[string]bool{upstream.CredentialID: true}
		for _, id := range upstream.CredentialIDs {
			credentialIDs[id] = true
		}
		for i := range cmd.Credentials {
			if credentialIDs[string(cmd.Credentials[i].Id)] {
				cmd.Credentials[i].Name = name + " API key"
				foundCredential = true
			}
		}
	})
	if !foundEndpoint || !foundTarget || !foundCredential {
		return fmt.Errorf("$.upstream: referenced resource not found")
	}
	return nil
}

func migrateManagedUpstream(
	draft *configdraft.Draft,
	upstream simpleUpstream,
	values map[string]string,
	stages map[generated.MutableCredentialCommandProvider]string,
) error {
	if upstream.ObjectID == "" {
		return fmt.Errorf("$.upstream: only managed upstreams support protocol migration")
	}
	working := draft.LocalCommand()
	object, ok := managedconfig.FindObject(working, upstream.ObjectID)
	if !ok {
		return fmt.Errorf("$.upstream: managed object not found")
	}
	desired := parseUpstreamFormats(values)
	if len(desired) == 0 {
		return fmt.Errorf("$.api_formats: select at least one API format")
	}
	endpointIndex := -1
	for i := range working.Endpoints {
		if string(working.Endpoints[i].Id) == upstream.EndpointID {
			endpointIndex = i
			break
		}
	}
	if endpointIndex < 0 {
		return fmt.Errorf("$.upstream: endpoint not found")
	}

	credentials := map[generated.MutableCredentialCommandProvider]string{}
	for _, id := range upstream.CredentialIDs {
		if credential, ok := findCredential(working, id); ok {
			credentials[credential.Provider] = id
		}
	}
	base := strings.TrimPrefix(upstream.ObjectID, "upstream-")
	for _, format := range desired {
		provider, err := providerForFormat(format)
		if err != nil {
			return err
		}
		if credentials[provider] != "" {
			continue
		}
		stageID := stages[provider]
		if stageID == "" {
			return fmt.Errorf("$.api_key: re-enter the API key when adding %s", managedconfig.FormatLabel(format))
		}
		action, err := buildReplaceAction(stageID)
		if err != nil {
			return err
		}
		credentialID := uniqueManagedMemberID(working, "credential-"+base+"-"+providerName(provider))
		working.Credentials = append(working.Credentials, generated.MutableCredentialCommand{
			Id: generated.ConfigID(credentialID), Name: strings.TrimSpace(values["name"]) + " API key", Provider: provider, SecretAction: action,
		})
		credentials[provider] = credentialID
	}

	quotaID := upstream.QuotaID
	if quotaID == "" && len(upstream.TargetIDs) > 0 {
		if target, ok := findTarget(working, upstream.TargetIDs[0]); ok {
			quotaID = string(target.QuotaGroupId)
		}
	}
	byFormat := map[managedconfig.Format]string{}
	for _, id := range upstream.TargetIDs {
		if target, ok := findTarget(working, id); ok {
			if format, ok := managedconfig.FormatForTarget(target); ok {
				byFormat[format] = id
			}
		}
	}
	for _, format := range desired {
		if byFormat[format] != "" {
			continue
		}
		client := upstreamClientClaude
		if format == managedconfig.FormatOpenAIResponses {
			client = upstreamClientCodex
		}
		preset, err := upstreamPresetFor(client, string(format))
		if err != nil {
			return err
		}
		provider, _ := providerForFormat(format)
		targetID := uniqueManagedMemberID(working, "target-"+base+"-"+strings.ReplaceAll(string(format), "_", "-"))
		working.Targets = append(working.Targets, defaultUpstreamTarget(
			targetID, strings.TrimSpace(values["name"])+" · "+managedconfig.FormatLabel(format), upstream.EndpointID, credentials[provider], quotaID, preset,
		))
		byFormat[format] = targetID
	}

	desiredSet := map[managedconfig.Format]bool{}
	for _, format := range desired {
		desiredSet[format] = true
	}
	removedTargets := map[string]managedconfig.Format{}
	for format, id := range byFormat {
		if !desiredSet[format] {
			removedTargets[id] = format
			delete(byFormat, format)
		}
	}
	externalTargets := map[managedconfig.Format]string{}
	if replacementObjectID := strings.TrimSpace(values["migration_replacement_id"]); replacementObjectID != "" {
		replacementObject, ok := managedconfig.FindObject(working, replacementObjectID)
		if !ok || replacementObject.Kind != generated.ManagedObjectKindUpstream {
			return fmt.Errorf("$.api_formats: replacement upstream no longer exists")
		}
		for _, id := range managedconfig.Members(replacementObject, generated.ManagedResourceRefKindTarget) {
			if target, ok := findTarget(working, id); ok {
				if format, ok := managedconfig.FormatForTarget(target); ok {
					externalTargets[format] = id
				}
			}
		}
	}
	for i := range working.BackendSets {
		for j := range working.BackendSets[i].Candidates {
			oldID := string(working.BackendSets[i].Candidates[j].TargetId)
			if _, removing := removedTargets[oldID]; !removing {
				continue
			}
			replacement := replacementTargetForBackend(working, working.BackendSets[i].Id, byFormat)
			if replacement == "" {
				replacement = replacementTargetForBackend(working, working.BackendSets[i].Id, externalTargets)
			}
			if replacement == "" {
				return fmt.Errorf("$.api_formats: choose a compatible replacement upstream")
			}
			working.BackendSets[i].Candidates[j].TargetId = generated.ConfigID(replacement)
			for policyIndex := range working.ModelPolicies {
				for mappingIndex := range working.ModelPolicies[policyIndex].Mappings {
					if string(working.ModelPolicies[policyIndex].Mappings[mappingIndex].TargetId) == oldID {
						working.ModelPolicies[policyIndex].Mappings[mappingIndex].TargetId = generated.ConfigID(replacement)
					}
				}
			}
		}
		working.BackendSets[i].Candidates = dedupeBackendCandidates(working.BackendSets[i].Candidates)
	}
	for i := range working.ModelPolicies {
		working.ModelPolicies[i].Mappings = dedupeModelMappings(working.ModelPolicies[i].Mappings)
	}
	working.Targets = filterManagedTargets(working.Targets, removedTargets)

	usedCredentials := map[string]bool{}
	for _, target := range working.Targets {
		usedCredentials[string(target.CredentialId)] = true
	}
	ownedCredentials := map[string]bool{}
	for _, id := range upstream.CredentialIDs {
		ownedCredentials[id] = true
	}
	working.Credentials = filterManagedCredentials(working.Credentials, func(id string) bool {
		return !ownedCredentials[id] || usedCredentials[id]
	})

	name := strings.TrimSpace(values["name"])
	working.Endpoints[endpointIndex].Name = name
	working.Endpoints[endpointIndex].BaseUrl = strings.TrimSpace(values["base_url"])
	working.Endpoints[endpointIndex].AllowPrivateNetwork = strings.EqualFold(values["allow_private_network"], "true")
	object.Name = name
	object.Members = []generated.ManagedResourceRef{{Kind: generated.ManagedResourceRefKindEndpoint, Id: working.Endpoints[endpointIndex].Id}}
	for _, credential := range working.Credentials {
		for _, id := range credentials {
			if string(credential.Id) == id && usedCredentials[id] {
				object.Members = append(object.Members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindCredential, Id: credential.Id})
			}
		}
	}
	for _, id := range byFormat {
		object.Members = append(object.Members, generated.ManagedResourceRef{Kind: generated.ManagedResourceRefKindTarget, Id: generated.ConfigID(id)})
	}
	managedconfig.ReplaceObject(&working, object)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) { *cmd = working })
	for provider, stageID := range stages {
		if credentialID := credentials[provider]; stageID != "" && credentialID != "" {
			draft.SetCredentialReplace(credentialID, stageID)
		}
	}
	return nil
}

func dedupeBackendCandidates(values []generated.BackendCandidate) []generated.BackendCandidate {
	seen := map[string]bool{}
	out := values[:0]
	for _, value := range values {
		id := string(value.TargetId)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, value)
	}
	for index := range out {
		if index == 0 {
			out[index].Priority = 0
		}
	}
	return out
}

func dedupeModelMappings(values []generated.ModelMapping) []generated.ModelMapping {
	seen := map[string]bool{}
	out := values[:0]
	for _, value := range values {
		key := string(value.TargetId) + "\x00" + value.LogicalModel
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func replacementTargetForBackend(cmd generated.MutableConfigCommand, backendID generated.ConfigID, targets map[managedconfig.Format]string) string {
	for _, route := range cmd.Routes {
		if route.BackendSetId != backendID {
			continue
		}
		switch route.IngressProtocol {
		case generated.RouteConfigIngressProtocolOpenaiResponses:
			return targets[managedconfig.FormatOpenAIResponses]
		case generated.RouteConfigIngressProtocolAnthropicMessages:
			if id := targets[managedconfig.FormatAnthropicMessages]; id != "" {
				return id
			}
			return targets[managedconfig.FormatOpenAIChat]
		}
	}
	return ""
}

func uniqueManagedMemberID(cmd generated.MutableConfigCommand, base string) string {
	used := map[string]bool{}
	for _, target := range cmd.Targets {
		used[string(target.Id)] = true
	}
	for _, credential := range cmd.Credentials {
		used[string(credential.Id)] = true
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if !used[candidate] {
			return candidate
		}
	}
}

func providerName(provider generated.MutableCredentialCommandProvider) string {
	if provider == generated.MutableCredentialCommandProviderAnthropic {
		return "anthropic"
	}
	return "openai"
}

func filterManagedTargets(values []generated.MutableTargetCommand, removed map[string]managedconfig.Format) []generated.MutableTargetCommand {
	out := values[:0]
	for _, value := range values {
		if _, drop := removed[string(value.Id)]; !drop {
			out = append(out, value)
		}
	}
	return out
}

func filterManagedCredentials(values []generated.MutableCredentialCommand, keep func(string) bool) []generated.MutableCredentialCommand {
	out := values[:0]
	for _, value := range values {
		if keep(string(value.Id)) {
			out = append(out, value)
		}
	}
	return out
}
