package page_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/session/page"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConsumeThenFetchPreviewIfNeededStillFetches(t *testing.T) {
	// Host may Consume for tea.Cmd wiring; IfNeeded must still fetch when loading.
	clock := &fakeClock{now: baseTime()}
	sessions := []generated.AgentSession{sampleSession("s1", nil)}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return sessions, nil
		},
		Clock: clock,
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.NeedsPreviewFetch() {
		t.Fatal("selection without Backend must need fetch")
	}
	if !p.PeekNeedsPreviewFetch() {
		t.Fatal("PeekNeedsPreviewFetch must mirror pending before Consume")
	}
	if !p.ConsumePreviewFetch() {
		t.Fatal("Consume should clear pending once")
	}
	if p.PeekNeedsPreviewFetch() {
		t.Fatal("Peek must be false after Consume")
	}
	if !p.NeedsPreviewFetch() {
		t.Fatal("NeedsPreviewFetch must remain true while loading after Consume")
	}
	if !p.PreviewLoading() {
		t.Fatal("still loading after Consume")
	}

	be := &fakeBackend{preview: backend.Preview{Lines: []string{"after-consume"}}}
	page.SetBackendForTest(p, be)
	if err := p.FetchPreviewIfNeeded(context.Background()); err != nil {
		t.Fatalf("IfNeeded after Consume must fetch: %v", err)
	}
	if p.PreviewLoading() {
		t.Fatal("must exit loading")
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q want ready", p.PreviewState())
	}
	if got := p.PreviewLines(); len(got) != 1 || got[0] != "after-consume" {
		t.Fatalf("PreviewLines=%v", got)
	}
	if countCalls(be.calls, "preview") != 1 {
		t.Fatalf("want 1 preview call; calls=%v", be.calls)
	}
}

func TestPreviewLinesClearedWhileLoadingAfterSelectionChange(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"from-a"}}}
	sessions := []generated.AgentSession{sampleSession("a", nil), sampleSession("b", nil)}
	clock := &fakeClock{now: baseTime()}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return sessions, nil
		},
		Backend: be,
		Clock:   clock,
	})
	p.SetSize(120, 30)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.SelectID("a") {
		t.Fatal("select a")
	}
	if got := p.PreviewLines(); len(got) != 1 || got[0] != "from-a" {
		t.Fatalf("PreviewLines=%v want [from-a]", got)
	}

	// Switch Backend off so B stays loading (no auto-fetch).
	page.SetBackendForTest(p, nil)
	if !p.SelectID("b") {
		t.Fatal("select b")
	}
	if !p.PreviewLoading() {
		t.Fatal("expected loading after select b without Backend")
	}
	if got := p.PreviewLines(); len(got) != 0 {
		t.Fatalf("PreviewLines must be empty while loading; got %v", got)
	}
	res := p.PreviewResult()
	if len(res.Lines) != 0 {
		t.Fatalf("PreviewResult must be cleared while loading; got %#v", res)
	}
}

func TestClassifyPreviewErrorStaleCodePreferred(t *testing.T) {
	stale := page.ClassifyPreviewError(&coreclient.APIError{
		StatusCode: http.StatusConflict,
		Code:       string(generated.StalePrecondition),
	})
	if stale != sessionpreview.OutcomeStale {
		t.Fatalf("stale_precondition => %v want stale", stale)
	}

	empty409 := page.ClassifyPreviewError(&coreclient.APIError{
		StatusCode: http.StatusConflict,
		Code:       "",
	})
	if empty409 != sessionpreview.OutcomeStale {
		t.Fatalf("empty code 409 => %v want stale", empty409)
	}

	empty412 := page.ClassifyPreviewError(&coreclient.APIError{
		StatusCode: http.StatusPreconditionFailed,
		Code:       "",
	})
	if empty412 != sessionpreview.OutcomeStale {
		t.Fatalf("empty code 412 => %v want stale", empty412)
	}

	other409 := page.ClassifyPreviewError(&coreclient.APIError{
		StatusCode: http.StatusConflict,
		Code:       "some_other_conflict",
	})
	if other409 != sessionpreview.OutcomeError {
		t.Fatalf("409 with other code => %v want error", other409)
	}
}

func TestRetryKeyWithoutSelectionIgnored(t *testing.T) {
	p := page.New(page.Deps{Clock: &fakeClock{now: baseTime()}})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.SelectedID() != "" {
		t.Fatalf("setup selected=%q", p.SelectedID())
	}
	intent, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if consumed || intent == page.IntentRetry {
		t.Fatalf("r without selection must not emit IntentRetry; intent=%q consumed=%v", intent, consumed)
	}
}

