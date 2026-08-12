package page_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/session/page"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectionWithBackendExitsLoading(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"line-a"}}}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if p.PreviewLoading() {
		t.Fatal("selection with Backend must exit loading (ready/empty/error), never stuck")
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q want ready", p.PreviewState())
	}
	if !containsCall(be.calls, "preview") {
		t.Fatalf("selection must issue one preview request; calls=%v", be.calls)
	}
	if p.NeedsPreviewFetch() {
		t.Fatal("pending fetch must be consumed after auto-fetch")
	}
}

func TestSelectionEmptyPreviewExitsLoading(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: nil}}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if p.PreviewLoading() {
		t.Fatal("empty preview must exit loading")
	}
	if p.PreviewState() != sessionpreview.StateEmpty {
		t.Fatalf("state=%q want empty", p.PreviewState())
	}
}

func TestSelectionErrorPreviewExitsLoading(t *testing.T) {
	be := &fakeBackend{previewErr: errors.New("boom")}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if p.PreviewLoading() {
		t.Fatal("error preview must exit loading")
	}
	if p.PreviewState() != sessionpreview.StateError {
		t.Fatalf("state=%q want error", p.PreviewState())
	}
}

func TestNeedsAndConsumePreviewFetchWithoutBackend(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	sessions := []generated.AgentSession{sampleSession("s1", nil)}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			out := make([]generated.AgentSession, len(sessions))
			copy(out, sessions)
			return out, nil
		},
		Clock: clock,
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.NeedsPreviewFetch() {
		t.Fatal("selection without Backend must leave NeedsPreviewFetch pending")
	}
	if p.PreviewLoading() != true {
		t.Fatal("expected loading until host fetches")
	}
	if !p.ConsumePreviewFetch() {
		t.Fatal("ConsumePreviewFetch should return true once")
	}
	if p.PeekNeedsPreviewFetch() || p.ConsumePreviewFetch() {
		t.Fatal("ConsumePreviewFetch must clear pending flag")
	}
	// NeedsPreviewFetch stays true while loading so IfNeeded remains usable.
	if !p.NeedsPreviewFetch() || !p.PreviewLoading() {
		t.Fatal("after Consume, NeedsPreviewFetch must still reflect loading selection")
	}
}

func TestInvalidatePreviewSetsPendingAndFetches(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"v1"}}}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	be.calls = nil
	be.preview = backend.Preview{Lines: []string{"v2"}}
	p.InvalidatePreview("s1")
	if p.PreviewLoading() {
		t.Fatal("invalidate with Backend must auto-fetch and exit loading")
	}
	if got := p.PreviewLines(); len(got) != 1 || got[0] != "v2" {
		t.Fatalf("PreviewLines=%v want [v2]", got)
	}
	if !containsCall(be.calls, "preview") {
		t.Fatalf("invalidate must issue request; calls=%v", be.calls)
	}
	p.InvalidatePreview("other")
	if containsCall(be.calls[1:], "preview") {
		t.Fatal("invalidate other session must not refetch")
	}
}

func TestPreviewResultAccessors(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"a", "b"}, Truncated: true}}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if got := p.PreviewLines(); len(got) != 2 || got[0] != "a" {
		t.Fatalf("PreviewLines=%v", got)
	}
	res := p.PreviewResult()
	if len(res.Lines) != 2 || !res.Truncated {
		t.Fatalf("PreviewResult=%#v", res)
	}
}

func TestFetchPreviewIfNeededRespectsTTL(t *testing.T) {
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"cached"}}}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	callsAfterSelect := countCalls(be.calls, "preview")
	if err := p.FetchPreviewIfNeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countCalls(be.calls, "preview") != callsAfterSelect {
		t.Fatalf("FetchPreviewIfNeeded must respect Ready TTL; calls=%v", be.calls)
	}
	// Force fetch still works.
	if err := p.FetchPreview(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countCalls(be.calls, "preview") != callsAfterSelect+1 {
		t.Fatalf("FetchPreview must force; calls=%v", be.calls)
	}
}

