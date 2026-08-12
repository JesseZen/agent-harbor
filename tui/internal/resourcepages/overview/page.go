package overview

import (
	"fmt"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

const fieldLogLevel = "log_level"

// NewPage constructs the Overview Instance secondary resource page.
func NewPage(draft *configdraft.Draft) *Page {
	inner := resourcepage.New(resourcepage.Spec{
		Title: "Instance",
		Scope: "local",
		Columns: []resourceview.Column{
			{Title: "FIELD", MinWidth: 12, Priority: 0},
			{Title: "VALUE", MinWidth: 10, Priority: 1},
			{Title: "MUTABILITY", MinWidth: 10, Priority: 2},
		},
		Actions: resourcepage.ActionSet{
			Create:  false,
			Edit:    true,
			Delete:  false,
			Publish: false,
			Details: true,
			Filter:  true,
			Mark:    false,
		},
		Domain: string(configdraft.DomainInstance),
	})
	page := &Page{draft: draft, inner: inner}
	page.Refresh()
	return page
}

type Page struct {
	draft     *configdraft.Draft
	inner     *resourcepage.Page
	editing   bool
	details   bool
	enumIdx   int
	status    string
	editorGen int64
	editorRev string
}

func (p *Page) Inner() *resourcepage.Page { return p.inner }

// OverlayLines matches resourcepage banner lines so mouse hosts can compensate once.
func (p *Page) OverlayLines() int {
	lines := 0
	switch p.inner.State() {
	case resourcepage.StateDisconnected, resourcepage.StateEmpty, resourcepage.StateLoading,
		resourcepage.StateValidationError, resourcepage.StatePublicationError, resourcepage.StateStale:
		lines++
	}
	if p.status != "" {
		lines++
	}
	return lines
}

func (p *Page) Editing() bool             { return p.editing }
func (p *Page) ShowingDetails() bool      { return p.details }
func (p *Page) State() resourcepage.State { return p.inner.State() }

func (p *Page) SetSize(width, height int) { p.inner.SetSize(width, height) }

func (p *Page) Refresh() {
	if p.draft == nil {
		p.inner.SetState(resourcepage.StateEmpty)
		p.inner.SetRows(nil)
		p.inner.SetStatus("")
		return
	}
	if !p.draft.InConflict() && strings.HasPrefix(p.status, "generation conflict") {
		p.status = ""
	}
	p.rebaseEditorIfDraftMoved()
	cmd := p.draft.LocalCommand()
	p.inner.SetRows([]resourceview.Row{{
		ID:    fieldLogLevel,
		Cells: []string{fieldLogLevel, string(cmd.Instance.LogLevel), "mutable"},
	}})
	p.inner.SetDirty(p.draft.DomainDirty(configdraft.DomainInstance))

	switch {
	case p.draft.Disconnected():
		p.status = ""
		p.inner.SetState(resourcepage.StateDisconnected)
		p.inner.SetStatus("")
	case p.draft.InConflict():
		conflictMsg := "generation conflict — reapply or reload"
		p.inner.SetState(resourcepage.StateStale)
		// Preserve sticky validation while editing; conflict is also rendered
		// from draft.InConflict() in the editor view.
		if p.editing && p.status != "" && !strings.HasPrefix(p.status, "generation conflict") {
			p.inner.SetStatus(conflictMsg + "; " + p.status)
		} else {
			p.status = conflictMsg
			p.inner.SetStatus(p.status)
		}
	case p.status != "" && strings.Contains(p.status, "invalid"):
		p.inner.SetState(resourcepage.StateValidationError)
		p.inner.SetStatus(p.status)
	default:
		p.status = ""
		p.inner.SetState(resourcepage.StateSuccess)
		p.inner.SetStatus("")
	}
}

// rebaseEditorIfDraftMoved resyncs the open log_level editor when the shared
// draft generation/revision advanced (AcceptCurrent / reload).
func (p *Page) rebaseEditorIfDraftMoved() {
	if !p.editing || p.draft == nil {
		return
	}
	if p.draft.Generation() == p.editorGen && p.draft.Revision() == p.editorRev {
		return
	}
	value := string(p.draft.LocalCommand().Instance.LogLevel)
	p.enumIdx = indexOf(logLevelValues(), value)
	if p.enumIdx < 0 {
		p.enumIdx = 0
	}
	p.status = ""
	p.editorGen = p.draft.Generation()
	p.editorRev = p.draft.Revision()
}

func (p *Page) Update(message tea.Msg) (resourcepage.Intent, bool) {
	if p.editing {
		return p.updateEditor(message)
	}
	if p.details {
		if key, ok := message.(tea.KeyMsg); ok && (key.String() == "esc" || key.Type == tea.KeyEsc) {
			p.details = false
			return resourcepage.IntentNone, true
		}
		return resourcepage.IntentNone, true
	}

	// Suppress create/delete mouse hits before they become intents.
	if mouse, ok := message.(tea.MouseMsg); ok && mouse.Action == tea.MouseActionPress {
		_ = p.inner.View()
		hit := p.inner.Table().HitTest(mouse.X, mouse.Y-p.OverlayLines())
		if hit.Kind == resourceview.HitFooterAction && (hit.Action == resourceview.ActionCreate || hit.Action == resourceview.ActionDelete) {
			return resourcepage.IntentNone, true
		}
	}

	intent, consumed := p.inner.Update(message)
	switch intent {
	case resourcepage.IntentEdit:
		p.openEditor()
		return intent, true
	case resourcepage.IntentDetails:
		p.details = true
		return intent, true
	case resourcepage.IntentCreate, resourcepage.IntentDelete:
		return resourcepage.IntentNone, true
	case resourcepage.IntentPublish:
		if p.draft != nil && (p.draft.Disconnected() || !p.draft.CanPublish()) {
			return resourcepage.IntentNone, true
		}
		return intent, consumed
	default:
		return intent, consumed
	}
}

func (p *Page) openEditor() {
	p.editing = true
	p.details = false
	p.status = ""
	if p.draft != nil {
		p.editorGen = p.draft.Generation()
		p.editorRev = p.draft.Revision()
	}
	value := string(p.draft.LocalCommand().Instance.LogLevel)
	p.enumIdx = indexOf(logLevelValues(), value)
	if p.enumIdx < 0 {
		p.enumIdx = 0
	}
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

func (p *Page) updateEditor(message tea.Msg) (resourcepage.Intent, bool) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return resourcepage.IntentNone, true
	}
	switch key.String() {
	case "esc":
		p.editing = false
		p.status = ""
		p.Refresh()
		return resourcepage.IntentNone, true
	case "ctrl+s":
		if p.draft != nil && (p.draft.Disconnected() || !p.draft.CanPublish()) {
			return resourcepage.IntentNone, true
		}
		if err := p.ApplyEditorValue(logLevelValues()[p.enumIdx]); err != nil {
			return resourcepage.IntentNone, true
		}
		p.editing = false
		return resourcepage.IntentPublish, true
	case "left", "h", "up", "k":
		p.enumIdx = (p.enumIdx - 1 + len(logLevelValues())) % len(logLevelValues())
		return resourcepage.IntentNone, true
	case "right", "l", "down", "j":
		p.enumIdx = (p.enumIdx + 1) % len(logLevelValues())
		return resourcepage.IntentNone, true
	case "enter":
		if err := p.ApplyEditorValue(logLevelValues()[p.enumIdx]); err != nil {
			return resourcepage.IntentNone, true
		}
		p.editing = false
		return resourcepage.IntentNone, true
	}
	return resourcepage.IntentNone, true
}

