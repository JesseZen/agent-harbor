package quotas

import (
	"testing"
	"time"
)

func TestFormatNextUsesLocalWallClock(t *testing.T) {
	prev := time.Local
	time.Local = time.FixedZone("CST", 8*3600)
	t.Cleanup(func() { time.Local = prev })

	at := time.Date(2026, 7, 27, 12, 0, 3, 0, time.UTC)
	got := formatNext(at)
	if got != "20:00:03" {
		t.Fatalf("formatNext=%q want 20:00:03", got)
	}
}
