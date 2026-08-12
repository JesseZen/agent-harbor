package targets

import (
	"fmt"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
)

func renderDetails(draft *configdraft.Draft, kind Kind, id string, width, height int) string {
	switch kind {
	case KindCredentials:
		return renderCredentialDetails(draft, id, width, height)
	case KindEndpoints:
		return renderEndpointDetails(draft, id, width, height)
	case KindTargets:
		return renderTargetDetails(draft, id, width, height)
	default:
		return detailpane.Model{
			Title:  detailpane.NamedTitle(kind.Label(), id),
			Width:  width,
			Height: height,
		}.View()
	}
}

func renderTargetDetails(draft *configdraft.Draft, id string, width, height int) string {
	values := loadTargetEditorValues(id, draft)
	if len(values) == 0 {
		return missingDetails("Target", id, width, height)
	}
	name := values["name"]
	if name == "" {
		name = id
	}
	return detailpane.Model{
		Title:   detailpane.NamedTitle("Target", name),
		Summary: detailpane.RowsFromKeys([]string{"id", "adapter"}, values),
		Sections: []detailpane.Section{
			{Title: "Identity", Rows: detailpane.RowsFromKeys([]string{"name", "bridge", "capabilities"}, values)},
			{Title: "Binding", Rows: detailpane.RowsFromKeys([]string{"credential_id", "endpoint_id", "quota_group_id"}, values)},
			{Title: "Probe / backoff", Rows: detailpane.RowsFromKeys([]string{
				"failure_threshold", "initial_backoff_ms", "jitter_percent", "max_backoff_ms",
				"probe_timeout_ms", "recovery_success_threshold", "stable_probe_interval_ms",
			}, values)},
			{Title: "Throttle", Rows: detailpane.RowsFromKeys([]string{"default_cooling_ms", "max_cooling_ms"}, values)},
		},
		Width:  width,
		Height: height,
	}.View()
}

func renderEndpointDetails(draft *configdraft.Draft, id string, width, height int) string {
	values := loadEndpointEditorValues(id, draft)
	if len(values) == 0 {
		return missingDetails("Endpoint", id, width, height)
	}
	name := values["name"]
	if name == "" {
		name = id
	}
	return detailpane.Model{
		Title:   detailpane.NamedTitle("Endpoint", name),
		Summary: detailpane.RowsFromKeys([]string{"id", "base_url"}, values),
		Sections: []detailpane.Section{
			{Title: "Identity", Rows: detailpane.RowsFromKeys([]string{"name", "base_url"}, values)},
			{Title: "Network", Rows: detailpane.RowsFromKeys([]string{
				"http2_enabled", "allow_private_network", "proxy_url", "tls_server_name",
			}, values)},
			{Title: "Connection pool", Rows: detailpane.RowsFromKeys([]string{
				"idle_connection_timeout_ms", "max_idle_connections",
			}, values)},
		},
		Width:  width,
		Height: height,
	}.View()
}

func renderCredentialDetails(draft *configdraft.Draft, id string, width, height int) string {
	lines := credentialDetailLines(draft, id)
	if len(lines) == 0 {
		return missingDetails("Credential", id, width, height)
	}
	values := map[string]string{}
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		values[key] = value
	}
	name := values["name"]
	if name == "" {
		name = id
	}
	return detailpane.Model{
		Title:   detailpane.NamedTitle("Credential", name),
		Summary: detailpane.RowsFromKeys([]string{"id", "provider"}, values),
		Sections: []detailpane.Section{
			{Title: "Identity", Rows: detailpane.RowsFromKeys([]string{"name", "provider", "generation"}, values)},
			{Title: "Secret", Rows: detailpane.RowsFromKeys([]string{
				"secret_action", "secret_action.mode", "secret_binding",
				"secret_action.ref.kind", "secret_action.ref.exportable",
				"secret_action.ref.env.name", "secret_action.ref.file.path",
				"secret_action.ref.keychain.service", "secret_action.ref.keychain.account",
			}, values)},
		},
		Width:  width,
		Height: height,
	}.View()
}

func missingDetails(kind, id string, width, height int) string {
	return detailpane.Model{
		Title: detailpane.NamedTitle(kind, id),
		Sections: []detailpane.Section{{
			Title: "Status",
			Rows:  []detailpane.Row{{Label: "error", Value: i18n.T("detail.value.not_found")}},
		}},
		Width:  width,
		Height: height,
	}.View()
}

