package targets

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
)

func applyTargetCreate(draft *configdraft.Draft, values map[string]string) error {
	id := strings.TrimSpace(values["id"])
	if id == "" {
		return fmt.Errorf("$.id: required")
	}
	if resourceExists(draft, KindTargets, id) {
		return fmt.Errorf("$.id: already exists")
	}
	tgt, err := buildTarget(values, generated.MutableTargetCommand{}, true)
	if err != nil {
		return err
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = append(cmd.Targets, tgt)
	})
	return nil
}

func applyTargetEdit(draft *configdraft.Draft, id string, values map[string]string) error {
	base, ok := findTarget(draft.LocalCommand(), id)
	if !ok {
		return fmt.Errorf("$.id: not found")
	}
	tgt, err := buildTarget(values, base, false)
	if err != nil {
		return err
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Targets {
			if string(cmd.Targets[i].Id) == id {
				cmd.Targets[i] = tgt
				return
			}
		}
	})
	return nil
}

func buildTarget(values map[string]string, base generated.MutableTargetCommand, creating bool) (generated.MutableTargetCommand, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = strings.TrimSpace(values["name"])
	out.Adapter = generated.MutableTargetCommandAdapter(strings.TrimSpace(values["adapter"]))
	out.Bridge = generated.MutableTargetCommandBridge(strings.TrimSpace(values["bridge"]))
	out.CredentialId = generated.ConfigID(strings.TrimSpace(values["credential_id"]))
	out.EndpointId = generated.ConfigID(strings.TrimSpace(values["endpoint_id"]))
	out.QuotaGroupId = generated.ConfigID(strings.TrimSpace(values["quota_group_id"]))
	caps, err := parseCapabilities(values["capabilities"])
	if err != nil {
		return out, err
	}
	out.Capabilities = caps
	out.HealthPolicy = generated.HealthPolicyConfig{
		FailureThreshold:         parseIntOr(values, "failure_threshold", out.HealthPolicy.FailureThreshold),
		InitialBackoffMs:         parseInt64Or(values, "initial_backoff_ms", out.HealthPolicy.InitialBackoffMs),
		JitterPercent:            parseIntOr(values, "jitter_percent", out.HealthPolicy.JitterPercent),
		MaxBackoffMs:             parseInt64Or(values, "max_backoff_ms", out.HealthPolicy.MaxBackoffMs),
		ProbeTimeoutMs:           parseInt64Or(values, "probe_timeout_ms", out.HealthPolicy.ProbeTimeoutMs),
		RecoverySuccessThreshold: parseIntOr(values, "recovery_success_threshold", out.HealthPolicy.RecoverySuccessThreshold),
		StableProbeIntervalMs:    parseInt64Or(values, "stable_probe_interval_ms", out.HealthPolicy.StableProbeIntervalMs),
	}
	out.ThrottlePolicy = generated.ThrottlePolicyConfig{
		DefaultCoolingMs: parseInt64Or(values, "default_cooling_ms", out.ThrottlePolicy.DefaultCoolingMs),
		MaxCoolingMs:     parseInt64Or(values, "max_cooling_ms", out.ThrottlePolicy.MaxCoolingMs),
	}
	return out, nil
}

func parseCapabilities(raw string) ([]generated.MutableTargetCommandCapabilities, error) {
	parts := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("$.capabilities: required")
	}
	out := make([]generated.MutableTargetCommandCapabilities, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		cap := strings.TrimSpace(part)
		if cap == "" {
			continue
		}
		if _, ok := seen[cap]; ok {
			continue
		}
		typed := generated.MutableTargetCommandCapabilities(cap)
		if !typed.Valid() {
			return nil, fmt.Errorf("$.capabilities: invalid")
		}
		seen[cap] = struct{}{}
		out = append(out, typed)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("$.capabilities: required")
	}
	return out, nil
}

