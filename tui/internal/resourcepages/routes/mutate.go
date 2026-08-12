package routes

import (
	"fmt"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
)

func applyCreate(draft *configdraft.Draft, kind Kind, values map[string]string) error {
	id := strings.TrimSpace(values["id"])
	if id == "" {
		return fmt.Errorf("$.id: required")
	}
	var applyErr error
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		if resourceExistsInCmd(cmd, kind, id) {
			applyErr = fmt.Errorf("$.id: already exists")
			return
		}
		switch kind {
		case KindRoutes:
			route, err := buildRoute(values, generated.RouteConfig{}, true)
			if err != nil {
				applyErr = err
				return
			}
			cmd.Routes = append(cmd.Routes, route)
		case KindBackendSets:
			bs, err := buildBackendSet(values, generated.BackendSetConfig{
				Candidates: []generated.BackendCandidate{},
			}, true)
			if err != nil {
				applyErr = err
				return
			}
			cmd.BackendSets = append(cmd.BackendSets, bs)
		case KindContentPolicies:
			cp, err := buildContentPolicy(values, generated.ContentPolicyConfig{}, true)
			if err != nil {
				applyErr = err
				return
			}
			cmd.ContentPolicies = append(cmd.ContentPolicies, cp)
		case KindModelPolicies:
			mp, err := buildModelPolicy(values, generated.ModelPolicyConfig{
				Mappings: []generated.ModelMapping{},
			}, true)
			if err != nil {
				applyErr = err
				return
			}
			cmd.ModelPolicies = append(cmd.ModelPolicies, mp)
		case KindModelProjections:
			proj, err := buildModelProjection(values, generated.ModelProjectionConfig{
				LogicalModels: []string{},
			}, true)
			if err != nil {
				applyErr = err
				return
			}
			cmd.ModelProjections = append(cmd.ModelProjections, proj)
		case KindTransforms:
			xf, err := buildTransform(values, generated.CompatibilityTransformConfig{}, true)
			if err != nil {
				applyErr = err
				return
			}
			cmd.CompatibilityTransforms = append(cmd.CompatibilityTransforms, xf)
		default:
			applyErr = fmt.Errorf("unsupported kind %s", kind)
		}
	})
	return applyErr
}

func applyEdit(draft *configdraft.Draft, kind Kind, id string, values map[string]string) error {
	var applyErr error
	draft.Mutate(func(c *generated.MutableConfigCommand) {
		switch kind {
		case KindRoutes:
			idx := indexOfID(len(c.Routes), func(i int) string { return string(c.Routes[i].Id) }, id)
			if idx < 0 {
				applyErr = fmt.Errorf("$.id: not found")
				return
			}
			route, err := buildRoute(values, c.Routes[idx], false)
			if err != nil {
				applyErr = err
				return
			}
			c.Routes[idx] = route
		case KindBackendSets:
			idx := indexOfID(len(c.BackendSets), func(i int) string { return string(c.BackendSets[i].Id) }, id)
			if idx < 0 {
				applyErr = fmt.Errorf("$.id: not found")
				return
			}
			bs, err := buildBackendSet(values, c.BackendSets[idx], false)
			if err != nil {
				applyErr = err
				return
			}
			c.BackendSets[idx] = bs
		case KindContentPolicies:
			idx := indexOfID(len(c.ContentPolicies), func(i int) string { return string(c.ContentPolicies[i].Id) }, id)
			if idx < 0 {
				applyErr = fmt.Errorf("$.id: not found")
				return
			}
			cp, err := buildContentPolicy(values, c.ContentPolicies[idx], false)
			if err != nil {
				applyErr = err
				return
			}
			c.ContentPolicies[idx] = cp
		case KindModelPolicies:
			idx := indexOfID(len(c.ModelPolicies), func(i int) string { return string(c.ModelPolicies[i].Id) }, id)
			if idx < 0 {
				applyErr = fmt.Errorf("$.id: not found")
				return
			}
			mp, err := buildModelPolicy(values, c.ModelPolicies[idx], false)
			if err != nil {
				applyErr = err
				return
			}
			c.ModelPolicies[idx] = mp
		case KindModelProjections:
			idx := indexOfID(len(c.ModelProjections), func(i int) string { return string(c.ModelProjections[i].Id) }, id)
			if idx < 0 {
				applyErr = fmt.Errorf("$.id: not found")
				return
			}
			proj, err := buildModelProjection(values, c.ModelProjections[idx], false)
			if err != nil {
				applyErr = err
				return
			}
			c.ModelProjections[idx] = proj
		case KindTransforms:
			idx := indexOfID(len(c.CompatibilityTransforms), func(i int) string { return string(c.CompatibilityTransforms[i].Id) }, id)
			if idx < 0 {
				applyErr = fmt.Errorf("$.id: not found")
				return
			}
			xf, err := buildTransform(values, c.CompatibilityTransforms[idx], false)
			if err != nil {
				applyErr = err
				return
			}
			c.CompatibilityTransforms[idx] = xf
		default:
			applyErr = fmt.Errorf("unsupported kind %s", kind)
		}
	})
	return applyErr
}

