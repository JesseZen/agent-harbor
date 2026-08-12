package resourcepage

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPageMouseAdjustsForDisconnectedBanner(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 12)
	page.SetState(StateDisconnected)
	page.SetRows(FixtureRows(3))
	_ = page.View()

	click := tea.MouseMsg{X: 2, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	page.Update(click)
	if page.SelectedID() != "item-002" {
		t.Fatalf("banner-adjusted click selected %q, want item-002", page.SelectedID())
	}
}

func TestPageKeyboardSlashReturnsFilterIntent(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 12)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(2))
	_ = page.View()

	intent, consumed := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !consumed || intent != IntentFilter {
		t.Fatalf("slash => intent=%q consumed=%v, want filter true", intent, consumed)
	}
	if page.table.LastAction() != resourceview.ActionFilter {
		t.Fatalf("table lastAction=%q, want filter", page.table.LastAction())
	}
}

func TestPageFooterCreateIntent(t *testing.T) {
	page := New(testSpec())
	page.SetSize(80, 12)
	page.SetState(StateSuccess)
	page.SetRows(FixtureRows(1))
	_ = page.View()

	hit := page.table.FooterActionHit(resourceview.ActionCreate)
	if hit.Kind != resourceview.HitFooterAction || hit.Action != resourceview.ActionCreate {
		t.Fatalf("footer create hit = %#v", hit)
	}

	intent, consumed := page.Update(tea.MouseMsg{
		X: hit.X, Y: hit.Y + page.overlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !consumed || intent != IntentCreate {
		t.Fatalf("footer create => intent=%q consumed=%v", intent, consumed)
	}
}
