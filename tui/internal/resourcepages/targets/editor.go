package targets

import (
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	descgen "github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	"github.com/sahilm/fuzzy"
)

type editorMode int

const (
	editorNone editorMode = iota
	editorCreate
	editorEdit
)

type editorState struct {
	mode            editorMode
	kind            Kind
	id              string
	values          map[string]string
	cursor          int
	err             string
	secretMode      SecretActionMode
	focusToken      bool
	tokenBuf        *secretinput.Buffer
	generation      string
	draft           *configdraft.Draft
	replaceRequired bool
	selectorOpen    bool
	selectorIndex   int
	selectorFilter  string
}

func newCredentialEditor(mode editorMode, id string, draft *configdraft.Draft) editorState {
	values := map[string]string{
		"id":                        "",
		"name":                      "",
		"provider":                  "openai",
		"external_kind":             "env",
		"external_env_name":         "",
		"external_file_path":        "",
		"external_keychain_service": "",
		"external_keychain_account": "",
		"external_exportable":       "false",
	}
	secretMode := SecretActionReplace
	generation := "-"
	if mode == editorEdit {
		secretMode = SecretActionPreserve
		if c, ok := findCredential(draft.LocalCommand(), id); ok {
			values["id"] = string(c.Id)
			values["name"] = c.Name
			values["provider"] = string(c.Provider)
			if external, err := c.SecretAction.AsCredentialSecretAction2(); err == nil && external.Mode == generated.CredentialSecretActionExternalRef {
				secretMode = SecretActionExternal
				loadExternalValues(values, external.Ref)
			} else if replace, err := c.SecretAction.AsCredentialSecretAction1(); err == nil && replace.Mode == generated.CredentialSecretActionReplace {
				// Restore Replace so meta-only Save cannot silently DELETE a
				// pending unpublished replace stage via Preserve discard.
				secretMode = SecretActionReplace
			}
		}
		generation = credentialGeneration(draft, id)
	}
	return editorState{
		mode:       mode,
		kind:       KindCredentials,
		id:         id,
		values:     values,
		secretMode: secretMode,
		tokenBuf:   secretinput.New(),
		generation: generation,
	}
}

func newTargetEditor(mode editorMode, id string, draft *configdraft.Draft) editorState {
	values := map[string]string{}
	if mode == editorCreate {
		values = seedTargetCreateDefaults()
	}
	if mode == editorEdit {
		values = loadTargetEditorValues(id, draft)
	}
	return editorState{
		mode:   mode,
		kind:   KindTargets,
		id:     id,
		values: values,
		draft:  draft,
	}
}

func seedTargetCreateDefaults() map[string]string {
	values := map[string]string{}
	desc, ok := resourcepage.Lookup(KindTargets.DescriptorKind())
	if ok {
		for _, field := range desc.Fields {
			if len(field.Children) > 0 {
				for _, child := range field.Children {
					if child.DefaultValue != "" {
						values[child.Name] = child.DefaultValue
					}
				}
				continue
			}
			if field.DefaultValue != "" {
				values[field.Name] = field.DefaultValue
			}
		}
	}
	// Required health/throttle ints without descriptor defaults — Core sample.
	sample := map[string]string{
		"failure_threshold":          "2",
		"initial_backoff_ms":         "100",
		"jitter_percent":             "10",
		"max_backoff_ms":             "1000",
		"probe_timeout_ms":           "1000",
		"recovery_success_threshold": "1",
		"stable_probe_interval_ms":   "5000",
		"default_cooling_ms":         "1000",
		"max_cooling_ms":             "5000",
	}
	for k, v := range sample {
		if values[k] == "" {
			values[k] = v
		}
	}
	return values
}

func loadTargetEditorValues(id string, draft *configdraft.Draft) map[string]string {
	values := map[string]string{}
	t, ok := findTarget(draft.LocalCommand(), id)
	if !ok {
		return values
	}
	values["id"] = string(t.Id)
	values["name"] = t.Name
	values["adapter"] = string(t.Adapter)
	values["bridge"] = string(t.Bridge)
	values["capabilities"] = joinCapabilities(t.Capabilities)
	values["credential_id"] = string(t.CredentialId)
	values["endpoint_id"] = string(t.EndpointId)
	values["quota_group_id"] = string(t.QuotaGroupId)
	values["failure_threshold"] = strconv.Itoa(t.HealthPolicy.FailureThreshold)
	values["initial_backoff_ms"] = strconv.FormatInt(t.HealthPolicy.InitialBackoffMs, 10)
	values["jitter_percent"] = strconv.Itoa(t.HealthPolicy.JitterPercent)
	values["max_backoff_ms"] = strconv.FormatInt(t.HealthPolicy.MaxBackoffMs, 10)
	values["probe_timeout_ms"] = strconv.FormatInt(t.HealthPolicy.ProbeTimeoutMs, 10)
	values["recovery_success_threshold"] = strconv.Itoa(t.HealthPolicy.RecoverySuccessThreshold)
	values["stable_probe_interval_ms"] = strconv.FormatInt(t.HealthPolicy.StableProbeIntervalMs, 10)
	values["default_cooling_ms"] = strconv.FormatInt(t.ThrottlePolicy.DefaultCoolingMs, 10)
	values["max_cooling_ms"] = strconv.FormatInt(t.ThrottlePolicy.MaxCoolingMs, 10)
	return values
}

