package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestTicket05StableDirectoryHitsAndDraft(t *testing.T) {
	for _, size := range [][2]int{{156, 39}, {96, 24}, {56, 14}, {45, 12}} {
		m := searchFixture()
		m.width = size[0]
		m.height = size[1]
		m.formActive = true
		m.screen = "new-form"
		m.draft.Directory = t.TempDir()
		m.draft.Name = "Keep name"
		m.openDirectoryPicker()
		for i := 0; i < 30; i++ {
			m.picker.entries = append(m.picker.entries, directoryEntry{fmt.Sprintf("/synthetic/日本語🚢/%02d", i), "recent"})
		}
		m.picker.cursor = 20
		p := m.directoryPage()
		for _, h := range p.Hits {
			if h.ID == "search" {
				for _, xy := range [][2]int{{h.X, h.Y}, {h.X + h.W - 1, h.Y + h.H - 1}} {
					if p.target(xy[0], xy[1]) != "search" {
						t.Fatal("search boundary unfocusable")
					}
				}
			}
		}
		found := false
		for _, h := range p.Hits {
			if strings.HasPrefix(h.ID, "dir-path:") {
				found = true
				n, _, _ := m.directoryPageInput(tea.MouseMsg{X: h.X + 1, Y: h.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				selected := n.(harborModel)
				if selected.picker.matches()[selected.picker.cursor].path != strings.TrimPrefix(h.ID, "dir-path:") {
					t.Fatal("wrong path")
				}
			}
		}
		if !found {
			t.Fatal("no visible directory", size, p.Lines)
		}
		for _, id := range []string{"dir-use", "dir-cancel"} {
			has := false
			for _, h := range p.Hits {
				if h.ID == id {
					has = true
				}
			}
			if !has {
				t.Fatal("inaccessible", id, size)
			}
		}
		n, _, _ := m.directoryPageInput(uiKey("esc"))
		back := n.(harborModel)
		if back.screen != "new-form" || back.draft.Name != "Keep name" || back.formFocus != 0 {
			t.Fatal("lost draft/focus")
		}
	}
}
func TestTicket05QueryEditInvalidatesOldScan(t *testing.T) {
	m := searchFixture()
	m.formActive = true
	m.draft.Directory = t.TempDir()
	m.openDirectoryPicker()
	cmd := m.browseDirectory(m.draft.Directory, true)
	old := m.picker.generation
	n, _ := m.updateDirectory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("plain")})
	m = n.(harborModel)
	if m.picker.generation <= old || m.picker.searching || m.picker.scope != "known sources" {
		t.Fatal("query did not invalidate browse")
	}
	n, _ = m.Update(cmd())
	m = n.(harborModel)
	if m.picker.scope != "known sources" || len(m.picker.entries) != len(m.picker.known) {
		t.Fatal("late scan replaced known entries")
	}
}
func TestTicket05UseExactPathAndPreserveOtherDraftFields(t *testing.T) {
	m := searchFixture()
	m.formActive = true
	m.screen = "new-form"
	m.draft.Directory = t.TempDir()
	m.draft.Name = "Unchanged"
	m.draft.Connection = "local"
	m.openDirectoryPicker()
	path := t.TempDir()
	m.picker.entries = []directoryEntry{{path, "recent"}}
	m.picker.cursor = 0
	n, cmd, _ := m.directoryPageInput(uiKey("enter"))
	m = n.(harborModel)
	if cmd != nil || m.screen != "new-form" || m.draft.Directory != path || m.draft.Name != "Unchanged" || m.draft.Connection != "local" || m.creating {
		t.Fatal("use changed draft or launched", m.draft)
	}
}
func TestTicket05FullPathReader(t *testing.T) {
	m := searchFixture()
	m.width = 56
	m.height = 14
	m.formActive = true
	m.draft.Directory = t.TempDir()
	m.openDirectoryPicker()
	m.picker.entries = []directoryEntry{{"/" + strings.Repeat("日本語🚢/", 60) + "end-marker", "recent"}}
	m.picker.cursor = 0
	n, _, _ := m.directoryPageInput(uiKey("ctrl+p"))
	m = n.(harborModel)
	for i := 0; i < 100; i++ {
		n, _, _ = m.directoryPageInput(uiKey("down"))
		m = n.(harborModel)
	}
	if !strings.Contains(strings.Join(m.directoryPage().Lines, "\n"), "end-marker") {
		t.Fatal("full path tail inaccessible")
	}
	n, _, _ = m.directoryPageInput(uiKey("esc"))
	if n.(harborModel).screen != "new-directory" || n.(harborModel).picker.details {
		t.Fatal("reader escaped parent")
	}
}
