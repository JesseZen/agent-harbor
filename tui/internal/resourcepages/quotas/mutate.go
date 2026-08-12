package quotas

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	rpgenerated "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
)

func validateValues(values map[string]string, creating bool) error {
	desc, ok := resourcepage.Lookup(rpgenerated.ResourceQuotaGroup)
	if !ok {
		return fmt.Errorf("missing descriptor for quota_group")
	}
	for _, field := range desc.Fields {
		if err := validateField(field, values[field.Name], creating); err != nil {
			return err
		}
	}
	return nil
}

func validateField(field resourcepage.FieldDescriptor, raw string, creating bool) error {
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
	case "integer":
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
	case "string", "reference":
		if field.Min != nil && int64(len(value)) < *field.Min {
			return fmt.Errorf("%s: below minimum length", field.JSONPath)
		}
	}
	return nil
}

func applyCreate(draft *configdraft.Draft, values map[string]string) error {
	id := strings.TrimSpace(values["id"])
	if id == "" {
		return fmt.Errorf("$.id: required")
	}
	if quotaExists(draft, id) {
		return fmt.Errorf("$.id: already exists")
	}
	group, err := buildQuota(values, generated.QuotaGroupConfig{}, true)
	if err != nil {
		return err
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.QuotaGroups = append(cmd.QuotaGroups, group)
	})
	return nil
}

func applyEdit(draft *configdraft.Draft, id string, values map[string]string) error {
	base, ok := findQuota(draft.LocalCommand(), id)
	if !ok {
		return fmt.Errorf("$.id: not found")
	}
	group, err := buildQuota(values, base, false)
	if err != nil {
		return err
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.QuotaGroups {
			if string(cmd.QuotaGroups[i].Id) == id {
				cmd.QuotaGroups[i] = group
				return
			}
		}
	})
	return nil
}

func applyDelete(draft *configdraft.Draft, id string) {
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		out := make([]generated.QuotaGroupConfig, 0, len(cmd.QuotaGroups)-1)
		for _, q := range cmd.QuotaGroups {
			if string(q.Id) != id {
				out = append(out, q)
			}
		}
		cmd.QuotaGroups = out
	})
}

func buildQuota(values map[string]string, base generated.QuotaGroupConfig, creating bool) (generated.QuotaGroupConfig, error) {
	desc, ok := resourcepage.Lookup(rpgenerated.ResourceQuotaGroup)
	if !ok {
		return generated.QuotaGroupConfig{}, fmt.Errorf("missing descriptor for quota_group")
	}
	defaults := descriptorDefaults(desc)

	out := base
	if creating {
		out.Id = generated.ConfigID(strings.TrimSpace(values["id"]))
	}
	out.Name = valueOrDefault(values, "name", out.Name)
	out.Rpm = intField(values, "rpm", out.Rpm, defaults["rpm"], creating)
	out.MaxConcurrency = intField(values, "max_concurrency", out.MaxConcurrency, defaults["max_concurrency"], creating)
	out.ForegroundCapacity = intField(values, "foreground_capacity", out.ForegroundCapacity, defaults["foreground_capacity"], creating)
	out.BackgroundCapacity = intField(values, "background_capacity", out.BackgroundCapacity, defaults["background_capacity"], creating)
	out.ForegroundWeight = intField(values, "foreground_weight", out.ForegroundWeight, defaults["foreground_weight"], creating)
	out.BackgroundWeight = intField(values, "background_weight", out.BackgroundWeight, defaults["background_weight"], creating)
	out.QueueTimeoutMs = int64Field(values, "queue_timeout_ms", out.QueueTimeoutMs, defaults["queue_timeout_ms"], creating)
	return out, nil
}

func descriptorDefaults(desc resourcepage.ResourceDescriptor) map[string]string {
	out := make(map[string]string, len(desc.Fields))
	for _, field := range desc.Fields {
		if field.DefaultValue != "" {
			out[field.Name] = field.DefaultValue
		}
	}
	return out
}

func valueOrDefault(values map[string]string, name, fallback string) string {
	if v, ok := values[name]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func intField(values map[string]string, name string, base int, defaultVal string, creating bool) int {
	raw := strings.TrimSpace(values[name])
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return base
		}
		return n
	}
	if !creating {
		return base
	}
	if defaultVal != "" {
		n, _ := strconv.Atoi(defaultVal)
		return n
	}
	return 0
}

func int64Field(values map[string]string, name string, base int64, defaultVal string, creating bool) int64 {
	raw := strings.TrimSpace(values[name])
	if raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return base
		}
		return n
	}
	if !creating {
		return base
	}
	if defaultVal != "" {
		n, _ := strconv.ParseInt(defaultVal, 10, 64)
		return n
	}
	return 0
}
