package resourceview

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFilterModeDeleteAndEmptyBackspaceDismiss(t *testing.T) {
	table := New("Routes", []Column{
		{Title: "ID", MinWidth: 8, Priority: 0},
		{Title: "NAME", MinWidth: 8, Priority: 1},
	})
	table.SetRows([]Row{
		{ID: "a", Cells: []string{"a", "alpha"}},
		{ID: "b", Cells: []string{"b", "beta"}},
	})
	table.SetSize(50, 12)

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !table.Filtering() {
		t.Fatal("expected filter mode after /")
	}
	table.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if table.Filtering() {
		t.Fatal("Delete should dismiss filter mode")
	}
	if got := table.Filter(); got != "" {
		t.Fatalf("filter after Delete = %q, want empty", got)
	}

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !table.Filtering() {
		t.Fatal("expected filter mode")
	}
	table.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if table.Filtering() {
		t.Fatal("Backspace on empty filter should dismiss filter mode")
	}

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'b'}})
	table.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if !table.Filtering() {
		t.Fatal("Backspace with text should keep filter mode")
	}
	if got := table.Filter(); got != "a" {
		t.Fatalf("filter after Backspace = %q, want %q", got, "a")
	}
}