func applyEndpointCreate(draft *configdraft.Draft, values map[string]string) error {
	id := strings.TrimSpace(values["id"])
	if id == "" {
		return fmt.Errorf("$.id: required")
	}
	if resourceExists(draft, KindEndpoints, id) {
		return fmt.Errorf("$.id: already exists")
	}
	ep, err := buildEndpoint(values, generated.EndpointConfig{}, true)
	if err != nil {
		return err
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Endpoints = append(cmd.Endpoints, ep)
	})
	return nil
}

func applyEndpointEdit(draft *configdraft.Draft, id string, values map[string]string) error {
	base, ok := findEndpoint(draft.LocalCommand(), id)
	if !ok {
		return fmt.Errorf("$.id: not found")
	}
	ep, err := buildEndpoint(values, base, false)
	if err != nil {
		return err
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Endpoints {
			if string(cmd.Endpoints[i].Id) == id {
				cmd.Endpoints[i] = ep
				return
			}
		}
	})
	return nil
}

func buildEndpoint(values map[string]string, base generated.EndpointConfig, creating bool) (generated.EndpointConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = strings.TrimSpace(values["name"])
	out.BaseUrl = strings.TrimSpace(values["base_url"])
	out.Http2Enabled = parseBoolOr(values, "http2_enabled", out.Http2Enabled)
	out.AllowPrivateNetwork = parseBoolOr(values, "allow_private_network", out.AllowPrivateNetwork)
	out.IdleConnectionTimeoutMs = parseInt64Or(values, "idle_connection_timeout_ms", defaultInt64(out.IdleConnectionTimeoutMs, 30000))
	out.MaxIdleConnections = parseIntOr(values, "max_idle_connections", defaultInt(out.MaxIdleConnections, 10))

	if raw, ok := values["proxy_url"]; ok {
		if strings.TrimSpace(raw) == "" {
			out.ProxyUrl = nil
		} else {
			v := strings.TrimSpace(raw)
			out.ProxyUrl = &v
		}
	}
	if raw, ok := values["tls_server_name"]; ok {
		if strings.TrimSpace(raw) == "" {
			out.TlsServerName = nil
		} else {
			v := strings.TrimSpace(raw)
			out.TlsServerName = &v
		}
	}
	return out, nil
}

func parseBoolOr(values map[string]string, name string, fallback bool) bool {
	raw := strings.TrimSpace(values[name])
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseIntOr(values map[string]string, name string, fallback int) int {
	raw := strings.TrimSpace(values[name])
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func parseInt64Or(values map[string]string, name string, fallback int64) int64 {
	raw := strings.TrimSpace(values[name])
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func defaultInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func defaultInt64(v, fallback int64) int64 {
	if v == 0 {
		return fallback
	}
	return v
}

func applyCredentialCreate(draft *configdraft.Draft, values map[string]string, action generated.CredentialSecretAction) error {
	id := strings.TrimSpace(values["id"])
	if resourceExists(draft, KindCredentials, id) {
		return fmt.Errorf("$.id: already exists")
	}
	cred := generated.MutableCredentialCommand{
		Id:           generated.ConfigID(id),
		Name:         strings.TrimSpace(values["name"]),
		Provider:     generated.MutableCredentialCommandProvider(strings.TrimSpace(values["provider"])),
		SecretAction: action,
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Credentials = append(cmd.Credentials, cred)
	})
	return nil
}

func applyCredentialEditMeta(draft *configdraft.Draft, id string, values map[string]string) error {
	name := strings.TrimSpace(values["name"])
	if name == "" {
		return fmt.Errorf("$.name: required")
	}
	found := false
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Credentials {
			if string(cmd.Credentials[i].Id) == id {
				cmd.Credentials[i].Name = name
				found = true
				return
			}
		}
	})
	if !found {
		return fmt.Errorf("$.id: not found")
	}
	return nil
}