func indexOfID(n int, idAt func(int) string, id string) int {
	for i := 0; i < n; i++ {
		if idAt(i) == id {
			return i
		}
	}
	return -1
}

func resourceExistsInCmd(cmd *generated.MutableConfigCommand, kind Kind, id string) bool {
	switch kind {
	case KindTrafficRules:
		object, ok := managedconfig.FindObject(*cmd, id)
		return ok && object.Kind == generated.ManagedObjectKindTrafficRule
	case KindRoutes:
		_, ok := findRoute(*cmd, id)
		return ok
	case KindBackendSets:
		_, ok := findBackendSet(*cmd, id)
		return ok
	case KindContentPolicies:
		_, ok := findContentPolicy(*cmd, id)
		return ok
	case KindModelPolicies:
		_, ok := findModelPolicy(*cmd, id)
		return ok
	case KindModelProjections:
		_, ok := findModelProjection(*cmd, id)
		return ok
	case KindTransforms:
		_, ok := findTransform(*cmd, id)
		return ok
	default:
		return false
	}
}

func applyDelete(draft *configdraft.Draft, kind Kind, id string) bool {
	removed := false
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		if !resourceExistsInCmd(cmd, kind, id) {
			return
		}
		removed = true
		switch kind {
		case KindTrafficRules:
			removed = managedconfig.DeleteObject(cmd, id)
		case KindRoutes:
			cmd.Routes = filterRoutes(cmd.Routes, id)
		case KindBackendSets:
			cmd.BackendSets = filterBackendSets(cmd.BackendSets, id)
		case KindContentPolicies:
			cmd.ContentPolicies = filterContentPolicies(cmd.ContentPolicies, id)
		case KindModelPolicies:
			cmd.ModelPolicies = filterModelPolicies(cmd.ModelPolicies, id)
		case KindModelProjections:
			cmd.ModelProjections = filterModelProjections(cmd.ModelProjections, id)
		case KindTransforms:
			cmd.CompatibilityTransforms = filterTransforms(cmd.CompatibilityTransforms, id)
		}
	})
	return removed
}

func filterRoutes(in []generated.RouteConfig, id string) []generated.RouteConfig {
	out := make([]generated.RouteConfig, 0, len(in))
	for _, item := range in {
		if string(item.Id) != id {
			out = append(out, item)
		}
	}
	return out
}

func filterBackendSets(in []generated.BackendSetConfig, id string) []generated.BackendSetConfig {
	out := make([]generated.BackendSetConfig, 0, len(in))
	for _, item := range in {
		if string(item.Id) != id {
			out = append(out, item)
		}
	}
	return out
}

func filterContentPolicies(in []generated.ContentPolicyConfig, id string) []generated.ContentPolicyConfig {
	out := make([]generated.ContentPolicyConfig, 0, len(in))
	for _, item := range in {
		if string(item.Id) != id {
			out = append(out, item)
		}
	}
	return out
}

func filterModelPolicies(in []generated.ModelPolicyConfig, id string) []generated.ModelPolicyConfig {
	out := make([]generated.ModelPolicyConfig, 0, len(in))
	for _, item := range in {
		if string(item.Id) != id {
			out = append(out, item)
		}
	}
	return out
}

func filterModelProjections(in []generated.ModelProjectionConfig, id string) []generated.ModelProjectionConfig {
	out := make([]generated.ModelProjectionConfig, 0, len(in))
	for _, item := range in {
		if string(item.Id) != id {
			out = append(out, item)
		}
	}
	return out
}

func filterTransforms(in []generated.CompatibilityTransformConfig, id string) []generated.CompatibilityTransformConfig {
	out := make([]generated.CompatibilityTransformConfig, 0, len(in))
	for _, item := range in {
		if string(item.Id) != id {
			out = append(out, item)
		}
	}
	return out
}

