package routes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	descgen "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
	"github.com/sahilm/fuzzy"
)

type editorMode int

const (
	editorNone editorMode = iota
	editorCreate
	editorEdit
)

type editorState struct {
	mode         editorMode
	kind         Kind
	id           string
	values       map[string]string
	cursor       int
	refIndex     int
	refFilter    string
	err          string
	scroll       int
	selectorOpen bool
}

func newEditor(kind Kind, mode editorMode, id string, draft *configdraft.Draft) editorState {
	values := map[string]string{}
	desc, _ := resourcepage.Lookup(kind.DescriptorKind())
	for _, field := range desc.Fields {
		if field.DefaultValue != "" {
			values[field.Name] = field.DefaultValue
		}
	}
	if mode == editorEdit {
		values = loadEditorValues(kind, id, draft)
	} else {
		seedCreateArrayDefaults(kind, values)
	}
	return editorState{
		mode:   mode,
		kind:   kind,
		id:     id,
		values: values,
	}
}

func seedCreateArrayDefaults(kind Kind, values map[string]string) {
	// Only empty required object-array slots — no invented DefaultValue-less seeds.
	switch kind {
	case KindBackendSets:
		values["candidates[0].target_id"] = ""
		values["candidates[0].priority"] = ""
		values["candidates[0].weight"] = ""
	case KindModelPolicies:
		values["mappings[0].logical_model"] = ""
		values["mappings[0].physical_model"] = ""
		values["mappings[0].target_id"] = ""
	case KindModelProjections:
		values["logical_models"] = ""
	}
}

func loadEditorValues(kind Kind, id string, draft *configdraft.Draft) map[string]string {
	cmd := draft.LocalCommand()
	values := map[string]string{}
	switch kind {
	case KindRoutes:
		if r, ok := findRoute(cmd, id); ok {
			values["id"] = string(r.Id)
			values["name"] = r.Name
			values["ingress_protocol"] = string(r.IngressProtocol)
			values["routing_policy"] = string(r.RoutingPolicy)
			values["backend_set_id"] = string(r.BackendSetId)
			values["model_policy_id"] = string(r.ModelPolicyId)
			if r.ContentPolicyId != nil {
				values["content_policy_id"] = string(*r.ContentPolicyId)
			}
			values["max_attempts"] = fmt.Sprintf("%d", r.MaxAttempts)
			values["max_request_body_bytes"] = fmt.Sprintf("%d", r.MaxRequestBodyBytes)
			values["request_deadline_ms"] = fmt.Sprintf("%d", r.RequestDeadlineMs)
			values["retry_deadline_ms"] = fmt.Sprintf("%d", r.RetryDeadlineMs)
			values["stream_idle_timeout_ms"] = fmt.Sprintf("%d", r.StreamIdleTimeoutMs)
		}
	case KindBackendSets:
		if b, ok := findBackendSet(cmd, id); ok {
			values["id"] = string(b.Id)
			values["name"] = b.Name
			if b.RequiredCapabilities != nil {
				parts := make([]string, 0, len(*b.RequiredCapabilities))
				for _, c := range *b.RequiredCapabilities {
					parts = append(parts, string(c))
				}
				values["required_capabilities"] = strings.Join(parts, ",")
			}
			for i, c := range b.Candidates {
				prefix := fmt.Sprintf("candidates[%d]", i)
				values[prefix+".target_id"] = string(c.TargetId)
				values[prefix+".priority"] = strconv.Itoa(c.Priority)
				values[prefix+".weight"] = strconv.Itoa(c.Weight)
			}
			if len(b.Candidates) == 0 {
				values["candidates[0].target_id"] = ""
				values["candidates[0].priority"] = ""
				values["candidates[0].weight"] = ""
			}
		}
	case KindContentPolicies:
		if c, ok := findContentPolicy(cmd, id); ok {
			values["id"] = string(c.Id)
			if c.Mode != nil {
				values["mode"] = string(*c.Mode)
			}
			if c.MaxInspectionBytes != nil {
				values["max_inspection_bytes"] = fmt.Sprintf("%d", *c.MaxInspectionBytes)
			}
		}
	case KindModelPolicies:
		if m, ok := findModelPolicy(cmd, id); ok {
			values["id"] = string(m.Id)
			values["name"] = m.Name
			values["catalog_ttl_ms"] = fmt.Sprintf("%d", m.CatalogTtlMs)
			values["discovery_timeout_ms"] = fmt.Sprintf("%d", m.DiscoveryTimeoutMs)
			for i, mapping := range m.Mappings {
				prefix := fmt.Sprintf("mappings[%d]", i)
				values[prefix+".logical_model"] = mapping.LogicalModel
				values[prefix+".physical_model"] = mapping.PhysicalModel
				values[prefix+".target_id"] = string(mapping.TargetId)
			}
			if len(m.Mappings) == 0 {
				values["mappings[0].logical_model"] = ""
				values["mappings[0].physical_model"] = ""
				values["mappings[0].target_id"] = ""
			}
		}
	case KindModelProjections:
		if m, ok := findModelProjection(cmd, id); ok {
			values["id"] = string(m.Id)
			values["name"] = m.Name
			values["logical_models"] = strings.Join(m.LogicalModels, ",")
		}
	case KindTransforms:
		if x, ok := findTransform(cmd, id); ok {
			values["id"] = string(x.Id)
			values["name"] = x.Name
			values["scope"] = string(x.Scope)
			values["scope_id"] = string(x.ScopeId)
			loadOperationValues(values, x.Operation)
		}
	}
	return values
}

