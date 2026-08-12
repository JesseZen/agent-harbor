package observations

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCoverageHelpers(t *testing.T) {
	_ = formatTime(time.Time{})
	_ = outcomeLabel(backend.Observation{PolicyDecision: "allowed"})
	_ = dashIfEmpty("")
	_ = boundCopyText("short")
	_ = boundCopyText(string(make([]byte, MaxCopyBytes+10)))
	_, _ = findObservation(nil, "x")

	page := NewPage()
	page.SetSize(80, 20)
	page.SetObservations(sampleObservations())
	page.Update(nil)
	page.SelectID("obs-1")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = page.View()
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if page.LastIntent() == resourcepage.IntentCreate {
		t.Fatal("create suppressed")
	}
}
