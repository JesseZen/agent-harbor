package quotas

import (
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayEditor
	overlayDetails
	overlayDeps
	overlayConfirmDelete
)

// Page is the Quota Groups resource page bound to the shared config draft.
type Page struct {
	draft            *configdraft.Draft
	host             *resourcepage.Page
	runtime          map[string]backend.QuotaGroup
	width            int
	height           int
	overlay          overlayKind
	editor           *Editor
	detailsID        string
	depsPaths        []string
	deleteID         string
	status           string
	forceState       bool
	forcedState      resourcepage.State
	lastIntent       resourcepage.Intent
	advancedUnlocked bool
}

// NewPage constructs a Quota Groups page bound to the shared draft.
func NewPage(draft *configdraft.Draft) *Page {
	page := &Page{
		draft:   draft,
		runtime: map[string]backend.QuotaGroup{},
		width:   120,
		height:  30,
	}
	page.rebuildHost()
	page.Sync()
	return page
}

func (p *Page) Host() *resourcepage.Page { return p.host }

func (p *Page) Editor() *Editor { return p.editor }

func (p *Page) SetRuntime(status []backend.QuotaGroup) {
	p.runtime = make(map[string]backend.QuotaGroup, len(status))
	for _, item := range status {
		p.runtime[item.ID] = item
	}
}

func (p *Page) Sync() { p.refresh() }

func (p *Page) SetSize(width, height int) {
	p.width = max(1, width)
	p.height = max(8, height)
	if p.host != nil {
		p.host.SetSize(p.width, p.height)
	}
}

func (p *Page) State() resourcepage.State {
	if p.forceState {
		return p.forcedState
	}
	if p.host == nil {
		return resourcepage.StateLoading
	}
	return p.host.State()
}

func (p *Page) SetState(state resourcepage.State) {
	p.forceState = true
	p.forcedState = state
	if p.host != nil {
		p.host.SetState(state)
	}
}

func (p *Page) ClearForcedState() {
	p.forceState = false
}

func (p *Page) SetStatus(status string) {
	p.status = status
	if p.host != nil {
		p.host.SetStatus(status)
	}
}

func (p *Page) LastIntent() resourcepage.Intent { return p.lastIntent }

func (p *Page) OverlayLines() int {
	if p.host == nil {
		return 0
	}
	switch p.host.State() {
	case resourcepage.StateDisconnected, resourcepage.StateEmpty, resourcepage.StateLoading,
		resourcepage.StateValidationError, resourcepage.StatePublicationError, resourcepage.StateStale:
		return 1
	default:
		return 0
	}
}

func (p *Page) TableHeaderY() int { return 1 }

func (p *Page) VisibleIDs() []string {
	if p.host == nil {
		return nil
	}
	return p.host.Table().VisibleRowIDs()
}

func (p *Page) SelectedID() string {
	if p.host == nil {
		return ""
	}
	return p.host.SelectedID()
}

