package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type resourceSpec struct {
	kind       string
	schemaName string
	idField    string
}

var resourceSpecs = []resourceSpec{
	{kind: "instance", schemaName: "MutableInstanceConfig", idField: ""},
	{kind: "client_profile", schemaName: "MutableClientProfile", idField: "id"},
	{kind: "route", schemaName: "RouteConfig", idField: "id"},
	{kind: "backend_set", schemaName: "BackendSetConfig", idField: "id"},
	{kind: "content_policy", schemaName: "ContentPolicyConfig", idField: "id"},
	{kind: "model_policy", schemaName: "ModelPolicyConfig", idField: "id"},
	{kind: "model_projection", schemaName: "ModelProjectionConfig", idField: "id"},
	{kind: "compatibility_transform", schemaName: "CompatibilityTransformConfig", idField: "id"},
	{kind: "endpoint", schemaName: "EndpointConfig", idField: "id"},
	{kind: "credential", schemaName: "MutableCredentialCommand", idField: "id"},
	{kind: "target", schemaName: "MutableTargetCommand", idField: "id"},
	{kind: "quota_group", schemaName: "QuotaGroupConfig", idField: "id"},
}

type openAPISchema struct {
	Type                 string                    `yaml:"type"`
	Format               string                    `yaml:"format"`
	Enum                 []any                     `yaml:"enum"`
	Minimum              *float64                  `yaml:"minimum"`
	Maximum              *float64                  `yaml:"maximum"`
	MinLength            *int                      `yaml:"minLength"`
	MaxLength            *int                      `yaml:"maxLength"`
	Pattern              string                    `yaml:"pattern"`
	Default              any                       `yaml:"default"`
	UniqueItems          bool                      `yaml:"uniqueItems"`
	ReadOnly             bool                      `yaml:"readOnly"`
	AdditionalProperties any                       `yaml:"additionalProperties"`
	Required             []string                  `yaml:"required"`
	Properties           map[string]*openAPISchema `yaml:"properties"`
	Items                *openAPISchema            `yaml:"items"`
	Ref                  string                    `yaml:"$ref"`
	OneOf                []*openAPISchema          `yaml:"oneOf"`
	AllOf                []*openAPISchema          `yaml:"allOf"`
}

type bundleDocument struct {
	Components struct {
		Schemas map[string]*openAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

func main() {
	input := flag.String("input", "", "bundled OpenAPI yaml path")
	output := flag.String("output", "", "output Go file path")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: gendesc -input bundle.yaml -output descriptors.go")
		os.Exit(2)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		exitErr(err)
	}

	var document bundleDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		exitErr(err)
	}

	byKind := make(map[string]resourceDescriptor, len(resourceSpecs))
	for _, spec := range resourceSpecs {
		schema, ok := document.Components.Schemas[spec.schemaName]
		if !ok {
			exitErr(fmt.Errorf("schema %q not found in bundle", spec.schemaName))
		}
		byKind[spec.kind] = resourceDescriptor{
			Kind:       spec.kind,
			SchemaName: spec.schemaName,
			IDField:    spec.idField,
			Fields:     describeSchema(document.Components.Schemas, schema, spec.schemaName, "$"),
		}
	}

	source, err := renderSource(byKind)
	if err != nil {
		exitErr(err)
	}
	if err := os.WriteFile(*output, source, 0o644); err != nil {
		exitErr(err)
	}
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

type fieldDescriptor struct {
	Name         string
	JSONPath     string
	Kind         string
	Required     bool
	ReadOnly     bool
	EnumValues   []string
	Min          *int64
	Max          *int64
	Pattern      string
	DefaultValue string
	ItemKind     string
	UniqueItems  bool
	RefResource  string
	Children     []fieldDescriptor
}

type resourceDescriptor struct {
	Kind       string
	SchemaName string
	IDField    string
	Fields     []fieldDescriptor
}

func describeSchema(all map[string]*openAPISchema, schema *openAPISchema, refName, jsonPath string) []fieldDescriptor {
	resolved := resolveSchema(all, schema, refName)
	if resolved == nil {
		return nil
	}
	if len(resolved.Properties) == 0 {
		return nil
	}
	requiredSet := make(map[string]struct{}, len(resolved.Required))
	for _, name := range resolved.Required {
		requiredSet[name] = struct{}{}
	}
	names := make([]string, 0, len(resolved.Properties))
	for name := range resolved.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]fieldDescriptor, 0, len(names))
	for _, name := range names {
		property := resolved.Properties[name]
		path := jsonPath
		if path == "$" {
			path = "$." + name
		} else {
			path = path + "." + name
		}
		_, required := requiredSet[name]
		fields = append(fields, describeField(all, property, name, path, required))
	}
	return fields
}

