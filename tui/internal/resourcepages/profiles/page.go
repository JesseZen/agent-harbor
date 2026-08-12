package profiles

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

var (
	configIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	envNamePattern  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// Referrer is an inbound reference path that blocks profile deletion.
type Referrer struct {
	Path string
}

// Options configures page collaboration with the host (e.g. session referrers).
type Options struct {
	Referrers func(profileID string) []Referrer
}

// NewPage constructs the Sessions-tab Profiles secondary resource page.
func NewPage(draft *configdraft.Draft, opts Options) *Page {
	inner := resourcepage.New(resourcepage.Spec{
		Title: "Profiles",
		Scope: "local",
		Columns: []resourceview.Column{
			{Title: "ID", MinWidth: 10, Priority: 0},
			{Title: "NAME", MinWidth: 10, Priority: 1},
			{Title: "LAUNCHER", MinWidth: 8, Priority: 2},
			{Title: "DEFAULT_ROUTE", MinWidth: 12, Priority: 3},
			{Title: "MODEL_PROJECTION", MinWidth: 14, Priority: 4},
		},
		Actions: resourcepage.ActionSet{
			Create:  true,
			Edit:    true,
			Delete:  true,
			Publish: false,
			Details: true,
			Filter:  true,
			Mark:    true,
		},
		Domain: string(configdraft.DomainProfiles),
	})
	page := &Page{draft: draft, inner: inner, opts: opts, fields: map[string]string{}, width: 80, height: 24}
	page.Refresh()
	return page
}

type mode int

const (
	modeList mode = iota
	modeEdit
	modeDetails
	modeConfirmDelete
	modeBlockedDelete
	modeRefSelect
)

type Page struct {
	draft            *configdraft.Draft
	inner            *resourcepage.Page
	opts             Options
	mode             mode
	creating         bool
	fields           map[string]string
	fieldOrder       []string
	fieldIndex       int
	status           string
	editorGen        int64
	editorRev        string
	blockedPaths     []string
	confirmID        string
	confirmCount     int
	refField         string
	refFilter        string
	refChoices       []string
	refCursor        int
	filteringRef     bool
	width            int
	height           int
	advancedUnlocked bool
}

func (p *Page) Inner() *resourcepage.Page { return p.inner }
func (p *Page) Editing() bool             { return p.mode == modeEdit || p.mode == modeRefSelect }
func (p *Page) ShowingDetails() bool      { return p.mode == modeDetails }
func (p *Page) ConfirmingDelete() bool    { return p.mode == modeConfirmDelete }
func (p *Page) DeleteBlocked() bool       { return p.mode == modeBlockedDelete }
func (p *Page) SelectingReference() bool  { return p.mode == modeRefSelect }
func (p *Page) State() resourcepage.State { return p.inner.State() }
func (p *Page) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.inner.SetSize(width, height)
}
func (p *Page) CurrentField() string {
	if p.fieldIndex < 0 || p.fieldIndex >= len(p.fieldOrder) {
		return ""
	}
	return p.fieldOrder[p.fieldIndex]
}

func (p *Page) EditorFieldNames() []string { return append([]string(nil), p.fieldOrder...) }

func (p *Page) OverlayLines() int {
	lines := 0
	switch p.inner.State() {
	case resourcepage.StateDisconnected, resourcepage.StateEmpty, resourcepage.StateLoading,
		resourcepage.StateValidationError, resourcepage.StatePublicationError, resourcepage.StateStale:
		lines++
	}
	// resourcepage renders SetStatus on its own line; keep host mouse math in sync.
	if p.status != "" {
		lines++
	}
	return lines
}