func loadOperationValues(values map[string]string, op generated.CompatibilityTransformOperation) {
	switch {
	case op.RenameModel != nil:
		values["operation"] = "rename_model"
		values["operation.source_model"] = op.RenameModel.SourceModel
		values["operation.destination_model"] = op.RenameModel.DestinationModel
	case op.DropHeader != nil:
		values["operation"] = "drop_header"
		values["operation.header_name"] = op.DropHeader.HeaderName
	case op.SetHeader != nil:
		values["operation"] = "set_header"
		values["operation.header_name"] = op.SetHeader.HeaderName
		values["operation.header_value"] = op.SetHeader.HeaderValue
	case op.NormalizeStreamEvent != nil:
		values["operation"] = "normalize_stream_event"
		values["operation.source_event"] = op.NormalizeStreamEvent.SourceEvent
		values["operation.destination_event"] = op.NormalizeStreamEvent.DestinationEvent
	default:
		// Do not invent an operation variant on edit-load.
	}
}

func operationChildFields(kind string) []string {
	switch kind {
	case "rename_model":
		return []string{"operation.source_model", "operation.destination_model"}
	case "drop_header":
		return []string{"operation.header_name"}
	case "set_header":
		return []string{"operation.header_name", "operation.header_value"}
	case "normalize_stream_event":
		return []string{"operation.source_event", "operation.destination_event"}
	default:
		return nil
	}
}

func (e *editorState) fieldNames() []string {
	if e.kind == KindTrafficRules {
		if e.mode == editorCreate {
			names := []string{"name", "launcher", "routing", "primary_target_id"}
			if e.values["routing"] != routingSingle {
				names = append(names, "backup_target_id")
			}
			names = append(names, "model_strategy")
			switch e.values["model_strategy"] {
			case modelOverride:
				names = append(names, "primary_upstream_model")
				if e.values["routing"] != routingSingle {
					names = append(names, "backup_upstream_model")
				}
			case modelMap:
				n := arrayLenFromValues(e.values, "model_mappings")
				if n < 1 {
					n = 1
				}
				for i := 0; i < n; i++ {
					prefix := fmt.Sprintf("model_mappings[%d]", i)
					names = append(names, prefix+".client_model", prefix+".primary_model")
					if e.values["routing"] != routingSingle {
						names = append(names, prefix+".backup_model")
					}
				}
			}
			return names
		}
		return []string{"client", "routing", "primary_target_id", "backup_target_id"}
	}
	desc, ok := resourcepage.Lookup(e.kind.DescriptorKind())
	if !ok {
		return nil
	}
	names := make([]string, 0, len(desc.Fields)+8)
	for _, field := range desc.Fields {
		switch {
		case field.Kind == descgen.FieldKindArray && len(field.Children) > 0:
			n := arrayLenFromValues(e.values, field.Name)
			if n < 1 {
				n = 1
			}
			for i := 0; i < n; i++ {
				for _, child := range field.Children {
					names = append(names, fmt.Sprintf("%s[%d].%s", field.Name, i, child.Name))
				}
			}
		case field.Name == "operation":
			names = append(names, "operation")
			names = append(names, operationChildFields(e.values["operation"])...)
		default:
			names = append(names, field.Name)
		}
	}
	return routeAdvancedLast(names)
}

func routeAdvancedLast(names []string) []string {
	basic := make([]string, 0, len(names))
	advanced := make([]string, 0, len(names))
	for _, name := range names {
		if routeAdvancedField(name) {
			advanced = append(advanced, name)
		} else {
			basic = append(basic, name)
		}
	}
	return append(basic, advanced...)
}

func routeAdvancedField(name string) bool {
	return name == "id" || strings.Contains(name, "timeout") || strings.Contains(name, "deadline") ||
		strings.Contains(name, "attempt") || strings.Contains(name, "request_body") ||
		strings.Contains(name, "idle") || strings.Contains(name, "generation")
}