func buildRoute(values map[string]string, base generated.RouteConfig, creating bool) (generated.RouteConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = valueOrDefault(values, "name", out.Name)
	if v := values["ingress_protocol"]; v != "" {
		out.IngressProtocol = generated.RouteConfigIngressProtocol(v)
	}
	if v := values["routing_policy"]; v != "" {
		out.RoutingPolicy = generated.RouteConfigRoutingPolicy(v)
	}
	if v := values["backend_set_id"]; v != "" {
		out.BackendSetId = generated.ConfigID(v)
	}
	if v := values["model_policy_id"]; v != "" {
		out.ModelPolicyId = generated.ConfigID(v)
	}
	if v, ok := values["content_policy_id"]; ok {
		if strings.TrimSpace(v) == "" {
			out.ContentPolicyId = nil
		} else {
			id := generated.ConfigID(strings.TrimSpace(v))
			out.ContentPolicyId = &id
		}
	}
	out.MaxAttempts = parseIntOr(values, "max_attempts", defaultInt(out.MaxAttempts, 2))
	out.MaxRequestBodyBytes = parseInt64Or(values, "max_request_body_bytes", defaultInt64(out.MaxRequestBodyBytes, 33554432))
	out.RequestDeadlineMs = parseInt64Or(values, "request_deadline_ms", out.RequestDeadlineMs)
	out.RetryDeadlineMs = parseInt64Or(values, "retry_deadline_ms", out.RetryDeadlineMs)
	out.StreamIdleTimeoutMs = parseInt64Or(values, "stream_idle_timeout_ms", out.StreamIdleTimeoutMs)
	return out, nil
}

func buildBackendSet(values map[string]string, base generated.BackendSetConfig, creating bool) (generated.BackendSetConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = valueOrDefault(values, "name", out.Name)
	candidates, err := parseCandidates(values)
	if err != nil {
		return out, err
	}
	out.Candidates = candidates
	if raw, ok := values["required_capabilities"]; ok {
		caps := parseCSV(raw)
		if len(caps) == 0 {
			out.RequiredCapabilities = nil
		} else {
			parsed := make([]generated.BackendSetConfigRequiredCapabilities, 0, len(caps))
			for _, c := range caps {
				parsed = append(parsed, generated.BackendSetConfigRequiredCapabilities(c))
			}
			out.RequiredCapabilities = &parsed
		}
	}
	return out, nil
}

func parseCandidates(values map[string]string) ([]generated.BackendCandidate, error) {
	n := arrayLenFromValues(values, "candidates")
	if n == 0 {
		return []generated.BackendCandidate{}, nil
	}
	out := make([]generated.BackendCandidate, 0, n)
	for i := 0; i < n; i++ {
		prefix := fmt.Sprintf("candidates[%d]", i)
		target := strings.TrimSpace(values[prefix+".target_id"])
		if target == "" {
			return nil, fmt.Errorf("$.candidates[%d].target_id: required", i)
		}
		priority, err := parseRequiredInt(values, prefix+".priority", fmt.Sprintf("$.candidates[%d].priority", i))
		if err != nil {
			return nil, err
		}
		weight, err := parseRequiredInt(values, prefix+".weight", fmt.Sprintf("$.candidates[%d].weight", i))
		if err != nil {
			return nil, err
		}
		if weight < 1 {
			return nil, fmt.Errorf("$.candidates[%d].weight: below minimum", i)
		}
		out = append(out, generated.BackendCandidate{
			TargetId: generated.ConfigID(target),
			Priority: priority,
			Weight:   weight,
		})
	}
	return out, nil
}

func buildContentPolicy(values map[string]string, base generated.ContentPolicyConfig, creating bool) (generated.ContentPolicyConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	if v, ok := values["mode"]; ok {
		if strings.TrimSpace(v) == "" {
			if creating {
				mode := generated.ContentPolicyConfigModeRedact
				out.Mode = &mode
			} else {
				out.Mode = nil
			}
		} else {
			mode := generated.ContentPolicyConfigMode(v)
			out.Mode = &mode
		}
	} else if creating && out.Mode == nil {
		mode := generated.ContentPolicyConfigModeRedact
		out.Mode = &mode
	}
	if v, ok := values["max_inspection_bytes"]; ok {
		if strings.TrimSpace(v) == "" {
			if creating {
				n := int64(1048576)
				out.MaxInspectionBytes = &n
			} else {
				out.MaxInspectionBytes = nil
			}
		} else {
			n := parseInt64Or(values, "max_inspection_bytes", 1048576)
			out.MaxInspectionBytes = &n
		}
	} else if creating && out.MaxInspectionBytes == nil {
		n := int64(1048576)
		out.MaxInspectionBytes = &n
	}
	return out, nil
}