func TestStaleWithReloadSessionUsesNewKey(t *testing.T) {
	var refs []backend.SessionRef
	be := &trackingPreviewBackend{
		fakeBackend: fakeBackend{previewErr: &coreclient.APIError{StatusCode: 409, Code: string(generated.StalePrecondition)}},
		onPreview: func(ref backend.SessionRef) {
			refs = append(refs, ref)
		},
	}
	// Second call succeeds after reload bumps updated_at.
	be.errUntil = 1
	be.success = backend.Preview{Lines: []string{"after-reload"}}
	clock := &fakeClock{now: baseTime()}
	session := sampleSession("s1", nil)
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return []generated.AgentSession{session}, nil
		},
		Backend: be,
		Clock:   clock,
		ReloadSession: func(_ context.Context, id string) (generated.AgentSession, error) {
			s := session
			s.UpdatedAt = baseTime().Add(time.Minute)
			session = s
			return s, nil
		},
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q want ready after reload+retry", p.PreviewState())
	}
	if len(refs) < 2 {
		t.Fatalf("want >=2 preview calls, got %d", len(refs))
	}
	if refs[0].ExpectedUpdatedAt.Equal(refs[1].ExpectedUpdatedAt) {
		t.Fatalf("second fetch must use reloaded updated_at; refs=%v", refs)
	}
}

func TestStaleWithoutReloadSessionRetriesOnceThenError(t *testing.T) {
	be := &fakeBackend{previewErr: &coreclient.APIError{StatusCode: 409, Code: string(generated.StalePrecondition)}}
	clock := &fakeClock{now: baseTime()}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return []generated.AgentSession{sampleSession("s1", nil)}, nil
		},
		Backend: be,
		Clock:   clock,
		// ReloadSession nil — document: retry once with same key, second stale → manual error.
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
		t.Fatalf("nil ReloadSession must retry once (2 calls); calls=%v", be.calls)
	}
	if p.PreviewLoading() {
		t.Fatal("must exit loading")
	}
}

func TestInFlightGenerationIgnoresStaleApply(t *testing.T) {
	var switched atomic.Bool
	be := &midFlightBackend{
		linesA: []string{"from-a"},
		linesB: []string{"from-b"},
		onFirstPreview: func(p *page.Page) {
			if switched.Swap(true) {
				return
			}
			p.SelectID("b")
		},
	}
	sessions := []generated.AgentSession{sampleSession("a", nil), sampleSession("b", nil)}
	clock := &fakeClock{now: baseTime()}
	var p *page.Page
	p = page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return sessions, nil
		},
		Backend: be,
		Clock:   clock,
	})
	be.page = func() *page.Page { return p }
	p.SetSize(120, 30)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// After mid-flight switch, selected is b and preview must be b's (or loading cleared for b).
	if p.SelectedID() != "b" {
		t.Fatalf("selected=%q want b", p.SelectedID())
	}
	lines := p.PreviewLines()
	if len(lines) != 1 || lines[0] != "from-b" {
		t.Fatalf("stale apply from a must be ignored; PreviewLines=%v state=%q", lines, p.PreviewState())
	}
}

func TestOverlayLinesIncludesPreviewBanner(t *testing.T) {
	be := &fakeBackend{previewErr: errors.New("x")}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if msg := p.PreviewMessage(); msg == "" {
		t.Fatal("expected error banner message")
	}
	if p.OverlayLines() < 1 {
		t.Fatalf("OverlayLines=%d want >=1 for preview banner", p.OverlayLines())
	}
}