func applyCredentialExternal(draft *configdraft.Draft, id string, values map[string]string) error {
	ref, err := buildExternalRef(values)
	if err != nil {
		return err
	}
	var action generated.CredentialSecretAction
	if err := action.FromCredentialSecretAction2(generated.CredentialSecretAction2{
		Mode: generated.CredentialSecretActionExternalRef,
		Ref:  ref,
	}); err != nil {
		return err
	}
	name := strings.TrimSpace(values["name"])
	found := false
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Credentials {
			if string(cmd.Credentials[i].Id) == id {
				cmd.Credentials[i].Name = name
				cmd.Credentials[i].SecretAction = action
				found = true
				return
			}
		}
	})
	if !found {
		return fmt.Errorf("$.id: not found")
	}
	return nil
}

func applyCredentialPreserve(draft *configdraft.Draft, id string, values map[string]string) error {
	if err := applyCredentialEditMeta(draft, id, values); err != nil {
		return err
	}
	var action generated.CredentialSecretAction
	_ = action.FromCredentialSecretAction0(generated.CredentialSecretAction0{
		Mode: generated.CredentialSecretActionPreserve,
	})
	found := false
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Credentials {
			if string(cmd.Credentials[i].Id) == id {
				cmd.Credentials[i].SecretAction = action
				found = true
				return
			}
		}
	})
	if !found {
		return fmt.Errorf("$.id: not found")
	}
	return nil
}

func replaceStageIDFromDraft(draft *configdraft.Draft, credentialID string) string {
	if draft == nil {
		return ""
	}
	c, ok := findCredential(draft.LocalCommand(), credentialID)
	if !ok {
		return ""
	}
	replace, err := c.SecretAction.AsCredentialSecretAction1()
	if err != nil || replace.Mode != generated.CredentialSecretActionReplace {
		return ""
	}
	return string(replace.StageId)
}

func applyDelete(draft *configdraft.Draft, kind Kind, id string) {
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		switch kind {
		case KindUpstreams:
			managedconfig.DeleteObject(cmd, id)
		case KindLimitPolicies:
			managedconfig.DeleteObject(cmd, id)
		case KindCredentials:
			out := make([]generated.MutableCredentialCommand, 0, len(cmd.Credentials))
			for _, c := range cmd.Credentials {
				if string(c.Id) != id {
					out = append(out, c)
				}
			}
			cmd.Credentials = out
		case KindTargets:
			out := make([]generated.MutableTargetCommand, 0, len(cmd.Targets))
			for _, t := range cmd.Targets {
				if string(t.Id) != id {
					out = append(out, t)
				}
			}
			cmd.Targets = out
		case KindEndpoints:
			out := make([]generated.EndpointConfig, 0, len(cmd.Endpoints))
			for _, e := range cmd.Endpoints {
				if string(e.Id) != id {
					out = append(out, e)
				}
			}
			cmd.Endpoints = out
		}
	})
}

func buildExternalRef(values map[string]string) (generated.ExternalSecretRef, error) {
	exportable := strings.EqualFold(strings.TrimSpace(values["external_exportable"]), "true")
	ref := generated.ExternalSecretRef{Exportable: exportable}
	switch strings.TrimSpace(values["external_kind"]) {
	case "env":
		ref.Env = &generated.EnvSecretLocator{Name: strings.TrimSpace(values["external_env_name"])}
	case "file":
		ref.File = &generated.FileSecretLocator{Path: strings.TrimSpace(values["external_file_path"])}
	case "keychain":
		ref.Keychain = &generated.KeychainSecretLocator{
			Service: strings.TrimSpace(values["external_keychain_service"]),
			Account: strings.TrimSpace(values["external_keychain_account"]),
		}
	default:
		return ref, fmt.Errorf("$.secret_action.ref: required locator")
	}
	return ref, nil
}

func buildReplaceAction(stageID string) (generated.CredentialSecretAction, error) {
	var action generated.CredentialSecretAction
	err := action.FromCredentialSecretAction1(generated.CredentialSecretAction1{
		Mode:    generated.CredentialSecretActionReplace,
		StageId: generated.SecretStageID(stageID),
	})
	return action, err
}