func (p *Page) SelectID(id string) {
	rows := p.rowsFromDraft()
	p.inner.SetRows(rows)
	_ = p.inner.View()
	// Clear any active/applied table filter so the full row set is searchable.
	p.inner.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	p.inner.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = p.inner.View()
	for range rows {
		p.inner.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	for range rows {
		if p.inner.SelectedID() == id {
			return
		}
		p.inner.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

func (p *Page) Refresh() {
	if p.draft == nil {
		p.status = ""
		p.inner.SetState(resourcepage.StateEmpty)
		p.inner.SetRows(nil)
		p.inner.SetStatus("")
		return
	}
	if !p.draft.InConflict() && strings.HasPrefix(p.status, "generation conflict") {
		p.status = ""
	}
	p.rebaseEditorIfDraftMoved()
	rows := p.rowsFromDraft()
	p.inner.SetRows(rows)
	p.inner.SetDirty(p.draft.DomainDirty(configdraft.DomainProfiles))
	switch {
	case p.draft.Disconnected():
		p.status = ""
		p.inner.SetState(resourcepage.StateDisconnected)
		p.inner.SetStatus("")
	case p.draft.InConflict():
		conflictMsg := "generation conflict — reapply or reload"
		p.inner.SetState(resourcepage.StateStale)
		// Preserve sticky validation while editing; conflict is also rendered
		// from draft.InConflict() in editor/ref-select views.
		if (p.mode == modeEdit || p.mode == modeRefSelect) &&
			p.status != "" && !strings.HasPrefix(p.status, "generation conflict") {
			p.inner.SetStatus(conflictMsg + "; " + p.status)
		} else {
			p.status = conflictMsg
			p.inner.SetStatus(p.status)
		}
	case p.mode == modeEdit && p.status != "":
		p.inner.SetState(resourcepage.StateValidationError)
		p.inner.SetStatus(p.status)
	case len(rows) == 0:
		p.inner.SetState(resourcepage.StateEmpty)
		if p.mode == modeList || p.status == "" {
			p.status = ""
			p.inner.SetStatus("")
		}
	default:
		if p.mode == modeList {
			p.status = ""
			p.inner.SetStatus("")
			p.inner.SetState(resourcepage.StateSuccess)
		} else if p.status != "" {
			p.inner.SetState(resourcepage.StateValidationError)
			p.inner.SetStatus(p.status)
		} else {
			p.inner.SetState(resourcepage.StateSuccess)
			p.inner.SetStatus("")
		}
	}
}

// rebaseEditorIfDraftMoved reloads open-edit fields when the shared draft
// generation/revision advanced (AcceptCurrent / reload) so F2 cannot overwrite
// freshly accepted remote state with a stale editor buffer.
func (p *Page) rebaseEditorIfDraftMoved() {
	if p.mode != modeEdit && p.mode != modeRefSelect {
		return
	}
	if p.creating || p.draft == nil {
		return
	}
	if p.draft.InConflict() {
		return
	}
	if p.draft.Generation() == p.editorGen && p.draft.Revision() == p.editorRev {
		return
	}
	id := strings.TrimSpace(p.fields["id"])
	profile, ok := p.findProfile(id)
	if !ok {
		p.mode = modeList
		p.status = ""
		p.editorGen = 0
		p.editorRev = ""
		return
	}
	p.fields = profileToFields(profile)
	p.fieldOrder = fieldNames()
	p.fieldIndex = 0
	p.mode = modeEdit
	p.refField = ""
	p.filteringRef = false
	// Drop sticky validation/conflict status — rebased fields are a fresh buffer.
	p.status = ""
	p.editorGen = p.draft.Generation()
	p.editorRev = p.draft.Revision()
}

func (p *Page) rowsFromDraft() []resourceview.Row {
	profiles := p.draft.LocalCommand().ClientProfiles
	rows := make([]resourceview.Row, 0, len(profiles))
	for _, profile := range profiles {
		rows = append(rows, resourceview.Row{
			ID: string(profile.Id),
			Cells: []string{
				string(profile.Id),
				profile.Name,
				string(profile.Launcher),
				string(profile.DefaultRouteId),
				string(profile.ModelProjectionId),
			},
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func (p *Page) Update(message tea.Msg) (resourcepage.Intent, bool) {
	switch p.mode {
	case modeEdit:
		return p.updateEditor(message)
	case modeRefSelect:
		return p.updateRefSelect(message)
	case modeDetails:
		if key, ok := message.(tea.KeyMsg); ok && key.String() == "esc" {
			p.mode = modeList
			return resourcepage.IntentNone, true
		}
		return resourcepage.IntentNone, true
	case modeConfirmDelete:
		return p.updateConfirm(message)
	case modeBlockedDelete:
		if key, ok := message.(tea.KeyMsg); ok && (key.String() == "esc" || key.String() == "enter") {
			p.mode = modeList
			p.confirmID = ""
			p.blockedPaths = nil
			return resourcepage.IntentNone, true
		}
		return resourcepage.IntentNone, true
	}
	if key, ok := message.(tea.KeyMsg); ok && key.String() == "u" && (p.inner == nil || !p.inner.Table().Filtering()) {
		p.advancedUnlocked = !p.advancedUnlocked
		if p.advancedUnlocked {
			p.status = "Advanced editing unlocked for this page"
		} else {
			p.status = "Managed members are read-only · u unlock"
		}
		p.inner.SetStatus(p.status)
		return resourcepage.IntentNone, true
	}

	intent, consumed := p.inner.Update(message)
	switch intent {
	case resourcepage.IntentCreate:
		p.openEditor(true)
		return intent, true
	case resourcepage.IntentEdit:
		if p.inner.SelectedID() == "" {
			return resourcepage.IntentNone, true
		}
		if p.managedProfileLocked(p.inner.SelectedID()) {
			p.status = "This profile belongs to a managed traffic rule · press u to unlock advanced editing"
			p.inner.SetStatus(p.status)
			return resourcepage.IntentNone, true
		}
		p.openEditor(false)
		return intent, true
	case resourcepage.IntentDelete:
		if p.managedProfileLocked(p.inner.SelectedID()) {
			p.status = "This profile belongs to a managed traffic rule · press u to unlock advanced editing"
			p.inner.SetStatus(p.status)
			return resourcepage.IntentNone, true
		}
		p.beginDelete(p.inner.SelectedID())
		return intent, true
	case resourcepage.IntentDetails:
		p.mode = modeDetails
		return intent, true
	case resourcepage.IntentPublish:
		if p.draft != nil && (p.draft.Disconnected() || !p.draft.CanPublish()) {
			return resourcepage.IntentNone, true
		}
		return intent, consumed
	default:
		return intent, consumed
	}
}

func (p *Page) managedProfileLocked(id string) bool {
	if p.advancedUnlocked || p.draft == nil || id == "" {
		return false
	}
	_, owned := managedconfig.OwnerOf(p.draft.LocalCommand(), generated.ManagedResourceRefKindClientProfile, id)
	return owned
}

func (p *Page) beginDelete(id string) {
	if id == "" {
		return
	}
	refs := p.collectReferrers(id)
	if len(refs) > 0 {
		p.mode = modeBlockedDelete
		p.confirmID = id
		p.blockedPaths = make([]string, 0, len(refs))
		for _, ref := range refs {
			p.blockedPaths = append(p.blockedPaths, ref.Path)
		}
		sort.Strings(p.blockedPaths)
		return
	}
	p.mode = modeConfirmDelete
	p.confirmID = id
	p.confirmCount = len(p.draft.LocalCommand().ClientProfiles) - 1
}

func (p *Page) collectReferrers(id string) []Referrer {
	var refs []Referrer
	cmd := p.draft.LocalCommand()
	for i, xf := range cmd.CompatibilityTransforms {
		if xf.Scope == generated.ClientProfile && string(xf.ScopeId) == id {
			refs = append(refs, Referrer{
				Path: fmt.Sprintf("compatibility_transforms[%d].scope_id", i),
			})
		}
	}
	if p.opts.Referrers != nil {
		refs = append(refs, p.opts.Referrers(id)...)
	}
	return refs
}

func (p *Page) updateConfirm(message tea.Msg) (resourcepage.Intent, bool) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return resourcepage.IntentNone, true
	}
	switch key.String() {
	case "enter", "y":
		p.deleteProfile(p.confirmID)
		p.mode = modeList
		p.confirmID = ""
		p.status = ""
		p.Refresh()
		return p.applyIntent(), true
	case "esc", "n":
		p.mode = modeList
		p.confirmID = ""
	}
	return resourcepage.IntentNone, true
}

func (p *Page) deleteProfile(id string) {
	p.draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		out := make([]generated.MutableClientProfile, 0, len(cmd.ClientProfiles))
		for _, profile := range cmd.ClientProfiles {
			if string(profile.Id) != id {
				out = append(out, profile)
			}
		}
		cmd.ClientProfiles = out
	})
}

func (p *Page) openEditor(creating bool) {
	p.mode = modeEdit
	p.creating = creating
	p.fieldIndex = 0
	p.status = ""
	p.fieldOrder = fieldNames()
	if p.draft != nil {
		p.editorGen = p.draft.Generation()
		p.editorRev = p.draft.Revision()
	}
	if creating {
		p.fields = make(map[string]string, len(p.fieldOrder))
		for _, name := range p.fieldOrder {
			p.fields[name] = ""
		}
		p.retainConflictBanner()
		return
	}
	profile, ok := p.findProfile(p.inner.SelectedID())
	if !ok {
		p.mode = modeList
		return
	}
	p.fields = profileToFields(profile)
	// Keep conflict banner visible: editor View only renders p.status.
	p.retainConflictBanner()
}

func (p *Page) retainConflictBanner() {
	if p.draft == nil || !p.draft.InConflict() {
		return
	}
	p.status = "generation conflict — reapply or reload"
	p.inner.SetState(resourcepage.StateStale)
	p.inner.SetStatus(p.status)
}

func (p *Page) findProfile(id string) (generated.MutableClientProfile, bool) {
	for _, profile := range p.draft.LocalCommand().ClientProfiles {
		if string(profile.Id) == id {
			return profile, true
		}
	}
	return generated.MutableClientProfile{}, false
}

func (p *Page) updateEditor(message tea.Msg) (resourcepage.Intent, bool) {
	if mouse, ok := message.(tea.MouseMsg); ok {
		return p.updateEditorMouse(mouse)
	}
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return resourcepage.IntentNone, true
	}
	name := p.CurrentField()
	switch key.String() {
	case "esc":
		p.mode = modeList
		p.status = ""
		p.Refresh()
		return resourcepage.IntentNone, true
	case "ctrl+s":
		if err := p.SaveEditor(); err != nil {
			return resourcepage.IntentNone, true
		}
		return p.applyIntent(), true
	case "f2":
		if err := p.SaveEditor(); err != nil {
			return resourcepage.IntentNone, true
		}
		return p.applyIntent(), true
	case "tab", "down":
		if len(p.fieldOrder) > 0 {
			p.fieldIndex = (p.fieldIndex + 1) % len(p.fieldOrder)
		}
		return resourcepage.IntentNone, true
	case "shift+tab", "up":
		if len(p.fieldOrder) > 0 {
			p.fieldIndex = (p.fieldIndex - 1 + len(p.fieldOrder)) % len(p.fieldOrder)
		}
		return resourcepage.IntentNone, true
	case "enter":
		if name == "launcher" || isReferenceField(name) {
			p.openRefSelect(name)
			return resourcepage.IntentNone, true
		}
		if name == "launcher" {
			p.cycleLauncher(1)
			return resourcepage.IntentNone, true
		}
		return resourcepage.IntentNone, true
	case "left":
		if name == "launcher" {
			p.cycleLauncher(-1)
		}
		return resourcepage.IntentNone, true
	case "right":
		if name == "launcher" {
			p.cycleLauncher(1)
		}
		return resourcepage.IntentNone, true
	case "backspace":
		if name == "id" && !p.creating {
			return resourcepage.IntentNone, true
		}
		if name == "launcher" {
			p.openRefSelect(name)
			return resourcepage.IntentNone, true
		}
		val := p.fields[name]
		oldName := p.fields["name"]
		if val != "" {
			runes := []rune(val)
			p.fields[name] = string(runes[:len(runes)-1])
		}
		if name == "name" {
			p.syncGeneratedID(oldName)
		}
		return resourcepage.IntentNone, true
	}
	if key.Type == tea.KeyRunes && len(key.Runes) > 0 {
		if name == "id" && !p.creating {
			return resourcepage.IntentNone, true
		}
		if name == "launcher" {
			p.openRefSelect(name)
			return p.updateRefSelect(message)
		}
		oldName := p.fields["name"]
		for _, r := range key.Runes {
			if unicode.IsControl(r) {
				continue
			}
			p.fields[name] += string(r)
		}
		if name == "name" {
			p.syncGeneratedID(oldName)
		}
		return resourcepage.IntentNone, true
	}
	return resourcepage.IntentNone, true
}

func (p *Page) syncGeneratedID(oldName string) {
	if !p.creating || p.draft == nil {
		return
	}
	used := formui.UsedConfigIDs(p.draft.LocalCommand())
	oldAuto := formui.UniqueID(oldName, "profile", used)
	if strings.TrimSpace(p.fields["id"]) == "" || p.fields["id"] == oldAuto {
		p.fields["id"] = formui.UniqueID(p.fields["name"], "profile", used)
	}
}

func (p *Page) cycleLauncher(delta int) {
	if next, ok := formui.CycleValue(p.fields["launcher"], launcherValues(), delta); ok {
		p.fields["launcher"] = next
	}
}

func (p *Page) openRefSelect(field string) {
	p.mode = modeRefSelect
	p.refField = field
	p.refFilter = ""
	p.filteringRef = false
	p.refChoices = p.filteredRefChoices(field, "")
	p.refCursor = 0
	current := p.fields[field]
	if field == "compatibility_transform_ids" {
		// multi-select starts empty filter list; selected shown from field value
		return
	}
	for i, choice := range p.refChoices {
		if choice == current {
			p.refCursor = i
			return
		}
	}
}

func (p *Page) filteredRefChoices(field, filter string) []string {
	all := p.ReferenceChoices(field)
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return all
	}
	lowerChoices := make([]string, len(all))
	for index, choice := range all {
		lowerChoices[index] = strings.ToLower(choice)
	}
	matches := fuzzy.Find(strings.ToLower(filter), lowerChoices)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, all[match.Index])
	}
	return out
}

