package resourceview

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMouseClickSelectsRow(t *testing.T) {
	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{
		{ID: "first", Cells: []string{"first"}},
		{ID: "second", Cells: []string{"second"}},
	})
	// height ≥ 8 keeps both rows visible under the k9s box chrome
	// (top/header/separator/footer/bottom).
	table.SetSize(40, 8)

	table.Update(tea.KeyMsg{Type: tea.KeyDown})
	if table.SelectedID() != "second" {
		t.Fatalf("keyboard selected = %q, want second", table.SelectedID())
	}

	hit := table.HitTest(2, 2)
	if hit.Kind != HitRow || hit.RowIndex != 0 {
		t.Fatalf("HitTest(2,2) = %#v, want row 0", hit)
	}

	table.Update(tea.MouseMsg{X: 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if table.SelectedID() != "first" {
		t.Fatalf("mouse selected = %q, want first", table.SelectedID())
	}
}

func TestMouseDoubleClickTriggersDetails(t *testing.T) {
	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{{ID: "only", Cells: []string{"only"}}})
	table.SetSize(40, 6)

	click := tea.MouseMsg{X: 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	table.Update(click)
	table.lastClickTime = time.Now().Add(-100 * time.Millisecond)
	table.Update(click)

	if table.LastAction() != ActionDetails {
		t.Fatalf("LastAction = %q, want details", table.LastAction())
	}
}

func TestMouseHeaderClickSortsColumn(t *testing.T) {
	table := New("Targets", []Column{
		{Title: "ID", MinWidth: 8, Priority: 0},
		{Title: "NAME", MinWidth: 8, Priority: 1},
	})
	table.SetRows([]Row{
		{ID: "b", Cells: []string{"b", "bravo"}},
		{ID: "a", Cells: []string{"a", "alpha"}},
	})
	table.SetSize(40, 6)

	hit := table.HitTest(12, 1)
	if hit.Kind != HitHeader {
		t.Fatalf("HitTest header = %#v", hit)
	}

	table.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if table.sortColumn != hit.ColumnIndex || !table.sortAscending {
		t.Fatalf("sort state = column %d asc %v", table.sortColumn, table.sortAscending)
	}
	if got := table.VisibleRowIDs(); len(got) != 2 || got[0] != "a" {
		t.Fatalf("sorted ids = %#v", got)
	}
}

func TestMouseWheelPagesSelection(t *testing.T) {
	rows := make([]Row, 0, 20)
	for index := 0; index < 20; index++ {
		id := string(rune('a' + index))
		rows = append(rows, Row{ID: id, Cells: []string{id}})
	}
	table := New("Targets", []Column{{Title: "ID", MinWidth: 4, Priority: 0}})
	table.SetRows(rows)
	table.SetSize(40, 8)

	table.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if table.cursor <= 0 {
		t.Fatalf("wheel down did not advance cursor: %d", table.cursor)
	}
	start := table.cursor
	table.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if table.cursor >= start {
		t.Fatalf("wheel up did not move cursor back: before=%d after=%d", start, table.cursor)
	}
}

func TestMouseAndKeyboardShareSelection(t *testing.T) {
	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{
		{ID: "a", Cells: []string{"a"}},
		{ID: "b", Cells: []string{"b"}},
		{ID: "c", Cells: []string{"c"}},
	})
	table.SetSize(40, 8)

	table.Update(tea.MouseMsg{X: 2, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if table.SelectedID() != "b" {
		t.Fatalf("mouse selected = %q, want b", table.SelectedID())
	}
	table.Update(tea.KeyMsg{Type: tea.KeyDown})
	if table.SelectedID() != "c" {
		t.Fatalf("keyboard selected = %q, want c", table.SelectedID())
	}
	table.Update(tea.MouseMsg{X: 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if table.SelectedID() != "a" {
		t.Fatalf("mouse reselected = %q, want a", table.SelectedID())
	}
}

func TestFooterFilterClickStartsFilter(t *testing.T) {
	table := New("Targets", []Column{{Title: "ID", MinWidth: 8, Priority: 0}})
	table.SetRows([]Row{{ID: "a", Cells: []string{"a"}}})
	table.SetSize(60, 6)

	hit := table.FooterFilterHit()
	if hit.Kind != HitFooterFilter {
		t.Fatalf("HitTest footer = %#v, want filter", hit)
	}

	table.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !table.Filtering() || table.LastAction() != ActionFilter {
		t.Fatalf("filtering=%v action=%q", table.Filtering(), table.LastAction())
	}
}
