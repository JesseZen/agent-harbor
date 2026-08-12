package page

import (
	"context"
	"errors"
	"net/http"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
)

// ClassifyPreviewError maps backend/API/context errors to preview outcomes.
func ClassifyPreviewError(err error) sessionpreview.OutcomeKind {
	if err == nil {
		return sessionpreview.OutcomeSuccess
	}
	if errors.Is(err, context.Canceled) {
		return sessionpreview.OutcomeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return sessionpreview.OutcomeTimeout
	}
	var apiErr *coreclient.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == string(generated.PreviewUnavailable):
			return sessionpreview.OutcomeUnavailable
		case apiErr.Code == string(generated.StalePrecondition):
			return sessionpreview.OutcomeStale
		case apiErr.Code == "" &&
			(apiErr.StatusCode == http.StatusConflict || apiErr.StatusCode == http.StatusPreconditionFailed):
			// Legacy/transport fallback only when Code is empty.
			return sessionpreview.OutcomeStale
		}
	}
	return sessionpreview.OutcomeError
}

// PeekNeedsPreviewFetch reports whether a fetch is pending without clearing it.
func (p *Page) PeekNeedsPreviewFetch() bool {
	return p.pendingPreviewFetch
}

// NeedsPreviewFetch reports whether a preview request should be issued.
// True when a fetch is pending, or when the machine is loading a selection
// (so ConsumePreviewFetch alone cannot strand the auto/IfNeeded path).
func (p *Page) NeedsPreviewFetch() bool {
	if p.pendingPreviewFetch {
		return true
	}
	return p.preview.Loading() && p.inner.SelectedID() != ""
}

// ConsumePreviewFetch clears and returns whether a fetch was pending.
// Use ONLY when the host tea.Cmd path will call FetchPreview (force).
// Prefer FetchPreviewIfNeeded, which does not require Consume first.
func (p *Page) ConsumePreviewFetch() bool {
	if !p.pendingPreviewFetch {
		return false
	}
	p.pendingPreviewFetch = false
	return true
}

// ensurePreviewBound rebinds the preview machine to id when the table still
// highlights that session but the machine was reset (e.g. load-error).
// Returns false when id is missing from the local session map.
func (p *Page) ensurePreviewBound(id string) bool {
	session, ok := p.sessions[id]
	if !ok || id == "" {
		return false
	}
	if p.preview.SelectedKey().SessionID == id {
		return true
	}
	cmd := p.preview.Select(sessionpreview.CacheKey{
		SessionID: id,
		UpdatedAt: session.UpdatedAt,
	})
	if cmd.ShouldFetch {
		p.markPreviewFetchNeeded()
	}
	return true
}

// InvalidatePreview handles SSE/session_changed for the selected session.
// It queues a fetch; when Backend is set the page auto-fetches.
func (p *Page) InvalidatePreview(sessionID string) {
	// After resetPreviewState the machine may be unbound while the table still
	// highlights sessionID — rebind so SSE invalidation can enter loading.
	if p.inner.SelectedID() == sessionID {
		_ = p.ensurePreviewBound(sessionID)
	}
	p.preview.Invalidate(sessionID)
	if !p.preview.Loading() {
		return
	}
	p.markPreviewFetchNeeded()
	p.maybeAutoFetchPreview(context.Background())
}

// PreviewResult returns the last applied preview payload.
func (p *Page) PreviewResult() sessionpreview.Result {
	return p.preview.Result()
}

// PreviewLines returns ready preview lines (may be empty).
func (p *Page) PreviewLines() []string {
	return p.preview.Result().Lines
}

// FetchPreview forces a preview request (manual r / explicit), ignoring Ready/Empty TTL.
func (p *Page) FetchPreview(ctx context.Context) error {
	id := p.inner.SelectedID()
	if !p.ensurePreviewBound(id) {
		return errors.New("no session selected")
	}
	if p.deps.Backend == nil {
		return errors.New("backend not configured")
	}
	cmd := p.preview.ManualRetry()
	if !cmd.ShouldFetch {
		return errors.New("no session selected")
	}
	p.markPreviewFetchNeeded()
	p.pendingPreviewFetch = false
	return p.doPreviewFetch(ctx, p.previewGeneration)
}

