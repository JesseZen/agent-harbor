package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"strings"
	"testing"
)

// Opt-in screenshots use only synthetic data and never invoke the service.
func TestVisualPreview(t *testing.T) {
	out := os.Getenv("HARBOR_VISUAL_OUT")
	if out == "" {
		t.Skip("opt-in visual fixture")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		intro := onboardingModel{width: size[0], height: size[1], report: onboardingReport{Ready: true, Herdr: "herdr 0.9.5 · Ready", Codex: "codex-cli 0.167.0 · Ready", Magpie: "magpie 0.1.3528 · Ready"}}
		os.WriteFile(fmt.Sprintf("%s/onboarding-%d.ansi", out, size[0]), []byte(intro.View()), 0600)
		for _, screen := range []string{"sessions", "detail", "new-review", "magpie", "confirm", "error"} {
			m := newHarborModel(&fakeApplicationService{})
			m.width = size[0]
			m.height = size[1]
			m.screen = screen
			m.action = "stop"
			m.reviewed = session{Name: "Harbor API", WorkingDirectory: "/projects/harbor", CLI: "codex", Mode: "magpie", MagpieModel: "relay-a/fast", InstanceID: "managed"}
			m.sessions = []sessionView{{session: m.reviewed, Status: "Running"}, {session: session{Name: "Docs review", CLI: "codex", WorkingDirectory: "/projects/docs", Mode: "local"}, Status: "Exited"}}
			m.draft = sessionDraft{Name: "Harbor API", Directory: "/projects/harbor", Connection: "magpie", Instance: "managed", Model: "relay-a/fast"}
			m.environment.CodexVersion = "0.167.0"
			m.reports = []instanceReport{{magpieInstance: magpieInstance{ID: "managed", Name: "Harbor-managed", Address: "http://127.0.0.1:3435", Kind: "managed"}, Health: instanceVerified}}
			if screen == "error" {
				m.notice = "Gateway offline. Start the managed instance from Magpie (m → s), then retry. The conversation and selected connection have been kept."
			}
			os.WriteFile(fmt.Sprintf("%s/%s-%d.ansi", out, screen, size[0]), []byte(strings.ReplaceAll(m.View(), "\n", "\r\n")), 0600)
		}
	}
}