func TestTickPreviewRefetchesAfterReadyTTL(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"v1"}}}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return []generated.AgentSession{sampleSession("s1", nil)}, nil
		},
		Backend: be,
		Clock:   clock,
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q", p.PreviewState())
	}
	initial := countCalls(be.calls, "preview")
	if initial < 1 {
		t.Fatal("expected initial preview fetch")
	}

	clock.now = clock.now.Add(sessionpreview.ReadyTTL)
	be.preview = backend.Preview{Lines: []string{"v2"}}
	shouldFetch := p.TickPreview()
	if shouldFetch {
		t.Fatal("with Backend, TickPreview must auto-fetch and return false")
	}
	if countCalls(be.calls, "preview") != initial+1 {
		t.Fatalf("TTL expiry must refetch once; calls=%v initial=%d", be.calls, initial)
	}
	if got := p.PreviewLines(); len(got) != 1 || got[0] != "v2" {
		t.Fatalf("PreviewLines=%v want [v2]", got)
	}
}

func TestReloadSessionErrorRetriesOnceThenManualError(t *testing.T) {
	be := &fakeBackend{previewErr: &coreclient.APIError{
		StatusCode: http.StatusConflict,
		Code:       string(generated.StalePrecondition),
	}}
	var reloadCalls int
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return []generated.AgentSession{sampleSession("s1", nil)}, nil
		},
		Backend: be,
		Clock:   &fakeClock{now: baseTime()},
		ReloadSession: func(context.Context, string) (generated.AgentSession, error) {
			reloadCalls++
			return generated.AgentSession{}, errors.New("reload failed")
		},
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewState() != sessionpreview.StateError {
		t.Fatalf("state=%q want error", p.PreviewState())
	}
	if p.PreviewMessage() != sessionpreview.StaleManualErrorMessage {
		t.Fatalf("msg=%q", p.PreviewMessage())
	}
	if countCalls(be.calls, "preview") != 2 {
		t.Fatalf("reload error must still retry once (2 preview calls); calls=%v", be.calls)
	}
	if reloadCalls < 1 {
		t.Fatal("ReloadSession must be attempted")
	}
	if p.PreviewLoading() {
		t.Fatal("must exit loading")
	}
}

func TestMouseWheelAndDoubleClickWithVisibleBanner(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("a", nil),
		sampleSession("b", nil),
		sampleSession("c", nil),
	}
	be := &fakeBackend{previewErr: errors.New("banner-err")}
	p := newTestPage(t, sessions, be)
	p.SetSize(120, 30)
	_ = p.View()
	if p.OverlayLines() < 1 || p.PreviewMessage() == "" {
		t.Fatalf("need visible preview banner; overlay=%d msg=%q", p.OverlayLines(), p.PreviewMessage())
	}

	before := p.SelectedID()
	p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5 + p.OverlayLines()})
	after := p.SelectedID()
	if after == "" || after == before {
		p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5 + p.OverlayLines()})
		after = p.SelectedID()
		if after == before {
			t.Fatalf("wheel with banner overlay must change selection; before=%q after=%q overlay=%d", before, after, p.OverlayLines())
		}
	}

	if !p.SelectID("b") {
		t.Fatal("SelectID(b)")
	}
	// Keep error banner visible.
	be.previewErr = errors.New("still-err")
	_ = p.FetchPreview(context.Background())
	_ = p.View()
	if p.OverlayLines() < 1 {
		t.Fatalf("banner must remain for double-click; overlay=%d", p.OverlayLines())
	}
	rowY := 3
	click := tea.MouseMsg{X: 2, Y: rowY + p.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	p.Update(click)
	p.Update(click)
	if !p.ShowingDetails() && p.LastIntent() != resourcepage.IntentDetails {
		t.Fatalf("double-click with banner failed; intent=%q details=%v selected=%q overlay=%d",
			p.LastIntent(), p.ShowingDetails(), p.SelectedID(), p.OverlayLines())
	}
}

func TestNilLoadRefreshResetsPreviewMachine(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"x"}}}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("setup state=%q", p.PreviewState())
	}
	page.SetDepsLoadNilForTest(p)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewLoading() {
		t.Fatal("nil Load refresh must leave preview non-loading")
	}
	if p.NeedsPreviewFetch() {
		t.Fatal("nil Load refresh must clear pending fetch")
	}
	if got := p.PreviewLines(); len(got) != 0 {
		t.Fatalf("preview lines must be cleared; got %v", got)
	}
}
