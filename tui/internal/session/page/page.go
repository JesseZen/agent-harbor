package page

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Intent extends resourcepage intents with Sessions lifecycle/attach actions.
type Intent = resourcepage.Intent

const (
	IntentNone            = resourcepage.IntentNone
	IntentDetails         = resourcepage.IntentDetails
	IntentCreate          = resourcepage.IntentCreate
	IntentFilter          = resourcepage.IntentFilter
	IntentCommands        = resourcepage.IntentCommands
	IntentAttach   Intent = "attach"
	IntentLaunch   Intent = "launch"
	IntentResume   Intent = "resume"
	IntentEnd      Intent = "end"
	IntentRetry    Intent = "retry_preview"
)

const safeLoadSessionsError = "failed to load sessions"

// Deps wires Core-authoritative session load and backend lifecycle/preview.
//
// TICKET-011 composition:
//   - With Backend: selection / InvalidatePreview / r / TickPreview auto-fetch.
//   - Without Backend: poll NeedsPreviewFetch (or PeekNeedsPreviewFetch) and call
//     FetchPreviewIfNeeded (preferred) or ConsumePreviewFetch then FetchPreview.
//   - Call InvalidatePreview on SSE session_changed for the selected session.
//   - Gate PreviewLines/PreviewResult on !PreviewLoading (or PreviewState ready/empty).
//   - Add OverlayLines to mouse Y under global chrome (preview banner + status).
type Deps struct {
	Load          func(ctx context.Context) ([]generated.AgentSession, error)
	Backend       backend.Backend
	Clock         sessionpreview.Clock
	ReloadSession func(ctx context.Context, id string) (generated.AgentSession, error)
	// CreateRequest builds the payload for IntentCreate. Required when creating;
	// if nil, ExecuteIntent returns a clear error instead of sending empty {}.
	CreateRequest func() backend.CreateSessionRequest
}

type mode int

const (
	modeList mode = iota
	modeDetails
)

// Page is the K9s Sessions resource page factory for TICKET-011 composition.
//
// Wiring contract:
//   - With Backend set: Select / InvalidatePreview / manual r / TickPreview
//     auto-fetch synchronously via FetchPreviewIfNeeded.
//   - Without Backend: after selection/invalidate/retry/tick, NeedsPreviewFetch
//     is true until the host calls FetchPreviewIfNeeded (TTL-respecting) or
//     FetchPreview (force). ConsumePreviewFetch is only for tea.Cmd hosts that
//     will call FetchPreview; IfNeeded does not require Consume first.
//   - InvalidatePreview on SSE session_changed for the selected session.
//   - Check PreviewLoading (or PreviewState) before rendering PreviewLines.
//   - OverlayLines counts status overlays plus the preview banner for mouse Y.
type Page struct {
	deps                Deps
	inner               *resourcepage.Page
	preview             *sessionpreview.Machine
	clock               sessionpreview.Clock
	mode                mode
	sessions            map[string]generated.AgentSession
	order               []string
	lastIntent          Intent
	status              string
	width               int
	height              int
	pendingPreviewFetch bool
	previewGeneration   uint64
}

// New constructs a Sessions page. See Page and Deps godoc for TICKET-011 wiring.
func New(deps Deps) *Page {
	clock := deps.Clock
	if clock == nil {
		clock = realClock{}
	}
	inner := resourcepage.New(resourcepage.Spec{
		Title: "Sessions",
		Scope: "all",
		Columns: []resourceview.Column{
			{Title: "NAME", MinWidth: 12, Priority: 0},
			{Title: "LIFECYCLE", MinWidth: 9, Priority: 1},
			{Title: "ACTIVITY", MinWidth: 9, Priority: 2},
			{Title: "SOURCE", MinWidth: 8, Priority: 3},
			{Title: "PROVIDER", MinWidth: 8, Priority: 4},
			{Title: "WORKSPACE", MinWidth: 12, Priority: 5},
			{Title: "AGE", MinWidth: 4, Priority: 6, Align: resourceview.AlignRight},
		},
		Actions: resourcepage.ActionSet{
			Create:  true,
			Edit:    false,
			Delete:  false,
			Publish: false,
			Details: true,
			Filter:  true,
			Mark:    true,
		},
		Domain: "sessions",
	})
	return &Page{
		deps:     deps,
		inner:    inner,
		preview:  sessionpreview.New(clock),
		clock:    clock,
		sessions: map[string]generated.AgentSession{},
		width:    80,
		height:   20,
	}
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (p *Page) Inner() *resourcepage.Page { return p.inner }

func (p *Page) ShowingDetails() bool { return p.mode == modeDetails }

func (p *Page) State() resourcepage.State { return p.inner.State() }

func (p *Page) LastIntent() Intent { return p.lastIntent }

func (p *Page) SelectedID() string { return p.inner.SelectedID() }

func (p *Page) PreviewState() sessionpreview.State { return p.preview.ViewState() }

func (p *Page) PreviewLoading() bool { return p.preview.Loading() }

func (p *Page) PreviewMessage() string { return p.preview.Message() }

// OverlayLines counts status overlays plus the Sessions preview banner line
// prepended by View (AH-R030 mouse parity).
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
	if p.renderPreviewBanner() != "" {
		lines++
	}
	return lines
}