// ApplyEditorValue validates and writes a log_level value into the shared draft.
func (p *Page) ApplyEditorValue(value string) error {
	level := generated.MutableInstanceConfigLogLevel(value)
	if !level.Valid() {
		p.status = "$.log_level: invalid enum value"
		p.inner.SetState(resourcepage.StateValidationError)
		p.inner.SetStatus(p.status)
		return fmt.Errorf("invalid log_level %q", value)
	}
	p.draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = level
	})
	p.status = ""
	p.inner.SetState(resourcepage.StateSuccess)
	p.Refresh()
	return nil
}

func (p *Page) View() string {
	if p.editing {
		return p.renderEditor()
	}
	if p.details {
		return p.renderDetails()
	}
	return p.inner.View()
}

func (p *Page) renderEditor() string {
	values := logLevelValues()
	current := values[p.enumIdx]
	lines := []string{
		"Edit Instance.log_level",
		fmt.Sprintf("value: <%s>", current),
		"←/→ cycle  enter save  esc cancel",
	}
	conflictShown := false
	if p.draft != nil && p.draft.InConflict() {
		lines = append(lines, "generation conflict — reapply or reload")
		conflictShown = true
	}
	if p.status != "" && !(conflictShown && strings.HasPrefix(p.status, "generation conflict")) {
		lines = append(lines, p.status)
	}
	return strings.Join(lines, "\n")
}

func (p *Page) renderDetails() string {
	cmd := p.draft.LocalCommand()
	width, height := p.inner.Size()
	return detailpane.Model{
		Title: detailpane.KindLabel("Instance"),
		Sections: []detailpane.Section{{
			Title: "Configuration",
			Rows: []detailpane.Row{
				{Label: "log_level", Value: string(cmd.Instance.LogLevel)},
				{Label: "mutability", Value: "mutable"},
			},
		}},
		Width:  width,
		Height: height,
	}.View()
}

func logLevelValues() []string {
	return []string{
		string(generated.MutableInstanceConfigLogLevelSimple),
		string(generated.MutableInstanceConfigLogLevelDetail),
	}
}

func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}
