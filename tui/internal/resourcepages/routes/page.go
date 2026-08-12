package routes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayEditor
	overlayDetails
	overlayDeps
	overlayConfirmDelete
	overlayRestoreSimple
)

// Page is a Bubble Tea model for the Routes family.
type Page struct {
	draft                *configdraft.Draft
	statusProvider       RouteStatusProvider
	discoverModels       ModelDiscoverer
	trafficRuleReferrers func(string) []string
	kind                 Kind
	tablePage            *resourcepage.Page
	width                int
	height               int
	overlay              overlayKind
	editor               editorState
	depsPaths            []string
	detailsID            string
	deleteID             string
	restoreID            string
	overlayScroll        int
	lastIntent           resourcepage.Intent
	forcedState          resourcepage.State
	forceState           bool
	status               string
	advancedUnlocked     bool
}

type targetModelsLoadedMsg struct {
	targetID string
	models   []string
	err      error
}

func targetModelsValueKey(targetID string) string   { return "__target_models." + targetID }
func targetModelsPendingKey(targetID string) string { return "__target_models_pending." + targetID }
func encodeTargetModels(models []string) string     { return strings.Join(models, "\x1f") }
func decodeTargetModels(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "\x1f")
}

func (p *Page) Init() tea.Cmd { return nil }

func (p *Page) Kind() Kind { return p.kind }

func (p *Page) SetKind(kind Kind) {
	p.kind = kind
	p.advancedUnlocked = false
	p.CancelOverlay()
	p.rebuildTable()
	p.Refresh()
	if p.kind != KindTrafficRules {
		p.SetStatus("Managed members are read-only · u unlock")
	}
}

func (p *Page) managedMemberLocked(id string) bool {
	if p.kind == KindTrafficRules || p.advancedUnlocked || p.draft == nil || id == "" {
		return false
	}
	kind, ok := routeManagedResourceKind(p.kind)
	if !ok {
		return false
	}
	_, owned := managedconfig.OwnerOf(p.draft.LocalCommand(), kind, id)
	return owned
}

func routeManagedResourceKind(kind Kind) (generated.ManagedResourceRefKind, bool) {
	switch kind {
	case KindRoutes:
		return generated.ManagedResourceRefKindRoute, true
	case KindBackendSets:
		return generated.ManagedResourceRefKindBackendSet, true
	case KindContentPolicies:
		return generated.ManagedResourceRefKindContentPolicy, true
	case KindModelPolicies:
		return generated.ManagedResourceRefKindModelPolicy, true
	case KindModelProjections:
		return generated.ManagedResourceRefKindModelProjection, true
	case KindTransforms:
		return generated.ManagedResourceRefKindCompatibilityTransform, true
	default:
		return "", false
	}
}

func (p *Page) SetSize(width, height int) {
	p.width = max(1, width)
	p.height = max(8, height)
	if p.tablePage != nil {
		p.tablePage.SetSize(p.width, p.tableHeight())
	}
}

func (p *Page) State() resourcepage.State {
	if p.forceState {
		return p.forcedState
	}
	if p.tablePage == nil {
		return resourcepage.StateLoading
	}
	return p.tablePage.State()
}

func (p *Page) SetState(state resourcepage.State) {
	p.forceState = true
	p.forcedState = state
	if p.tablePage != nil {
		p.tablePage.SetState(state)
	}
}

func (p *Page) SetStatus(status string) {
	p.status = status
	if p.tablePage != nil {
		p.tablePage.SetStatus(status)
	}
}

func (p *Page) LastIntent() resourcepage.Intent { return p.lastIntent }

func (p *Page) Table() *resourceview.Model {
	if p.tablePage == nil {
		return nil
	}
	return p.tablePage.Table()
}

func (p *Page) StripHeight() int { return stripHeight() }

func (p *Page) BannerOffset() int {
	if p.tablePage == nil {
		return 0
	}
	// Match resourcepage.Page.overlayLines: state banner + optional status line.
	lines := 0
	switch p.tablePage.State() {
	case resourcepage.StateDisconnected, resourcepage.StateEmpty, resourcepage.StateLoading,
		resourcepage.StateValidationError, resourcepage.StatePublicationError, resourcepage.StateStale:
		lines++
	}
	if p.status != "" {
		lines++
	}
	return lines
}

