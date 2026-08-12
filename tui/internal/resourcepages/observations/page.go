package observations

import (
	"fmt"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/detailpane"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

type copyFunc func(text string) error

// Page is the read-only Observations resource page.
type Page struct {
	host          *resourcepage.Page
	observations  []backend.Observation
	disconnected  bool
	width         int
	height        int
	details       bool
	lastIntent    resourcepage.Intent
	copyFn        copyFunc
	forceState    bool
	forcedState   resourcepage.State
}

// NewPage constructs the Observations page.
func NewPage() *Page {
	page := &Page{width: 120, height: 30}
	page.rebuildHost()
	page.refresh()
	return page
}

func (p *Page) Host() *resourcepage.Page { return p.host }

func (p *Page) SetObservations(observations []backend.Observation) {
	p.observations = append([]backend.Observation(nil), observations...)
	p.refresh()
}

func (p *Page) SetDisconnected(v bool) {
	p.disconnected = v
	p.refresh()
}

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

func (p *Page) ShowingDetails() bool { return p.details }

// CopySelected copies bounded detail text for the selected observation.
func (p *Page) CopySelected() (string, error) {
	id := p.SelectedID()
	if id == "" {
		return "", nil
	}
	obs, ok := findObservation(p.observations, id)
	if !ok {
		return "", nil
	}
	text := boundCopyText(detailText(obs))
	if p.copyFn != nil {
		if err := p.copyFn(text); err != nil {
			return "", err
		}
	}
	return text, nil
}

func (p *Page) rebuildHost() {
	p.host = resourcepage.New(resourcepage.Spec{
		Title:   "Observations",
		Scope:   "all",
		Columns: observationColumns(),
		Actions: resourcepage.ActionSet{
			Create:  false,
			Edit:    false,
			Delete:  false,
			Publish: false,
			Details: true,
			Filter:  true,
			Mark:    true,
		},
		Domain: "observations",
	})
	p.host.SetSize(p.width, p.height)
}

func (p *Page) refresh() {
	if p.host == nil {
		p.rebuildHost()
	}
	rows := rowsFromObservations(p.observations)
	p.host.SetRows(rows)
	p.host.SetDirty(false)
	if p.forceState {
		p.host.SetState(p.forcedState)
		return
	}
	switch {
	case p.disconnected:
		p.host.SetState(resourcepage.StateDisconnected)
	case len(rows) == 0:
		p.host.SetState(resourcepage.StateEmpty)
	default:
		p.host.SetState(resourcepage.StateSuccess)
	}
}

func (p *Page) Update(msg tea.Msg) tea.Cmd {
	p.lastIntent = resourcepage.IntentNone
	if msg == nil {
		return nil
	}

	if p.details {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			p.details = false
		}
		return nil
	}

	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "y" {
		if p.host != nil && p.host.Table().Filtering() {
			intent, consumed := p.host.Update(msg)
			if consumed {
				p.handleIntent(intent)
			}
			return nil
		}
		text, err := p.CopySelected()
		if p.host != nil {
			if err != nil {
				p.host.SetStatus("copy failed: " + err.Error())
			} else if text != "" {
				p.host.SetStatus("copied observation details")
			}
		}
		p.lastIntent = resourcepage.IntentNone
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

func (p *Page) handleIntent(intent resourcepage.Intent) {
	switch intent {
	case resourcepage.IntentDetails:
		if p.SelectedID() != "" {
			p.details = true
			p.lastIntent = resourcepage.IntentDetails
		}
	case resourcepage.IntentFilter:
		p.lastIntent = resourcepage.IntentFilter
	case resourcepage.IntentCreate, resourcepage.IntentEdit, resourcepage.IntentDelete, resourcepage.IntentPublish:
		p.lastIntent = resourcepage.IntentNone
	default:
		p.lastIntent = intent
	}
}

func (p *Page) View() string {
	if p.details {
		return p.renderDetails()
	}
	p.host.SetSize(p.width, p.height)
	return p.host.View()
}

func (p *Page) renderDetails() string {
	obs, ok := findObservation(p.observations, p.SelectedID())
	if !ok {
		return detailpane.Model{
			Title: detailpane.KindLabel("Observation"),
			Sections: []detailpane.Section{{
				Title: "Status",
				Rows:  []detailpane.Row{{Label: "error", Value: i18n.T("detail.value.not_found")}},
			}},
			Width:  p.width,
			Height: p.height,
		}.View()
	}
	return detailpane.Model{
		Title:   detailpane.NamedTitle("Observation", obs.ID),
		Summary: []detailpane.Row{{Label: "type", Value: obs.Type}, {Label: "outcome", Value: outcomeLabel(obs)}},
		Sections: []detailpane.Section{
			{Title: "Timing", Rows: []detailpane.Row{
				{Label: "occurred_at", Value: obs.OccurredAt.Local().Format(time.RFC3339)},
			}},
			{Title: "Refs", Rows: detailpane.RowsFromKeys([]string{
				"session_id", "route_id", "target_id", "quota_group_id",
			}, map[string]string{
				"session_id":     obs.SessionID,
				"route_id":       obs.RouteID,
				"target_id":      obs.TargetID,
				"quota_group_id": obs.QuotaGroupID,
			})},
			{Title: "Decision", Rows: []detailpane.Row{
				{Label: "semantic_outcome", Value: dashIfEmpty(obs.SemanticOutcome)},
				{Label: "policy_decision", Value: dashIfEmpty(obs.PolicyDecision)},
				{Label: "decision_reason", Value: obs.DecisionReason},
				{Label: "snapshot_generation", Value: fmt.Sprintf("%d", obs.SnapshotGeneration)},
			}},
		},
		Width:  p.width,
		Height: p.height,
	}.View()
}
