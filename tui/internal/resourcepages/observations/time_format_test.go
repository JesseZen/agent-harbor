package observations

import (
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
)

func TestFormatTimeUsesLocalWallClockWithoutZoneSuffix(t *testing.T) {
	prev := time.Local
	time.Local = time.FixedZone("CST", 8*3600)
	t.Cleanup(func() { time.Local = prev })

	at := time.Date(2026, 7, 27, 14, 30, 0, 0, time.UTC)
	got := formatTime(at)
	if got != "22:30:00" {
		t.Fatalf("formatTime=%q want 22:30:00", got)
	}
	if strings.ContainsAny(got, "Z+") {
		t.Fatalf("table time must not include zone suffix: %q", got)
	}
}

func TestDetailTextUsesLocalRFC3339WithOffset(t *testing.T) {
	prev := time.Local
	time.Local = time.FixedZone("CST", 8*3600)
	t.Cleanup(func() { time.Local = prev })

	obs := backend.Observation{
		ID:         "obs-1",
		Type:       "request",
		OccurredAt: time.Date(2026, 7, 27, 14, 30, 0, 0, time.UTC),
	}
	text := detailText(obs)
	want := "occurred_at: 2026-07-27T22:30:00+08:00"
	if !strings.Contains(text, want) {
		t.Fatalf("detail missing %q:\n%s", want, text)
	}
	if strings.Contains(text, "occurred_at: 2026-07-27T14:30:00Z") {
		t.Fatalf("detail still uses UTC Z:\n%s", text)
	}
}
