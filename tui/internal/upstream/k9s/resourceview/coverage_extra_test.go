package resourceview

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTableKeyboardCoverageExtras(t *testing.T) {
	table := New("Routes", []Column{
		{Title: "ID", MinWidth: 8, Priority: 0},
		{Title: "NAME", MinWidth: 8, Priority: 1},
	})
	table.SetTitle("Routes")
	table.SetScope("ns")
	table.SetDirty(true)
	table.SetDisconnected(true, "offline")
	if !table.Disconnected() {
		t.Fatal("expected disconnected")
	}
	table.SetRows([]Row{
		{ID: "a", Cells: []string{"a", "alpha"}},
		{ID: "b", Cells: []string{"b", "beta"}},
		{ID: "c", Cells: []string{"c", "gamma"}},
	})
	table.SetSize(50, 12)
	if table.View() == "" {
		t.Fatal("empty disconnected view")
	}
	table.SetDisconnected(false, "")

	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	table.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	table.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	table.Update(tea.KeyMsg{Type: tea.KeyHome})
	table.Update(tea.KeyMsg{Type: tea.KeyEnd})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	table.Update(tea.KeyMsg{Type: tea.KeySpace})
	if len(table.MarkedIDs()) == 0 {
		t.Fatal("expected mark")
	}
	table.Update(tea.KeyMsg{Type: tea.KeySpace})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	table.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	table.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if table.Filtering() {
		t.Fatal("filter should clear on esc")
	}
}