func (p *Page) TableHeaderY() int {
	// relative to table surface (after strip+banner handled by Update)
	return 1
}

func (p *Page) VisibleIDs() []string {
	if p.tablePage == nil {
		return nil
	}
	return p.tablePage.Table().VisibleRowIDs()
}

func (p *Page) SelectedID() string {
	if p.tablePage == nil {
		return ""
	}
	return p.tablePage.SelectedID()
}

func (p *Page) SelectID(id string) {
	if p.tablePage == nil {
		return
	}
	table := p.tablePage.Table()
	ids := table.VisibleRowIDs()
	for i, candidate := range ids {
		if candidate == id {
			// move cursor via home then down
			p.tablePage.Update(tea.KeyMsg{Type: tea.KeyHome})
			for j := 0; j < i; j++ {
				p.tablePage.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			return
		}
	}
}

func (p *Page) ShowingDetails() bool { return p.overlay == overlayDetails }

func (p *Page) CancelOverlay() {
	p.overlay = overlayNone
	p.depsPaths = nil
	p.detailsID = ""
	p.deleteID = ""
	p.restoreID = ""
	p.overlayScroll = 0
	p.editor = editorState{}
	p.SetStatus("")
	if p.forceState {
		p.forceState = false
		p.Refresh()
	}
}

func (p *Page) BeginCreate() {
	if p.draft.Disconnected() {
		p.SetStatus("Disconnected: editing is unavailable")
		return
	}
	if p.kind == KindTrafficRules {
		p.overlay = overlayEditor
		p.editor = newTrafficRuleCreateEditor()
		p.lastIntent = resourcepage.IntentCreate
		return
	}
	p.overlay = overlayEditor
	p.editor = newEditor(p.kind, editorCreate, "", p.draft)
	p.lastIntent = resourcepage.IntentCreate
}

func (p *Page) BeginEdit() {
	if p.draft.Disconnected() {
		p.SetStatus("Disconnected: editing is unavailable")
		return
	}
	id := p.SelectedID()
	if id == "" {
		return
	}
	if p.managedMemberLocked(id) {
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return
	}
	if p.kind == KindTrafficRules {
		rule, ok := findTrafficRule(p.draft.LocalCommand(), id, p.statusProvider)
		if !ok || !rule.Editable {
			p.SetStatus("Custom internal configuration · press R to preview restoring the simple structure")
			return
		}
		p.overlay = overlayEditor
		p.editor = newTrafficRuleEditor(rule, p.draft)
	} else {
		p.overlay = overlayEditor
		p.editor = newEditor(p.kind, editorEdit, id, p.draft)
	}
	p.lastIntent = resourcepage.IntentEdit
}

func (p *Page) OpenDetails() {
	id := p.SelectedID()
	if id == "" {
		return
	}
	p.overlay = overlayDetails
	p.detailsID = id
	p.overlayScroll = 0
	p.lastIntent = resourcepage.IntentDetails
}

func (p *Page) SetEditorValues(values map[string]string) {
	p.editor.setValues(values)
}

func (p *Page) EditorIDEditable() bool { return p.editor.idEditable() }

func (p *Page) EditorFieldNames() []string { return p.editor.fieldNames() }

func (p *Page) SaveEditor() error {
	if p.editor.kind == KindTrafficRules {
		var err error
		if p.editor.mode == editorCreate {
			err = p.saveTrafficRuleCreateEditor()
		} else {
			err = p.saveTrafficRuleEditor()
		}
		if err == nil {
			p.lastIntent = resourcepage.IntentPublish
		}
		return err
	}
	creating := p.editor.mode == editorCreate
	if creating && strings.TrimSpace(p.editor.values["id"]) == "" {
		p.editor.values["id"] = formui.UniqueID(p.editor.values["name"], routeIDPrefix(p.editor.kind), formui.UsedConfigIDs(p.draft.LocalCommand()))
	}
	if err := validateValues(p.editor.kind, p.editor.values, creating, p.draft); err != nil {
		p.editor.err = err.Error()
		p.forceState = true
		p.forcedState = resourcepage.StateValidationError
		p.SetStatus(err.Error())
		if p.tablePage != nil {
			p.tablePage.SetState(resourcepage.StateValidationError)
		}
		return err
	}
	var err error
	if creating {
		err = applyCreate(p.draft, p.editor.kind, p.editor.values)
	} else {
		err = applyEdit(p.draft, p.editor.kind, p.editor.id, p.editor.values)
	}
	if err != nil {
		p.editor.err = err.Error()
		p.forceState = true
		p.forcedState = resourcepage.StateValidationError
		p.SetStatus(err.Error())
		if p.tablePage != nil {
			p.tablePage.SetState(resourcepage.StateValidationError)
		}
		return err
	}
	p.forceState = false
	p.SetStatus("")
	p.CancelOverlay()
	p.Refresh()
	p.lastIntent = resourcepage.IntentPublish
	return nil
}

func routeIDPrefix(kind Kind) string {
	switch kind {
	case KindBackendSets:
		return "backend"
	case KindContentPolicies:
		return "content"
	case KindModelPolicies:
		return "model-policy"
	case KindModelProjections:
		return "models"
	case KindTransforms:
		return "transform"
	default:
		return "route"
	}
}

func (p *Page) saveTrafficRuleEditor() error {
	if err := applyTrafficRule(p.draft, p.editor.id, p.editor.values); err != nil {
		p.editor.err = err.Error()
		p.forceState = true
		p.forcedState = resourcepage.StateValidationError
		p.SetStatus(err.Error())
		return err
	}
	p.forceState = false
	p.CancelOverlay()
	p.Refresh()
	return nil
}

func (p *Page) saveTrafficRuleCreateEditor() error {
	if err := createSimpleTrafficRule(p.draft, p.editor.values); err != nil {
		p.editor.err = err.Error()
		p.forceState = true
		p.forcedState = resourcepage.StateValidationError
		p.SetStatus(err.Error())
		return err
	}
	p.forceState = false
	p.CancelOverlay()
	p.Refresh()
	return nil
}

// TryDelete is the programmatic delete helper used by tests. Returns (blocked, paths).
// When blocked, draft is unchanged and the dependency overlay is shown. When unblocked,
// it deletes immediately (UI path must use beginDeleteIntent instead).
func (p *Page) TryDelete() (bool, []string) {
	id := p.SelectedID()
	if id == "" {
		return false, nil
	}
	if p.managedMemberLocked(id) {
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return true, []string{"managed object ownership"}
	}
	paths := p.inboundRefs(id)
	p.lastIntent = resourcepage.IntentDelete
	if len(paths) > 0 {
		p.overlay = overlayDeps
		p.depsPaths = paths
		p.deleteID = id
		p.overlayScroll = 0
		return true, paths
	}
	_ = applyDelete(p.draft, p.kind, id)
	p.CancelOverlay()
	p.Refresh()
	return false, nil
}

func (p *Page) remainingAfterConfirmDelete() int {
	n := countFor(p.draft, p.kind)
	if !resourceExists(p.draft, p.kind, p.deleteID) {
		return n
	}
	// Inbound refs mean delete will not apply — remaining stays the live count.
	if len(p.inboundRefs(p.deleteID)) > 0 {
		return n
	}
	return max(0, n-1)
}

func (p *Page) confirmDeleteDependentPaths() []string {
	if p.deleteID == "" || !resourceExists(p.draft, p.kind, p.deleteID) {
		return nil
	}
	return p.inboundRefs(p.deleteID)
}

// confirmApplyDelete runs the confirm Enter/mouse path: re-check inbound refs,
// refuse silent success when the ID was already removed from the shared draft.
func (p *Page) confirmApplyDelete() {
	id := p.deleteID
	label := p.kind.Label()
	if p.managedMemberLocked(id) {
		p.CancelOverlay()
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return
	}
	if !resourceExists(p.draft, p.kind, id) {
		p.CancelOverlay()
		p.SetStatus(fmt.Sprintf("%s %s: already removed", label, id))
		p.Refresh()
		return
	}
	paths := p.inboundRefs(id)
	if len(paths) > 0 {
		p.overlay = overlayDeps
		p.depsPaths = paths
		p.overlayScroll = 0
		return
	}
	if !applyDelete(p.draft, p.kind, id) {
		p.CancelOverlay()
		p.SetStatus(fmt.Sprintf("%s %s: already removed", label, id))
		p.Refresh()
		return
	}
	p.CancelOverlay()
	p.Refresh()
	p.lastIntent = resourcepage.IntentPublish
}

// beginDeleteIntent handles keyboard/footer delete: blocked → deps dialog;
// unblocked → confirm overlay (never silent apply).
func (p *Page) beginDeleteIntent() {
	id := p.SelectedID()
	if id == "" {
		return
	}
	if p.managedMemberLocked(id) {
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return
	}
	paths := p.inboundRefs(id)
	p.deleteID = id
	p.lastIntent = resourcepage.IntentDelete
	p.overlayScroll = 0
	if len(paths) > 0 {
		p.overlay = overlayDeps
		p.depsPaths = paths
		return
	}
	p.overlay = overlayConfirmDelete
	p.depsPaths = nil
}

func (p *Page) inboundRefs(id string) []string {
	paths := InboundRefs(p.draft, p.kind, id)
	if p.kind != KindTrafficRules || p.trafficRuleReferrers == nil {
		return paths
	}
	rule, ok := findTrafficRule(p.draft.LocalCommand(), id, p.statusProvider)
	if !ok {
		return paths
	}
	return append(paths, p.trafficRuleReferrers(rule.ProfileID)...)
}

func (p *Page) Refresh() {
	if p.tablePage == nil {
		p.rebuildTable()
	}
	rows := rowsForStatus(p.draft, p.kind, p.statusProvider)
	p.tablePage.SetRows(rows)
	p.tablePage.SetDirty(p.draft.DomainDirty(configdraft.DomainRoutes))

	if p.forceState {
		p.tablePage.SetState(p.forcedState)
		return
	}
	if p.draft.Disconnected() {
		p.tablePage.SetState(resourcepage.StateDisconnected)
		return
	}
	if p.draft.InConflict() {
		p.tablePage.SetState(resourcepage.StatePublicationError)
		return
	}
	if len(rows) == 0 {
		p.tablePage.SetState(resourcepage.StateEmpty)
		return
	}
	p.tablePage.SetState(resourcepage.StateSuccess)
}

func (p *Page) rebuildTable() {
	actions := resourcepage.ActionSet{Create: true, Edit: true, Delete: true, Publish: false, Details: true, Filter: true, Mark: true}
	if p.kind == KindTrafficRules {
		actions.Details = false
	}
	p.tablePage = resourcepage.New(resourcepage.Spec{
		Title:   p.kind.Title(),
		Scope:   "all",
		Columns: columnsFor(p.kind),
		Actions: actions,
		Domain:  string(configdraft.DomainRoutes),
	})
	p.tablePage.SetSize(p.width, p.tableHeight())
}

func (p *Page) tableHeight() int {
	return max(6, p.height-stripHeight())
}

func (p *Page) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.lastIntent = resourcepage.IntentNone
	if msg == nil {
		return p, nil
	}
	if loaded, ok := msg.(targetModelsLoadedMsg); ok {
		if p.overlay != overlayEditor || p.editor.values == nil {
			return p, nil
		}
		delete(p.editor.values, targetModelsPendingKey(loaded.targetID))
		if loaded.err == nil {
			p.editor.values[targetModelsValueKey(loaded.targetID)] = encodeTargetModels(loaded.models)
		} else if p.overlay == overlayEditor {
			p.SetStatus("Model discovery unavailable; enter a custom model ID")
		}
		return p, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		if p.overlay != overlayNone {
			return p.updateOverlayKey(key)
		}
		if p.kind != KindTrafficRules && key.String() == "u" && (p.tablePage == nil || !p.tablePage.Table().Filtering()) {
			p.advancedUnlocked = !p.advancedUnlocked
			if p.advancedUnlocked {
				p.SetStatus("Advanced editing unlocked for this page")
			} else {
				p.SetStatus("Managed members are read-only · u unlock")
			}
			return p, nil
		}
		if p.kind == KindTrafficRules && key.String() == "R" && (p.tablePage == nil || !p.tablePage.Table().Filtering()) {
			id := p.SelectedID()
			if rule, ok := findTrafficRule(p.draft.LocalCommand(), id, p.statusProvider); ok && !rule.Editable {
				if _, err := previewRestoreTrafficRule(p.draft.LocalCommand(), id); err != nil {
					p.SetStatus("Cannot restore simple structure: " + err.Error())
					return p, nil
				}
				p.restoreID = id
				p.overlay = overlayRestoreSimple
				return p, nil
			}
		}
		switch {
		case key.Alt && key.Type == tea.KeyRight:
			p.shiftKind(1)
			return p, nil
		case key.Alt && key.Type == tea.KeyLeft:
			p.shiftKind(-1)
			return p, nil
		}
	}

	if mouse, ok := msg.(tea.MouseMsg); ok {
		if p.overlay != overlayNone {
			model, cmd := p.handleOverlayMouse(mouse)
			if p.overlay == overlayEditor {
				cmd = tea.Batch(cmd, p.ensureEditorModelCatalogs())
			}
			return model, cmd
		}
		if kind, hit := hitTestStrip(mouse.X, mouse.Y, p.width); hit && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			p.SetKind(kind)
			return p, nil
		}
		// Adjust mouse Y into table coordinates (strip + optional banner).
		adjusted := mouse
		adjusted.Y -= stripHeight()
		intent, _ := p.tablePage.Update(adjusted)
		return p.handleIntent(intent)
	}

	intent, consumed := p.tablePage.Update(msg)
	if consumed {
		return p.handleIntent(intent)
	}
	return p, nil
}