// FetchPreviewIfNeeded issues a preview request when one is pending or the
// machine is already loading a selection (Consume is optional for this path).
// Ready/Empty TTL cache hits are respected when neither condition holds.
func (p *Page) FetchPreviewIfNeeded(ctx context.Context) error {
	if p.deps.Backend == nil {
		return errors.New("backend not configured")
	}
	if !p.NeedsPreviewFetch() {
		return nil
	}
	gen := p.previewGeneration
	p.pendingPreviewFetch = false
	return p.doPreviewFetch(ctx, gen)
}

func (p *Page) markPreviewFetchNeeded() {
	p.previewGeneration++
	p.pendingPreviewFetch = true
}

func (p *Page) maybeAutoFetchPreview(ctx context.Context) {
	if p.deps.Backend == nil || !p.pendingPreviewFetch {
		return
	}
	_ = p.FetchPreviewIfNeeded(ctx)
}

func (p *Page) doPreviewFetch(ctx context.Context, generation uint64) error {
	id := p.inner.SelectedID()
	session, ok := p.sessions[id]
	if !ok {
		if generation == p.previewGeneration {
			p.preview.Apply(sessionpreview.OutcomeError, sessionpreview.Result{}, nil)
		}
		return errors.New("no session selected")
	}
	ref := backend.SessionRef{ID: id, ExpectedUpdatedAt: session.UpdatedAt}
	preview, err := p.deps.Backend.Preview(ctx, ref)
	if generation != p.previewGeneration {
		// Selection/invalidate moved on; ignore this response.
		return nil
	}
	if id != p.inner.SelectedID() {
		return nil
	}
	if err != nil {
		outcome := ClassifyPreviewError(err)
		switch outcome {
		case sessionpreview.OutcomeStale:
			// When ReloadSession is nil or fails, retry once with the same cache key
			// (machine staleRetries). A second stale outcome becomes the manual error.
			var refreshedKey *sessionpreview.CacheKey
			if p.deps.ReloadSession != nil {
				reloaded, reloadErr := p.deps.ReloadSession(ctx, id)
				if reloadErr == nil {
					p.sessions[id] = reloaded
					key := sessionpreview.CacheKey{SessionID: id, UpdatedAt: reloaded.UpdatedAt}
					refreshedKey = &key
				}
			}
			if generation != p.previewGeneration {
				return nil
			}
			p.preview.Apply(sessionpreview.OutcomeStale, sessionpreview.Result{}, refreshedKey)
			if p.preview.Loading() {
				_, shouldFetch := p.preview.Tick()
				if shouldFetch {
					return p.doPreviewFetch(ctx, generation)
				}
			}
			return err
		default:
			p.preview.Apply(outcome, sessionpreview.Result{}, nil)
			return err
		}
	}

	result := sessionpreview.Result{
		Lines:     preview.Lines,
		Empty:     len(preview.Lines) == 0,
		Truncated: preview.Truncated,
	}
	if result.Empty {
		p.preview.Apply(sessionpreview.OutcomeEmpty, result, nil)
	} else {
		p.preview.Apply(sessionpreview.OutcomeSuccess, result, nil)
	}
	return nil
}

// TickPreview advances TTL/backoff timers; returns whether a fetch should run.
// When Backend is set, auto-fetches. Otherwise sets NeedsPreviewFetch for the host.
func (p *Page) TickPreview() (shouldFetch bool) {
	_, shouldFetch = p.preview.Tick()
	if !shouldFetch {
		return false
	}
	p.markPreviewFetchNeeded()
	if p.deps.Backend != nil {
		_ = p.FetchPreviewIfNeeded(context.Background())
		return false
	}
	return true
}
