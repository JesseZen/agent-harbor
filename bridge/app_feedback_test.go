package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedbackExpiryIdentity(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	n, _ := m.Update(appResult{})
	first := n.(harborModel)
	n, _ = first.Update(appResult{})
	second := n.(harborModel)
	n, _ = second.Update(appNoticeExpired{first.noticeID})
	if n.(harborModel).notice == "" {
		t.Fatal("old expiry removed new success")
	}
	n, _ = n.Update(appNoticeExpired{second.noticeID})
	if n.(harborModel).notice != "" {
		t.Fatal("current expiry did not clear")
	}
	second.notice = "persistent failure"
	second.noticeTransient = false
	n, _ = second.Update(appNoticeExpired{second.noticeID})
	if n.(harborModel).notice == "" {
		t.Fatal("persistent error expired")
	}
}

func TestManagementFailureOwnershipAndPunctuation(t *testing.T) {
	for _, detail := range []string{"", "  ", "Offline.", "Offline!  ", "..."} {
		for _, kind := range []string{"managed", "external"} {
			r := instanceReport{magpieInstance: magpieInstance{ID: managedInstanceID, Kind: kind}, Detail: detail}
			text := managementFailure(r)
			if strings.Contains(text, ": .") || strings.Contains(text, "..") {
				t.Fatal(text)
			}
			if kind == "managed" && !strings.Contains(text, "Start managed") {
				t.Fatal(text)
			}
			if kind == "external" && !strings.Contains(text, "separately") {
				t.Fatal(text)
			}
		}
	}
}

func TestFeedbackTextAndRecovery(t *testing.T) {
	for _, screen := range []string{"rename", "filter", "new-project", "new-name", "magpie-bind", "magpie-filter", "new-instance-filter", "new-catalog-filter"} {
		m := newHarborModel(&fakeApplicationService{})
		m.screen = screen
		m.notice = "failure"
		m.input.Focus()
		if strings.Contains(m.feedbackLine(), "! details") {
			t.Fatal("unavailable shortcut advertised")
		}
		dismissed, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if dismissed.(harborModel).notice != "" || dismissed.(harborModel).screen != screen {
			t.Fatal("text field dismissal")
		}
		n, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
		if n.(harborModel).input.Value() != "!" || n.(harborModel).screen != screen {
			t.Fatal(screen)
		}
	}
	f := &fakeApplicationService{}
	m := newHarborModel(f)
	m.screen = "magpie"
	m.notice = "failure"
	m.noticeCanStart = true
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	n, cmd := n.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if cmd != nil || n.(harborModel).screen != "magpie-confirm" {
		t.Fatal("start must require confirmation")
	}
	m.reports = []instanceReport{{Health: "offline", Detail: "object failure"}}
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if n.(harborModel).notice != "" || n.(harborModel).reports[0].Health != "offline" {
		t.Fatal("dismiss altered fault")
	}
}

func TestFeedbackVisual(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		m := newHarborModel(&fakeApplicationService{})
		m.width = size[0]
		m.height = size[1]
		m.notice = strings.Repeat("Gateway unavailable; review identity. ", 100) + "END_ERROR"
		for _, screen := range []string{"sessions", "error"} {
			m.screen = screen
			if screen == "error" {
				m.scroll = m.scrollLimit()
			}
			view := m.View()
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("height overflow")
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatal("width overflow")
				}
			}
			if !strings.Contains(cleanUI(view), "Back") && !strings.Contains(cleanUI(view), "Sessions") {
				t.Fatal("lost navigation", view)
			}
			if screen == "sessions" && !strings.Contains(cleanUI(view), "Refresh") {
				t.Fatal("feedback replaced actions")
			}
			if screen == "error" && !strings.Contains(view, "END_ERROR") {
				t.Fatal("cannot scroll to end")
			}
			if dir := os.Getenv("HARBOR_FEEDBACK_VISUAL"); dir != "" {
				os.MkdirAll(dir, 0700)
				os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.ansi", screen, size[0])), []byte(view), 0600)
			}
		}
	}
}

// Opt-in terminal fixture uses the production Bubble Tea program and UI with
// synthetic reports; it performs no gateway or session process operations.
func TestFeedbackTerminalProbe(t *testing.T) {
	if os.Getenv("HARBOR_FEEDBACK_TERMINAL") == "" {
		t.Skip("opt-in PTY fixture")
	}
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "magpie"
	m.reports = []instanceReport{{magpieInstance: magpieInstance{ID: "external-fixture", Kind: "external", Name: "Fixture gateway"}, Health: "offline", Detail: strings.Repeat("Synthetic gateway unavailable. ", 80) + "END_ERROR"}}
	m.notice = managementFailure(m.reports[0])
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		t.Fatal(err)
	}
}
