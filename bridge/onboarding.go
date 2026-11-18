package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// Startup checks dependencies only. Connections are chosen per Session;
// probing never binds or starts a Magpie instance.
type onboardingReport struct {
	Herdr, Codex, Magpie string
	Ready                bool
}

var onboardingAppService onboardingService = harborService{}

type onboardingService interface {
	Onboarding() (onboardingReport, error)
}

func (harborService) Onboarding() (onboardingReport, error) {
	r := onboardingReport{Herdr: "missing; install bundled Herdr 0.9.5", Codex: "missing or unsupported executable; install native Codex", Magpie: "not bundled; local sessions remain available"}
	root, err := bridgeHome()
	if err != nil {
		return r, err
	}
	if err := ensureHome(root); err != nil {
		return r, err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := herdrCommand(env, "--version")
	probe := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	probe.Env = command.Env
	if out, e := probe.Output(); e == nil {
		r.Herdr = strings.TrimSpace(string(out))
	}
	if v := codexVersion(os.Environ()); v != "unknown" {
		r.Codex = v
	}
	r.Ready = r.Herdr == "herdr 0.9.5" && strings.HasPrefix(r.Codex, "codex-cli ")
	if path, e := bundledMagpiePath(); e == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, path, "--version")
		cmd.Env = os.Environ()
		if output, err := cmd.Output(); err == nil && strings.HasPrefix(strings.TrimSpace(string(output)), "magpie ") {
			r.Magpie = strings.TrimSpace(string(output))
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.Magpie = "version check timed out; press r to retry"
		} else if err != nil {
			r.Magpie = "version check failed; press r to retry or check the executable"
		} else {
			r.Magpie = "unrecognized executable; install the bundled Magpie candidate"
		}
		cancel()
	}
	return r, nil
}

type onboardingRefreshed struct {
	report onboardingReport
	err    error
}
type onboardingModel struct {
	scroll     int
	help       bool
	focus      string
	helpScroll int

	service       onboardingService
	report        onboardingReport
	busy, done    bool
	notice        string
	width, height int
}

func (m onboardingModel) Init() tea.Cmd { return nil }
func (m onboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlZ {
		return m, tea.Suspend
	}

	if next, cmd, ok := m.secondaryInput(msg); ok {
		return next, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case onboardingRefreshed:
		m.busy = false
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.report = msg.report
			if m.report.Ready {
				m.done = true
				return m, tea.Quit
			}
			m.notice = "Checks refreshed"
			m.scroll = 0
		}
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		key := msg.String()
		if m.help {
			if key == "esc" || key == "?" {
				m.help = false
				m.scroll = m.helpScroll
			}
			if key == "q" || key == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch key {
		case "?":
			m.help = true
			m.helpScroll = m.scroll
			m.scroll = 0
			return m, nil
		case "r":
			m.busy = true
			return m, func() tea.Msg { r, e := m.service.Onboarding(); return onboardingRefreshed{r, e} }
		case "ctrl+d":
			m.scroll += max(1, m.height-4)
			return m, nil
		case "ctrl+u":
			m.scroll = max(0, m.scroll-max(1, m.height-4))
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m, tea.Quit
		case "enter":
			if m.report.Ready {
				m.done = true
				return m, tea.Quit
			}
			m.notice = "Fix the dependency reported above, then refresh."
		}

	}
	return m, nil
}
func (m onboardingModel) View() string { return m.secondaryPage().view() }

func ensureOnboarding(s onboardingService) error {
	r, err := s.Onboarding()
	if err != nil {
		return err
	}
	if r.Ready {
		return nil
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("dependency checks require an interactive terminal\nHerdr: %s\nCodex: %s\nMagpie: %s", r.Herdr, r.Codex, r.Magpie)
	}
	model, err := tea.NewProgram(onboardingModel{service: s, report: r}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return fmt.Errorf("dependency checks need an interactive terminal: %w", err)
	}
	if !model.(onboardingModel).done {
		return errors.New("dependency checks cancelled; no workspace was opened")
	}
	return nil
}
