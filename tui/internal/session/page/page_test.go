package page_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeBackend struct {
	backend.UnimplementedBackend
	previewErr      error
	preview         backend.Preview
	calls           []string
	CreateSessionFn func(context.Context, backend.CreateSessionRequest) (backend.Session, error)
}

func (b *fakeBackend) CreateSession(ctx context.Context, req backend.CreateSessionRequest) (backend.Session, error) {
	b.calls = append(b.calls, "create")
	if b.CreateSessionFn != nil {
		return b.CreateSessionFn(ctx, req)
	}
	return backend.Session{ID: "new"}, nil
}
func (b *fakeBackend) LaunchSession(context.Context, backend.SessionRef, time.Duration) (backend.Session, error) {
	b.calls = append(b.calls, "launch")
	return backend.Session{}, nil
}
func (b *fakeBackend) ResumeSession(context.Context, backend.SessionRef, time.Duration) (backend.Session, error) {
	b.calls = append(b.calls, "resume")
	return backend.Session{}, nil
}
func (b *fakeBackend) EndSession(context.Context, backend.EndSessionRequest) error {
	b.calls = append(b.calls, "end")
	return nil
}
func (b *fakeBackend) PrepareAttach(context.Context, backend.SessionRef, io.Writer) (backend.AttachCommand, error) {
	b.calls = append(b.calls, "attach")
	return backend.AttachCommand{SessionID: "s1"}, nil
}
func (b *fakeBackend) Preview(context.Context, backend.SessionRef) (backend.Preview, error) {
	b.calls = append(b.calls, "preview")
	if b.previewErr != nil {
		return backend.Preview{}, b.previewErr
	}
	return b.preview, nil
}

func ptr[T any](v T) *T { return &v }

func baseTime() time.Time {
	return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
}

func sampleSession(id string, mut func(*generated.AgentSession)) generated.AgentSession {
	provider := generated.AgentSessionNativeProviderCodex
	s := generated.AgentSession{
		Id:                   generated.SessionID(id),
		Label:                id + "-label",
		Lifecycle:            generated.AgentSessionLifecycleRunning,
		NativeActivity:       generated.AgentSessionNativeActivityIdle,
		ActivitySource:       generated.SessionActivitySourceHook,
		HookHealth:           generated.AgentSessionHookHealthActive,
		HookHealthObservedAt: baseTime(),
		CreatedAt:            baseTime().Add(-2 * time.Hour),
		UpdatedAt:            baseTime(),
		Workspace:            "/tmp/ws/" + id,
		ClientProfileId:      "prof-a",
		RouteId:              "route-a",
		NativeProvider:       &provider,
		NativeSessionId:      ptr("native-" + id),
		SessionCredential:    generated.SessionCredentialView{Status: generated.None},
	}
	if mut != nil {
		mut(&s)
	}
	return s
}

func newTestPage(t *testing.T, sessions []generated.AgentSession, be *fakeBackend) *page.Page {
	t.Helper()
	if be == nil {
		be = &fakeBackend{preview: backend.Preview{Lines: []string{"preview"}}}
	}
	clock := &fakeClock{now: baseTime()}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			out := make([]generated.AgentSession, len(sessions))
			copy(out, sessions)
			return out, nil
		},
		Backend: be,
		Clock:   clock,
		CreateRequest: func() backend.CreateSessionRequest {
			return backend.CreateSessionRequest{Label: "new-session"}
		},
		ReloadSession: func(_ context.Context, id string) (generated.AgentSession, error) {
			for _, s := range sessions {
				if string(s.Id) == id {
					s.UpdatedAt = baseTime().Add(time.Minute)
					return s, nil
				}
			}
			return generated.AgentSession{}, errors.New("missing")
		},
	})
	p.SetSize(160, 45)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return p
}

func TestRenderHookPaneProcessLifecycleUnknownSources(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("hook", func(s *generated.AgentSession) {
			s.ActivitySource = generated.SessionActivitySourceHook
			s.NativeActivity = generated.AgentSessionNativeActivityRunning
		}),
		sampleSession("pane", func(s *generated.AgentSession) {
			s.ActivitySource = generated.SessionActivitySourcePane
			s.NativeActivity = generated.AgentSessionNativeActivityWaiting
		}),
		sampleSession("proc", func(s *generated.AgentSession) {
			s.ActivitySource = generated.SessionActivitySourceProcess
			s.NativeActivity = generated.AgentSessionNativeActivityFailed
		}),
		sampleSession("life", func(s *generated.AgentSession) {
			s.ActivitySource = generated.SessionActivitySourceLifecycle
			s.NativeActivity = generated.AgentSessionNativeActivityUnknown
		}),
		sampleSession("unk", func(s *generated.AgentSession) {
			s.ActivitySource = generated.SessionActivitySourceUnknown
			s.NativeActivity = generated.AgentSessionNativeActivityIdle
		}),
	}
	p := newTestPage(t, sessions, nil)
	view := ansi.Strip(p.View())
	for _, want := range []string{"hook", "pane", "process", "lifecycle", "unknown", "running", "waiting", "failed", "idle"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in view:\n%s", want, view)
		}
	}
}