func buildModelPolicy(values map[string]string, base generated.ModelPolicyConfig, creating bool) (generated.ModelPolicyConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = valueOrDefault(values, "name", out.Name)
	ttl, err := parseRequiredInt64(values, "catalog_ttl_ms", "$.catalog_ttl_ms")
	if err != nil {
		return out, err
	}
	out.CatalogTtlMs = ttl
	timeout, err := parseRequiredInt64(values, "discovery_timeout_ms", "$.discovery_timeout_ms")
	if err != nil {
		return out, err
	}
	out.DiscoveryTimeoutMs = timeout
	mappings, err := parseMappings(values)
	if err != nil {
		return out, err
	}
	out.Mappings = mappings
	return out, nil
}

func parseMappings(values map[string]string) ([]generated.ModelMapping, error) {
	n := arrayLenFromValues(values, "mappings")
	if n == 0 {
		return []generated.ModelMapping{}, nil
	}
	out := make([]generated.ModelMapping, 0, n)
	for i := 0; i < n; i++ {
		prefix := fmt.Sprintf("mappings[%d]", i)
		logical := strings.TrimSpace(values[prefix+".logical_model"])
		physical := strings.TrimSpace(values[prefix+".physical_model"])
		target := strings.TrimSpace(values[prefix+".target_id"])
		if logical == "" {
			return nil, fmt.Errorf("$.mappings[%d].logical_model: required", i)
		}
		if physical == "" {
			return nil, fmt.Errorf("$.mappings[%d].physical_model: required", i)
		}
		if target == "" {
			return nil, fmt.Errorf("$.mappings[%d].target_id: required", i)
		}
		out = append(out, generated.ModelMapping{
			LogicalModel:  logical,
			PhysicalModel: physical,
			TargetId:      generated.ConfigID(target),
		})
	}
	return out, nil
}

func buildModelProjection(values map[string]string, base generated.ModelProjectionConfig, creating bool) (generated.ModelProjectionConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = valueOrDefault(values, "name", out.Name)
	if raw, ok := values["logical_models"]; ok {
		out.LogicalModels = parseCSV(raw)
	} else if creating && out.LogicalModels == nil {
		out.LogicalModels = []string{}
	}
	return out, nil
}

func buildTransform(values map[string]string, base generated.CompatibilityTransformConfig, creating bool) (generated.CompatibilityTransformConfig, error) {
	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = valueOrDefault(values, "name", out.Name)
	if v := values["scope"]; v != "" {
		out.Scope = generated.CompatibilityTransformConfigScope(v)
	} else if creating && out.Scope == "" {
		out.Scope = generated.ClientProfile
	}
	if v := values["scope_id"]; v != "" {
		out.ScopeId = generated.ConfigID(v)
	}
	op, err := parseOperation(values)
	if err != nil {
		return out, err
	}
	out.Operation = op
	return out, nil
}

func parseOperation(values map[string]string) (generated.CompatibilityTransformOperation, error) {
	kind := strings.TrimSpace(values["operation"])
	var op generated.CompatibilityTransformOperation
	if kind == "" {
		return op, fmt.Errorf("$.operation: required")
	}
	switch kind {
	case "rename_model":
		src := strings.TrimSpace(values["operation.source_model"])
		dst := strings.TrimSpace(values["operation.destination_model"])
		if src == "" || dst == "" {
			return op, fmt.Errorf("$.operation.rename_model: source_model and destination_model required")
		}
		op.RenameModel = &generated.RenameModelTransform{SourceModel: src, DestinationModel: dst}
	case "drop_header":
		name := strings.TrimSpace(values["operation.header_name"])
		if name == "" {
			return op, fmt.Errorf("$.operation.drop_header.header_name: required")
		}
		op.DropHeader = &generated.HeaderNameTransform{HeaderName: name}
	case "set_header":
		name := strings.TrimSpace(values["operation.header_name"])
		val := values["operation.header_value"]
		if name == "" {
			return op, fmt.Errorf("$.operation.set_header.header_name: required")
		}
		op.SetHeader = &generated.SetHeaderTransform{HeaderName: name, HeaderValue: val}
	case "normalize_stream_event":
		src := strings.TrimSpace(values["operation.source_event"])
		dst := strings.TrimSpace(values["operation.destination_event"])
		if src == "" || dst == "" {
			return op, fmt.Errorf("$.operation.normalize_stream_event: source_event and destination_event required")
		}
		op.NormalizeStreamEvent = &generated.NormalizeStreamEventTransform{SourceEvent: src, DestinationEvent: dst}
	default:
		return op, fmt.Errorf("$.operation: invalid kind %q", kind)
	}
	return op, nil
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
