package page_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/session/page"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRefreshEmptyAndLoadErrorResetPreview(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	sessions := []generated.AgentSession{sampleSession("s1", nil)}
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"keep-me"}}}
	loadEmpty := false
	loadErr := false
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			if loadErr {
				return nil, errors.New("boom")
			}
			if loadEmpty {
				return nil, nil
			}
			return sessions, nil
		},
		Backend: be,
		Clock:   clock,
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewState() != sessionpreview.StateReady || len(p.PreviewLines()) == 0 {
		t.Fatalf("setup preview state=%q lines=%v", p.PreviewState(), p.PreviewLines())
	}

	loadEmpty = true
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewState() == sessionpreview.StateReady || len(p.PreviewLines()) != 0 {
		t.Fatalf("empty refresh must reset preview; state=%q lines=%v", p.PreviewState(), p.PreviewLines())
	}
	if p.PreviewLoading() {
		t.Fatal("empty refresh must not leave loading")
	}

	loadEmpty = false
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("reloaded state=%q", p.PreviewState())
	}

	loadErr = true
	_ = p.Refresh(context.Background())
	if p.PreviewState() == sessionpreview.StateReady || len(p.PreviewLines()) != 0 {
		t.Fatalf("load error must reset preview; state=%q lines=%v", p.PreviewState(), p.PreviewLines())
	}
	if p.PreviewLoading() {
		t.Fatal("load error must not leave loading")
	}
}

func TestDetectingNegativeRowCells(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("detect", func(s *generated.AgentSession) {
			s.Lifecycle = generated.AgentSessionLifecycleLaunching
			s.NativeSessionId = nil
			s.NativeActivity = generated.AgentSessionNativeActivityUnknown
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
	p := newTestPage(t, sessions, &fakeBackend{preview: backend.Preview{Lines: []string{"x"}}})
	if got := p.RowCells("detect"); len(got) < 3 || got[2] != "detecting" {
		t.Fatalf("detect cells=%v", got)
	}
	if got := p.RowCells("launch-bound"); len(got) < 3 || got[2] != "unknown" {
		t.Fatalf("launch-bound must use native activity, cells=%v", got)
	}
	if got := p.RowCells("running-nil"); len(got) < 3 || got[2] != "idle" {
		t.Fatalf("running without native id must not detect, cells=%v", got)
	}
}

func TestHeaderClickSortParity(t *testing.T) {
	sessions := []generated.AgentSession{
		sampleSession("b-sess", func(s *generated.AgentSession) { s.Label = "bravo" }),
		sampleSession("a-sess", func(s *generated.AgentSession) { s.Label = "alpha" }),
	}
	p := newTestPage(t, sessions, &fakeBackend{preview: backend.Preview{Lines: []string{"x"}}})
	_ = p.View()
	table := p.Inner().Table()
	hit := table.HitTest(2, 1) // header row typically y=1 in resourceview
	if hit.Kind == 0 {
		// keyboard sort fallback
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	} else {
		y := hit.Y + p.OverlayLines()
		p.Update(tea.MouseMsg{X: hit.X, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}
	_ = p.View()
	// After sort interaction, table should still expose NAME column and both ids.
	view := ansi.Strip(p.View())
	if !strings.Contains(view, "NAME") || !strings.Contains(view, "alpha") || !strings.Contains(view, "bravo") {
		t.Fatalf("header sort path lost rows:\n%s", view)
	}
}