func (p *Page) updateRefSelect(message tea.Msg) (resourcepage.Intent, bool) {
	if mouse, ok := message.(tea.MouseMsg); ok {
		return p.updateEditorMouse(mouse)
	}
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return resourcepage.IntentNone, true
	}
	switch key.String() {
	case "esc":
		if p.refFilter != "" {
			p.refFilter = ""
			p.filteringRef = false
			p.refChoices = p.filteredRefChoices(p.refField, "")
			p.refCursor = 0
			return resourcepage.IntentNone, true
		}
		p.mode = modeEdit
		p.refField = ""
		return resourcepage.IntentNone, true
	case "ctrl+s":
		p.mode = modeEdit
		p.refField = ""
		if err := p.SaveEditor(); err != nil {
			return resourcepage.IntentNone, true
		}
		return p.applyIntent(), true
	case "/":
		p.filteringRef = true
		p.refFilter = ""
		return resourcepage.IntentNone, true
	case "up", "k", "ctrl+p":
		if len(p.refChoices) > 0 {
			p.refCursor = (p.refCursor - 1 + len(p.refChoices)) % len(p.refChoices)
		}
		return resourcepage.IntentNone, true
	case "down", "j", "ctrl+n":
		if len(p.refChoices) > 0 {
			p.refCursor = (p.refCursor + 1) % len(p.refChoices)
		}
		return resourcepage.IntentNone, true
	case "enter":
		if p.refField == "compatibility_transform_ids" {
			p.mode = modeEdit
			p.refField = ""
			p.refFilter = ""
			p.filteringRef = false
			return resourcepage.IntentNone, true
		}
		if len(p.refChoices) == 0 {
			return resourcepage.IntentNone, true
		}
		choice := p.refChoices[p.refCursor]
		p.fields[p.refField] = choice
		p.mode = modeEdit
		p.refField = ""
		p.refFilter = ""
		p.filteringRef = false
		return resourcepage.IntentNone, true
	case " ":
		if p.refField == "compatibility_transform_ids" && len(p.refChoices) > 0 {
			p.toggleMultiRef(p.refChoices[p.refCursor])
		}
		return resourcepage.IntentNone, true
	case "backspace":
		if p.refFilter != "" {
			runes := []rune(p.refFilter)
			p.refFilter = string(runes[:len(runes)-1])
			p.refChoices = p.filteredRefChoices(p.refField, p.refFilter)
			p.refCursor = 0
		}
		return resourcepage.IntentNone, true
	}
	if key.Type == tea.KeyRunes && len(key.Runes) > 0 {
		p.filteringRef = true
		p.refFilter += string(key.Runes)
		p.refChoices = p.filteredRefChoices(p.refField, p.refFilter)
		p.refCursor = 0
		return resourcepage.IntentNone, true
	}
	return resourcepage.IntentNone, true
}