func (p *Page) SelectID(id string) {
	if p.host == nil {
		return
	}
	ids := p.host.Table().VisibleRowIDs()
	for i, candidate := range ids {
		if candidate == id {
			p.host.Update(tea.KeyMsg{Type: tea.KeyHome})
			for j := 0; j < i; j++ {
				p.host.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			return
		}
	}
}

func (p *Page) ShowingDetails() bool { return p.overlay == overlayDetails }

func (p *Page) CancelOverlay() {
	p.overlay = overlayNone
	p.editor = nil
	p.detailsID = ""
	p.depsPaths = nil
	p.deleteID = ""
	p.forceState = false
}

func (p *Page) BeginCreate() {
	p.overlay = overlayEditor
	p.editor = newEditor(editorCreate, "", p.draft)
	p.lastIntent = resourcepage.IntentCreate
}

func (p *Page) BeginEdit() {
	id := p.SelectedID()
	if id == "" {
		return
	}
	if p.managedQuotaLocked(id) {
		p.SetStatus("This quota belongs to a managed limit policy · press u to unlock advanced editing")
		return
	}
	p.overlay = overlayEditor
	p.editor = newEditor(editorEdit, id, p.draft)
	p.lastIntent = resourcepage.IntentEdit
}

func (p *Page) OpenDetails() {
	id := p.SelectedID()
	if id == "" {
		return
	}
	p.overlay = overlayDetails
	p.detailsID = id
	p.lastIntent = resourcepage.IntentDetails
}

func (p *Page) SetEditorValues(values map[string]string) {
	if p.editor != nil {
		p.editor.setValues(values)
	}
}

func (p *Page) EditorIDEditable() bool {
	if p.editor == nil {
		return false
	}
	return p.editor.idEditable()
}

func (p *Page) EditorFieldNames() []string {
	if p.editor == nil {
		return nil
	}
	return p.editor.fieldNames()
}

func (p *Page) SaveEditor() error {
	if p.editor == nil {
		return nil
	}
	creating := p.editor.mode == editorCreate
	if creating && strings.TrimSpace(p.editor.values["id"]) == "" {
		p.editor.values["id"] = formui.UniqueID(p.editor.values["name"], "quota", formui.UsedConfigIDs(p.draft.LocalCommand()))
	}
	if err := validateValues(p.editor.values, creating); err != nil {
		p.editor.err = err.Error()
		p.forceState = true
		p.forcedState = resourcepage.StateValidationError
		p.host.SetState(resourcepage.StateValidationError)
		p.host.SetStatus(err.Error())
		return err
	}
	var err error
	if creating {
		err = applyCreate(p.draft, p.editor.values)
	} else {
		err = applyEdit(p.draft, p.editor.id, p.editor.values)
	}
	if err != nil {
		p.editor.err = err.Error()
		p.forceState = true
		p.forcedState = resourcepage.StateValidationError
		p.host.SetState(resourcepage.StateValidationError)
		p.host.SetStatus(err.Error())
		return err
	}
	p.CancelOverlay()
	p.forceState = false
	p.refresh()
	p.lastIntent = resourcepage.IntentPublish
	return nil
}

// DeleteBlocked reports whether delete is blocked and lists inbound reference paths.
func (p *Page) DeleteBlocked(id string) (bool, []string) {
	paths := inboundRefs(p.draft, id)
	if len(paths) > 0 {
		return true, paths
	}
	return false, nil
}

// TryDelete opens a delete dialog for the selected row (blocked or confirm).
func (p *Page) TryDelete() (bool, []string) {
	id := p.SelectedID()
	if id == "" {
		return false, nil
	}
	if p.managedQuotaLocked(id) {
		p.SetStatus("This quota belongs to a managed limit policy · press u to unlock advanced editing")
		return true, []string{"managed object ownership"}
	}
	paths := inboundRefs(p.draft, id)
	p.deleteID = id
	p.depsPaths = paths
	p.lastIntent = resourcepage.IntentDelete
	if len(paths) > 0 {
		p.overlay = overlayDeps
		return true, paths
	}
	p.overlay = overlayConfirmDelete
	return false, nil
}

// ConfirmDelete applies delete after the confirm overlay when not blocked.
func (p *Page) ConfirmDelete() {
	if p.overlay != overlayConfirmDelete || p.deleteID == "" {
		return
	}
	if p.managedQuotaLocked(p.deleteID) {
		p.CancelOverlay()
		p.SetStatus("This quota belongs to a managed limit policy · press u to unlock advanced editing")
		return
	}
	applyDelete(p.draft, p.deleteID)
	p.CancelOverlay()
	p.refresh()
	p.lastIntent = resourcepage.IntentPublish
}

func (p *Page) rebuildHost() {
	p.host = resourcepage.New(resourcepage.Spec{
		Title:   "Quota Groups",
		Scope:   "all",
		Columns: quotaColumns(),
		Actions: resourcepage.ActionSet{
			Create:  true,
			Edit:    true,
			Delete:  true,
			Publish: false,
			Details: true,
			Filter:  true,
			Mark:    true,
		},
		Domain: string(configdraft.DomainQuotas),
	})
	p.host.SetSize(p.width, p.height)
}

func (p *Page) refresh() {
	if p.host == nil {
		p.rebuildHost()
	}
	rows := rowsFor(p.draft, p.runtime)
	p.host.SetRows(rows)
	p.host.SetDirty(p.draft.DomainDirty(configdraft.DomainQuotas))

	if p.forceState {
		p.host.SetState(p.forcedState)
		return
	}
	if p.draft.Disconnected() {
		p.host.SetState(resourcepage.StateDisconnected)
		return
	}
	if len(rows) == 0 {
		p.host.SetState(resourcepage.StateEmpty)
		return
	}
	p.host.SetState(resourcepage.StateSuccess)
}

func (p *Page) Update(msg tea.Msg) tea.Cmd {
	p.lastIntent = resourcepage.IntentNone
	if msg == nil {
		return nil
	}

	if key, ok := msg.(tea.KeyMsg); ok && p.overlay != overlayNone {
		p.updateOverlayKey(key)
		return nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "u" && (p.host == nil || !p.host.Table().Filtering()) {
		p.advancedUnlocked = !p.advancedUnlocked
		if p.advancedUnlocked {
			p.SetStatus("Advanced editing unlocked for this page")
		} else {
			p.SetStatus("Managed members are read-only · u unlock")
		}
		return nil
	}

	if p.overlay != overlayNone {
		if mouse, ok := msg.(tea.MouseMsg); ok && p.overlay == overlayEditor && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			layout := p.editor.formLayout(p.width, p.height)
			if mouse.Y == layout.FooterLine {
				if mouse.X < p.width/2 {
					_ = p.SaveEditor()
				} else {
					p.CancelOverlay()
				}
				return nil
			}
			for name, line := range layout.FieldLines {
				if line == mouse.Y {
					p.editor.cursor = quotaFieldIndex(p.editor.fieldNames(), name)
					break
				}
			}
		}
		return nil
	}

	if mouse, ok := msg.(tea.MouseMsg); ok {
		// Pass viewport coordinates unchanged; resourcepage.Page.Update already
		// subtracts banner/status overlay lines before table hit-testing.
		intent, _ := p.host.Update(mouse)
		p.handleIntent(intent)
		return nil
	}

	intent, consumed := p.host.Update(msg)
	if consumed {
		p.handleIntent(intent)
	}
	return nil
}

func (p *Page) managedQuotaLocked(id string) bool {
	if p.advancedUnlocked || p.draft == nil || id == "" {
		return false
	}
	_, owned := managedconfig.OwnerOf(p.draft.LocalCommand(), generated.ManagedResourceRefKindQuotaGroup, id)
	return owned
}

func quotaFieldIndex(fields []string, name string) int {
	for index, field := range fields {
		if field == name {
			return index
		}
	}
	return 0
}

func (p *Page) handleIntent(intent resourcepage.Intent) {
	p.lastIntent = intent
	switch intent {
	case resourcepage.IntentCreate:
		p.BeginCreate()
	case resourcepage.IntentEdit:
		p.BeginEdit()
	case resourcepage.IntentDelete:
		_, _ = p.TryDelete()
	case resourcepage.IntentDetails:
		p.OpenDetails()
	case resourcepage.IntentPublish:
		if p.draft.Disconnected() || !p.draft.CanPublish() {
			p.lastIntent = resourcepage.IntentNone
		}
	}
}

func (p *Page) updateOverlayKey(key tea.KeyMsg) {
	switch p.overlay {
	case overlayEditor:
		if key.Type == tea.KeyRunes && len(key.Runes) > 0 {
			name := p.editor.fieldNames()[p.editor.cursor]
			if name != "id" || p.editor.idEditable() {
				oldName := p.editor.values["name"]
				p.editor.values[name] += string(key.Runes)
				if name == "name" {
					p.syncQuotaID(oldName)
				}
			}
			return
		}
		switch key.String() {
		case "esc":
			p.CancelOverlay()
		case "ctrl+s":
			_ = p.SaveEditor()
		case "enter":
			names := p.editor.fieldNames()
			if len(names) > 0 {
				p.editor.cursor = (p.editor.cursor + 1) % len(names)
			}
		case "up", "k":
			if p.editor.cursor > 0 {
				p.editor.cursor--
			}
		case "down", "j":
			if p.editor.cursor < len(p.editor.fieldNames())-1 {
				p.editor.cursor++
			}
		case "tab":
			if p.editor.cursor < len(p.editor.fieldNames())-1 {
				p.editor.cursor++
			} else {
				p.editor.cursor = 0
			}
		case "shift+tab":
			if p.editor.cursor > 0 {
				p.editor.cursor--
			} else {
				p.editor.cursor = len(p.editor.fieldNames()) - 1
			}
		case "backspace":
			name := p.editor.fieldNames()[p.editor.cursor]
			if name != "id" || p.editor.idEditable() {
				oldName := p.editor.values["name"]
				runes := []rune(p.editor.values[name])
				if len(runes) > 0 {
					p.editor.values[name] = string(runes[:len(runes)-1])
				}
				if name == "name" {
					p.syncQuotaID(oldName)
				}
			}
		}
	case overlayDetails, overlayDeps:
		if key.String() == "esc" {
			p.CancelOverlay()
		}
	case overlayConfirmDelete:
		switch key.String() {
		case "esc":
			p.CancelOverlay()
		case "enter":
			p.ConfirmDelete()
		}
	}
}

func (p *Page) syncQuotaID(oldName string) {
	if p.editor == nil || p.editor.mode != editorCreate {
		return
	}
	used := formui.UsedConfigIDs(p.draft.LocalCommand())
	oldAuto := formui.UniqueID(oldName, "quota", used)
	if strings.TrimSpace(p.editor.values["id"]) == "" || p.editor.values["id"] == oldAuto {
		p.editor.values["id"] = formui.UniqueID(p.editor.values["name"], "quota", used)
	}
}

func (p *Page) View() string {
	if p.overlay == overlayEditor && p.editor != nil {
		return p.editor.render(p.width, p.height)
	}
	if p.overlay == overlayDetails {
		return renderDetails(p.draft, p.detailsID, p.width, p.height)
	}
	if p.overlay == overlayDeps {
		remaining := quotaCount(p.draft) - 1
		return renderDependencyDialog(p.deleteID, p.depsPaths, remaining, p.width, p.height)
	}
	if p.overlay == overlayConfirmDelete {
		remaining := quotaCount(p.draft) - 1
		return renderConfirmDeleteDialog(p.deleteID, remaining, p.width, p.height)
	}
	p.host.SetSize(p.width, p.height)
	return p.host.View()
}
