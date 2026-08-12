package routes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
)

func validateValues(kind Kind, values map[string]string, creating bool, draft *configdraft.Draft) error {
	desc, ok := resourcepage.Lookup(kind.DescriptorKind())
	if !ok {
		return fmt.Errorf("missing descriptor for %s", kind)
	}
	for _, field := range desc.Fields {
		switch {
		case field.Kind == generated.FieldKindArray && len(field.Children) > 0:
			if err := validateObjectArray(field, values, draft); err != nil {
				return err
			}
		case field.Name == "operation":
			if err := validateOperationValues(values); err != nil {
				return err
			}
		case field.Kind == generated.FieldKindArray && field.ItemKind == generated.FieldKindString:
			items := parseCSV(values[field.Name])
			if field.Required && len(items) == 0 {
				return fmt.Errorf("%s: required", field.JSONPath)
			}
			if err := validateUniqueItems(field, items); err != nil {
				return err
			}
		case field.Kind == generated.FieldKindArray && field.ItemKind == generated.FieldKindEnum:
			items := parseCSV(values[field.Name])
			if err := validateUniqueItems(field, items); err != nil {
				return err
			}
			enumVals := field.EnumValues
			if len(enumVals) == 0 && field.Name == "required_capabilities" {
				enumVals = capabilityEnumValues()
			}
			for _, item := range items {
				if len(enumVals) == 0 {
					break
				}
				if err := validateField(generated.FieldDescriptor{
					Name: field.Name, JSONPath: field.JSONPath, Kind: generated.FieldKindEnum,
					EnumValues: enumVals,
				}, item, creating); err != nil {
					return err
				}
			}
		default:
			if err := validateField(field, values[field.Name], creating); err != nil {
				return err
			}
			if err := validateReferenceField(field, field.Name, values[field.Name], values, draft); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateUniqueItems(field generated.FieldDescriptor, items []string) error {
	if !field.UniqueItems {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			return fmt.Errorf("%s: duplicate item %q", field.JSONPath, item)
		}
		seen[item] = struct{}{}
	}
	return nil
}

func validateObjectArray(field generated.FieldDescriptor, values map[string]string, draft *configdraft.Draft) error {
	n := arrayLenFromValues(values, field.Name)
	if field.Required && n == 0 {
		return fmt.Errorf("%s: required", field.JSONPath)
	}
	for i := 0; i < n; i++ {
		for _, child := range field.Children {
			key := fmt.Sprintf("%s[%d].%s", field.Name, i, child.Name)
			path := fmt.Sprintf("%s[%d].%s", field.JSONPath, i, child.Name)
			childCopy := child
			childCopy.JSONPath = path
			if err := validateField(childCopy, values[key], true); err != nil {
				return err
			}
			if err := validateReferenceField(childCopy, key, values[key], values, draft); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateReferenceField(field generated.FieldDescriptor, key, raw string, values map[string]string, draft *configdraft.Draft) error {
	if draft == nil {
		return nil
	}
	if field.Kind != generated.FieldKindReference || field.Name == "id" {
		return nil
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil // required emptiness handled by validateField
	}
	opts, known := knownReferenceOptions(draft, key, values)
	if !known {
		// Unmapped field name — skip membership.
		return nil
	}
	for _, opt := range opts {
		if opt == value {
			return nil
		}
	}
	return fmt.Errorf("%s: unknown reference %q", field.JSONPath, value)
}

func validateOperationValues(values map[string]string) error {
	kind := strings.TrimSpace(values["operation"])
	if kind == "" {
		return fmt.Errorf("$.operation: required")
	}
	switch kind {
	case "rename_model", "drop_header", "set_header", "normalize_stream_event":
		_, err := parseOperation(values)
		return err
	default:
		return fmt.Errorf("$.operation: invalid kind %q", kind)
	}
}

func validateField(field generated.FieldDescriptor, raw string, creating bool) error {
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
	case generated.FieldKindInteger:
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
	case generated.FieldKindEnum:
		if len(field.EnumValues) > 0 {
			ok := false
			for _, candidate := range field.EnumValues {
				if candidate == value {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("%s: invalid enum value", field.JSONPath)
			}
		}
	case generated.FieldKindString, generated.FieldKindReference:
		if field.Min != nil && int64(len(value)) < *field.Min {
			return fmt.Errorf("%s: below minimum length", field.JSONPath)
		}
	case generated.FieldKindArray:
		if field.Required && value == "" {
			return fmt.Errorf("%s: required", field.JSONPath)
		}
	}
	return nil
}

func valueOrDefault(values map[string]string, name, fallback string) string {
	if v, ok := values[name]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func parseRequiredInt(values map[string]string, name, path string) (int, error) {
	raw := strings.TrimSpace(values[name])
	if raw == "" {
		return 0, fmt.Errorf("%s: required", path)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer", path)
	}
	return n, nil
}

func parseRequiredInt64(values map[string]string, name, path string) (int64, error) {
	raw := strings.TrimSpace(values[name])
	if raw == "" {
		return 0, fmt.Errorf("%s: required", path)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer", path)
	}
	return n, nil
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