func joinCapabilities(caps []generated.MutableTargetCommandCapabilities) string {
	parts := make([]string, 0, len(caps))
	for _, c := range caps {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, ",")
}

func newEndpointEditor(mode editorMode, id string, draft *configdraft.Draft) editorState {
	values := map[string]string{}
	desc, ok := resourcepage.Lookup(KindEndpoints.DescriptorKind())
	if ok {
		for _, field := range desc.Fields {
			if field.DefaultValue != "" {
				values[field.Name] = field.DefaultValue
			}
		}
	}
	// Required fields without descriptor defaults — seed from Core sample config.
	if values["http2_enabled"] == "" {
		values["http2_enabled"] = "false"
	}
	if values["allow_private_network"] == "" {
		values["allow_private_network"] = "false"
	}
	if values["idle_connection_timeout_ms"] == "" {
		values["idle_connection_timeout_ms"] = "30000"
	}
	if values["max_idle_connections"] == "" {
		values["max_idle_connections"] = "10"
	}
	if mode == editorEdit {
		values = loadEndpointEditorValues(id, draft)
	}
	return editorState{
		mode:   mode,
		kind:   KindEndpoints,
		id:     id,
		values: values,
	}
}

func loadEndpointEditorValues(id string, draft *configdraft.Draft) map[string]string {
	values := map[string]string{}
	e, ok := findEndpoint(draft.LocalCommand(), id)
	if !ok {
		return values
	}
	values["id"] = string(e.Id)
	values["name"] = e.Name
	values["base_url"] = e.BaseUrl
	values["http2_enabled"] = strconv.FormatBool(e.Http2Enabled)
	values["allow_private_network"] = strconv.FormatBool(e.AllowPrivateNetwork)
	values["idle_connection_timeout_ms"] = strconv.FormatInt(e.IdleConnectionTimeoutMs, 10)
	values["max_idle_connections"] = strconv.Itoa(e.MaxIdleConnections)
	if e.ProxyUrl != nil {
		values["proxy_url"] = *e.ProxyUrl
	}
	if e.TlsServerName != nil {
		values["tls_server_name"] = *e.TlsServerName
	}
	return values
}

func loadExternalValues(values map[string]string, ref generated.ExternalSecretRef) {
	values["external_exportable"] = strconvBool(ref.Exportable)
	switch {
	case ref.Env != nil:
		values["external_kind"] = "env"
		values["external_env_name"] = ref.Env.Name
	case ref.File != nil:
		values["external_kind"] = "file"
		values["external_file_path"] = ref.File.Path
	case ref.Keychain != nil:
		values["external_kind"] = "keychain"
		values["external_keychain_service"] = ref.Keychain.Service
		values["external_keychain_account"] = ref.Keychain.Account
	}
}

func strconvBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (e *editorState) fieldNames() []string {
	if e.kind == KindUpstreams {
		names := []string{"name"}
		if e.mode == editorCreate && e.draft != nil && len(managedconfig.ProjectTrafficRules(e.draft.LocalCommand())) == 0 {
			names = append(names, "launcher")
		}
		names = append(names, "api_formats", "base_url", "api_key", "limit_policy_id", "allow_private_network")
		return names
	}
	if e.kind == KindLimitPolicies {
		return []string{"name", "rpm", "max_concurrency", "foreground_capacity", "background_capacity", "queue_timeout_ms", "foreground_weight", "background_weight"}
	}
	if e.kind == KindEndpoints {
		desc, ok := resourcepage.Lookup(KindEndpoints.DescriptorKind())
		if !ok {
			return nil
		}
		names := make([]string, 0, len(desc.Fields))
		for _, field := range desc.Fields {
			names = append(names, field.Name)
		}
		return advancedLast(names)
	}
	if e.kind == KindTargets {
		return advancedLast(targetFieldNames())
	}
	names := []string{"name", "secret_mode"}
	switch e.secretMode {
	case SecretActionReplace:
		names = append(names, "token")
	case SecretActionExternal:
		names = append(names, "external_kind", "external_exportable")
		switch e.values["external_kind"] {
		case "env":
			names = append(names, "external_env_name")
		case "file":
			names = append(names, "external_file_path")
		case "keychain":
			names = append(names, "external_keychain_service", "external_keychain_account")
		}
	}
	names = append(names, "id", "provider", "generation")
	return names
}

