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

func TestLoadErrorDoesNotShowFalsePreviewEmpty(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	fail := false
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"ok"}}}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			if fail {
				return nil, errors.New("transient")
			}
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
		t.Fatalf("setup state=%q", p.PreviewState())
	}

	fail = true
	_ = p.Refresh(context.Background())
	view := ansi.Strip(p.View())
	if strings.Contains(view, "Preview empty") {
		t.Fatalf("load error must not show false Preview empty:\n%s", view)
	}
	if p.SelectedID() != "s1" {
		t.Fatalf("table selection should remain s1, got %q", p.SelectedID())
	}
}

func TestManualRetryRebindsAfterLoadErrorReset(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	fail := false
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"after-r"}}}
	p := page.New(page.Deps{
		Load: func(context.Context) ([]generated.AgentSession, error) {
			if fail {
				return nil, errors.New("transient")
			}
			return []generated.AgentSession{sampleSession("s1", nil)}, nil
		},
		Backend: be,
		Clock:   clock,
	})
	p.SetSize(80, 20)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	_ = p.Refresh(context.Background())

	before := countCall(be.calls, "preview")
	intent, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !consumed || intent != page.IntentRetry {
		t.Fatalf("intent=%q consumed=%v", intent, consumed)
	}
	if countCall(be.calls, "preview") <= before {
		t.Fatal("r after load-error reset must issue a preview fetch")
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q want ready", p.PreviewState())
	}
	if got := p.PreviewLines(); len(got) != 1 || got[0] != "after-r" {
		t.Fatalf("lines=%v", got)
	}
}

func countCall(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}