func (p *Page) handleIntent(intent resourcepage.Intent) (tea.Model, tea.Cmd) {
	p.lastIntent = intent
	switch intent {
	case resourcepage.IntentCreate:
		p.BeginCreate()
	case resourcepage.IntentEdit:
		p.BeginEdit()
	case resourcepage.IntentDelete:
		p.beginDeleteIntent()
	case resourcepage.IntentDetails:
		p.OpenDetails()
	case resourcepage.IntentPublish:
		if p.draft.Disconnected() || !p.draft.CanPublish() {
			p.lastIntent = resourcepage.IntentNone
		}
	}
	return p, nil
}

func (p *Page) handleOverlayMouse(mouse tea.MouseMsg) (tea.Model, tea.Cmd) {
	if mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
		return p, nil
	}
	// Overlay content is rendered below the strip; footer is always last viewport row.
	y := mouse.Y - stripHeight()
	contentH := p.height - stripHeight()
	footerY := contentH - 1
	switch p.overlay {
	case overlayConfirmDelete:
		if y == footerY {
			// Left half ≈ confirm, right half ≈ cancel (aligned with painted footer).
			if mouse.X < p.width/2 {
				p.confirmApplyDelete()
			} else {
				p.CancelOverlay()
			}
		}
	case overlayRestoreSimple:
		if y == footerY {
			if mouse.X < p.width/2 {
				if err := restoreTrafficRuleSimple(p.draft, p.restoreID); err != nil {
					p.SetStatus("Restore failed: " + err.Error())
					return p, nil
				}
				p.CancelOverlay()
				p.Refresh()
				p.lastIntent = resourcepage.IntentPublish
			} else {
				p.CancelOverlay()
			}
		}
	case overlayDeps, overlayDetails:
		if y == footerY {
			p.CancelOverlay()
		}
	case overlayEditor:
		layout := p.editor.formLayout(p.width, contentH, p.draft)
		if y == layout.FooterLine || y == footerY {
			// Editor footer: cancel on right half.
			if mouse.X >= p.width/2 {
				p.CancelOverlay()
			} else {
				_ = p.SaveEditor()
			}
			return p, nil
		}
		for name, line := range layout.FieldLines {
			if line == y {
				next := routeFieldIndex(p.editor.fieldNames(), name)
				if next != p.editor.cursor {
					p.editor.refIndex = 0
					p.editor.refFilter = ""
					p.editor.selectorOpen = false
				}
				p.editor.cursor = next
				p.editor.openSelector(p.draft)
				return p, nil
			}
		}
		if option, ok := layout.OptionLines[y]; ok {
			p.editor.refIndex = option
			// logical_models uses click as token selection for removal/reorder.
			if p.editor.focusedName() != "logical_models" {
				_ = p.editor.applyRef(p.draft)
				if !isUniqueArrayField(p.editor.focusedName()) {
					p.editor.selectorOpen = false
				}
			}
		}
	}
	return p, nil
}