func advancedLast(names []string) []string {
	basic := make([]string, 0, len(names))
	advanced := make([]string, 0, len(names))
	for _, name := range names {
		if targetAdvancedField(name) {
			advanced = append(advanced, name)
		} else {
			basic = append(basic, name)
		}
	}
	return append(basic, advanced...)
}

func targetAdvancedField(name string) bool {
	return strings.Contains(name, "health") || strings.Contains(name, "timeout") ||
		strings.Contains(name, "rate") || strings.Contains(name, "throttle") ||
		name == "allow_private_network" || name == "id" || name == "generation" || strings.HasPrefix(name, "external_") || name == "provider" ||
		name == "foreground_capacity" || name == "background_capacity" || name == "foreground_weight" || name == "background_weight"
}

func (e *editorState) idEditable() bool       { return e.mode == editorCreate }
func (e *editorState) providerEditable() bool { return e.mode == editorCreate }

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
	if e.focusToken {
		for i, n := range names {
			if n == "token" || n == "api_key" {
				e.cursor = i
				break
			}
		}
	}
	if e.cursor < 0 || e.cursor >= len(names) {
		return ""
	}
	return names[e.cursor]
}

func (e *editorState) focusedReadonly() bool {
	name := e.focusedName()
	switch name {
	case "id":
		return !e.idEditable()
	case "provider":
		return !e.providerEditable()
	case "generation":
		return true
	default:
		return false
	}
}

func (e *editorState) focusedIsSelector() bool {
	name := e.focusedName()
	if name == "" || e.focusedReadonly() {
		return false
	}
	switch e.formField(name).Kind {
	case formui.Select, formui.Reference, formui.MultiSelect:
		return true
	default:
		return false
	}
}

func (e *editorState) focusedIsToggle() bool {
	name := e.focusedName()
	return name != "" && !e.focusedReadonly() && e.formField(name).Kind == formui.Toggle
}

func (e *editorState) focusedIsFixedSelect() bool {
	name := e.focusedName()
	return name != "" && !e.focusedReadonly() && e.formField(name).Kind == formui.Select
}

func (e *editorState) cycleFocusedSelect(delta int) bool {
	if e.selectorOpen || !e.focusedIsFixedSelect() {
		return false
	}
	options := e.selectorValues(e.focusedName())
	next, ok := formui.CycleValue(e.focusedValue(), options, delta)
	if !ok {
		return false
	}
	for index, option := range options {
		if option == next {
			e.chooseOption(index)
			return true
		}
	}
	return false
}

func (e *editorState) openSelector() bool {
	if !e.focusedIsSelector() {
		return false
	}
	e.selectorOpen = true
	e.selectorFilter = ""
	e.selectorIndex = 0
	current := e.focusedValue()
	for index, option := range e.filteredSelectorValues() {
		if option == current {
			e.selectorIndex = index
			break
		}
	}
	return true
}

func (e *editorState) closeSelector() {
	e.selectorOpen = false
	e.selectorFilter = ""
	e.selectorIndex = 0
}

func (e *editorState) focusedValue() string {
	if e.focusedName() == "secret_mode" {
		return string(e.secretMode)
	}
	return e.values[e.focusedName()]
}

func (e *editorState) filteredSelectorValues() []string {
	options := e.selectorValues(e.focusedName())
	query := strings.TrimSpace(e.selectorFilter)
	if query == "" || len(options) == 0 {
		return options
	}
	lowerOptions := make([]string, len(options))
	for index, option := range options {
		lowerOptions[index] = strings.ToLower(targetSelectorLabel(e.draft, e.focusedName(), option))
	}
	matches := fuzzy.Find(strings.ToLower(query), lowerOptions)
	filtered := make([]string, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, options[match.Index])
	}
	return filtered
}

func (e *editorState) moveSelector(delta int) bool {
	if !e.selectorOpen {
		return false
	}
	options := e.filteredSelectorValues()
	if len(options) == 0 {
		e.selectorIndex = 0
		return true
	}
	e.selectorIndex += delta
	if e.selectorIndex < 0 {
		e.selectorIndex = len(options) - 1
	}
	if e.selectorIndex >= len(options) {
		e.selectorIndex = 0
	}
	return true
}

func (e *editorState) moveField(delta int) {
	e.focusToken = false
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
	e.closeSelector()
}

