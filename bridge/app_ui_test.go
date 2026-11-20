package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSettingsSearchOwnsInputAndPreservesDraft(t *testing.T) {
	m := newSettingsModel()
	m.values.Harbor = "prefix+t"
	step := func(k tea.KeyMsg) { n, _ := m.Update(k); m = n.(settingsModel) }
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Magpie")})
	if m.closed || !m.searching || len(m.filtered()) != 1 || m.filtered()[0] != 2 {
		t.Fatal(m)
	}
	step(tea.KeyMsg{Type: tea.KeyEsc})
	if m.query != "" || !m.searching || m.values.Harbor != "prefix+t" {
		t.Fatal("search lost draft")
	}
	step(tea.KeyMsg{Type: tea.KeyEsc})
	if m.searching || m.closed {
		t.Fatal("Esc did not leave only search")
	}
}
func TestSettingsLayoutControlsRemainReachable(t *testing.T) {
	for _, sz := range [][2]int{{45, 12}, {48, 14}, {60, 20}, {100, 30}, {160, 45}} {
		m := newSettingsModel()
		m.width, m.height = sz[0], sz[1]
		p := m.layout()
		if len(strings.Split(p.view(), "\n")) != m.height {
			t.Fatal(sz)
		}
		for _, line := range strings.Split(p.view(), "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("overflow", sz, line)
			}
		}
		for _, id := range []string{"control:0", "control:1", "control:2", "control:3", "control:4", "save", "back", "search"} {
			found := false
			for _, r := range p.Hits {
				if r.ID == id {
					found = true
					if r.Y+r.H > m.height {
						t.Fatal("clipped", sz, id)
					}
				}
			}
			if !found {
				t.Fatal("missing", sz, id)
			}
		}
	}
}

func TestSettingsNavigationProtectsUnsavedValues(t *testing.T) {
	m := newSettingsModel()
	m.values.Harbor = "prefix+t"
	next, cmd := m.navigate("sessions")
	m = next.(settingsModel)
	if cmd != nil || m.closed || m.nav != "" || !strings.Contains(m.notice, "Save") {
		t.Fatal("discarded unsaved settings")
	}
	m.saved = m.values
	next, cmd = m.navigate("sessions")
	m = next.(settingsModel)
	if cmd == nil || !m.closed || m.nav != "sessions" {
		t.Fatal("cannot navigate after save")
	}
}
