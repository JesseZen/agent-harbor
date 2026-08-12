package targets

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	descgen "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
)

func validateTargetValues(values map[string]string, creating bool, draft *configdraft.Draft) error {
	desc, ok := resourcepage.Lookup(KindTargets.DescriptorKind())
	if !ok {
		return fmt.Errorf("missing descriptor for targets")
	}
	for _, field := range desc.Fields {
		if len(field.Children) > 0 {
			for _, child := range field.Children {
				if err := validateTargetField(child, values[child.Name], creating); err != nil {
					return err
				}
			}
			continue
		}
		if err := validateTargetField(field, values[field.Name], creating); err != nil {
			return err
		}
	}
	if err := validateTargetReferences(values, draft); err != nil {
		return err
	}
	return nil
}

func validateTargetReferences(values map[string]string, draft *configdraft.Draft) error {
	if draft == nil {
		return nil
	}
	cmd := draft.LocalCommand()
	checks := []struct {
		name string
		path string
		ok   func(string) bool
	}{
		{
			name: "credential_id",
			path: "$.credential_id",
			ok: func(id string) bool {
				for _, c := range cmd.Credentials {
					if string(c.Id) == id {
						return true
					}
				}
				return false
			},
		},
		{
			name: "endpoint_id",
			path: "$.endpoint_id",
			ok: func(id string) bool {
				for _, ep := range cmd.Endpoints {
					if string(ep.Id) == id {
						return true
					}
				}
				return false
			},
		},
		{
			name: "quota_group_id",
			path: "$.quota_group_id",
			ok: func(id string) bool {
				for _, q := range cmd.QuotaGroups {
					if string(q.Id) == id {
						return true
					}
				}
				return false
			},
		},
	}
	for _, check := range checks {
		id := strings.TrimSpace(values[check.name])
		if id == "" {
			continue
		}
		if !check.ok(id) {
			return fmt.Errorf("%s: unknown", check.path)
		}
	}
	return nil
}

func validateTargetField(field descgen.FieldDescriptor, raw string, creating bool) error {
	if field.Name == "id" && !creating {
		return nil
	}
	if field.Name == "generation" {
		// Generation is view/status only — never required on MutableTargetCommand.
		return nil
	}
	value := strings.TrimSpace(raw)
	if field.Required && value == "" && field.DefaultValue == "" {
		return fmt.Errorf("%s: required", field.JSONPath)
	}
	if value == "" {
		return nil
	}
	switch field.Kind {
	case descgen.FieldKindInteger:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: invalid integer", field.JSONPath)
		}
		if field.Min != nil && n < *field.Min {
			return fmt.Errorf("%s: below minimum", field.JSONPath)
		}
		if field.Max != nil && n > *field.Max {
			return fmt.Errorf("%s: above maximum", field.JSONPath)
		}
	case descgen.FieldKindBoolean:
		switch strings.ToLower(value) {
		case "true", "false":
		default:
			return fmt.Errorf("%s: invalid boolean", field.JSONPath)
		}
	case descgen.FieldKindEnum:
		if len(field.EnumValues) > 0 {
			ok := false
			for _, allowed := range field.EnumValues {
				if value == allowed {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("%s: invalid", field.JSONPath)
			}
		}
	case descgen.FieldKindArray:
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' ' || r == ';'
		})
		if field.Required && len(parts) == 0 {
			return fmt.Errorf("%s: required", field.JSONPath)
		}
		seen := map[string]struct{}{}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if field.UniqueItems {
				if _, ok := seen[part]; ok {
					return fmt.Errorf("%s: duplicate items", field.JSONPath)
				}
				seen[part] = struct{}{}
			}
			if err := validateArrayItem(field, part); err != nil {
				return err
			}
		}
	case descgen.FieldKindString, descgen.FieldKindReference:
		if field.Min != nil && int64(len(value)) < *field.Min {
			return fmt.Errorf("%s: below minimum length", field.JSONPath)
		}
	}
	return nil
}