func (e *editorState) appendRunes(runes []rune) {
	if e.focusedReadonly() {
		return
	}
	if e.focusedIsToggle() {
		return
	}
	if e.focusedIsSelector() {
		if !e.selectorOpen {
			e.openSelector()
		}
		e.selectorFilter += string(runes)
		e.selectorIndex = 0
		e.err = ""
		return
	}
	name := e.focusedName()
	switch name {
	case "", "generation", "token", "api_key":
		return
	case "secret_mode":
		e.cycleSecretMode(string(runes))
		return
	case "external_kind":
		e.cycleExternalKind(string(runes))
		return
	}
	oldName := e.values["name"]
	e.values[name] = e.values[name] + string(runes)
	if name == "name" {
		e.syncGeneratedID(oldName)
	}
}

func (e *editorState) backspace() {
	if e.focusedReadonly() {
		return
	}
	if e.focusedIsToggle() {
		return
	}
	if e.focusedIsSelector() {
		if !e.selectorOpen {
			e.openSelector()
		}
		runes := []rune(e.selectorFilter)
		if len(runes) > 0 {
			e.selectorFilter = string(runes[:len(runes)-1])
			e.selectorIndex = 0
		}
		return
	}
	name := e.focusedName()
	if name == "" || name == "token" || name == "api_key" || name == "secret_mode" || name == "generation" {
		return
	}
	oldName := e.values["name"]
	runes := []rune(e.values[name])
	if len(runes) == 0 {
		return
	}
	e.values[name] = string(runes[:len(runes)-1])
	if name == "name" {
		e.syncGeneratedID(oldName)
	}
}

func (e *editorState) syncGeneratedID(oldName string) {
	if e.mode != editorCreate || e.kind == KindUpstreams || e.draft == nil {
		return
	}
	used := formui.UsedConfigIDs(e.draft.LocalCommand())
	prefix := targetIDPrefix(e.kind)
	oldAuto := formui.UniqueID(oldName, prefix, used)
	if strings.TrimSpace(e.values["id"]) == "" || e.values["id"] == oldAuto {
		e.values["id"] = formui.UniqueID(e.values["name"], prefix, used)
	}
}

func (e *editorState) cycleSecretMode(_ string) {
	if e.replaceRequired {
		e.secretMode = SecretActionReplace
		e.focusToken = true
		e.err = "$.secret_action: replace required"
		return
	}
	switch e.secretMode {
	case SecretActionPreserve:
		e.secretMode = SecretActionReplace
	case SecretActionReplace:
		e.secretMode = SecretActionExternal
	case SecretActionExternal:
		if e.mode == editorCreate {
			e.secretMode = SecretActionReplace
		} else {
			e.secretMode = SecretActionPreserve
		}
	default:
		e.secretMode = SecretActionReplace
	}
	e.focusToken = e.secretMode == SecretActionReplace
}

func (e *editorState) setSecretMode(mode SecretActionMode) {
	if e.mode == editorCreate && mode == SecretActionPreserve {
		e.err = "$.secret_action: preserve unavailable on create"
		return
	}
	if e.replaceRequired && mode != SecretActionReplace {
		e.err = "$.secret_action: replace required"
		e.secretMode = SecretActionReplace
		e.focusToken = true
		return
	}
	e.secretMode = mode
	e.err = ""
	e.focusToken = mode == SecretActionReplace
}

func (e *editorState) cycleExternalKind(_ string) {
	switch e.values["external_kind"] {
	case "env":
		e.values["external_kind"] = "file"
	case "file":
		e.values["external_kind"] = "keychain"
	default:
		e.values["external_kind"] = "env"
	}
}

func targetFieldNames() []string {
	desc, ok := resourcepage.Lookup(KindTargets.DescriptorKind())
	if !ok {
		return nil
	}
	names := make([]string, 0, len(desc.Fields)+16)
	for _, field := range desc.Fields {
		if field.Name == "generation" {
			continue
		}
		if len(field.Children) > 0 {
			for _, child := range field.Children {
				names = append(names, child.Name)
			}
			continue
		}
		names = append(names, field.Name)
	}
	return names
}

func (e *editorState) applyFocusedSelector(draft *configdraft.Draft) bool {
	if draft != nil {
		e.draft = draft
	}
	if !e.focusedIsSelector() {
		return false
	}
	if !e.selectorOpen {
		return e.openSelector()
	}
	if e.formField(e.focusedName()).Kind == formui.MultiSelect {
		e.closeSelector()
		return true
	}
	e.chooseOption(e.selectorIndex)
	return true
}