func routeFieldIndex(fields []string, name string) int {
	for index, field := range fields {
		if field == name {
			return index
		}
	}
	return 0
}

func (p *Page) updateOverlayKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch p.overlay {
	case overlayEditor:
		p.handleEditorKey(key)
		return p, p.ensureEditorModelCatalogs()
	case overlayDetails, overlayDeps:
		// Prefer key.Type for arrows/Esc — KeyUp.String() is also "up", so a
		// second string switch would double-step overlayScroll.
		switch key.Type {
		case tea.KeyUp:
			if p.overlayScroll > 0 {
				p.overlayScroll--
			}
		case tea.KeyDown:
			p.overlayScroll++
		case tea.KeyEsc:
			p.CancelOverlay()
		default:
			switch key.String() {
			case "esc":
				p.CancelOverlay()
			case "up":
				if p.overlayScroll > 0 {
					p.overlayScroll--
				}
			case "down":
				p.overlayScroll++
			}
		}
	case overlayConfirmDelete:
		// Arrows scroll live deps; Enter still runs confirmApplyDelete (may pivot to deps).
		switch key.Type {
		case tea.KeyUp:
			if p.overlayScroll > 0 {
				p.overlayScroll--
			}
		case tea.KeyDown:
			p.overlayScroll++
		case tea.KeyEsc:
			p.CancelOverlay()
		case tea.KeyEnter:
			p.confirmApplyDelete()
		default:
			switch key.String() {
			case "esc":
				p.CancelOverlay()
			case "enter":
				p.confirmApplyDelete()
			case "up":
				if p.overlayScroll > 0 {
					p.overlayScroll--
				}
			case "down":
				p.overlayScroll++
			}
		}
	case overlayRestoreSimple:
		switch key.String() {
		case "esc":
			p.CancelOverlay()
		case "enter":
			if err := restoreTrafficRuleSimple(p.draft, p.restoreID); err != nil {
				p.SetStatus("Restore failed: " + err.Error())
				return p, nil
			}
			p.CancelOverlay()
			p.Refresh()
			p.lastIntent = resourcepage.IntentPublish
		}
	}
	return p, nil
}

