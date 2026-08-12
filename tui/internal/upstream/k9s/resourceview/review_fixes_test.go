package resourceview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHeaderUsesMutedBandWithAccentLabels(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{{ID: "a", Cells: []string{"a"}}})
	table.SetSize(40, 6)
	view := table.View()

	if !strings.Contains(view, trueColorSignature(TokenHeaderBg, true)) {
		t.Fatalf("header missing muted TokenHeaderBg background:\n%s", view)
	}
	if !strings.Contains(view, trueColorSignature(TokenHeader, false)) {
		t.Fatalf("header missing TokenHeader accent foreground:\n%s", view)
	}
	if !strings.Contains(view, trueColorSignature(TokenSelected, true)) {
		t.Fatalf("selected row missing TokenSelected background:\n%s", view)
	}
}

func TestKeyboardSlashSetsActionFilter(t *testing.T) {
	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{{ID: "a", Cells: []string{"a"}}})
	table.SetSize(40, 6)

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if table.LastAction() != ActionFilter {
		t.Fatalf("LastAction = %q, want filter", table.LastAction())
	}
	if !table.Filtering() {
		t.Fatal("expected filtering mode")
	}
}

func TestFooterActionHits(t *testing.T) {
	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{{ID: "a", Cells: []string{"a"}}})
	table.SetSize(80, 6)
	_ = table.View()

	for _, tc := range []struct {
		label  string
		action Action
	}{
		{"new", ActionCreate},
		{"edit", ActionEdit},
		{"del", ActionDelete},
		{"pub", ActionPublish},
	} {
		hit := table.footerActionHit(tc.action)
		if hit.Kind != HitFooterAction || hit.Action != tc.action {
			t.Fatalf("%s footer hit = %#v", tc.label, hit)
		}
		table.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if table.LastAction() != tc.action {
			t.Fatalf("%s click LastAction = %q", tc.label, table.LastAction())
		}
	}
}