func (e *editorState) selectorValues(name string) []string {
	switch name {
	case "launcher":
		return []string{upstreamClientCodex, upstreamClientClaude}
	case "api_formats":
		return []string{upstreamAPIResponses, upstreamAPIChat, upstreamAPIAnthropic}
	case "limit_policy_id":
		values := []string{"new"}
		if e.draft != nil {
			for _, policy := range managedconfig.ProjectLimitPolicies(e.draft.LocalCommand()) {
				values = append(values, string(policy.Object.Id))
			}
		}
		return values
	case "secret_mode":
		if e.replaceRequired {
			return []string{string(SecretActionReplace)}
		}
		if e.mode == editorCreate {
			return []string{string(SecretActionReplace), string(SecretActionExternal)}
		}
		return []string{string(SecretActionPreserve), string(SecretActionReplace), string(SecretActionExternal)}
	case "external_kind":
		return []string{"env", "file", "keychain"}
	case "provider":
		if e.providerEditable() {
			return credentialProviderEnumValues()
		}
	case "adapter":
		return targetAdapterEnumValues()
	case "bridge":
		return targetBridgeEnumValues()
	case "capabilities":
		return targetCapabilitiesEnumValues()
	case "credential_id", "endpoint_id", "quota_group_id":
		return referenceOptionIDs(name, e.draft)
	}
	if descriptor, ok := targetEditorFieldDescriptor(e.kind, name); ok && descriptor.Kind == descgen.FieldKindEnum {
		return append([]string(nil), descriptor.EnumValues...)
	}
	return nil
}

func (e *editorState) toggleFocusedSelector() bool {
	if !e.selectorOpen || !e.focusedIsSelector() || e.formField(e.focusedName()).Kind != formui.MultiSelect {
		return false
	}
	options := e.filteredSelectorValues()
	if len(options) == 0 {
		return true
	}
	if e.selectorIndex < 0 || e.selectorIndex >= len(options) {
		e.selectorIndex = 0
	}
	name := e.focusedName()
	selected := map[string]bool{}
	for _, value := range strings.FieldsFunc(e.values[name], func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if value = strings.TrimSpace(value); value != "" {
			selected[value] = true
		}
	}
	value := options[e.selectorIndex]
	if selected[value] {
		delete(selected, value)
	} else {
		selected[value] = true
	}
	ordered := make([]string, 0, len(selected))
	for _, option := range e.selectorValues(name) {
		if selected[option] {
			ordered = append(ordered, option)
		}
	}
	e.values[name] = strings.Join(ordered, ",")
	return true
}

func upstreamAPIOptions(client string) []string {
	if client == upstreamClientCodex {
		return []string{upstreamAPIResponses, upstreamAPIAnthropic}
	}
	return []string{upstreamAPIAnthropic, upstreamAPIChat}
}

func stringInOptions(value string, options []string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func (e *editorState) applyTargetSelector(name string, draft *configdraft.Draft) bool {
	if draft == nil {
		draft = e.draft
	}
	switch name {
	case "adapter":
		e.cycleEnum(name, targetAdapterEnumValues())
		return true
	case "bridge":
		e.cycleEnum(name, targetBridgeEnumValues())
		return true
	case "capabilities":
		e.cycleCapabilities()
		return true
	case "credential_id", "endpoint_id", "quota_group_id":
		ids := referenceOptionIDs(name, draft)
		if len(ids) == 0 {
			e.err = "no options"
			return true
		}
		e.err = ""
		e.cycleEnum(name, ids)
		return true
	}
	return false
}

func referenceOptionIDs(name string, draft *configdraft.Draft) []string {
	if draft == nil {
		return nil
	}
	cmd := draft.LocalCommand()
	switch name {
	case "credential_id":
		ids := make([]string, 0, len(cmd.Credentials))
		for _, c := range cmd.Credentials {
			ids = append(ids, string(c.Id))
		}
		return ids
	case "endpoint_id":
		ids := make([]string, 0, len(cmd.Endpoints))
		for _, ep := range cmd.Endpoints {
			ids = append(ids, string(ep.Id))
		}
		return ids
	case "quota_group_id":
		ids := make([]string, 0, len(cmd.QuotaGroups))
		for _, q := range cmd.QuotaGroups {
			ids = append(ids, string(q.Id))
		}
		return ids
	default:
		return nil
	}
}

func (e *editorState) cycleEnum(name string, order []string) {
	if len(order) == 0 {
		return
	}
	cur := strings.TrimSpace(e.values[name])
	for i, v := range order {
		if v == cur {
			e.values[name] = order[(i+1)%len(order)]
			return
		}
	}
	e.values[name] = order[0]
}

func (e *editorState) cycleCapabilities() {
	order := targetCapabilitiesEnumValues()
	if len(order) == 0 {
		return
	}
	cur := strings.TrimSpace(e.values["capabilities"])
	parts := strings.FieldsFunc(cur, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	})
	set := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			set[p] = struct{}{}
		}
	}
	// Toggle next capability after the last present one; empty starts at chat.
	if len(parts) == 0 {
		e.values["capabilities"] = order[0]
		return
	}
	last := parts[len(parts)-1]
	idx := 0
	for i, v := range order {
		if v == last {
			idx = i
			break
		}
	}
	next := order[(idx+1)%len(order)]
	if _, ok := set[next]; ok && len(parts) == 1 {
		e.values["capabilities"] = next
		return
	}
	if _, ok := set[next]; ok {
		// remove next and keep remaining unique order
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p != next {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			out = []string{next}
		}
		e.values["capabilities"] = strings.Join(out, ",")
		return
	}
	e.values["capabilities"] = strings.Join(append(parts, next), ",")
}