func describeField(all map[string]*openAPISchema, schema *openAPISchema, name, jsonPath string, required bool) fieldDescriptor {
	field := fieldDescriptor{Name: name, JSONPath: jsonPath, Required: required}
	if schema == nil {
		field.Kind = "object"
		return field
	}

	if isSecretField(name, schema) {
		field.Kind = "secret"
		field.ReadOnly = schema.ReadOnly
		resolved := resolveSchema(all, schema, refBaseName(schema.Ref))
		if resolved != nil {
			for _, branch := range append(resolved.OneOf, resolved.AllOf...) {
				refName := ""
				if branch != nil && branch.Ref != "" {
					refName = refBaseName(branch.Ref)
				}
				field.Children = append(field.Children, describeSchema(all, branch, refName, jsonPath)...)
			}
		}
		return field
	}

	if schema.Ref != "" {
		refName := refBaseName(schema.Ref)
		field.Kind = "reference"
		field.RefResource = refName
		if nested, ok := all[refName]; ok && nested.Type == "object" && len(nested.Properties) > 0 {
			field.Children = describeSchema(all, nested, refName, jsonPath)
		}
		field.ReadOnly = schema.ReadOnly
		return field
	}

	if len(schema.OneOf) > 0 || len(schema.AllOf) > 0 {
		field.Kind = "object"
		for _, branch := range append(schema.OneOf, schema.AllOf...) {
			field.Children = append(field.Children, describeSchema(all, branch, refBaseName(branch.Ref), jsonPath)...)
		}
		field.ReadOnly = schema.ReadOnly
		return field
	}

	switch schema.Type {
	case "string":
		field.Kind = "string"
		if len(schema.Enum) > 0 {
			field.Kind = "enum"
			field.EnumValues = stringifyEnum(schema.Enum)
		}
		field.Pattern = schema.Pattern
		if schema.MinLength != nil {
			min := int64(*schema.MinLength)
			field.Min = &min
		}
		if schema.MaxLength != nil {
			max := int64(*schema.MaxLength)
			field.Max = &max
		}
	case "boolean":
		field.Kind = "boolean"
	case "integer":
		field.Kind = "integer"
		field.Min = floatToInt64(schema.Minimum)
		field.Max = floatToInt64(schema.Maximum)
	case "number":
		field.Kind = "number"
		field.Min = floatToInt64(schema.Minimum)
		field.Max = floatToInt64(schema.Maximum)
	case "array":
		field.Kind = "array"
		field.UniqueItems = schema.UniqueItems
		if schema.Items != nil {
			item := describeItem(all, schema.Items)
			field.ItemKind = item.Kind
			field.Children = item.Children
			if item.RefResource != "" && field.ItemKind == "reference" {
				field.RefResource = item.RefResource
			}
		}
	case "object":
		field.Kind = "object"
		field.Children = describeSchema(all, schema, "", jsonPath)
	default:
		if schema.Ref != "" {
			field.Kind = "reference"
			field.RefResource = refBaseName(schema.Ref)
		} else {
			field.Kind = "object"
		}
	}

	if schema.Default != nil {
		field.DefaultValue = fmt.Sprint(schema.Default)
	}
	field.ReadOnly = schema.ReadOnly
	return field
}

