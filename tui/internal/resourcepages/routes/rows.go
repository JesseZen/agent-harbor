package routes

import (
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
)

func rowsFor(draft *configdraft.Draft, kind Kind) []resourceview.Row {
	return rowsForStatus(draft, kind, nil)
}

func rowsForStatus(draft *configdraft.Draft, kind Kind, status RouteStatusProvider) []resourceview.Row {
	cmd := draft.LocalCommand()
	switch kind {
	case KindTrafficRules:
		return trafficRows(draft, status)
	case KindRoutes:
		rows := make([]resourceview.Row, 0, len(cmd.Routes))
		for _, r := range cmd.Routes {
			rows = append(rows, resourceview.Row{
				ID: string(r.Id),
				Cells: []string{
					string(r.Id),
					r.Name,
					string(r.IngressProtocol),
					string(r.RoutingPolicy),
					string(r.BackendSetId),
					string(r.ModelPolicyId),
					strconv.Itoa(r.MaxAttempts),
				},
			})
		}
		return rows
	case KindBackendSets:
		rows := make([]resourceview.Row, 0, len(cmd.BackendSets))
		for _, b := range cmd.BackendSets {
			caps := ""
			if b.RequiredCapabilities != nil {
				parts := make([]string, 0, len(*b.RequiredCapabilities))
				for _, c := range *b.RequiredCapabilities {
					parts = append(parts, string(c))
				}
				caps = strings.Join(parts, ",")
			}
			rows = append(rows, resourceview.Row{
				ID: string(b.Id),
				Cells: []string{
					string(b.Id),
					b.Name,
					strconv.Itoa(len(b.Candidates)),
					caps,
				},
			})
		}
		return rows
	case KindContentPolicies:
		rows := make([]resourceview.Row, 0, len(cmd.ContentPolicies))
		for _, c := range cmd.ContentPolicies {
			mode := ""
			if c.Mode != nil {
				mode = string(*c.Mode)
			}
			maxBytes := ""
			if c.MaxInspectionBytes != nil {
				maxBytes = strconv.FormatInt(*c.MaxInspectionBytes, 10)
			}
			rows = append(rows, resourceview.Row{
				ID:    string(c.Id),
				Cells: []string{string(c.Id), mode, maxBytes},
			})
		}
		return rows
	case KindModelPolicies:
		rows := make([]resourceview.Row, 0, len(cmd.ModelPolicies))
		for _, m := range cmd.ModelPolicies {
			rows = append(rows, resourceview.Row{
				ID: string(m.Id),
				Cells: []string{
					string(m.Id),
					m.Name,
					strconv.Itoa(len(m.Mappings)),
					strconv.FormatInt(m.CatalogTtlMs, 10),
					strconv.FormatInt(m.DiscoveryTimeoutMs, 10),
				},
			})
		}
		return rows
	case KindModelProjections:
		rows := make([]resourceview.Row, 0, len(cmd.ModelProjections))
		for _, m := range cmd.ModelProjections {
			rows = append(rows, resourceview.Row{
				ID: string(m.Id),
				Cells: []string{
					string(m.Id),
					m.Name,
					strconv.Itoa(len(m.LogicalModels)),
				},
			})
		}
		return rows
	case KindTransforms:
		rows := make([]resourceview.Row, 0, len(cmd.CompatibilityTransforms))
		for _, x := range cmd.CompatibilityTransforms {
			rows = append(rows, resourceview.Row{
				ID: string(x.Id),
				Cells: []string{
					string(x.Id),
					x.Name,
					string(x.Scope),
					string(x.ScopeId),
					operationLabel(x.Operation),
				},
			})
		}
		return rows
	default:
		return nil
	}
}

func operationLabel(op generated.CompatibilityTransformOperation) string {
	switch {
	case op.DropHeader != nil:
		return "drop_header"
	case op.NormalizeStreamEvent != nil:
		return "normalize_stream_event"
	case op.RenameModel != nil:
		return "rename_model"
	case op.SetHeader != nil:
		return "set_header"
	default:
		return ""
	}
}

func countFor(draft *configdraft.Draft, kind Kind) int {
	return len(rowsForStatus(draft, kind, nil))
}

func findRoute(cmd generated.MutableConfigCommand, id string) (generated.RouteConfig, bool) {
	for _, r := range cmd.Routes {
		if string(r.Id) == id {
			return r, true
		}
	}
	return generated.RouteConfig{}, false
}

func findBackendSet(cmd generated.MutableConfigCommand, id string) (generated.BackendSetConfig, bool) {
	for _, b := range cmd.BackendSets {
		if string(b.Id) == id {
			return b, true
		}
	}
	return generated.BackendSetConfig{}, false
}

func findContentPolicy(cmd generated.MutableConfigCommand, id string) (generated.ContentPolicyConfig, bool) {
	for _, c := range cmd.ContentPolicies {
		if string(c.Id) == id {
			return c, true
		}
	}
	return generated.ContentPolicyConfig{}, false
}

func findModelPolicy(cmd generated.MutableConfigCommand, id string) (generated.ModelPolicyConfig, bool) {
	for _, m := range cmd.ModelPolicies {
		if string(m.Id) == id {
			return m, true
		}
	}
	return generated.ModelPolicyConfig{}, false
}

func findModelProjection(cmd generated.MutableConfigCommand, id string) (generated.ModelProjectionConfig, bool) {
	for _, m := range cmd.ModelProjections {
		if string(m.Id) == id {
			return m, true
		}
	}
	return generated.ModelProjectionConfig{}, false
}

func findTransform(cmd generated.MutableConfigCommand, id string) (generated.CompatibilityTransformConfig, bool) {
	for _, x := range cmd.CompatibilityTransforms {
		if string(x.Id) == id {
			return x, true
		}
	}
	return generated.CompatibilityTransformConfig{}, false
}

func resourceExists(draft *configdraft.Draft, kind Kind, id string) bool {
	cmd := draft.LocalCommand()
	switch kind {
	case KindTrafficRules:
		_, ok := managedconfig.FindObject(cmd, id)
		return ok
	case KindRoutes:
		_, ok := findRoute(cmd, id)
		return ok
	case KindBackendSets:
		_, ok := findBackendSet(cmd, id)
		return ok
	case KindContentPolicies:
		_, ok := findContentPolicy(cmd, id)
		return ok
	case KindModelPolicies:
		_, ok := findModelPolicy(cmd, id)
		return ok
	case KindModelProjections:
		_, ok := findModelProjection(cmd, id)
		return ok
	case KindTransforms:
		_, ok := findTransform(cmd, id)
		return ok
	default:
		return false
	}
}