func (e *editorState) toggleBool(name string) {
	if strings.EqualFold(strings.TrimSpace(e.values[name]), "true") {
		e.values[name] = "false"
	} else {
		e.values[name] = "true"
	}
}

func (e *editorState) cycleProvider() {
	order := credentialProviderEnumValues()
	if len(order) == 0 {
		return
	}
	cur := e.values["provider"]
	for i, p := range order {
		if p == cur {
			e.values["provider"] = order[(i+1)%len(order)]
			return
		}
	}
	e.values["provider"] = order[0]
}

// credentialProviderEnumValues prefers descriptor EnumValues, falling back to
// generated constants so cycling/validation stay aligned with Valid().
func credentialProviderEnumValues() []string {
	if values := descriptorEnumValues(KindCredentials.DescriptorKind(), "provider"); len(values) > 0 {
		return values
	}
	return []string{
		string(generated.MutableCredentialCommandProviderOpenai),
		string(generated.MutableCredentialCommandProviderOpenaiCompatible),
		string(generated.MutableCredentialCommandProviderSub2apiOpenai),
		string(generated.MutableCredentialCommandProviderAnthropic),
	}
}

func targetAdapterEnumValues() []string {
	if values := descriptorEnumValues(KindTargets.DescriptorKind(), "adapter"); len(values) > 0 {
		return values
	}
	return []string{
		string(generated.MutableTargetCommandAdapterOpenai),
		string(generated.MutableTargetCommandAdapterOpenaiCompatible),
		string(generated.MutableTargetCommandAdapterSub2apiOpenai),
		string(generated.MutableTargetCommandAdapterAnthropic),
	}
}

func targetBridgeEnumValues() []string {
	if values := descriptorEnumValues(KindTargets.DescriptorKind(), "bridge"); len(values) > 0 {
		return values
	}
	return []string{
		string(generated.MutableTargetCommandBridgeOpenaiChat),
		string(generated.MutableTargetCommandBridgeOpenaiResponses),
		string(generated.MutableTargetCommandBridgeAnthropicMessages),
		string(generated.MutableTargetCommandBridgeAnthropicToOpenai),
		string(generated.MutableTargetCommandBridgeOpenaiToAnthropic),
	}
}

// targetCapabilitiesEnumValues prefers descriptor EnumValues, falling back to
// generated constants so cycling stays aligned with Valid().
func targetCapabilitiesEnumValues() []string {
	if values := descriptorEnumValues(KindTargets.DescriptorKind(), "capabilities"); len(values) > 0 {
		return values
	}
	return []string{
		string(generated.MutableTargetCommandCapabilitiesChat),
		string(generated.MutableTargetCommandCapabilitiesMessages),
		string(generated.MutableTargetCommandCapabilitiesModelDiscovery),
		string(generated.MutableTargetCommandCapabilitiesProbe),
		string(generated.MutableTargetCommandCapabilitiesResponses),
		string(generated.MutableTargetCommandCapabilitiesStreaming),
		string(generated.MutableTargetCommandCapabilitiesTools),
		string(generated.MutableTargetCommandCapabilitiesVision),
	}
}

func descriptorEnumValues(kind resourcepage.ResourceKind, fieldName string) []string {
	desc, ok := resourcepage.Lookup(kind)
	if !ok {
		return nil
	}
	for _, field := range desc.Fields {
		if field.Name == fieldName && len(field.EnumValues) > 0 {
			return append([]string(nil), field.EnumValues...)
		}
	}
	return nil
}

func (e *editorState) render(width, height int) string {
	return e.formLayout(width, height).View
}

func (e *editorState) formLayout(width, height int) formui.Layout {
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
		field := e.formField(name)
		if field.Advanced && advancedStart == len(names) {
			advancedStart = i
		}
		if errorField == name {
			field.Error = errorDetail
		}
		fields = append(fields, field)
	}
	hint := i18n.T("form.footer.target")
	if e.focusedIsFixedSelect() && !e.selectorOpen {
		hint += "  " + i18n.T("form.select.cycle")
	}
	return formui.Render(formui.Spec{
		Title: title, Context: targetFormContext(e.kind), Notice: notice, Fields: fields,
		Focus: e.cursor, AdvancedExpanded: e.cursor >= advancedStart, Width: width, Height: height,
		Footer: hint,
	})
}