func validateArrayItem(field descgen.FieldDescriptor, part string) error {
	switch field.ItemKind {
	case descgen.FieldKindEnum:
		if len(field.EnumValues) > 0 {
			for _, allowed := range field.EnumValues {
				if part == allowed {
					return nil
				}
			}
			return fmt.Errorf("%s: invalid", field.JSONPath)
		}
		if field.Name == "capabilities" {
			if !generated.MutableTargetCommandCapabilities(part).Valid() {
				return fmt.Errorf("%s: invalid", field.JSONPath)
			}
		}
	}
	return nil
}

func validateEndpointValues(values map[string]string, creating bool) error {
	desc, ok := resourcepage.Lookup(KindEndpoints.DescriptorKind())
	if !ok {
		return fmt.Errorf("missing descriptor for endpoints")
	}
	for _, field := range desc.Fields {
		if err := validateEndpointField(field, values[field.Name], creating); err != nil {
			return err
		}
	}
	return nil
}

func validateEndpointField(field descgen.FieldDescriptor, raw string, creating bool) error {
	if field.Name == "id" && !creating {
		return nil
	}
	value := strings.TrimSpace(raw)
	if field.Required && value == "" && field.DefaultValue == "" {
		return fmt.Errorf("%s: required", field.JSONPath)
	}
	if value == "" {
		return nil
	}
	switch field.Kind {
	case descgen.FieldKindInteger:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: invalid integer", field.JSONPath)
		}
		if field.Min != nil && n < *field.Min {
			return fmt.Errorf("%s: below minimum", field.JSONPath)
		}
		if field.Max != nil && n > *field.Max {
			return fmt.Errorf("%s: above maximum", field.JSONPath)
		}
	case descgen.FieldKindBoolean:
		switch strings.ToLower(value) {
		case "true", "false":
		default:
			return fmt.Errorf("%s: invalid boolean", field.JSONPath)
		}
	case descgen.FieldKindString, descgen.FieldKindReference:
		if field.Min != nil && int64(len(value)) < *field.Min {
			return fmt.Errorf("%s: below minimum length", field.JSONPath)
		}
	}
	return nil
}

func validateCredentialValues(values map[string]string, creating bool, mode SecretActionMode) error {
	id := strings.TrimSpace(values["id"])
	if id == "" {
		return fmt.Errorf("$.id: required")
	}
	name := strings.TrimSpace(values["name"])
	if name == "" {
		return fmt.Errorf("$.name: required")
	}
	provider := strings.TrimSpace(values["provider"])
	if provider == "" {
		return fmt.Errorf("$.provider: required")
	}
	if !generated.MutableCredentialCommandProvider(provider).Valid() {
		return fmt.Errorf("$.provider: invalid")
	}
	if creating && mode == SecretActionPreserve {
		return fmt.Errorf("$.secret_action: preserve unavailable on create")
	}
	switch mode {
	case SecretActionPreserve, SecretActionReplace:
		return nil
	case SecretActionExternal:
		return validateExternalValues(values)
	default:
		return fmt.Errorf("$.secret_action.mode: required")
	}
}

func validateExternalValues(values map[string]string) error {
	kind := strings.TrimSpace(values["external_kind"])
	switch kind {
	case "env":
		name := strings.TrimSpace(values["external_env_name"])
		if name == "" {
			return fmt.Errorf("$.secret_action.ref.env.name: required")
		}
	case "file":
		path := strings.TrimSpace(values["external_file_path"])
		if path == "" {
			return fmt.Errorf("$.secret_action.ref.file.path: required")
		}
	case "keychain":
		if strings.TrimSpace(values["external_keychain_service"]) == "" {
			return fmt.Errorf("$.secret_action.ref.keychain.service: required")
		}
		if strings.TrimSpace(values["external_keychain_account"]) == "" {
			return fmt.Errorf("$.secret_action.ref.keychain.account: required")
		}
	default:
		return fmt.Errorf("$.secret_action.ref: required locator")
	}
	return nil
}