func (p *Page) ensureEditorModelCatalogs() tea.Cmd {
	if p.discoverModels == nil || p.overlay != overlayEditor || p.editor.kind != KindTrafficRules || p.editor.mode != editorCreate {
		return nil
	}
	strategy := p.editor.values["model_strategy"]
	if strategy != modelOverride && strategy != modelMap {
		return nil
	}
	commands := []tea.Cmd{}
	for _, targetID := range []string{p.editor.values["primary_target_id"], p.editor.values["backup_target_id"]} {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" || p.editor.values[targetModelsValueKey(targetID)] != "" || p.editor.values[targetModelsPendingKey(targetID)] != "" {
			continue
		}
		p.editor.values[targetModelsPendingKey(targetID)] = "1"
		discover := p.discoverModels
		id := targetID
		commands = append(commands, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			models, err := discover(ctx, id)
			return targetModelsLoadedMsg{targetID: id, models: models, err: err}
		})
	}
	return tea.Batch(commands...)
}

func (p *Page) handleEditorKey(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEsc:
		if p.editor.selectorOpen {
			if p.editor.refFilter != "" {
				p.editor.refFilter = ""
				p.editor.refIndex = 0
			} else {
				p.editor.selectorOpen = false
			}
			return
		}
		p.CancelOverlay()
		return
	case tea.KeyCtrlS:
		_ = p.SaveEditor()
		return
	case tea.KeyEnter:
		if p.editor.focusedIsSelector(p.draft) {
			if !p.editor.selectorOpen {
				p.editor.openSelector(p.draft)
				return
			}
			if !p.editor.applyRef(p.draft) {
				p.editor.err = "no matching option"
			} else {
				p.editor.selectorOpen = false
			}
			return
		}
		_ = p.SaveEditor()
		return
	case tea.KeyTab:
		p.editor.moveField(1)
		return
	case tea.KeyShiftTab:
		p.editor.moveField(-1)
		return
	case tea.KeyBackspace:
		oldName := p.editor.values["name"]
		p.editor.backspace()
		p.syncRouteID(oldName)
		return
	case tea.KeyDelete:
		p.editor.deleteForward()
		return
	case tea.KeyCtrlN:
		_ = p.editor.addArrayItem()
		return
	case tea.KeyCtrlX:
		_ = p.editor.removeArrayItem()
		return
	case tea.KeyCtrlUp:
		_ = p.editor.reorderArrayItem(-1)
		return
	case tea.KeyCtrlDown:
		_ = p.editor.reorderArrayItem(1)
		return
	case tea.KeyUp:
		if p.editor.selectorOpen && p.editor.focusedIsSelector(p.draft) && p.editor.moveRef(-1, p.draft) {
			return
		}
		p.editor.moveField(-1)
		return
	case tea.KeyDown:
		if p.editor.selectorOpen && p.editor.focusedIsSelector(p.draft) && p.editor.moveRef(1, p.draft) {
			return
		}
		p.editor.moveField(1)
		return
	case tea.KeyLeft:
		if !key.Alt {
			_ = p.editor.cycleFocusedSelect(p.draft, -1)
		}
		return
	case tea.KeyRight:
		if !key.Alt {
			_ = p.editor.cycleFocusedSelect(p.draft, 1)
		}
		return
	case tea.KeyRunes:
		if key.Alt {
			return
		}
		if len(key.Runes) == 1 && key.Runes[0] == ' ' && p.editor.focusedName() == "required_capabilities" {
			if !p.editor.selectorOpen {
				p.editor.openSelector(p.draft)
				return
			}
			_ = p.editor.toggleMultiSelect(p.draft)
			return
		}
		oldName := p.editor.values["name"]
		p.editor.appendRunes(key.Runes)
		p.syncRouteID(oldName)
		return
	}

	// Fallback for string-matched keys (some terminals).
	switch key.String() {
	case "esc":
		if p.editor.selectorOpen {
			if p.editor.refFilter != "" {
				p.editor.refFilter = ""
				p.editor.refIndex = 0
			} else {
				p.editor.selectorOpen = false
			}
			return
		}
		p.CancelOverlay()
	case "ctrl+s":
		_ = p.SaveEditor()
	case "up":
		if p.editor.selectorOpen && p.editor.focusedIsSelector(p.draft) && p.editor.moveRef(-1, p.draft) {
			return
		}
		p.editor.moveField(-1)
	case "down":
		if p.editor.selectorOpen && p.editor.focusedIsSelector(p.draft) && p.editor.moveRef(1, p.draft) {
			return
		}
		p.editor.moveField(1)
	case "backspace":
		oldName := p.editor.values["name"]
		p.editor.backspace()
		p.syncRouteID(oldName)
	case "delete":
		p.editor.deleteForward()
	}
}

