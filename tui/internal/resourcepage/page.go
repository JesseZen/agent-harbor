package resourcepage

import (
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type State string

const (
	StateLoading          State = "loading"
	StateEmpty            State = "empty"
	StateSuccess          State = "success"
	StateStale            State = "stale"
	StateValidationError  State = "validation_error"
	StatePublicationError State = "publication_error"
	StateDisconnected     State = "disconnected"
)

type ActionSet struct {
	Create  bool
	Edit    bool
	Delete  bool
	Publish bool
	Details bool
	Filter  bool
	Mark    bool
}

type Spec struct {
	Title   string
	Scope   string
	Columns []resourceview.Column
	Actions ActionSet
	Domain  string
}

type Intent string

const (
	IntentNone     Intent = ""
	IntentDetails  Intent = "details"
	IntentCreate   Intent = "create"
	IntentEdit     Intent = "edit"
	IntentDelete   Intent = "delete"
	IntentPublish  Intent = "publish"
	IntentCommands Intent = "commands"
	IntentFilter   Intent = "filter"
)

type Page struct {
	spec   Spec
	table  *resourceview.Model
	state  State
	status string
	width  int
	height int
}

func New(spec Spec) *Page {
	table := resourceview.New(spec.Title, spec.Columns)
	table.SetScope(spec.Scope)
	table.SetFooterActions(resourceview.FooterActions{
		Create: spec.Actions.Create, Edit: spec.Actions.Edit, Delete: spec.Actions.Delete,
		Publish: spec.Actions.Publish, Filter: spec.Actions.Filter, Mark: spec.Actions.Mark,
	})
	return &Page{spec: spec, table: table, state: StateLoading, width: 80, height: 20}
}

func (page *Page) Table() *resourceview.Model { return page.table }

func (page *Page) SetRows(rows []resourceview.Row) { page.table.SetRows(rows) }

func (page *Page) SetState(state State) { page.state = state }

func (page *Page) State() State { return page.state }

func (page *Page) SetStatus(status string) { page.status = status }

func (page *Page) SetDirty(dirty bool) {
	page.table.SetDirty(dirty)
}

func (page *Page) SetSize(width, height int) {
	page.width = max(1, width)
	page.height = max(6, height)
}

// Size returns the last SetSize dimensions.
func (page *Page) Size() (width, height int) {
	return page.width, page.height
}

func (page *Page) SelectedID() string { return page.table.SelectedID() }

func (page *Page) overlayLines() int {
	lines := 0
	switch page.state {
	case StateDisconnected, StateEmpty, StateLoading, StateValidationError, StatePublicationError, StateStale:
		lines++
	}
	if page.status != "" {
		lines++
	}
	return lines
}

func (page *Page) Update(message tea.Msg) (Intent, bool) {
	if key, ok := message.(tea.KeyMsg); ok && !page.table.Filtering() {
		if intent, consumed := page.handlePageKey(key); consumed {
			return intent, true
		}
	}

	if mouse, ok := message.(tea.MouseMsg); ok {
		if offset := page.overlayLines(); offset > 0 {
			mouse.Y -= offset
			message = mouse
		}
	}

	consumed := page.table.Update(message)
	if !consumed {
		return IntentNone, false
	}

	switch page.table.LastAction() {
	case resourceview.ActionDetails:
		if page.spec.Actions.Details {
			return IntentDetails, true
		}
	case resourceview.ActionFilter:
		if page.spec.Actions.Filter {
			return IntentFilter, true
		}
	case resourceview.ActionCreate:
		if page.spec.Actions.Create {
			return IntentCreate, true
		}
	case resourceview.ActionEdit:
		if page.spec.Actions.Edit && page.SelectedID() != "" {
			return IntentEdit, true
		}
	case resourceview.ActionDelete:
		if page.spec.Actions.Delete && page.SelectedID() != "" {
			return IntentDelete, true
		}
	case resourceview.ActionPublish:
		if page.spec.Actions.Publish && page.state != StateDisconnected {
			return IntentPublish, true
		}
	}
	return IntentNone, true
}

func (page *Page) handlePageKey(key tea.KeyMsg) (Intent, bool) {
	if page.state == StateDisconnected && key.String() == "ctrl+s" {
		return IntentNone, false
	}
	switch key.String() {
	case "n":
		if page.spec.Actions.Create {
			return IntentCreate, true
		}
	case "e":
		if page.spec.Actions.Edit && page.SelectedID() != "" {
			return IntentEdit, true
		}
	case "d":
		if page.spec.Actions.Delete && page.SelectedID() != "" {
			return IntentDelete, true
		}
	case "enter":
		if page.spec.Actions.Details && page.SelectedID() != "" {
			return IntentDetails, true
		}
	case "ctrl+s":
		if page.spec.Actions.Publish && page.state != StateDisconnected {
			return IntentPublish, true
		}
	case ":":
		return IntentCommands, true
	}
	return IntentNone, false
}

func (page *Page) View() string {
	tableHeight := page.height
	parts := make([]string, 0, 3)

	if banner := page.renderStateBanner(page.width); banner != "" {
		parts = append(parts, banner)
		tableHeight--
	}
	if page.status != "" {
		parts = append(parts, page.renderStatus(page.width))
		tableHeight--
	}

	page.table.SetSize(page.width, max(4, tableHeight))
	parts = append(parts, page.table.View())
	return strings.Join(parts, "\n")
}

func (page *Page) renderStateBanner(width int) string {
	switch page.state {
	case StateDisconnected:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenError)).Bold(true)
		return style.Render(truncate("Disconnected — showing the last known configuration", width))
	case StateEmpty:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenWarning))
		return style.Render(truncate("No resources", width))
	case StateLoading:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenText)).Faint(true)
		return style.Render(truncate("Loading…", width))
	case StateValidationError:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenError))
		return style.Render(truncate("Validation error", width))
	case StatePublicationError:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenError))
		return style.Render(truncate("Publication error", width))
	case StateStale:
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenWarning))
		return style.Render(truncate("Stale snapshot", width))
	default:
		return ""
	}
}

func (page *Page) renderStatus(width int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenText)).Faint(true)
	return style.Render(truncate(page.status, width))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}
