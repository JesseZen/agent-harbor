package coreclient

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestProjectStatusCoversLifecycle(t *testing.T) {
	cases := map[generated.AgentSessionLifecycle]backend.Status{
		generated.AgentSessionLifecycleCreated:   backend.StatusCreated,
		generated.AgentSessionLifecycleLaunching: backend.StatusLaunching,
		generated.AgentSessionLifecycleRunning:   backend.StatusRunning,
		generated.AgentSessionLifecycleEnding:    backend.StatusEnding,
		generated.AgentSessionLifecycleIdle:      backend.StatusIdle,
		generated.AgentSessionLifecycleFailed:    backend.StatusFailed,
		generated.AgentSessionLifecycleEnded:     backend.StatusEnded,
		generated.AgentSessionLifecycle("other"): backend.StatusFailed,
	}
	for lifecycle, want := range cases {
		if got := projectStatus(lifecycle); got != want {
			t.Fatalf("projectStatus(%q) = %q, want %q", lifecycle, got, want)
		}
	}
	if optionalString((*string)(nil)) != "" {
		t.Fatal("nil optionalString")
	}
	value := "route"
	if optionalString(&value) != "route" {
		t.Fatal("optionalString value")
	}
}
