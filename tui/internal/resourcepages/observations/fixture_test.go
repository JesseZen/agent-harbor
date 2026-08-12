package observations

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
)

func sampleObservations() []backend.Observation {
	base := time.Date(2026, 7, 27, 14, 30, 0, 0, time.UTC)
	return []backend.Observation{
		{
			ID:                "obs-1",
			Type:              "request",
			OccurredAt:        base.Add(-10 * time.Second),
			SessionID:         "ses-aaa",
			RouteID:           "route-primary",
			TargetID:          "target-openai",
			DecisionReason:    "healthy weighted candidate",
			SemanticOutcome:   "success",
			SnapshotGeneration: 42,
		},
		{
			ID:             "obs-2",
			Type:           "quota",
			OccurredAt:     base.Add(-28 * time.Second),
			QuotaGroupID:   "quota-background",
			DecisionReason: "background permit queued",
		},
	}
}

func newTestPage(t *testing.T) *Page {
	t.Helper()
	page := NewPage()
	page.SetSize(120, 30)
	page.SetObservations(sampleObservations())
	return page
}