func arrayLenFromValues(values map[string]string, arrayName string) int {
	prefix := arrayName + "["
	maxIdx := -1
	for key := range values {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		bracket := strings.IndexByte(rest, ']')
		if bracket <= 0 {
			continue
		}
		idx, err := strconv.Atoi(rest[:bracket])
		if err != nil {
			continue
		}
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	return maxIdx + 1
}

func (e *editorState) idEditable() bool {
	return e.mode == editorCreate
}

func (e *editorState) setValues(values map[string]string) {
	if e.values == nil {
		e.values = map[string]string{}
	}
	for k, v := range values {
		e.values[k] = v
	}
}

func (e *editorState) focusedName() string {
	names := e.fieldNames()
	if e.cursor < 0 || e.cursor >= len(names) {
		return ""
	}
	return names[e.cursor]
}

func (e *editorState) focusedReadonly() bool {
	if e.kind == KindTrafficRules && e.mode != editorCreate && e.focusedName() == "client" {
		return true
	}
	return e.focusedName() == "id" && !e.idEditable()
}

func (e *editorState) focusedIsSelector(draft *configdraft.Draft) bool {
	name := e.focusedName()
	if name == "" || name == "id" {
		return false
	}
	if name == "required_capabilities" || name == "logical_models" {
		return true
	}
	return len(selectorOptions(draft, e.kind, name, e.values)) > 0 || isReferenceFieldName(name)
}

func isReferenceFieldName(name string) bool {
	if name == "operation" {
		return true
	}
	if strings.HasSuffix(name, "_id") {
		return true
	}
	return strings.Contains(name, ".target_id") || isUpstreamModelField(name)
}

func isUpstreamModelField(name string) bool {
	return name == "primary_upstream_model" || name == "backup_upstream_model" ||
		strings.HasSuffix(name, ".primary_model") || strings.HasSuffix(name, ".backup_model")
}

func isUniqueArrayField(name string) bool {
	return name == "required_capabilities" || name == "logical_models"
}

func isFixedSelectField(kind Kind, name string) bool {
	if kind == KindTrafficRules {
		switch name {
		case "launcher", "routing", "model_strategy":
			return true
		}
	}
	return len(enumValuesForField(kind, name)) > 0
}

func (e *editorState) filteredRefs(draft *configdraft.Draft) []string {
	name := e.focusedName()
	if name == "" || name == "id" {
		return nil
	}
	var opts []string
	switch name {
	case "logical_models":
		opts = parseCSV(e.values["logical_models"])
	case "required_capabilities":
		opts = capabilityEnumValues()
	default:
		opts = selectorOptions(draft, e.kind, name, e.values)
	}
	if len(opts) == 0 && isUpstreamModelField(name) && strings.TrimSpace(e.refFilter) != "" {
		return []string{strings.TrimSpace(e.refFilter)}
	}
	if len(opts) == 0 {
		return nil
	}
	query := strings.TrimSpace(e.refFilter)
	if query == "" || name == "logical_models" {
		// logical_models shows current tokens unfiltered; filter is for new tokens.
		if name == "logical_models" {
			return opts
		}
	}
	if query == "" {
		return opts
	}
	lowerOptions := make([]string, len(opts))
	for index, option := range opts {
		lowerOptions[index] = strings.ToLower(routeSelectorLabel(draft, name, e.values, option))
	}
	matches := fuzzy.Find(strings.ToLower(query), lowerOptions)
	shown := make([]string, 0, len(matches))
	for _, match := range matches {
		shown = append(shown, opts[match.Index])
	}
	if isUpstreamModelField(name) {
		custom := strings.TrimSpace(e.refFilter)
		exact := false
		for _, opt := range opts {
			if opt == custom {
				exact = true
				break
			}
		}
		if custom != "" && !exact {
			shown = append(shown, custom)
		}
	}
	return shown
}

func (e *editorState) appendRunes(runes []rune) {
	if e.focusedReadonly() {
		return
	}
	name := e.focusedName()
	if name == "" {
		return
	}
	if isUniqueArrayField(name) || isReferenceFieldName(name) || isFixedSelectField(e.kind, name) {
		e.selectorOpen = true
		e.refFilter += string(runes)
		e.refIndex = 0
		e.err = ""
		return
	}
	if fieldIsInteger(e.kind, name) {
		for _, r := range runes {
			if r < '0' || r > '9' {
				return
			}
		}
	}
	e.values[name] = e.values[name] + string(runes)
	e.refIndex = 0
}

func (e *editorState) backspace() {
	if e.focusedReadonly() {
		return
	}
	name := e.focusedName()
	if name == "" {
		return
	}
	if isUniqueArrayField(name) || isReferenceFieldName(name) || isFixedSelectField(e.kind, name) {
		e.selectorOpen = true
		runes := []rune(e.refFilter)
		if len(runes) == 0 {
			return
		}
		e.refFilter = string(runes[:len(runes)-1])
		e.refIndex = 0
		return
	}
	runes := []rune(e.values[name])
	if len(runes) == 0 {
		return
	}
	e.values[name] = string(runes[:len(runes)-1])
	e.refIndex = 0
}

func (e *editorState) deleteForward() {
	// Cursor-less editor: treat delete like backspace at end.
	e.backspace()
}

func (e *editorState) moveField(delta int) {
	names := e.fieldNames()
	if len(names) == 0 {
		return
	}
	e.cursor += delta
	if e.cursor < 0 {
		e.cursor = 0
	}
	if e.cursor >= len(names) {
		e.cursor = len(names) - 1
	}
	e.refIndex = 0
	e.refFilter = ""
	e.selectorOpen = false
}

func (e *editorState) openSelector(draft *configdraft.Draft) bool {
	if !e.focusedIsSelector(draft) {
		return false
	}
	e.selectorOpen = true
	e.refFilter = ""
	e.refIndex = 0
	current := e.values[e.focusedName()]
	for index, option := range e.filteredRefs(draft) {
		if option == current {
			e.refIndex = index
			break
		}
	}
	return true
}

func (e *editorState) cycleFocusedSelect(draft *configdraft.Draft, delta int) bool {
	if e.selectorOpen {
		return false
	}
	name := e.focusedName()
	if name == "" || e.formField(name, draft).Kind != formui.Select {
		return false
	}
	options := selectorOptions(draft, e.kind, name, e.values)
	next, ok := formui.CycleValue(e.values[name], options, delta)
	if !ok {
		return false
	}
	e.refFilter = ""
	for index, option := range options {
		if option == next {
			e.refIndex = index
			return e.applyRef(draft)
		}
	}
	return false
}

func (e *editorState) moveRef(delta int, draft *configdraft.Draft) bool {
	opts := e.filteredRefs(draft)
	if len(opts) == 0 {
		return false
	}
	e.refIndex += delta
	if e.refIndex < 0 {
		e.refIndex = 0
	}
	if e.refIndex >= len(opts) {
		e.refIndex = len(opts) - 1
	}
	return true
}

func (e *editorState) applyRef(draft *configdraft.Draft) bool {
	name := e.focusedName()
	if name == "" || e.focusedReadonly() {
		return false
	}
	if name == "required_capabilities" {
		return e.toggleMultiSelect(draft)
	}
	if name == "logical_models" {
		return e.addUniqueToken()
	}
	opts := e.filteredRefs(draft)
	if len(opts) == 0 {
		return false
	}
	if e.refIndex < 0 || e.refIndex >= len(opts) {
		e.refIndex = 0
	}
	applied := opts[e.refIndex]
	e.values[name] = applied
	e.refFilter = ""
	e.err = ""
	// Highlight the applied value in the full option list.
	all := selectorOptions(draft, e.kind, name, e.values)
	e.refIndex = 0
	for i, opt := range all {
		if opt == applied {
			e.refIndex = i
			break
		}
	}
	if name == "operation" {
		// Clear stale child keys for other variants.
		for _, key := range []string{
			"operation.source_model", "operation.destination_model",
			"operation.header_name", "operation.header_value",
			"operation.source_event", "operation.destination_event",
		} {
			delete(e.values, key)
		}
		for _, child := range operationChildFields(e.values["operation"]) {
			if _, ok := e.values[child]; !ok {
				e.values[child] = ""
			}
		}
	}
	if name == "model_strategy" && applied == modelMap && arrayLenFromValues(e.values, "model_mappings") == 0 {
		e.values["model_mappings[0].client_model"] = ""
		e.values["model_mappings[0].primary_model"] = ""
		e.values["model_mappings[0].backup_model"] = ""
	}
	return true
}

func (e *editorState) toggleMultiSelect(draft *configdraft.Draft) bool {
	name := e.focusedName()
	if name != "required_capabilities" {
		return false
	}
	opts := e.filteredRefs(draft)
	if len(opts) == 0 {
		return false
	}
	if e.refIndex < 0 || e.refIndex >= len(opts) {
		e.refIndex = 0
	}
	token := opts[e.refIndex]
	items := parseCSV(e.values[name])
	next := make([]string, 0, len(items)+1)
	found := false
	for _, item := range items {
		if item == token {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		next = append(next, token)
	}
	e.values[name] = strings.Join(next, ",")
	e.err = ""
	return true
}

func (e *editorState) addUniqueToken() bool {
	name := e.focusedName()
	if name != "logical_models" {
		return false
	}
	token := strings.TrimSpace(e.refFilter)
	if token == "" {
		return false
	}
	items := parseCSV(e.values[name])
	for _, item := range items {
		if item == token {
			e.refFilter = ""
			return true
		}
	}
	items = append(items, token)
	e.values[name] = strings.Join(items, ",")
	e.refFilter = ""
	e.refIndex = len(items) - 1
	e.err = ""
	return true
}

func (e *editorState) removeUniqueToken() bool {
	name := e.focusedName()
	if name != "logical_models" {
		return false
	}
	items := parseCSV(e.values[name])
	if len(items) == 0 {
		return false
	}
	if e.refIndex < 0 || e.refIndex >= len(items) {
		e.refIndex = 0
	}
	items = append(items[:e.refIndex], items[e.refIndex+1:]...)
	e.values[name] = strings.Join(items, ",")
	if e.refIndex >= len(items) && e.refIndex > 0 {
		e.refIndex = len(items) - 1
	}
	return true
}

func (e *editorState) addArrayItem() bool {
	name := e.focusedName()
	if name == "logical_models" {
		return e.addUniqueToken()
	}
	arrayName, _, _, ok := parseArrayField(name)
	if !ok {
		return false
	}
	n := arrayLenFromValues(e.values, arrayName)
	prefix := fmt.Sprintf("%s[%d]", arrayName, n)
	switch arrayName {
	case "candidates":
		e.values[prefix+".target_id"] = ""
		e.values[prefix+".priority"] = ""
		e.values[prefix+".weight"] = ""
	case "mappings":
		e.values[prefix+".logical_model"] = ""
		e.values[prefix+".physical_model"] = ""
		e.values[prefix+".target_id"] = ""
	case "model_mappings":
		e.values[prefix+".client_model"] = ""
		e.values[prefix+".primary_model"] = ""
		e.values[prefix+".backup_model"] = ""
	default:
		return false
	}
	return true
}

func (e *editorState) removeArrayItem() bool {
	name := e.focusedName()
	if name == "logical_models" {
		return e.removeUniqueToken()
	}
	arrayName, idx, child, ok := parseArrayField(name)
	if !ok {
		return false
	}
	n := arrayLenFromValues(e.values, arrayName)
	if n <= 1 {
		return false
	}
	clearArrayIndex(e.values, arrayName, idx)
	// Renumber higher indices down.
	for i := idx + 1; i < n; i++ {
		moveArrayIndex(e.values, arrayName, i, i-1)
	}
	clearArrayIndex(e.values, arrayName, n-1)
	// Clamp cursor onto same child of nearest remaining index.
	newIdx := idx
	if newIdx >= n-1 {
		newIdx = n - 2
	}
	if newIdx < 0 {
		newIdx = 0
	}
	want := fmt.Sprintf("%s[%d].%s", arrayName, newIdx, child)
	names := e.fieldNames()
	for i, candidate := range names {
		if candidate == want {
			e.cursor = i
			return true
		}
	}
	if e.cursor >= len(names) {
		e.cursor = len(names) - 1
	}
	if e.cursor < 0 {
		e.cursor = 0
	}
	return true
}

func (e *editorState) reorderArrayItem(delta int) bool {
	name := e.focusedName()
	arrayName, idx, _, ok := parseArrayField(name)
	if !ok {
		return false
	}
	n := arrayLenFromValues(e.values, arrayName)
	j := idx + delta
	if j < 0 || j >= n {
		return false
	}
	swapArrayIndex(e.values, arrayName, idx, j)
	// Keep cursor on same child field at new index.
	_, _, child, _ := parseArrayField(name)
	names := e.fieldNames()
	want := fmt.Sprintf("%s[%d].%s", arrayName, j, child)
	for i, candidate := range names {
		if candidate == want {
			e.cursor = i
			break
		}
	}
	return true
}

func parseArrayField(name string) (arrayName string, index int, child string, ok bool) {
	open := strings.IndexByte(name, '[')
	close := strings.IndexByte(name, ']')
	if open <= 0 || close <= open+1 || close+1 >= len(name) || name[close+1] != '.' {
		return "", 0, "", false
	}
	idx, err := strconv.Atoi(name[open+1 : close])
	if err != nil {
		return "", 0, "", false
	}
	return name[:open], idx, name[close+2:], true
}

func arrayChildKeys(arrayName string) []string {
	switch arrayName {
	case "candidates":
		return []string{"target_id", "priority", "weight"}
	case "mappings":
		return []string{"logical_model", "physical_model", "target_id"}
	default:
		return nil
	}
}

func clearArrayIndex(values map[string]string, arrayName string, index int) {
	for _, child := range arrayChildKeys(arrayName) {
		delete(values, fmt.Sprintf("%s[%d].%s", arrayName, index, child))
	}
}

func moveArrayIndex(values map[string]string, arrayName string, from, to int) {
	for _, child := range arrayChildKeys(arrayName) {
		fromKey := fmt.Sprintf("%s[%d].%s", arrayName, from, child)
		toKey := fmt.Sprintf("%s[%d].%s", arrayName, to, child)
		values[toKey] = values[fromKey]
		delete(values, fromKey)
	}
}

func swapArrayIndex(values map[string]string, arrayName string, i, j int) {
	for _, child := range arrayChildKeys(arrayName) {
		a := fmt.Sprintf("%s[%d].%s", arrayName, i, child)
		b := fmt.Sprintf("%s[%d].%s", arrayName, j, child)
		values[a], values[b] = values[b], values[a]
	}
}

func fieldDisplayName(name string) string {
	switch name {
	case "model_strategy":
		return "model handling"
	case "primary_target_id":
		return "primary upstream"
	case "backup_target_id":
		return "backup upstream"
	case "primary_upstream_model":
		return "upstream model"
	case "backup_upstream_model":
		return "backup model"
	}
	arrayName, index, child, ok := parseArrayField(name)
	if !ok || arrayName != "model_mappings" {
		return name
	}
	prefix := fmt.Sprintf("map %d / ", index+1)
	switch child {
	case "client_model":
		return prefix + "CLI model"
	case "primary_model":
		return prefix + "primary"
	case "backup_model":
		return prefix + "backup"
	default:
		return name
	}
}

func (e *editorState) render(width, height int, draft *configdraft.Draft) string {
	return e.formLayout(width, height, draft).View
}

func (e *editorState) formLayout(width, height int, draft *configdraft.Draft) formui.Layout {
	action := "form.action.create"
	if e.mode == editorEdit {
		action = "form.action.edit"
	}
	resource := i18n.T("form.resource." + string(e.kind))
	title := i18n.T(action, map[string]string{"resource": resource})
	errorField, errorDetail := formui.CleanError(e.err)
	notice := ""
	if e.err != "" && errorField == "" {
		notice = errorDetail
	}
	names := e.fieldNames()
	fields := make([]formui.Field, 0, len(names))
	advancedStart := len(names)
	for i, name := range names {
		field := e.formField(name, draft)
		if field.Advanced && advancedStart == len(names) {
			advancedStart = i
		}
		if errorField == name || strings.HasPrefix(name, errorField+"[") {
			field.Error = errorDetail
		}
		fields = append(fields, field)
	}
	footer := i18n.T("form.footer.route")
	if name := e.focusedName(); name != "" && !e.selectorOpen && e.formField(name, draft).Kind == formui.Select {
		footer += "  " + i18n.T("form.select.cycle")
	}
	if name := e.focusedName(); name != "" {
		if _, _, _, ok := parseArrayField(name); ok {
			footer += "  ctrl+n add  ctrl+x remove  ctrl+↑/↓ reorder"
		}
		if name == "logical_models" {
			footer += "  ctrl+n add-token  ctrl+x remove-token"
		}
		if name == "required_capabilities" {
			footer += "  space toggle"
		}
	}
	layout := formui.Render(formui.Spec{
		Title: title, Context: routeFormContext(e.kind), Notice: notice, Fields: fields,
		Focus: e.cursor, AdvancedExpanded: e.cursor >= advancedStart, Width: width, Height: height,
		Scroll: e.scroll, Footer: footer,
	})
	e.scroll = layout.Scroll
	return layout
}

func (e *editorState) formField(name string, draft *configdraft.Draft) formui.Field {
	label := fieldDisplayName(name)
	field := formui.Field{ID: name, Label: formui.FriendlyLabel(label), Value: e.values[name], Section: routeFieldSection(e.kind, name)}
	if descriptor, ok := lookupFieldDesc(e.kind, name); ok {
		field.Required = descriptor.Required
		switch descriptor.Kind {
		case descgen.FieldKindBoolean:
			field.Kind = formui.Toggle
		case descgen.FieldKindInteger, descgen.FieldKindNumber:
			field.Kind = formui.Integer
		case descgen.FieldKindEnum:
			field.Kind = formui.Select
		case descgen.FieldKindArray:
			field.Kind = formui.MultiSelect
		case descgen.FieldKindReference:
			field.Kind = formui.Reference
		case descgen.FieldKindObject:
			field.Kind = formui.Repeater
		}
	}
	if _, _, _, ok := parseArrayField(name); ok {
		field.Kind = formui.Repeater
	}
	if e.kind == KindTrafficRules {
		switch name {
		case "launcher", "routing", "model_strategy":
			field.Kind = formui.Select
		case "primary_target_id", "backup_target_id":
			field.Display = friendlyTargetOption(draft, e.values[name])
		}
	}
	if e.focusedIsSelector(draft) && name == e.focusedName() {
		options := e.filteredRefs(draft)
		selected := map[string]bool{}
		if isUniqueArrayField(name) {
			for _, value := range parseCSV(e.values[name]) {
				selected[value] = true
			}
		}
		for _, option := range options {
			label := routeSelectorLabel(draft, name, e.values, option)
			field.Options = append(field.Options, formui.Option{Label: label, Value: option, Selected: selected[option]})
		}
		field.OptionIndex = e.refIndex
		field.Expanded = e.selectorOpen
		if e.refFilter != "" {
			field.Help = i18n.T("form.select.search", map[string]string{"query": e.refFilter})
		}
		field.EmptyText = i18n.T("form.select.none")
		if isUpstreamModelField(name) {
			field.EmptyText = i18n.T("form.select.models_none")
		}
		if e.refFilter != "" {
			field.EmptyText = i18n.T("form.select.empty")
		}
	}
	if name == e.focusedName() {
		if constraint := fieldConstraintSuffix(e.kind, name); constraint != "" && field.Help == "" {
			field.Help = constraint
		}
	}
	if isReferenceFieldName(name) {
		field.Kind = formui.Reference
	}
	if isUniqueArrayField(name) {
		field.Kind = formui.MultiSelect
	}
	field.Advanced = routeAdvancedField(name)
	if field.Advanced {
		field.Section = "Advanced"
	}
	if name == "id" && !e.idEditable() {
		field.Kind, field.ReadOnly = formui.ReadOnly, true
	} else if name == "id" {
		field.Kind = formui.Text
	}
	if name == "id" {
		field.Help = "Generated from the name; customize before saving if needed"
	}
	if strings.HasSuffix(name, "_ms") {
		field.Unit = "ms"
	} else if strings.HasSuffix(name, "_bytes") {
		field.Unit = "bytes"
	}
	return field
}

func friendlyTargetOption(draft *configdraft.Draft, id string) string {
	if draft == nil || strings.TrimSpace(id) == "" {
		return ""
	}
	target, ok := findRouteTarget(draft.LocalCommand(), id)
	if !ok {
		return id
	}
	return target.Name
}

func routeSelectorLabel(draft *configdraft.Draft, field string, values map[string]string, id string) string {
	if draft == nil || strings.TrimSpace(id) == "" {
		return id
	}
	base := field
	if _, _, child, ok := parseArrayField(field); ok {
		base = child
	} else if index := strings.LastIndex(field, "."); index >= 0 {
		base = field[index+1:]
	}
	cmd := draft.LocalCommand()
	switch base {
	case "target_id", "primary_target_id", "backup_target_id":
		return friendlyTargetOption(draft, id)
	case "backend_set_id":
		for _, item := range cmd.BackendSets {
			if string(item.Id) == id {
				return item.Name
			}
		}
	case "model_policy_id":
		for _, item := range cmd.ModelPolicies {
			if string(item.Id) == id {
				return item.Name
			}
		}
	case "content_policy_id":
		for _, item := range cmd.ContentPolicies {
			if string(item.Id) == id {
				return string(item.Id)
			}
		}
	case "scope_id":
		switch strings.TrimSpace(values["scope"]) {
		case "route":
			for _, item := range cmd.Routes {
				if string(item.Id) == id {
					return item.Name
				}
			}
		case "client_profile":
			for _, item := range cmd.ClientProfiles {
				if string(item.Id) == id {
					return item.Name
				}
			}
		case "target":
			return friendlyTargetOption(draft, id)
		}
	}
	return id
}

func routeFormContext(kind Kind) string {
	if kind == KindTrafficRules {
		return i18n.T("form.context.traffic_rule")
	}
	return i18n.T("form.context.advanced_route")
}

func routeFieldSection(kind Kind, name string) string {
	if kind != KindTrafficRules {
		switch {
		case strings.Contains(name, "policy") || strings.HasSuffix(name, "_id"):
			return "References"
		case strings.Contains(name, "backend") || strings.Contains(name, "mapping") || strings.Contains(name, "logical_models"):
			return "Entries"
		default:
			return "Basics"
		}
	}
	switch {
	case name == "name" || name == "launcher" || name == "client":
		return "Client"
	case name == "model_strategy" || strings.Contains(name, "model_mappings"):
		return "Models"
	case strings.Contains(name, "target") || strings.Contains(name, "upstream_model") || strings.Contains(name, ".primary_model") || strings.Contains(name, ".backup_model"):
		return "Upstreams"
	case name == "routing":
		return "Routing"
	default:
		return "Basics"
	}
}

func selectorOptions(draft *configdraft.Draft, kind Kind, field string, values map[string]string) []string {
	if kind == KindTrafficRules {
		switch field {
		case "launcher":
			return simpleRuleLauncherValues()
		case "routing":
			return []string{routingSingle, routingFailover, routingBalance}
		case "model_strategy":
			return []string{modelPreserve, modelOverride, modelMap}
		case "primary_target_id", "backup_target_id":
			if strings.TrimSpace(values["profile_id"]) == "" {
				return compatibleCreateTargets(draft, values["launcher"])
			}
			return compatibleTrafficTargets(draft, values["profile_id"])
		}
		if isUpstreamModelField(field) {
			targetID := values["primary_target_id"]
			if field == "backup_upstream_model" || strings.HasSuffix(field, ".backup_model") {
				targetID = values["backup_target_id"]
			}
			return decodeTargetModels(values[targetModelsValueKey(targetID)])
		}
	}
	if opts, known := knownReferenceOptions(draft, field, values); known {
		return opts
	}
	return enumValuesForField(kind, field)
}

func enumValuesForField(kind Kind, field string) []string {
	if field == "required_capabilities" {
		return capabilityEnumValues()
	}
	desc, ok := resourcepage.Lookup(kind.DescriptorKind())
	if !ok {
		return nil
	}
	base := field
	if _, _, child, parsed := parseArrayField(field); parsed {
		base = child
	}
	for _, f := range desc.Fields {
		if f.Name == field && f.Kind == descgen.FieldKindEnum {
			return append([]string(nil), f.EnumValues...)
		}
		if f.Name == base && f.Kind == descgen.FieldKindEnum {
			return append([]string(nil), f.EnumValues...)
		}
		for _, child := range f.Children {
			if child.Name == base && child.Kind == descgen.FieldKindEnum {
				return append([]string(nil), child.EnumValues...)
			}
		}
	}
	return nil
}

func capabilityEnumValues() []string {
	return []string{
		string(generated.BackendSetConfigRequiredCapabilitiesChat),
		string(generated.BackendSetConfigRequiredCapabilitiesMessages),
		string(generated.BackendSetConfigRequiredCapabilitiesResponses),
		string(generated.BackendSetConfigRequiredCapabilitiesStreaming),
		string(generated.BackendSetConfigRequiredCapabilitiesTools),
		string(generated.BackendSetConfigRequiredCapabilitiesVision),
	}
}

func knownReferenceOptions(draft *configdraft.Draft, field string, values map[string]string) ([]string, bool) {
	if draft == nil {
		return nil, isKnownReferenceName(field)
	}
	cmd := draft.LocalCommand()
	base := field
	if _, _, child, ok := parseArrayField(field); ok {
		base = child
	} else if i := strings.LastIndex(field, "."); i >= 0 {
		base = field[i+1:]
	}
	switch {
	case field == "backend_set_id" || base == "backend_set_id":
		return idsOf(cmd.BackendSets, func(b generated.BackendSetConfig) string { return string(b.Id) }), true
	case field == "model_policy_id" || base == "model_policy_id":
		return idsOf(cmd.ModelPolicies, func(m generated.ModelPolicyConfig) string { return string(m.Id) }), true
	case field == "content_policy_id" || base == "content_policy_id":
		return idsOf(cmd.ContentPolicies, func(c generated.ContentPolicyConfig) string { return string(c.Id) }), true
	case field == "scope_id" || base == "scope_id":
		switch strings.TrimSpace(values["scope"]) {
		case "route":
			return idsOf(cmd.Routes, func(r generated.RouteConfig) string { return string(r.Id) }), true
		case "client_profile":
			return idsOf(cmd.ClientProfiles, func(p generated.MutableClientProfile) string { return string(p.Id) }), true
		case "target":
			return idsOf(cmd.Targets, func(t generated.MutableTargetCommand) string { return string(t.Id) }), true
		default:
			return nil, true
		}
	case field == "operation":
		return []string{"rename_model", "drop_header", "set_header", "normalize_stream_event"}, true
	case base == "target_id":
		return idsOf(cmd.Targets, func(t generated.MutableTargetCommand) string { return string(t.Id) }), true
	default:
		return nil, false
	}
}

func isKnownReferenceName(field string) bool {
	base := field
	if _, _, child, ok := parseArrayField(field); ok {
		base = child
	} else if i := strings.LastIndex(field, "."); i >= 0 {
		base = field[i+1:]
	}
	switch base {
	case "backend_set_id", "model_policy_id", "content_policy_id", "scope_id", "target_id", "operation":
		return true
	default:
		return false
	}
}

func referenceOptions(draft *configdraft.Draft, field string, values map[string]string) []string {
	opts, _ := knownReferenceOptions(draft, field, values)
	return opts
}

func idsOf[T any](items []T, id func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, id(item))
	}
	return out
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func lookupFieldDesc(kind Kind, name string) (descgen.FieldDescriptor, bool) {
	if kind == KindTrafficRules {
		required := map[string]bool{
			"name": true, "launcher": true, "model_strategy": true, "client": true,
			"routing": true, "primary_target_id": true,
		}
		if value, ok := required[name]; ok {
			return descgen.FieldDescriptor{Name: name, JSONPath: "$." + name, Required: value}, true
		}
		if name == "backup_target_id" {
			return descgen.FieldDescriptor{Name: name, JSONPath: "$." + name}, true
		}
		return descgen.FieldDescriptor{}, false
	}
	desc, ok := resourcepage.Lookup(kind.DescriptorKind())
	if !ok {
		return descgen.FieldDescriptor{}, false
	}
	base := name
	if _, _, child, parsed := parseArrayField(name); parsed {
		base = child
		for _, f := range desc.Fields {
			for _, c := range f.Children {
				if c.Name == base {
					return c, true
				}
			}
		}
		return descgen.FieldDescriptor{}, false
	}
	for _, f := range desc.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return descgen.FieldDescriptor{}, false
}

func fieldIsInteger(kind Kind, name string) bool {
	f, ok := lookupFieldDesc(kind, name)
	return ok && f.Kind == descgen.FieldKindInteger
}

func fieldConstraintSuffix(kind Kind, name string) string {
	f, ok := lookupFieldDesc(kind, name)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 4)
	if f.Min != nil {
		parts = append(parts, fmt.Sprintf("min=%d", *f.Min))
	}
	if f.Max != nil {
		parts = append(parts, fmt.Sprintf("max=%d", *f.Max))
	}
	if f.DefaultValue != "" {
		parts = append(parts, "default="+f.DefaultValue)
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " ") + "]"
}