func (p *Page) updateEditorMouse(mouse tea.MouseMsg) (resourcepage.Intent, bool) {
	if mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
		return resourcepage.IntentNone, true
	}
	layout := p.editorLayout()
	if mouse.Y == layout.FooterLine {
		if mouse.X < p.width/2 {
			if p.mode == modeRefSelect {
				p.mode = modeEdit
				p.refField = ""
			}
			if err := p.SaveEditor(); err == nil {
				return p.applyIntent(), true
			}
		} else {
			p.mode = modeList
			p.status = ""
			p.Refresh()
		}
		return resourcepage.IntentNone, true
	}
	for name, line := range layout.FieldLines {
		if line == mouse.Y {
			for index, field := range p.fieldOrder {
				if field == name {
					p.fieldIndex = index
					break
				}
			}
			if name == "launcher" || isReferenceField(name) {
				p.openRefSelect(name)
			} else if p.mode == modeRefSelect {
				p.mode = modeEdit
				p.refField = ""
				p.refFilter = ""
				p.filteringRef = false
			}
			return resourcepage.IntentNone, true
		}
	}
	if option, ok := layout.OptionLines[mouse.Y]; ok && p.mode == modeRefSelect && option < len(p.refChoices) {
		p.refCursor = option
		choice := p.refChoices[option]
		if p.refField == "compatibility_transform_ids" {
			p.toggleMultiRef(choice)
		} else {
			p.fields[p.refField] = choice
			p.mode = modeEdit
			p.refField = ""
		}
	}
	return resourcepage.IntentNone, true
}