func TestDetectingOnlyLaunchingWithoutNativeID(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("detect", func(s *generated.AgentSession) {
			s.Lifecycle = generated.AgentSessionLifecycleLaunching
			s.NativeSessionId = nil
			s.NativeActivity = generated.AgentSessionNativeActivityUnknown
			s.ActivitySource = generated.SessionActivitySourceUnknown
		}),
		sampleSession("launch-bound", func(s *generated.AgentSession) {
			s.Lifecycle = generated.AgentSessionLifecycleLaunching
			s.NativeSessionId = ptr("native-bound")
			s.NativeActivity = generated.AgentSessionNativeActivityUnknown
		}),
		sampleSession("running-nil", func(s *generated.AgentSession) {
			s.Lifecycle = generated.AgentSessionLifecycleRunning
			s.NativeSessionId = nil
			s.NativeActivity = generated.AgentSessionNativeActivityIdle
		}),
	}
	p := newTestPage(t, sessions, nil)
	view := ansi.Strip(p.View())
	if !strings.Contains(view, "detecting") {
		t.Fatalf("expected detecting for launching without native id:\n%s", view)
	}
	// Count detecting occurrences — only one row.
	if strings.Count(view, "detecting") != 1 {
		t.Fatalf("detecting count=%d want 1:\n%s", strings.Count(view, "detecting"), view)
	}
}

func TestHookHealthStaleInvalidExplicit(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("stale", func(s *generated.AgentSession) {
			s.HookHealth = generated.AgentSessionHookHealthStale
			s.HookHealthObservedAt = baseTime().Add(-time.Minute)
		}),
		sampleSession("invalid", func(s *generated.AgentSession) {
			s.HookHealth = generated.AgentSessionHookHealthInvalid
			s.HookHealthObservedAt = baseTime().Add(-2 * time.Minute)
		}),
	}
	p := newTestPage(t, sessions, nil)
	p.SelectID("stale")
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(p.View())
	if !strings.Contains(view, "stale") || !strings.Contains(strings.ToLower(view), "hook") {
		t.Fatalf("details missing stale hook health:\n%s", view)
	}
	if !strings.Contains(view, "native") || !strings.Contains(view, "native-stale") {
		t.Fatalf("details missing native id:\n%s", view)
	}
	if !strings.Contains(view, "observed") && !strings.Contains(view, "hook_health_observed_at") {
		t.Fatalf("details missing observation time:\n%s", view)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	p.SelectID("invalid")
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view = ansi.Strip(p.View())
	if !strings.Contains(view, "invalid") {
		t.Fatalf("details missing invalid hook health:\n%s", view)
	}
}

func TestColumnsAndNoLifecycleToWorkingMapping(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("run", func(s *generated.AgentSession) {
			s.Lifecycle = generated.AgentSessionLifecycleRunning
			s.NativeActivity = generated.AgentSessionNativeActivityIdle
			s.ActivitySource = generated.SessionActivitySourcePane
		}),
	}
	p := newTestPage(t, sessions, nil)
	view := ansi.Strip(p.View())
	for _, col := range []string{"NAME", "LIFECYCLE", "ACTIVITY", "SOURCE", "PROVIDER", "WORKSPACE", "AGE"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %s:\n%s", col, view)
		}
	}
	if strings.Contains(view, "working") {
		t.Fatalf("must not map lifecycle→working:\n%s", view)
	}
	if !strings.Contains(view, "idle") || !strings.Contains(view, "running") {
		t.Fatalf("must show authoritative activity idle and lifecycle running:\n%s", view)
	}
	cells := p.RowCells("run")
	if len(cells) < 3 || cells[2] != "idle" {
		t.Fatalf("ACTIVITY cell=%v want idle (not derived from lifecycle)", cells)
	}
}

func TestAttachLaunchResumeEndIntentsPreserved(t *testing.T) {
	be := &fakeBackend{}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	p.SelectID("s1")

	cases := []struct {
		key  string
		want string
		call string
	}{
		{"n", string(page.IntentCreate), "create"},
		{"a", string(page.IntentAttach), "attach"},
		{"l", string(page.IntentLaunch), "launch"},
		{"R", string(page.IntentResume), "resume"},
		{"x", string(page.IntentEnd), "end"},
	}
	for _, tc := range cases {
		be.calls = nil
		intent, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
		if !consumed || string(intent) != tc.want {
			t.Fatalf("key %q => intent=%q consumed=%v want %q", tc.key, intent, consumed, tc.want)
		}
		if err := p.ExecuteIntent(context.Background(), intent); err != nil {
			t.Fatalf("execute %s: %v", intent, err)
		}
		if len(be.calls) == 0 || be.calls[0] != tc.call {
			t.Fatalf("key %q backend calls=%v want %q", tc.key, be.calls, tc.call)
		}
	}
	// No publish
	intent, _ := p.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("sessions must not expose publish")
	}
}

