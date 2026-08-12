package page_test

import (
	"context"
	"errors"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/session/page"
	"github.com/asheshgoplani/agent-deck/internal/sessionpreview"
)

func TestInvalidatePreviewRebindsAfterLoadErrorReset(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	fail := false
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"sse"}}}
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
	p.InvalidatePreview("s1")
	if !p.PreviewLoading() && p.PreviewState() != sessionpreview.StateReady {
		// After invalidate+auto-fetch with Backend, should leave loading via ready/empty/error.
		if p.PreviewState() != sessionpreview.StateReady {
			t.Fatalf("state=%q loading=%v after invalidate", p.PreviewState(), p.PreviewLoading())
		}
	}
	if countCall(be.calls, "preview") <= before {
		t.Fatal("InvalidatePreview after unbound must rebind and fetch for selected session")
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q want ready", p.PreviewState())
	}
}

func TestFetchPreviewRebindsAfterLoadErrorReset(t *testing.T) {
	clock := &fakeClock{now: baseTime()}
	fail := false
	be := &fakeBackend{preview: backend.Preview{Lines: []string{"force"}}}
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
	if err := p.FetchPreview(context.Background()); err != nil {
		t.Fatalf("FetchPreview after unbound: %v", err)
	}
	if countCall(be.calls, "preview") <= before {
		t.Fatal("FetchPreview must rebind and fetch")
	}
	if p.PreviewState() != sessionpreview.StateReady {
		t.Fatalf("state=%q", p.PreviewState())
	}
	if got := p.PreviewLines(); len(got) != 1 || got[0] != "force" {
		t.Fatalf("lines=%v", got)
	}
}
