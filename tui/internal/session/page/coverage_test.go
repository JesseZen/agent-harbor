package page_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/session/page"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCoverageHelpersAndErrorPaths(t *testing.T) {
	p := page.New(page.Deps{})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.State() != resourcepage.StateEmpty {
		t.Fatalf("state=%q", p.State())
	}
	if cells := p.RowCells("missing"); cells != nil {
		t.Fatalf("missing row cells=%v", cells)
	}

	loadErr := errors.New("load boom")
	p = page.New(page.Deps{
		Load:  func(context.Context) ([]generated.AgentSession, error) { return nil, loadErr },
		Clock: &fakeClock{now: baseTime()},
	})
	if err := p.Refresh(context.Background()); !errors.Is(err, loadErr) {
		t.Fatalf("refresh err=%v", err)
	}

	sessions := []generated.AgentSession{
		sampleSession("age-s", func(s *generated.AgentSession) {
			s.CreatedAt = baseTime().Add(-30 * time.Second)
		}),
		sampleSession("age-m", func(s *generated.AgentSession) {
			s.CreatedAt = baseTime().Add(-30 * time.Minute)
		}),
		sampleSession("age-d", func(s *generated.AgentSession) {
			s.CreatedAt = baseTime().Add(-72 * time.Hour)
		}),
		sampleSession("nil-native", func(s *generated.AgentSession) {
			s.NativeSessionId = nil
			s.NativeProvider = nil
		}),
	}
	be := &fakeBackend{preview: backend.Preview{SessionID: "age-s", Lines: []string{"hello"}}}
	p = newTestPage(t, sessions, be)
	_ = p.View()
	for _, id := range []string{"age-s", "age-m", "age-d"} {
		cells := p.RowCells(id)
		if len(cells) != 7 {
			t.Fatalf("%s cells=%v", id, cells)
		}
	}
	p.SelectID("nil-native")
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ansi.Strip(p.View())
	if !strings.Contains(view, "native_session_id:") {
		t.Fatalf("details:\n%s", view)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})

	p.SelectID("age-s")
	if err := p.FetchPreview(context.Background()); err != nil {
		t.Fatalf("preview success: %v", err)
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q", p.PreviewState())
	}

	be.preview = backend.Preview{SessionID: "age-s", Lines: nil}
	_, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = p.FetchPreview(context.Background())
	if p.PreviewState() != sessionpreview.StateEmpty {
		t.Fatalf("empty preview state=%q", p.PreviewState())
	}
	_ = p.View()
	_ = p.TickPreview()

	p2 := page.New(page.Deps{Backend: be, Clock: &fakeClock{now: baseTime()}})
	if err := p2.ExecuteIntent(context.Background(), page.IntentAttach); err == nil {
		t.Fatal("attach without selection should fail")
	}
	if err := p2.ExecuteIntent(context.Background(), page.IntentDetails); err == nil {
		t.Fatal("unsupported intent should fail")
	}
	p3 := page.New(page.Deps{Clock: &fakeClock{now: baseTime()}})
	if err := p3.ExecuteIntent(context.Background(), page.IntentCreate); err == nil {
		t.Fatal("nil backend should fail")
	}

	be.previewErr = errors.New("nope")
	p4 := newTestPage(t, []generated.AgentSession{sampleSession("s1", nil)}, be)
	p4.SelectID("s1")
	_ = p4.FetchPreview(context.Background())
	_ = p4.View()
	intent, _ := p4.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if intent != page.IntentRetry {
		t.Fatalf("retry intent=%q", intent)
	}

	// details with no selection
	p5 := page.New(page.Deps{Clock: &fakeClock{now: baseTime()}})
	p5.SetSize(80, 20)
	// force details mode via enter without rows — Open via Update after empty refresh
	_ = p5.Refresh(context.Background())
	p5.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = p5.View()
}