func describeItem(all map[string]*openAPISchema, schema *openAPISchema) fieldDescriptor {
	if schema.Ref != "" {
		refName := refBaseName(schema.Ref)
		item := fieldDescriptor{Kind: "reference", RefResource: refName}
		if nested, ok := all[refName]; ok && nested.Type == "object" {
			item.Children = describeSchema(all, nested, refName, "$")
		}
		return item
	}
	switch schema.Type {
	case "string":
		if len(schema.Enum) > 0 {
			return fieldDescriptor{Kind: "enum", EnumValues: stringifyEnum(schema.Enum)}
		}
		return fieldDescriptor{Kind: "string"}
	case "boolean":
		return fieldDescriptor{Kind: "boolean"}
	case "integer":
		return fieldDescriptor{Kind: "integer"}
	case "number":
		return fieldDescriptor{Kind: "number"}
	case "object":
		return fieldDescriptor{Kind: "object", Children: describeSchema(all, schema, "", "$")}
	default:
		return fieldDescriptor{Kind: "object"}
	}
}

func isSecretField(name string, schema *openAPISchema) bool {
	if name == "secret_action" || name == "secret_binding" {
		return true
	}
	return len(schema.OneOf) > 0 && strings.Contains(name, "secret")
}

func resolveSchema(all map[string]*openAPISchema, schema *openAPISchema, refName string) *openAPISchema {
	if schema == nil {
		if refName != "" {
			return all[refName]
		}
		return nil
	}
	if schema.Ref != "" {
		return resolveSchema(all, all[refBaseName(schema.Ref)], refBaseName(schema.Ref))
	}
	return schema
}

func refBaseName(ref string) string {
	ref = strings.TrimPrefix(ref, "#/components/schemas/")
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func stringifyEnum(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprint(value))
	}
	return out
}

func floatToInt64(value *float64) *int64 {
	if value == nil {
		return nil
	}
	v := int64(*value)
	return &v
}

func renderSource(byKind map[string]resourceDescriptor) ([]byte, error) {
	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var buffer bytes.Buffer
	buffer.WriteString("// Code generated by internal/resourcepage/gendesc; DO NOT EDIT.\n\n")
	buffer.WriteString("package generated\n\n")
	buffer.WriteString("type FieldKind string\n\n")
	buffer.WriteString("const (\n")
	buffer.WriteString("\tFieldKindString FieldKind = \"string\"\n")
	buffer.WriteString("\tFieldKindEnum FieldKind = \"enum\"\n")
	buffer.WriteString("\tFieldKindBoolean FieldKind = \"boolean\"\n")
	buffer.WriteString("\tFieldKindInteger FieldKind = \"integer\"\n")
	buffer.WriteString("\tFieldKindNumber FieldKind = \"number\"\n")
	buffer.WriteString("\tFieldKindArray FieldKind = \"array\"\n")
	buffer.WriteString("\tFieldKindObject FieldKind = \"object\"\n")
	buffer.WriteString("\tFieldKindReference FieldKind = \"reference\"\n")
	buffer.WriteString("\tFieldKindSecret FieldKind = \"secret\"\n")
	buffer.WriteString(")\n\n")
	buffer.WriteString("type FieldDescriptor struct {\n")
	buffer.WriteString("\tName string\n")
	buffer.WriteString("\tJSONPath string\n")
	buffer.WriteString("\tKind FieldKind\n")
	buffer.WriteString("\tRequired bool\n")
	buffer.WriteString("\tReadOnly bool\n")
	buffer.WriteString("\tEnumValues []string\n")
	buffer.WriteString("\tRefResource string\n")
	buffer.WriteString("\tMin *int64\n")
	buffer.WriteString("\tMax *int64\n")
	buffer.WriteString("\tPattern string\n")
	buffer.WriteString("\tDefaultValue string\n")
	buffer.WriteString("\tItemKind FieldKind\n")
	buffer.WriteString("\tUniqueItems bool\n")
	buffer.WriteString("\tChildren []FieldDescriptor\n")
	buffer.WriteString("}\n\n")
	buffer.WriteString("type ResourceKind string\n\n")
	buffer.WriteString("const (\n")
	for _, kind := range kinds {
		buffer.WriteString(fmt.Sprintf("\tResource%s ResourceKind = %q\n", exportedKind(kind), kind))
	}
	buffer.WriteString(")\n\n")
	buffer.WriteString("type ResourceDescriptor struct {\n")
	buffer.WriteString("\tKind ResourceKind\n")
	buffer.WriteString("\tSchemaName string\n")
	buffer.WriteString("\tIDField string\n")
	buffer.WriteString("\tFields []FieldDescriptor\n")
	buffer.WriteString("}\n\n")
	buffer.WriteString("func int64Ptr(value int64) *int64 { return &value }\n\n")
	buffer.WriteString("var Descriptors = map[ResourceKind]ResourceDescriptor{\n")
	for _, kind := range kinds {
		desc := byKind[kind]
		buffer.WriteString(fmt.Sprintf("\tResource%s: {\n", exportedKind(kind)))
		buffer.WriteString(fmt.Sprintf("\t\tKind:       Resource%s,\n", exportedKind(kind)))
		buffer.WriteString(fmt.Sprintf("\t\tSchemaName: %q,\n", desc.SchemaName))
		buffer.WriteString(fmt.Sprintf("\t\tIDField:    %q,\n", desc.IDField))
		buffer.WriteString("\t\tFields: ")
		writeFields(&buffer, desc.Fields, 2)
		buffer.WriteString(",\n\t},\n")
	}
	buffer.WriteString("}\n")
	return format.Source(buffer.Bytes())
}

