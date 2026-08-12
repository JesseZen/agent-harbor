package resourceview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTablePreservesCompleteK9sSelectionFilterSortAndResponsiveContract(t *testing.T) {
	table := NewScoped("Targets", "all", []Column{
		{Title: "ID", MinWidth: 12, Priority: 0},
		{Title: "HEALTH", MinWidth: 9, Priority: 1},
		{Title: "ADAPTER", MinWidth: 10, Priority: 2},
	})
	table.SetRows([]Row{
		{ID: "target_b", Cells: []string{"target_b", "degraded", "anthropic"}},
		{ID: "target_a", Cells: []string{"target_a", "healthy", "openai"}},
	})
	table.SetSize(64, 12)

	table.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := table.SelectedID(); got != "target_a" {
		t.Fatalf("selected = %q, want target_a", got)
	}
	table.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := table.MarkedIDs(); len(got) != 1 || got[0] != "target_a" {
		t.Fatalf("marks = %#v", got)
	}

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if got := table.VisibleRowIDs(); len(got) != 2 || got[0] != "target_a" {
		t.Fatalf("sorted ids = %#v", got)
	}

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "degraded" {
		table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	table.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := table.VisibleRowIDs(); len(got) != 1 || got[0] != "target_b" {
		t.Fatalf("filtered ids = %#v", got)
	}

	wide := table.View()
	for _, expected := range []string{"Targets(all)", "ID", "HEALTH", "ADAPTER", "target_b", "degraded", "1/1"} {
		if !strings.Contains(wide, expected) {
			t.Fatalf("wide table omitted %q:\n%s", expected, wide)
		}
	}
	if strings.Contains(wide, "#bb9af7") {
		t.Fatalf("forbidden purple styling present:\n%s", wide)
	}

	table.SetSize(28, 10)
	narrow := table.View()
	if strings.Contains(narrow, "ADAPTER") || !strings.Contains(narrow, "HEALTH") {
		t.Fatalf("responsive columns are wrong:\n%s", narrow)
	}

	table.SetRows([]Row{
		{ID: "target_b", Cells: []string{"target_b", "degraded", "anthropic"}},
		{ID: "target_a", Cells: []string{"target_a", "healthy", "openai"}},
	})
	if table.SelectedID() != "target_b" || len(table.MarkedIDs()) != 1 || table.MarkedIDs()[0] != "target_a" {
		t.Fatalf("refresh lost K9s selection state: selected=%q marks=%#v", table.SelectedID(), table.MarkedIDs())
	}
}

func TestDisconnectedStateRendersError(t *testing.T) {
	table := NewScoped("Targets", "all", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetDisconnected(true, "Core unavailable")
	table.SetSize(40, 6)
	view := table.View()
	if !strings.Contains(view, "Core unavailable") {
		t.Fatalf("disconnected view missing error:\n%s", view)
	}
}