func TestMouseYAccountsForPreviewBanner(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("a", nil),
		sampleSession("b", nil),
		sampleSession("c", nil),
	}
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"ok"}}}
	p := newTestPage(t, sessions, be)
	p.SetSize(120, 30)
	_ = p.View()

	// Force a visible banner (empty) so OverlayLines includes +1.
	be.preview = backend.Preview{Lines: nil}
	p.InvalidatePreview(p.SelectedID())
	_ = p.View()
	bannerLines := 0
	if strings.Contains(ansi.Strip(p.View()), "Preview empty") || p.PreviewMessage() != "" || p.PreviewLoading() {
		bannerLines = 1
	}
	if bannerLines != 1 && p.OverlayLines() < 1 {
		t.Fatalf("need visible preview banner for mouse test; overlay=%d view=\n%s", p.OverlayLines(), ansi.Strip(p.View()))
	}

	hit := p.Inner().Table().FooterActionHit(resourceview.ActionCreate)
	if hit.Kind == 0 {
		t.Fatal("footer create hit missing")
	}
	intent, consumed := p.Update(tea.MouseMsg{
		X: hit.X, Y: hit.Y + p.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !consumed || intent != resourcepage.IntentCreate {
		t.Fatalf("footer create with banner offset => intent=%q consumed=%v", intent, consumed)
	}
}

func TestKeyboardMouseParityStrict(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("a", nil),
		sampleSession("b", nil),
		sampleSession("c", nil),
	}
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"ok"}}}
	p := newTestPage(t, sessions, be)
	p.SetSize(120, 30)
	_ = p.View()

	intent, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !consumed && !p.Inner().Table().Filtering() && intent != resourcepage.IntentFilter {
		t.Fatalf("filter key failed; intent=%q filtering=%v", intent, p.Inner().Table().Filtering())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if ok := p.SelectID("a"); !ok {
		t.Fatal("SelectID(a) must succeed")
	}
	intent, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent != resourcepage.IntentDetails && !p.ShowingDetails() {
		t.Fatalf("enter details failed; intent=%q details=%v", intent, p.ShowingDetails())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})

	intent, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	if intent != resourcepage.IntentCommands {
		t.Fatalf("commands intent=%q", intent)
	}

	_ = p.View()
	before := p.SelectedID()
	p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5 + p.OverlayLines()})
	after := p.SelectedID()
	if after == "" || after == before {
		// wheel should move selection when multiple rows
		p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5 + p.OverlayLines()})
		after = p.SelectedID()
		if after == before {
			t.Fatalf("wheel did not change selection; before=%q after=%q", before, after)
		}
	}

	hit := p.Inner().Table().FooterActionHit(resourceview.ActionCreate)
	if hit.Kind == 0 {
		t.Fatal("expected footer create hit")
	}
	intent, consumed = p.Update(tea.MouseMsg{
		X: hit.X, Y: hit.Y + p.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !consumed || intent != resourcepage.IntentCreate {
		t.Fatalf("footer create => intent=%q consumed=%v", intent, consumed)
	}

	p.SelectID("b")
	_ = p.View()
	rowY := 3
	click := tea.MouseMsg{X: 2, Y: rowY + p.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	p.Update(click)
	p.Update(click)
	if !p.ShowingDetails() && p.LastIntent() != resourcepage.IntentDetails {
		t.Fatalf("double-click details failed; intent=%q details=%v selected=%q", p.LastIntent(), p.ShowingDetails(), p.SelectedID())
	}
}

func TestCreateSessionUsesCreateRequest(t *testing.T) {
	be := &fakeBackend{}
	var saw backend.CreateSessionRequest
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			return []generated.AgentSession{sampleSession("s1", nil)}, nil
		},
		Backend: be,
		Clock:   &fakeClock{now: baseTime()},
		CreateRequest: func() backend.CreateSessionRequest {
			return backend.CreateSessionRequest{Label: "from-deps", Workspace: "/ws"}
		},
	})
	p.SetSize(80, 20)
	_ = p.Refresh(context.Background())
	be.CreateSessionFn = func(_ context.Context, req backend.CreateSessionRequest) (backend.Session, error) {
		saw = req
		return backend.Session{ID: "new"}, nil
	}
	if err := p.ExecuteIntent(context.Background(), page.IntentCreate); err != nil {
		t.Fatal(err)
	}
	if saw.Label != "from-deps" || saw.Workspace != "/ws" {
		t.Fatalf("CreateSessionRequest=%#v", saw)
	}

	p2 := page.New(page.Deps{
		Backend: be,
		Clock:   &fakeClock{now: baseTime()},
	})
	if err := p2.ExecuteIntent(context.Background(), page.IntentCreate); err == nil {
		t.Fatal("nil CreateRequest must return clear error")
	} else if !strings.Contains(err.Error(), "create request") {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectIDMissReturnsFalse(t *testing.T) {
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, &fakeBackend{preview: backend.Preview{Lines: []string{"x"}}})
	if p.SelectID("missing") {
		t.Fatal("SelectID miss must return false")
	}
	if p.SelectID("s1") != true {
		t.Fatal("SelectID hit must return true")
	}
}