func (e *editorState) formField(name string) formui.Field {
	value := e.values[name]
	if name == "generation" {
		value = e.generation
	}
	if name == "secret_mode" {
		value = string(e.secretMode)
	}
	if (name == "token" || name == "api_key") && e.tokenBuf != nil {
		value = e.tokenBuf.View()
	}
	field := formui.Field{ID: name, Label: formui.FriendlyLabel(name), Value: value, Section: "Connection"}
	if descriptor, ok := targetEditorFieldDescriptor(e.kind, name); ok {
		field.Required = descriptor.Required
		switch descriptor.Kind {
		case descgen.FieldKindBoolean:
			field.Kind = formui.Toggle
		case descgen.FieldKindInteger, descgen.FieldKindNumber:
			field.Kind = formui.Integer
		case descgen.FieldKindEnum:
			field.Kind = formui.Select
			for _, value := range descriptor.EnumValues {
				field.Options = append(field.Options, formui.Option{Label: value, Value: value, Selected: value == e.values[name]})
			}
		case descgen.FieldKindArray:
			field.Kind = formui.MultiSelect
		case descgen.FieldKindReference:
			field.Kind = formui.Reference
		}
	}
	switch name {
	case "api_key", "token":
		field.Kind, field.Required = formui.Secret, e.mode == editorCreate || e.replaceRequired
		field.Placeholder = "Paste secret"
	case "allow_private_network":
		field.Kind = formui.Toggle
		field.Help = "Enable only for localhost or private network URLs"
	case "launcher":
		field.Kind = formui.Select
		field.Options = optionsForValues([]string{upstreamClientCodex, upstreamClientClaude}, e.values[name])
	case "api_formats":
		field.Kind, field.Required = formui.MultiSelect, true
		field.Options = optionsForValues(e.selectorValues(name), e.values[name])
		field.Section = "Connection"
	case "limit_policy_id":
		field.Kind = formui.Reference
		field.Options = limitPolicyOptions(e.draft, e.values[name])
		field.Display = limitPolicyDisplay(e.draft, e.values[name])
		field.Help = "Use a private default policy or share an existing limit policy"
	case "rpm":
		field.Kind, field.Required, field.Unit = formui.Integer, true, "req/min"
		field.Section = "Limits"
	case "max_concurrency":
		field.Kind, field.Required, field.Unit = formui.Integer, true, "requests"
		field.Section = "Limits"
	case "foreground_capacity", "background_capacity", "foreground_weight", "background_weight", "queue_timeout_ms":
		field.Kind, field.Section, field.Advanced = formui.Integer, "Advanced", true
	case "secret_mode":
		field.Kind = formui.Select
		field.Options = optionsForValues([]string{string(SecretActionPreserve), string(SecretActionReplace), string(SecretActionExternal)}, string(e.secretMode))
	case "external_kind":
		field.Kind = formui.Select
		field.Options = optionsForValues([]string{"env", "file", "keychain"}, e.values[name])
	case "adapter":
		field.Kind = formui.Select
		field.Options = optionsForValues(targetAdapterEnumValues(), e.values[name])
	case "bridge":
		field.Kind = formui.Select
		field.Options = optionsForValues(targetBridgeEnumValues(), e.values[name])
	case "capabilities":
		field.Kind, field.Section = formui.MultiSelect, "Capabilities"
		field.Options = optionsForValues(targetCapabilitiesEnumValues(), e.values[name])
	case "credential_id", "endpoint_id", "quota_group_id":
		field.Kind = formui.Reference
		field.Options = optionsForValues(referenceOptionIDs(name, e.draft), e.values[name])
	}
	if targetAdvancedField(name) {
		field.Section, field.Advanced = "Advanced", true
	}
	if name == "id" || name == "generation" || name == "provider" && !e.providerEditable() {
		field.ReadOnly = e.focusedReadonly() || name == "generation" || name == "id" && !e.idEditable()
		if field.ReadOnly {
			field.Kind = formui.ReadOnly
		}
	}
	if name == "id" {
		field.Help = "Generated from the name; customize before saving if needed"
	}
	if strings.HasSuffix(name, "_ms") {
		field.Unit = "ms"
	}
	if name == e.focusedName() && e.selectorOpen && (field.Kind == formui.Select || field.Kind == formui.Reference || field.Kind == formui.MultiSelect) {
		selected := map[string]bool{}
		for _, value := range strings.FieldsFunc(e.focusedValue(), func(r rune) bool { return r == ',' || r == ';' }) {
			if value = strings.TrimSpace(value); value != "" {
				selected[value] = true
			}
		}
		values := e.filteredSelectorValues()
		field.Options = make([]formui.Option, 0, len(values))
		for _, value := range values {
			field.Options = append(field.Options, formui.Option{Label: targetSelectorLabel(e.draft, name, value), Value: value, Selected: selected[value]})
		}
		field.Expanded = true
		field.OptionIndex = e.selectorIndex
		if e.selectorFilter != "" {
			field.Help = i18n.T("form.select.search", map[string]string{"query": e.selectorFilter})
		}
		field.EmptyText = i18n.T("form.select.none")
		if e.selectorFilter != "" {
			field.EmptyText = i18n.T("form.select.empty")
		}
		return field
	}
	for index, option := range field.Options {
		if option.Selected {
			field.OptionIndex = index
			break
		}
	}
	return field
}