func writeFields(buffer *bytes.Buffer, fields []fieldDescriptor, indent int) {
	pad := strings.Repeat("\t", indent)
	buffer.WriteString("[]FieldDescriptor{\n")
	for _, field := range fields {
		buffer.WriteString(pad + "{\n")
		writeField(buffer, field, indent+1)
		buffer.WriteString(pad + "},\n")
	}
	buffer.WriteString(strings.Repeat("\t", indent-1) + "}")
}

func writeField(buffer *bytes.Buffer, field fieldDescriptor, indent int) {
	pad := strings.Repeat("\t", indent)
	buffer.WriteString(fmt.Sprintf("%sName: %q,\n", pad, field.Name))
	buffer.WriteString(fmt.Sprintf("%sJSONPath: %q,\n", pad, field.JSONPath))
	buffer.WriteString(fmt.Sprintf("%sKind: FieldKind%s,\n", pad, exportedKind(field.Kind)))
	buffer.WriteString(fmt.Sprintf("%sRequired: %t,\n", pad, field.Required))
	buffer.WriteString(fmt.Sprintf("%sReadOnly: %t,\n", pad, field.ReadOnly))
	if len(field.EnumValues) > 0 {
		buffer.WriteString(pad + "EnumValues: []string{")
		for index, value := range field.EnumValues {
			if index > 0 {
				buffer.WriteString(", ")
			}
			buffer.WriteString(fmt.Sprintf("%q", value))
		}
		buffer.WriteString("},\n")
	}
	if field.RefResource != "" {
		buffer.WriteString(fmt.Sprintf("%sRefResource: %q,\n", pad, field.RefResource))
	}
	if field.Min != nil {
		buffer.WriteString(fmt.Sprintf("%sMin: int64Ptr(%d),\n", pad, *field.Min))
	}
	if field.Max != nil {
		buffer.WriteString(fmt.Sprintf("%sMax: int64Ptr(%d),\n", pad, *field.Max))
	}
	if field.Pattern != "" {
		buffer.WriteString(fmt.Sprintf("%sPattern: %q,\n", pad, field.Pattern))
	}
	if field.DefaultValue != "" {
		buffer.WriteString(fmt.Sprintf("%sDefaultValue: %q,\n", pad, field.DefaultValue))
	}
	if field.ItemKind != "" {
		buffer.WriteString(fmt.Sprintf("%sItemKind: FieldKind%s,\n", pad, exportedKind(field.ItemKind)))
	}
	if field.UniqueItems {
		buffer.WriteString(fmt.Sprintf("%sUniqueItems: true,\n", pad))
	}
	if len(field.Children) > 0 {
		buffer.WriteString(pad + "Children: ")
		writeFields(buffer, field.Children, indent+1)
		buffer.WriteString(",\n")
	}
}

func exportedKind(value string) string {
	parts := strings.Split(value, "_")
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}