func (p *Page) applyIntent() resourcepage.Intent {
	if p.draft == nil || p.draft.Disconnected() || !p.draft.CanPublish() {
		return resourcepage.IntentNone
	}
	return resourcepage.IntentPublish
}

func (p *Page) toggleMultiRef(choice string) {
	parts, _ := decodeStringList(p.fields["compatibility_transform_ids"])
	found := -1
	for i, part := range parts {
		if part == choice {
			found = i
			break
		}
	}
	if found >= 0 {
		parts = append(parts[:found], parts[found+1:]...)
	} else {
		parts = append(parts, choice)
	}
	sort.Strings(parts)
	p.fields["compatibility_transform_ids"] = encodeStringList(parts)
}

// SetEditorField updates a draft editor field. ID is read-only on edit.
func (p *Page) SetEditorField(name, value string) error {
	if p.mode != modeEdit && p.mode != modeRefSelect {
		return fmt.Errorf("editor is closed")
	}
	if name == "id" && !p.creating {
		return fmt.Errorf("id is read-only on edit")
	}
	if !knownField(name) {
		return fmt.Errorf("unknown field %q", name)
	}
	oldName := p.fields["name"]
	p.fields[name] = value
	if name == "name" {
		p.syncGeneratedID(oldName)
	}
	return nil
}