func TestLoadErrorStatusSanitized(t *testing.T) {
	p := page.New(page.Deps{
		Load:  func(context.Context) ([]generated.AgentSession, error) { return nil, errors.New("secret db dsn=xyz") },
		Clock: &fakeClock{now: baseTime()},
	})
	p.SetSize(80, 20)
	_ = p.Refresh(context.Background())
	view := ansi.Strip(p.View())
	if strings.Contains(view, "secret") || strings.Contains(view, "dsn=") {
		t.Fatalf("raw error leaked into view:\n%s", view)
	}
	if !strings.Contains(view, "failed to load sessions") {
		t.Fatalf("expected sanitized status in view:\n%s", view)
	}
	if p.OverlayLines() < 1 {
		t.Fatal("validation error should contribute overlay")
	}
}

func TestIntentRetryAutoFetches(t *testing.T) {
	be := &fakeBackend{previewErr: errors.New("fail")}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	if p.PreviewState() != sessionpreview.StateError {
		t.Fatalf("setup state=%q", p.PreviewState())
	}
	be.previewErr = nil
	be.preview = backend.Preview{Lines: []string{"retry-ok"}}
	be.calls = nil
	intent, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !consumed || intent != page.IntentRetry {
		t.Fatalf("intent=%q consumed=%v", intent, consumed)
	}
	if p.PreviewLoading() {
		t.Fatal("retry with Backend must auto-fetch and exit loading")
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q", p.PreviewState())
	}
	if !containsCall(be.calls, "preview") {
		t.Fatalf("retry must fetch; calls=%v", be.calls)
	}
}

func TestDetailsExposeNativeActivityObservedAt(t *testing.T) {
	obs := baseTime().Add(-30 * time.Second)
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", func(s *generated.AgentSession) {
		s.NativeActivityObservedAt = &obs
	})}, &fakeBackend{preview: backend.Preview{Lines: []string{"x"}}})
	p.SelectID("s1")
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(p.View())
	if !strings.Contains(view, "native_activity_observed_at") {
		t.Fatalf("details missing native_activity_observed_at:\n%s", view)
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func countCalls(calls []string, want string) int {
	n := 0
	for _, c := range calls {
		if c == want {
			n++
		}
	}
	return n
}

type trackingPreviewBackend struct {
	fakeBackend
	onPreview func(backend.SessionRef)
	errUntil  int
	success   backend.Preview
	n         int
}

func (b *trackingPreviewBackend) Preview(ctx context.Context, ref backend.SessionRef) (backend.Preview, error) {
	b.n++
	b.calls = append(b.calls, "preview")
	if b.onPreview != nil {
		b.onPreview(ref)
	}
	if b.n <= b.errUntil {
		if b.previewErr != nil {
			return backend.Preview{}, b.previewErr
		}
	}
	if b.success.SessionID != "" || len(b.success.Lines) > 0 {
		return b.success, nil
	}
	return b.fakeBackend.Preview(ctx, ref)
}

type midFlightBackend struct {
	backend.UnimplementedBackend
	linesA         []string
	linesB         []string
	onFirstPreview func(*page.Page)
	page           func() *page.Page
	calls          int
}

func (b *midFlightBackend) Preview(_ context.Context, ref backend.SessionRef) (backend.Preview, error) {
	b.calls++
	if b.calls == 1 && b.onFirstPreview != nil && b.page != nil {
		b.onFirstPreview(b.page())
	}
	switch ref.ID {
	case "a":
		return backend.Preview{SessionID: "a", Lines: b.linesA}, nil
	case "b":
		return backend.Preview{SessionID: "b", Lines: b.linesB}, nil
	default:
		return backend.Preview{}, errors.New("unknown")
	}
}
