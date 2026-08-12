package quotas

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCoverageHelpersAndEdgeCases(t *testing.T) {
	page, draft := newTestPage(t)

	_ = inboundRefs(draft, "missing")
	_, _ = findQuota(draft.LocalCommand(), "missing")
	_ = quotaExists(draft, "missing")
	_ = formatNext(sampleRuntime()[0].NextPermitAt)
	_ = rowsFor(draft, nil)

	page.BeginCreate()
	_ = page.View()
	page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page.Update(tea.KeyMsg{Type: tea.KeyUp})
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                  "quota-dup",
		"name":                "dup",
		"rpm":                 "60",
		"max_concurrency":     "2",
		"foreground_capacity": "1",
		"background_capacity": "1",
		"foreground_weight":   "9",
		"background_weight":   "1",
		"queue_timeout_ms":    "30000",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create dup setup: %v", err)
	}
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                  "quota-dup",
		"name":                "dup2",
		"rpm":                 "60",
		"max_concurrency":     "2",
		"foreground_capacity": "1",
		"background_capacity": "1",
		"foreground_weight":   "9",
		"background_weight":   "1",
		"queue_timeout_ms":    "30000",
	})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected duplicate id error")
	}

	page.SetState(resourcepage.StateStale)
	_ = page.OverlayLines()
	page.ClearForcedState()
	page.Sync()

	draft.SetDisconnected(true)
	page.Sync()
	page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if page.LastIntent() == resourcepage.IntentPublish {
		t.Fatal("publish suppressed")
	}

	page.CancelOverlay()
	page.SelectID("quota-default")
	page.OpenDetails()
	_ = page.View()
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})

	page.SelectID("quota-default")
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_ = page.View()
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})

	_, _ = buildQuota(map[string]string{
		"id": "q", "name": "n", "rpm": "1", "max_concurrency": "1",
		"foreground_capacity": "0", "background_capacity": "0",
		"foreground_weight": "9", "background_weight": "1", "queue_timeout_ms": "1",
	}, generated.QuotaGroupConfig{}, true)

	err := validateValues(map[string]string{
		"id": "q", "name": "", "rpm": "1", "max_concurrency": "1",
		"foreground_capacity": "0", "background_capacity": "0",
		"foreground_weight": "9", "background_weight": "1", "queue_timeout_ms": "1",
	}, true)
	if err == nil || !strings.Contains(err.Error(), "$.name") {
		t.Fatalf("validate err=%v", err)
	}
}