// ReferenceChoices returns selectable IDs from the shared draft for a reference field.
func (p *Page) ReferenceChoices(field string) []string {
	cmd := p.draft.LocalCommand()
	switch field {
	case "launcher":
		return launcherValues()
	case "default_route_id":
		out := make([]string, 0, len(cmd.Routes))
		for _, route := range cmd.Routes {
			out = append(out, string(route.Id))
		}
		sort.Strings(out)
		return out
	case "model_projection_id":
		out := make([]string, 0, len(cmd.ModelProjections))
		for _, proj := range cmd.ModelProjections {
			out = append(out, string(proj.Id))
		}
		sort.Strings(out)
		return out
	case "compatibility_transform_ids":
		out := make([]string, 0, len(cmd.CompatibilityTransforms))
		for _, xf := range cmd.CompatibilityTransforms {
			out = append(out, string(xf.Id))
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

// SaveEditor validates fields and mutates the shared draft.
func (p *Page) SaveEditor() error {
	if p.creating && strings.TrimSpace(p.fields["id"]) == "" {
		p.fields["id"] = formui.UniqueID(p.fields["name"], "profile", formui.UsedConfigIDs(p.draft.LocalCommand()))
	}
	profile, err := p.buildProfile()
	if err != nil {
		p.status = err.Error()
		p.focusErrorField(p.status)
		p.inner.SetState(resourcepage.StateValidationError)
		p.inner.SetStatus(p.status)
		return err
	}
	if p.creating {
		p.draft.Mutate(func(cmd *generated.MutableConfigCommand) {
			cmd.ClientProfiles = append(cmd.ClientProfiles, profile)
		})
	} else {
		updated := false
		p.draft.Mutate(func(cmd *generated.MutableConfigCommand) {
			for i := range cmd.ClientProfiles {
				if cmd.ClientProfiles[i].Id == profile.Id {
					cmd.ClientProfiles[i] = profile
					updated = true
					return
				}
			}
		})
		if !updated {
			err := fmt.Errorf("$.id: profile %q no longer in draft", profile.Id)
			p.status = err.Error()
			p.focusErrorField(p.status)
			p.inner.SetState(resourcepage.StateValidationError)
			p.inner.SetStatus(p.status)
			return err
		}
	}
	p.mode = modeList
	p.status = ""
	p.inner.SetState(resourcepage.StateSuccess)
	p.Refresh()
	return nil
}

func (p *Page) focusErrorField(message string) {
	field, _ := formui.CleanError(message)
	for index, name := range p.fieldOrder {
		if name == field {
			p.fieldIndex = index
			return
		}
	}
}

func (p *Page) buildProfile() (generated.MutableClientProfile, error) {
	id := strings.TrimSpace(p.fields["id"])
	if !configIDPattern.MatchString(id) {
		return generated.MutableClientProfile{}, fmt.Errorf("$.id: must match ConfigID pattern")
	}
	if p.creating {
		for _, existing := range p.draft.LocalCommand().ClientProfiles {
			if string(existing.Id) == id {
				return generated.MutableClientProfile{}, fmt.Errorf("$.id: duplicate_id %q", id)
			}
		}
	}
	name := strings.TrimSpace(p.fields["name"])
	if name == "" {
		return generated.MutableClientProfile{}, fmt.Errorf("$.name: required")
	}
	launcher := generated.MutableClientProfileLauncher(strings.TrimSpace(p.fields["launcher"]))
	if !launcher.Valid() {
		return generated.MutableClientProfile{}, fmt.Errorf("$.launcher: invalid enum")
	}
	routeID := strings.TrimSpace(p.fields["default_route_id"])
	if !containsID(p.ReferenceChoices("default_route_id"), routeID) {
		return generated.MutableClientProfile{}, fmt.Errorf("$.default_route_id: invalid reference %q", routeID)
	}
	projID := strings.TrimSpace(p.fields["model_projection_id"])
	if !containsID(p.ReferenceChoices("model_projection_id"), projID) {
		return generated.MutableClientProfile{}, fmt.Errorf("$.model_projection_id: invalid reference %q", projID)
	}
	xfIDs, err := decodeStringList(p.fields["compatibility_transform_ids"])
	if err != nil {
		return generated.MutableClientProfile{}, fmt.Errorf("$.compatibility_transform_ids: %v", err)
	}
	seenXF := map[string]struct{}{}
	for _, xf := range xfIDs {
		if !containsID(p.ReferenceChoices("compatibility_transform_ids"), xf) {
			return generated.MutableClientProfile{}, fmt.Errorf("$.compatibility_transform_ids: invalid reference %q", xf)
		}
		if _, dup := seenXF[xf]; dup {
			return generated.MutableClientProfile{}, fmt.Errorf("$.compatibility_transform_ids: uniqueItems violated")
		}
		seenXF[xf] = struct{}{}
	}
	args, err := decodeStringList(p.fields["arguments"])
	if err != nil {
		return generated.MutableClientProfile{}, fmt.Errorf("$.arguments: %v", err)
	}
	if len(args) > 64 {
		return generated.MutableClientProfile{}, fmt.Errorf("$.arguments: maxItems 64")
	}
	for _, arg := range args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return generated.MutableClientProfile{}, fmt.Errorf("$.arguments: invalid item")
		}
	}
	env, err := parseEnv(p.fields["environment"])
	if err != nil {
		return generated.MutableClientProfile{}, err
	}
	if len(env) > 128 {
		return generated.MutableClientProfile{}, fmt.Errorf("$.environment: maxItems 128")
	}
	seenEnv := map[string]struct{}{}
	for _, item := range env {
		if len(item.Name) > 128 || len(item.Value) > 8192 {
			return generated.MutableClientProfile{}, fmt.Errorf("$.environment: maxLength violated")
		}
		if _, dup := seenEnv[item.Name]; dup {
			return generated.MutableClientProfile{}, fmt.Errorf("$.environment: duplicate name %q", item.Name)
		}
		seenEnv[item.Name] = struct{}{}
	}
	profile := generated.MutableClientProfile{
		Id:                        generated.ConfigID(id),
		Name:                      name,
		Launcher:                  launcher,
		Arguments:                 args,
		Environment:               env,
		DefaultRouteId:            generated.ConfigID(routeID),
		ModelProjectionId:         generated.ConfigID(projID),
		CompatibilityTransformIds: toConfigIDs(xfIDs),
	}
	if root := strings.TrimSpace(p.fields["native_config_root"]); root != "" {
		if strings.ContainsRune(root, 0) || strings.HasPrefix(root, "~") || !strings.HasPrefix(root, "/") {
			return generated.MutableClientProfile{}, fmt.Errorf("$.native_config_root: must be absolute canonical path")
		}
		profile.NativeConfigRoot = &root
	}
	return profile, nil
}

func (p *Page) View() string {
	switch p.mode {
	case modeEdit:
		return p.renderEditor()
	case modeRefSelect:
		return p.renderEditor()
	case modeDetails:
		return p.renderDetails()
	case modeConfirmDelete:
		return fmt.Sprintf(
			"Delete profile %s?\nDependent changes: omit client_profiles id=%s\nResulting resource count: %d\nenter confirm  esc cancel",
			p.confirmID, p.confirmID, p.confirmCount,
		)
	case modeBlockedDelete:
		return fmt.Sprintf(
			"Cannot delete profile %s — inbound references:\n%s\nesc/enter close",
			p.confirmID, strings.Join(p.blockedPaths, "\n"),
		)
	}
	return p.inner.View()
}

func (p *Page) renderEditor() string {
	return p.editorLayout().View
}

func (p *Page) editorLayout() formui.Layout {
	action := "form.action.edit"
	if p.creating {
		action = "form.action.create"
	}
	title := i18n.T(action, map[string]string{"resource": i18n.T("form.resource.profile")})
	errorField, errorDetail := formui.CleanError(p.status)
	notice := ""
	if p.draft != nil && p.draft.InConflict() {
		notice = "generation conflict - reapply or reload"
	}
	if p.status != "" && errorField == "" && !strings.HasPrefix(p.status, "generation conflict") {
		if notice != "" {
			notice += "; "
		}
		notice += errorDetail
	}
	fields := make([]formui.Field, 0, len(p.fieldOrder))
	for _, name := range p.fieldOrder {
		field := formui.Field{ID: name, Label: formui.FriendlyLabel(name), Value: p.fields[name], Section: "Basics", Required: true}
		switch name {
		case "launcher":
			field.Kind = formui.Select
		case "default_route_id":
			field.Kind = formui.Reference
			field.Help = "Choose the traffic route used by this CLI"
		case "model_projection_id":
			field.Kind, field.Section = formui.Reference, "Models"
			field.Help = "Models shown to the CLI"
		case "compatibility_transform_ids":
			field.Kind, field.Section, field.Advanced = formui.MultiSelect, "Advanced", true
		case "arguments", "environment":
			field.Kind, field.Section, field.Advanced = formui.Repeater, "Advanced", true
			field.Required = false
		case "native_config_root":
			field.Section, field.Advanced, field.Required = "Advanced", true, false
			field.Placeholder = "Automatic"
		case "id":
			field.Section, field.Advanced = "Advanced", true
			field.ReadOnly = !p.creating
			field.Help = "Generated from the name; customize before saving if needed"
		}
		if errorField == name {
			field.Error = errorDetail
		}
		if p.mode == modeRefSelect && name == p.refField {
			field.Expanded = true
			field.Options = nil
			selected := map[string]bool{}
			if name == "compatibility_transform_ids" {
				for _, value := range parseSelectedIDs(p.fields[name]) {
					selected[value] = true
				}
			}
			for _, choice := range p.refChoices {
				field.Options = append(field.Options, formui.Option{Label: choice, Value: choice, Selected: selected[choice]})
			}
			field.OptionIndex = p.refCursor
			field.EmptyText = i18n.T("form.select.none")
			if p.filteringRef || p.refFilter != "" {
				field.Help = i18n.T("form.select.search", map[string]string{"query": p.refFilter})
				field.EmptyText = i18n.T("form.select.empty")
			}
		}
		fields = append(fields, field)
	}
	footer := i18n.T("form.footer.profile")
	if p.CurrentField() == "launcher" && p.mode != modeRefSelect {
		footer += "  " + i18n.T("form.select.cycle")
	}
	return formui.Render(formui.Spec{
		Title: title, Context: i18n.T("form.context.profile"), Notice: notice, Fields: fields, Focus: p.fieldIndex,
		AdvancedExpanded: p.fieldIndex >= 4, Width: p.width, Height: p.height,
		Footer: footer,
	})
}

func (p *Page) renderDetails() string {
	width, height := p.inner.Size()
	profile, ok := p.findProfile(p.inner.SelectedID())
	if !ok {
		return detailpane.Model{
			Title: detailpane.KindLabel("Profile"),
			Sections: []detailpane.Section{{
				Title: "Status",
				Rows:  []detailpane.Row{{Label: "error", Value: i18n.T("detail.value.not_found")}},
			}},
			Width:  width,
			Height: height,
		}.View()
	}
	fields := profileToFields(profile)
	names := fieldNames()
	name := fields["name"]
	if name == "" {
		name = fields["id"]
	}
	return detailpane.Model{
		Title:    detailpane.NamedTitle("Profile", name),
		Summary:  detailpane.RowsFromKeys([]string{"id", "name"}, fields),
		Sections: []detailpane.Section{{Title: "Configuration", Rows: detailpane.RowsFromKeys(names, fields)}},
		Width:    width,
		Height:   height,
	}.View()
}

func fieldNames() []string {
	return []string{"name", "launcher", "default_route_id", "model_projection_id", "native_config_root", "arguments", "environment", "compatibility_transform_ids", "id"}
}

func parseSelectedIDs(value string) []string {
	items, _ := decodeStringList(value)
	return items
}

func knownField(name string) bool {
	for _, field := range fieldNames() {
		if field == name {
			return true
		}
	}
	return false
}

func isReferenceField(name string) bool {
	switch name {
	case "default_route_id", "model_projection_id", "compatibility_transform_ids":
		return true
	default:
		return false
	}
}

func profileToFields(profile generated.MutableClientProfile) map[string]string {
	root := ""
	if profile.NativeConfigRoot != nil {
		root = *profile.NativeConfigRoot
	}
	xf := make([]string, 0, len(profile.CompatibilityTransformIds))
	for _, id := range profile.CompatibilityTransformIds {
		xf = append(xf, string(id))
	}
	envParts := make([]string, 0, len(profile.Environment))
	for _, item := range profile.Environment {
		envParts = append(envParts, item.Name+"="+item.Value)
	}
	return map[string]string{
		"id":                          string(profile.Id),
		"name":                        profile.Name,
		"launcher":                    string(profile.Launcher),
		"arguments":                   encodeStringList(profile.Arguments),
		"environment":                 encodeStringList(envParts),
		"default_route_id":            string(profile.DefaultRouteId),
		"model_projection_id":         string(profile.ModelProjectionId),
		"compatibility_transform_ids": encodeStringList(xf),
		"native_config_root":          root,
	}
}

// encodeStringList uses JSON so values may contain commas/semicolons safely.
func encodeStringList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeStringList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" {
		return []string{}, nil
	}
	if strings.HasPrefix(value, "[") {
		var out []string
		if err := json.Unmarshal([]byte(value), &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	// Convenience for tests/simple single IDs typed without JSON.
	return []string{value}, nil
}

func parseEnv(value string) ([]generated.EnvironmentVariableConfig, error) {
	parts, err := decodeStringList(value)
	if err != nil {
		return nil, fmt.Errorf("$.environment: %v", err)
	}
	out := make([]generated.EnvironmentVariableConfig, 0, len(parts))
	for _, part := range parts {
		name, val, ok := strings.Cut(part, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("$.environment: invalid entry %q", part)
		}
		if !envNamePattern.MatchString(name) {
			return nil, fmt.Errorf("$.environment.name: invalid pattern %q", name)
		}
		if reservedEnvName(name) {
			return nil, fmt.Errorf("$.environment.name: reserved %q", name)
		}
		if strings.ContainsRune(val, 0) {
			return nil, fmt.Errorf("$.environment.value: invalid pattern")
		}
		out = append(out, generated.EnvironmentVariableConfig{Name: name, Value: val})
	}
	return out, nil
}

var reservedEnvExact = map[string]struct{}{
	"AGENT_HARBOR_URL": {}, "AGENT_HARBOR_CONFIG_DIR": {}, "AGENT_HARBOR_EXECUTABLE": {},
	"AGENT_HARBOR_INSTANCE_ID": {}, "AGENT_HARBOR_AGENT_SESSION_ID": {},
	"AGENT_HARBOR_HOOK_CAPABILITY": {}, "AGENT_HARBOR_HOOK_CAPABILITY_EPOCH": {},
	"AGENT_HARBOR_HOOK_MAILBOX_DIR": {},
	"OPENAI_API_KEY":                {}, "OPENAI_BASE_URL": {},
	"ANTHROPIC_API_KEY": {}, "ANTHROPIC_BASE_URL": {}, "ANTHROPIC_AUTH_TOKEN": {},
	"CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST": {}, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": {},
	"CODEX_HOME": {}, "CLAUDE_CONFIG_DIR": {},
	"CODEX_MANAGED_PACKAGE_ROOT": {}, "CODEX_MANAGED_BY_NPM": {}, "CODEX_MANAGED_BY_BUN": {},
	"CODEX_MANAGED_BY_PNPM": {},
	"PATH":                  {}, "TERM": {}, "TMUX": {}, "TMUX_PANE": {}, "SHELL": {},
}

var reservedEnvSuffix = regexp.MustCompile(`_(KEY|TOKEN|SECRET|PASSWORD)$`)

func reservedEnvName(name string) bool {
	if _, ok := reservedEnvExact[name]; ok {
		return true
	}
	return reservedEnvSuffix.MatchString(name)
}

func toConfigIDs(values []string) []generated.ConfigID {
	out := make([]generated.ConfigID, len(values))
	for i, value := range values {
		out[i] = generated.ConfigID(value)
	}
	return out
}

func containsID(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func launcherValues() []string {
	return []string{"codex", "claude", "opencode", "pi", "grok"}
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}