func credentialDetailLines(draft *configdraft.Draft, id string) []string {
	c, ok := findCredential(draft.LocalCommand(), id)
	if !ok {
		return nil
	}
	lines := []string{
		"id: " + string(c.Id),
		"name: " + c.Name,
		"provider: " + string(c.Provider),
		"generation: " + credentialGeneration(draft, id),
	}
	if containsString(draft.ReplaceRequiredIDs(), id) {
		lines = append(lines, "secret_action: Replace required")
		return lines
	}
	lines = append(lines, "secret_action.mode: "+secretModeLabel(c))
	if external, err := c.SecretAction.AsCredentialSecretAction2(); err == nil && external.Mode == generated.CredentialSecretActionExternalRef {
		lines = append(lines, "secret_action.ref.kind: "+externalRefSummary(external.Ref))
		lines = append(lines, "secret_action.ref.exportable: "+strconvBool(external.Ref.Exportable))
		switch {
		case external.Ref.Env != nil:
			lines = append(lines, "secret_action.ref.env.name: "+external.Ref.Env.Name)
		case external.Ref.File != nil:
			lines = append(lines, "secret_action.ref.file.path: "+external.Ref.File.Path)
		case external.Ref.Keychain != nil:
			lines = append(lines,
				"secret_action.ref.keychain.service: "+external.Ref.Keychain.Service,
				"secret_action.ref.keychain.account: "+external.Ref.Keychain.Account,
			)
		}
	}
	if _, err := c.SecretAction.AsCredentialSecretAction1(); err == nil {
		lines = append(lines, "secret_action: Configured")
	}
	if _, err := c.SecretAction.AsCredentialSecretAction0(); err == nil {
		lines = append(lines, "secret_binding: managed (Configured)")
	}
	return lines
}

func secretModeLabel(c generated.MutableCredentialCommand) string {
	if preserve, err := c.SecretAction.AsCredentialSecretAction0(); err == nil && preserve.Mode == generated.CredentialSecretActionPreserve {
		return "preserve"
	}
	if replace, err := c.SecretAction.AsCredentialSecretAction1(); err == nil && replace.Mode == generated.CredentialSecretActionReplace {
		return "replace"
	}
	if external, err := c.SecretAction.AsCredentialSecretAction2(); err == nil && external.Mode == generated.CredentialSecretActionExternalRef {
		return "external_ref"
	}
	return "unknown"
}

func renderDependencyDialog(kind Kind, id string, paths []string, width, height int) string {
	rows := make([]detailpane.Row, 0, len(paths))
	for i, path := range paths {
		rows = append(rows, detailpane.Row{Label: fmt.Sprintf("%d", i+1), Value: path})
	}
	if len(rows) == 0 {
		rows = []detailpane.Row{{Label: "refs", Value: "none"}}
	}
	return detailpane.Model{
		Title:    fmt.Sprintf("Cannot delete %s · %s", kind.Label(), id),
		Sections: []detailpane.Section{{Title: "Inbound references", Rows: rows}},
		Width:    width,
		Height:   height,
	}.View()
}

func renderConfirmDelete(kind Kind, id string, remaining int, width, height int, status string) string {
	rows := []detailpane.Row{
		{Label: "deps", Value: "Dependent changes: none"},
		{Label: "summary", Value: fmt.Sprintf("Remaining after delete: %d", remaining)},
	}
	if status != "" {
		rows = append(rows, detailpane.Row{Label: "status", Value: status})
	}
	return detailpane.Model{
		Title:    fmt.Sprintf("Delete %s · %s?", kind.Label(), id),
		Sections: []detailpane.Section{{Title: "Confirm", Rows: rows}},
		Hints:    []string{"enter confirm  esc cancel"},
		Width:    width,
		Height:   height,
	}.View()
}

// clampOverlayLines height-clamps overlay text while keeping the last line
// (esc/cancel hint) discoverable on small viewports.
func clampOverlayLines(out string, height int) string {
	lines := strings.Split(out, "\n")
	if height <= 0 {
		return ""
	}
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	if height == 1 {
		return lines[len(lines)-1]
	}
	kept := append([]string(nil), lines[:height-1]...)
	kept = append(kept, lines[len(lines)-1])
	return strings.Join(kept, "\n")
}