func limitPolicyOptions(draft *configdraft.Draft, selected string) []formui.Option {
	options := []formui.Option{{Label: "Private default", Value: "new", Selected: selected == "new" || selected == ""}}
	if draft == nil {
		return options
	}
	for _, policy := range managedconfig.ProjectLimitPolicies(draft.LocalCommand()) {
		id := string(policy.Object.Id)
		options = append(options, formui.Option{Label: policy.Object.Name, Value: id, Selected: selected == id})
	}
	return options
}

func limitPolicyDisplay(draft *configdraft.Draft, id string) string {
	if id == "" || id == "new" {
		return "Private default"
	}
	if draft != nil {
		if object, ok := managedconfig.FindObject(draft.LocalCommand(), id); ok {
			return object.Name
		}
	}
	return id
}

func targetSelectorLabel(draft *configdraft.Draft, field, id string) string {
	if field == "limit_policy_id" {
		return limitPolicyDisplay(draft, id)
	}
	if draft == nil || strings.TrimSpace(id) == "" {
		return id
	}
	cmd := draft.LocalCommand()
	switch field {
	case "credential_id":
		if item, ok := findCredential(cmd, id); ok {
			return item.Name
		}
	case "endpoint_id":
		if item, ok := findEndpoint(cmd, id); ok {
			return item.Name
		}
	case "quota_group_id":
		for _, item := range cmd.QuotaGroups {
			if string(item.Id) == id {
				return item.Name
			}
		}
	}
	return id
}

func (e *editorState) chooseOption(index int) {
	values := e.filteredSelectorValues()
	if index < 0 || index >= len(values) {
		return
	}
	value := values[index]
	switch e.focusedName() {
	case "secret_mode":
		e.setSecretMode(SecretActionMode(value))
	case "capabilities", "api_formats":
		e.selectorIndex = index
		e.toggleFocusedSelector()
		return
	default:
		e.values[e.focusedName()] = value
		if e.focusedName() == "client" {
			options := upstreamAPIOptions(value)
			if !stringInOptions(e.values["api_type"], options) {
				e.values["api_type"] = options[0]
			}
		}
	}
	e.closeSelector()
}

func targetFormContext(kind Kind) string {
	switch kind {
	case KindUpstreams:
		return i18n.T("form.context.upstream")
	case KindLimitPolicies:
		return "Request rate and concurrency limits"
	case KindTargets:
		return i18n.T("form.context.target")
	case KindEndpoints:
		return i18n.T("form.context.endpoint")
	case KindCredentials:
		return i18n.T("form.context.credential")
	default:
		return "Resource configuration"
	}
}

func optionsForValues(values []string, selected string) []formui.Option {
	options := make([]formui.Option, 0, len(values))
	for _, value := range values {
		options = append(options, formui.Option{Label: value, Value: value, Selected: value == selected})
	}
	return options
}

func targetEditorFieldDescriptor(kind Kind, name string) (descgen.FieldDescriptor, bool) {
	desc, ok := resourcepage.Lookup(kind.DescriptorKind())
	if !ok {
		return descgen.FieldDescriptor{}, false
	}
	for _, field := range desc.Fields {
		if field.Name == name {
			return field, true
		}
		for _, child := range field.Children {
			if child.Name == name {
				return child, true
			}
		}
	}
	return descgen.FieldDescriptor{}, false
}

func (e *editorState) displayValue(name string) string {
	switch name {
	case "generation":
		return e.generation + " (read-only)"
	case "id":
		v := e.values["id"]
		if !e.idEditable() {
			return v + " (read-only)"
		}
		return v
	case "client", "api_type", "launcher", "api_formats", "limit_policy_id":
		return e.values[name]
	case "provider":
		v := e.values["provider"]
		if !e.providerEditable() {
			return v + " (read-only)"
		}
		return v
	case "secret_mode":
		return string(e.secretMode)
	case "token", "api_key":
		if e.tokenBuf == nil {
			return ""
		}
		masked := e.tokenBuf.View()
		if masked == "" {
			return "(paste token)"
		}
		return masked
	default:
		return e.values[name]
	}
}