func (p *Page) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.inner.SetSize(width, height)
}

func (p *Page) resetPreviewState() {
	p.preview = sessionpreview.New(p.clock)
	p.pendingPreviewFetch = false
	p.previewGeneration++
}

func (p *Page) Refresh(ctx context.Context) error {
	if p.deps.Load == nil {
		p.inner.SetRows(nil)
		p.inner.SetState(resourcepage.StateEmpty)
		p.sessions = map[string]generated.AgentSession{}
		p.order = nil
		p.resetPreviewState()
		return nil
	}
	sessions, err := p.deps.Load(ctx)
	if err != nil {
		p.inner.SetState(resourcepage.StateValidationError)
		p.status = safeLoadSessionsError
		p.inner.SetStatus(safeLoadSessionsError)
		p.resetPreviewState()
		return err
	}
	p.sessions = make(map[string]generated.AgentSession, len(sessions))
	p.order = make([]string, 0, len(sessions))
	rows := make([]resourceview.Row, 0, len(sessions))
	for _, session := range sessions {
		id := string(session.Id)
		p.sessions[id] = session
		p.order = append(p.order, id)
		rows = append(rows, rowFromSession(session, p.clock.Now()))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	sort.Strings(p.order)
	p.inner.SetRows(rows)
	if len(rows) == 0 {
		p.inner.SetState(resourcepage.StateEmpty)
		p.resetPreviewState()
		p.status = ""
		p.inner.SetStatus("")
		return nil
	}
	p.inner.SetState(resourcepage.StateSuccess)
	p.status = ""
	p.inner.SetStatus("")
	_ = p.inner.View()
	if id := p.inner.SelectedID(); id != "" {
		p.onSelectionChanged()
	} else {
		p.resetPreviewState()
	}
	return nil
}

func (p *Page) RowCells(id string) []string {
	session, ok := p.sessions[id]
	if !ok {
		return nil
	}
	return rowFromSession(session, p.clock.Now()).Cells
}

// SelectID moves selection to id. Returns false when the id is not present.
func (p *Page) SelectID(id string) bool {
	p.mode = modeList
	if _, ok := p.sessions[id]; !ok {
		p.status = "session not found"
		p.inner.SetStatus("session not found")
		return false
	}
	rows := make([]resourceview.Row, 0, len(p.sessions))
	for _, sid := range p.order {
		rows = append(rows, rowFromSession(p.sessions[sid], p.clock.Now()))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	p.inner.SetRows(rows)
	_ = p.inner.View()
	p.inner.Update(tea.KeyMsg{Type: tea.KeyHome})
	for range rows {
		if p.inner.SelectedID() == id {
			p.status = ""
			p.inner.SetStatus("")
			p.onSelectionChanged()
			return true
		}
		p.inner.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	p.status = "session not found"
	p.inner.SetStatus("session not found")
	return false
}

func (p *Page) onSelectionChanged() {
	id := p.inner.SelectedID()
	session, ok := p.sessions[id]
	if !ok {
		return
	}
	cmd := p.preview.Select(sessionpreview.CacheKey{
		SessionID: id,
		UpdatedAt: session.UpdatedAt,
	})
	if !cmd.ShouldFetch {
		return
	}
	p.markPreviewFetchNeeded()
	p.maybeAutoFetchPreview(context.Background())
}

func (p *Page) Update(message tea.Msg) (Intent, bool) {
	if p.mode == modeDetails {
		if key, ok := message.(tea.KeyMsg); ok && key.String() == "esc" {
			p.mode = modeList
			p.lastIntent = IntentNone
			return IntentNone, true
		}
		return IntentNone, true
	}

	if key, ok := message.(tea.KeyMsg); ok && !p.inner.Table().Filtering() {
		if intent, consumed := p.handleSessionKey(key); consumed {
			p.lastIntent = intent
			return intent, true
		}
	}

	// Preview banner is prepended in View; resourcepage already offsets its own
	// status overlays — subtract only the preview banner delta here (AH-R030).
	if mouse, ok := message.(tea.MouseMsg); ok {
		if p.renderPreviewBanner() != "" {
			mouse.Y--
			message = mouse
		}
	}

	prev := p.inner.SelectedID()
	intent, consumed := p.inner.Update(message)
	if p.inner.SelectedID() != prev {
		p.onSelectionChanged()
	}
	switch intent {
	case resourcepage.IntentDetails:
		p.mode = modeDetails
		p.lastIntent = IntentDetails
		return IntentDetails, true
	case resourcepage.IntentCreate:
		p.lastIntent = IntentCreate
		return IntentCreate, true
	case resourcepage.IntentCommands:
		p.lastIntent = IntentCommands
		return IntentCommands, true
	case resourcepage.IntentFilter:
		p.lastIntent = IntentFilter
		return IntentFilter, true
	default:
		if consumed {
			p.lastIntent = intent
		}
		return intent, consumed
	}
}

func (p *Page) handleSessionKey(key tea.KeyMsg) (Intent, bool) {
	switch key.String() {
	case "a":
		if p.inner.SelectedID() != "" {
			return IntentAttach, true
		}
	case "l":
		if p.inner.SelectedID() != "" {
			return IntentLaunch, true
		}
	case "R":
		if p.inner.SelectedID() != "" {
			return IntentResume, true
		}
	case "x":
		if p.inner.SelectedID() != "" {
			return IntentEnd, true
		}
	case "r":
		id := p.inner.SelectedID()
		if !p.ensurePreviewBound(id) {
			return IntentNone, false
		}
		cmd := p.preview.ManualRetry()
		if cmd.ShouldFetch {
			p.markPreviewFetchNeeded()
			p.maybeAutoFetchPreview(context.Background())
		} else if p.pendingPreviewFetch {
			// ensurePreviewBound may have queued Select's fetch already.
			p.maybeAutoFetchPreview(context.Background())
		}
		return IntentRetry, true
	}
	return IntentNone, false
}

func (p *Page) ExecuteIntent(ctx context.Context, intent Intent) error {
	if p.deps.Backend == nil {
		return fmt.Errorf("backend not configured")
	}
	id := p.inner.SelectedID()
	session, ok := p.sessions[id]
	ref := backend.SessionRef{ID: id, ExpectedUpdatedAt: session.UpdatedAt}
	switch intent {
	case IntentCreate:
		if p.deps.CreateRequest == nil {
			return fmt.Errorf("create request not configured")
		}
		_, err := p.deps.Backend.CreateSession(ctx, p.deps.CreateRequest())
		return err
	case IntentAttach:
		if !ok {
			return fmt.Errorf("no session selected")
		}
		_, err := p.deps.Backend.PrepareAttach(ctx, ref, io.Discard)
		return err
	case IntentLaunch:
		if !ok {
			return fmt.Errorf("no session selected")
		}
		_, err := p.deps.Backend.LaunchSession(ctx, ref, 0)
		return err
	case IntentResume:
		if !ok {
			return fmt.Errorf("no session selected")
		}
		_, err := p.deps.Backend.ResumeSession(ctx, ref, 0)
		return err
	case IntentEnd:
		if !ok {
			return fmt.Errorf("no session selected")
		}
		return p.deps.Backend.EndSession(ctx, backend.EndSessionRequest{Session: ref, Mode: backend.EndGraceful})
	default:
		return fmt.Errorf("unsupported intent %q", intent)
	}
}

func (p *Page) View() string {
	if p.mode == modeDetails {
		return p.renderDetails()
	}
	base := p.inner.View()
	previewBanner := p.renderPreviewBanner()
	if previewBanner == "" {
		return base
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(resourceview.TokenText)).Faint(true)
	return style.Render(previewBanner) + "\n" + base
}

func (p *Page) renderPreviewBanner() string {
	id := p.inner.SelectedID()
	if id == "" {
		return ""
	}
	if _, ok := p.sessions[id]; !ok {
		return ""
	}
	// Machine unbound (e.g. after resetPreviewState) — do not imply empty/ready.
	if p.preview.SelectedKey().SessionID != id {
		return ""
	}
	if msg := p.preview.Message(); msg != "" {
		return msg
	}
	switch p.preview.ViewState() {
	case sessionpreview.StateLoading:
		return "Preview loading…"
	case sessionpreview.StateUnavailable:
		return "Preview unavailable"
	case sessionpreview.StateEmpty:
		return "Preview empty"
	default:
		return ""
	}
}

func (p *Page) renderDetails() string {
	// Sessions keep the plain inspect dump (excluded from DetailPane chrome).
	id := p.inner.SelectedID()
	session, ok := p.sessions[id]
	if !ok {
		return "No session selected\nesc close"
	}
	nativeObs := ""
	if session.NativeActivityObservedAt != nil {
		nativeObs = session.NativeActivityObservedAt.Local().Format(time.RFC3339)
	}
	return strings.Join([]string{
		"Session details",
		fmt.Sprintf("id: %s", session.Id),
		fmt.Sprintf("label: %s", session.Label),
		fmt.Sprintf("lifecycle: %s", session.Lifecycle),
		fmt.Sprintf("native_activity: %s", session.NativeActivity),
		fmt.Sprintf("native_activity_observed_at: %s", nativeObs),
		fmt.Sprintf("activity_source: %s", session.ActivitySource),
		fmt.Sprintf("hook_health: %s", session.HookHealth),
		fmt.Sprintf("hook_health_observed_at: %s", session.HookHealthObservedAt.Local().Format(time.RFC3339)),
		fmt.Sprintf("native_session_id: %s", nativeID(session)),
		fmt.Sprintf("provider: %s", providerCell(session)),
		fmt.Sprintf("workspace: %s", session.Workspace),
		fmt.Sprintf("updated_at: %s", session.UpdatedAt.Local().Format(time.RFC3339)),
		fmt.Sprintf("created_at: %s", session.CreatedAt.Local().Format(time.RFC3339)),
		"esc close",
	}, "\n")
}