func TestKeyboardMouseParity(t *testing.T) {
	// Strict parity assertions live in TestKeyboardMouseParityStrict (fix-r1).
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
	if !consumed || intent != resourcepage.IntentFilter && !p.Inner().Table().Filtering() {
		if !p.Inner().Table().Filtering() && intent != resourcepage.IntentFilter {
			t.Fatalf("filter key failed; intent=%q filtering=%v", intent, p.Inner().Table().Filtering())
		}
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !p.SelectID("a") {
		t.Fatal("SelectID(a)")
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
	hit := p.Inner().Table().FooterActionHit(resourceview.ActionFilter)
	if hit.Kind != 0 {
		p.Update(tea.MouseMsg{X: hit.X, Y: hit.Y + p.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}

	before := p.SelectedID()
	p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5 + p.OverlayLines()})
	if p.SelectedID() == before {
		p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, Y: 5 + p.OverlayLines()})
	}
	if p.SelectedID() == before {
		t.Fatalf("wheel did not move selection from %q", before)
	}

	_ = p.View()
	rowY := 3
	click := tea.MouseMsg{X: 2, Y: rowY + p.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	p.Update(click)
	p.Update(click)
	if !p.ShowingDetails() && p.LastIntent() != resourcepage.IntentDetails {
		t.Fatalf("double-click details failed; intent=%q details=%v selected=%q", p.LastIntent(), p.ShowingDetails(), p.SelectedID())
	}
}

func TestPreviewClassifyOutcomes(t *testing.T) {
	be := &fakeBackend{
		previewErr: &coreclient.APIError{StatusCode: 409, Code: string(generated.StalePrecondition)},
	}
	p := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	p.SelectID("s1")
	_ = p.FetchPreview(context.Background())
	// Stale retries once then lands on manual error (backend stays stale).
	if p.PreviewState() != sessionpreview.StateError {
		t.Fatalf("after double stale want error; state=%q loading=%v msg=%q", p.PreviewState(), p.PreviewLoading(), p.PreviewMessage())
	}
	if p.PreviewMessage() != "Session changed again; press r" {
		t.Fatalf("stale message=%q", p.PreviewMessage())
	}
	if p.PreviewLoading() {
		t.Fatal("double stale must exit loading")
	}

	be.previewErr = &coreclient.APIError{StatusCode: 503, Code: string(generated.PreviewUnavailable)}
	p2 := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	p2.SelectID("s1")
	_ = p2.FetchPreview(context.Background())
	if p2.PreviewState() != sessionpreview.StateUnavailable {
		t.Fatalf("unavailable state=%q", p2.PreviewState())
	}

	be.previewErr = context.Canceled
	p3 := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	p3.SelectID("s1")
	_ = p3.FetchPreview(context.Background())
	if p3.PreviewLoading() {
		t.Fatal("canceled must exit loading")
	}

	be.previewErr = context.DeadlineExceeded
	p4 := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	p4.SelectID("s1")
	_ = p4.FetchPreview(context.Background())
	if p4.PreviewLoading() {
		t.Fatal("timeout must exit loading")
	}
}

func TestFourSizeGoldens(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("alpha", func(s *generated.AgentSession) {
			s.Label = "alpha-session"
			s.NativeActivity = generated.AgentSessionNativeActivityRunning
			s.ActivitySource = generated.SessionActivitySourceHook
		}),
		sampleSession("beta", func(s *generated.AgentSession) {
			s.Label = "beta-session"
			s.Lifecycle = generated.AgentSessionLifecycleLaunching
			s.NativeSessionId = nil
			s.NativeActivity = generated.AgentSessionNativeActivityUnknown
			s.ActivitySource = generated.SessionActivitySourceUnknown
			s.HookHealth = generated.AgentSessionHookHealthStale
			s.NativeProvider = nil
		}),
	}
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"golden-preview"}}}
	p := newTestPage(t, sessions, be)
	sizes := []struct{ w, h int }{{160, 45}, {120, 30}, {90, 30}, {70, 30}}
	for _, size := range sizes {
		p.SetSize(size.w, size.h)
		_ = p.Refresh(context.Background())
		got := ansi.Strip(p.View())
		if !strings.Contains(got, "NAME") || !strings.Contains(got, "alpha-session") {
			t.Fatalf("%dx%d lost identity:\n%s", size.w, size.h, got)
		}
		path := filepath.Join("testdata", "golden", "sessions_"+itoa(size.w)+"x"+itoa(size.h)+".ansi")
		assertOrUpdateGolden(t, path, got)
	}
}

func assertOrUpdateGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run UPDATE_GOLDEN=1): %v\n--- got ---\n%s", path, err, got)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(got) {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
