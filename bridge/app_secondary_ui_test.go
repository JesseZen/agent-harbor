package main

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func secondaryKey(m harborModel, key tea.KeyMsg) harborModel {
	next, _ := m.Update(key)
	return next.(harborModel)
}
func TestSecondaryActionsStayReachableAndScroll(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}, {32, 10}} {
		m := newHarborModel(&fakeApplicationService{})
		m.width = size[0]
		m.height = size[1]
		m.screen = "error"
		m.notice = strings.Repeat("原因 🚢 /a/long/path/", 150) + "END"
		m.errorReturn = "sessions"
		p, _ := m.secondaryPage()
		found := false
		for _, hit := range p.Hits {
			if hit.ID == "secondary:esc" {
				found = true
				next, _ := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				if next.(harborModel).screen != "sessions" {
					t.Fatal("back failed")
				}
			}
		}
		if !found {
			t.Fatal("back clipped", size)
		}
		for i := 0; i < 500; i++ {
			m = secondaryKey(m, tea.KeyMsg{Type: tea.KeyDown})
		}
		if !strings.Contains(m.View(), "END") {
			t.Fatal("end inaccessible", size)
		}
		for _, line := range strings.Split(m.View(), "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("width overflow")
			}
		}
	}
}
func TestSecondaryRenameOwnsTextAndLateResult(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "rename"
	m.input.Focus()
	m.reviewed = session{ID: "one", Name: "Before"}
	for _, c := range "q?!nxs/中文🚢" {
		m = secondaryKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
	}
	if m.screen != "rename" || m.input.Value() != "q?!nxs/中文🚢" {
		t.Fatal("shortcut leaked", m.screen, m.input.Value())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(harborModel)
	if m.screen != "busy" || cmd == nil {
		t.Fatal("rename not busy")
	}
	_, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if duplicate != nil {
		t.Fatal("duplicate submitted")
	}
	response := secondaryRenameResult{m.generation, m.reviewed.ID, errors.New("fixture rename failure")}
	next, _ = m.Update(response)
	failed := next.(harborModel)
	if failed.screen != "rename" || failed.input.Value() != m.input.Value() || !strings.Contains(failed.View(), "fixture rename failure") {
		t.Fatal("failed edit lost")
	}
	m.screen = "sessions"
	next, _ = m.Update(response)
	if next.(harborModel).screen != "sessions" {
		t.Fatal("late result stole page")
	}
}
func TestSecondaryOnboardingMouseReview(t *testing.T) {
	m := onboardingModel{service: &fakeOnboardingService{}, width: 60, height: 20, report: onboardingReport{Ready: true}}
	p := m.secondaryPage()
	for _, hit := range p.Hits {
		if hit.ID == "onboard:down" {
			next, _ := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			m = next.(onboardingModel)
		}
	}
	if m.cursor != 1 {
		t.Fatal("choice mouse failed")
	}
	p = m.secondaryPage()
	for _, hit := range p.Hits {
		if hit.ID == "onboard:enter" {
			m.report.Magpie = "magpie fixture"
			next, _ := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			m = next.(onboardingModel)
		}
	}
	if !m.review || m.busy {
		t.Fatal("review must not start")
	}
}
func TestSecondaryVisualEvidence(t *testing.T) {
	dir := os.Getenv("HARBOR_SECONDARY_ANSI_DIR")
	if dir == "" {
		t.Skip("explicit artifact directory only")
	}
	os.MkdirAll(dir, 0700)
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		for _, screen := range []string{"detail", "help", "rename", "error", "busy", "delete-history"} {
			m := newHarborModel(&fakeApplicationService{})
			m.width = size[0]
			m.height = size[1]
			m.screen = screen
			m.notice = strings.Repeat("原因 中文 🚢 /project/long/path/ ", 35)
			m.reviewed = session{ID: "fixture", Name: "Docs 中文 🚢"}
			m.input.Focus()
			m.input.SetValue("Rename 中文 🚢")
			m.sessions = []sessionView{{session: m.reviewed}}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("secondary-%s-%d.ansi", screen, size[0])), []byte(m.View()), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