func (p *Page) syncRouteID(oldName string) {
	if p.editor.mode != editorCreate || p.editor.kind == KindTrafficRules || p.editor.focusedName() != "name" {
		return
	}
	used := formui.UsedConfigIDs(p.draft.LocalCommand())
	prefix := routeIDPrefix(p.editor.kind)
	oldAuto := formui.UniqueID(oldName, prefix, used)
	if strings.TrimSpace(p.editor.values["id"]) == "" || p.editor.values["id"] == oldAuto {
		p.editor.values["id"] = formui.UniqueID(p.editor.values["name"], prefix, used)
	}
}

func (p *Page) shiftKind(delta int) {
	p.SetKind(KindTrafficRules)
}

func (p *Page) View() string {
	if p.overlay == overlayEditor {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			p.editor.render(p.width, p.height-stripHeight(), p.draft),
		}, "\n")
	}
	if p.overlay == overlayDetails {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderDetails(p.draft, p.kind, p.detailsID, p.width, p.height-stripHeight(), &p.overlayScroll),
		}, "\n")
	}
	if p.overlay == overlayDeps {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderDependencyDialog(p.kind, p.deleteID, p.depsPaths, p.width, p.height-stripHeight(), &p.overlayScroll),
		}, "\n")
	}
	if p.overlay == overlayConfirmDelete {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderConfirmDelete(
				p.kind,
				p.deleteID,
				p.confirmDeleteDependentPaths(),
				p.remainingAfterConfirmDelete(),
				p.width,
				p.height-stripHeight(),
				&p.overlayScroll,
			),
		}, "\n")
	}
	if p.overlay == overlayRestoreSimple {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderRestoreTrafficRule(p.draft.LocalCommand(), p.restoreID, p.width, p.height-stripHeight()),
		}, "\n")
	}

	p.tablePage.SetSize(p.width, p.tableHeight())
	return strings.Join([]string{
		renderStrip(p.kind, p.width),
		p.tablePage.View(),
	}, "\n")
}
