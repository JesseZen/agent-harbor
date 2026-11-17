package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// Onboarding is completed only after an explicit choice, and stores only
// Harbor-owned preferences. Probing never binds or starts an instance.
type onboardingReport struct {
	Herdr, Codex, Magpie string
	Ready, Completed     bool
	Instances            []gatewayProbe
	Warnings             []string
}
type onboardingChoice struct {
	Kind  string
	Probe gatewayProbe
}

var onboardingAppService onboardingService = harborService{}

type onboardingService interface {
	Onboarding() (onboardingReport, error)
	FinishOnboarding(onboardingChoice) error
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
	_, err = os.Stat(filepath.Join(root, "onboarding.done"))
	r.Completed = err == nil
	if err != nil && !os.IsNotExist(err) {
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
	if !r.Ready || r.Completed {
		return r, nil
	}
	addresses, warnings := onboardingAddresses()
	r.Warnings = append(r.Warnings, warnings...)
	for _, address := range addresses {
		r.Instances = append(r.Instances, probeGateway(address))
	}
	return r, nil
}
func (h harborService) FinishOnboarding(c onboardingChoice) error {
	switch c.Kind {
	case "local":
	case "managed":
		if err := h.Magpie("start", managedInstanceID); err != nil {
			return err
		}
		if _, err := useMagpieInstance(managedInstanceID); err != nil {
			return err
		}
	case "external":
		p := probeGateway(c.Probe.Address)
		if c.Probe.State != gatewayVerified || p.State != gatewayVerified || p.ConfigDir != c.Probe.ConfigDir || p.Executable != c.Probe.Executable {
			return errors.New("Magpie identity is unknown or changed; refresh and review it again")
		}
		i, err := bindExternalMagpie(p)
		if err != nil {
			return err
		}
		if _, err = useMagpieInstance(i.ID); err != nil {
			return err
		}
	default:
		return errors.New("choose local, managed, or a verified running instance")
	}
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if err = ensureHome(root); err != nil {
		return err
	}
	return writeStateFile(filepath.Join(root, "onboarding.done"), []byte("v1\n"))
}

type onboardingResult struct{ err error }
type onboardingRefreshed struct {
	report onboardingReport
	err    error
}
type onboardingModel struct {
	scroll     int
	help       bool
	focus      string
	helpScroll int

	service            onboardingService
	report             onboardingReport
	cursor             int
	review, busy, done bool
	notice             string
	width, height      int
}

func (m onboardingModel) choices() []onboardingChoice {
	out := []onboardingChoice{{Kind: "local"}, {Kind: "managed"}}
	for _, p := range m.report.Instances {
		out = append(out, onboardingChoice{Kind: "external", Probe: p})
	}
	return out
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
			m.cursor = min(m.cursor, len(m.choices())-1)
			m.notice = "Checks refreshed"
			m.scroll = 0
		}
	case onboardingResult:
		m.busy = false
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.review = false
		} else {
			m.done = true
			return m, tea.Quit
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
			if m.review {
				return m, nil
			}
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
			if m.review {
				m.review = false
			} else {
				return m, tea.Quit
			}
		case "j", "down":
			if !m.review {
				m.scroll = 0
				m.cursor = min(m.cursor+1, len(m.choices())-1)
			}
		case "k", "up":
			if !m.review {
				m.scroll = 0
				m.cursor = max(0, m.cursor-1)
			}
		case "enter":
			if !m.report.Ready {
				m.notice = "Install the required Herdr and native Codex, then run Agent Harbor again."
				return m, nil
			}
			c := m.choices()[m.cursor]
			if c.Kind == "managed" && !strings.HasPrefix(m.report.Magpie, "magpie ") {
				m.notice = "Install the bundled Magpie candidate, then refresh; Local Codex remains available."
				return m, nil
			}
			if c.Kind == "external" && c.Probe.State != gatewayVerified {
				m.notice = "Configuration identity is unknown or gateway is offline. Choose local only or a verified instance."
				return m, nil
			}
			if !m.review {
				m.review = true
				return m, nil
			}
			m.busy = true
			if c.Kind != "managed" {
				return m, func() tea.Msg { return onboardingResult{m.service.FinishOnboarding(c)} }
			}
			// The managed start emits diagnostics; release the terminal for it.
			return m, tea.Exec(onboardingExec{m.service, c}, func(e error) tea.Msg { return onboardingResult{e} })
		}
	}
	return m, nil
}
func (onboardingExec) SetStdin(io.Reader)  {}
func (onboardingExec) SetStdout(io.Writer) {}
func (onboardingExec) SetStderr(io.Writer) {}

type onboardingExec struct {
	service onboardingService
	choice  onboardingChoice
}

func (e onboardingExec) Run() error    { return e.service.FinishOnboarding(e.choice) }
func (m onboardingModel) View() string { return m.secondaryPage().view() }

func ensureOnboarding(s onboardingService) error {
	r, err := s.Onboarding()
	if err != nil {
		return err
	}
	if r.Completed && r.Ready {
		return nil
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("first-run checks require an interactive terminal\nHerdr: %s\nCodex: %s\nMagpie: %s", r.Herdr, r.Codex, r.Magpie)
	}
	model, err := tea.NewProgram(onboardingModel{service: s, report: r}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return fmt.Errorf("onboarding needs an interactive terminal: %w", err)
	}
	if !model.(onboardingModel).done {
		return errors.New("onboarding cancelled; no workspace was opened")
	}
	return nil
}

// Every source is a proposal, independently of the currently selected default.
// Canonicalize before probing to avoid duplicate requests and reject secrets.
func onboardingAddresses() ([]string, []string) {
	proposals := []string{}
	warnings := []string{}
	env, err := userEnvironment()
	if err != nil {
		return nil, []string{err.Error()}
	}
	values := map[string]string{}
	for _, pair := range env {
		key, value, _ := strings.Cut(pair, "=")
		switch key {
		case "HOME", "XDG_CONFIG_HOME", "MAGPIE_ADDR":
			values[key] = value
		}
	}
	if values["MAGPIE_ADDR"] != "" {
		proposals = append(proposals, values["MAGPIE_ADDR"])
	}
	config := values["XDG_CONFIG_HOME"]
	if config == "" {
		config = filepath.Join(values["HOME"], ".config")
	}
	data, err := os.ReadFile(filepath.Join(config, "magpie", "settings.json"))
	if err == nil {
		var settings struct {
			Port int `json:"port"`
		}
		if json.Unmarshal(data, &settings) != nil || settings.Port < 0 || settings.Port > 65535 {
			warnings = append(warnings, "Magpie settings could not supply a valid port; check settings.json.")
		} else if settings.Port > 0 {
			proposals = append(proposals, fmt.Sprintf("http://127.0.0.1:%d", settings.Port))
		}
	} else if !os.IsNotExist(err) {
		warnings = append(warnings, "Magpie settings could not be read; check its file permissions.")
	}
	registry, err := loadMagpieRegistry()
	if err != nil {
		warnings = append(warnings, "Harbor's Magpie registry could not be read; check it before selecting an instance.")
	} else {
		for _, i := range registry.Instances {
			proposals = append(proposals, i.Address)
		}
	}
	proposals = append(proposals, "http://127.0.0.1:3425")
	addresses := []string{}
	seen := map[string]bool{}
	for _, proposal := range proposals {
		address, err := parseGatewayAddress(proposal)
		if err != nil {
			warnings = append(warnings, "A proposed Magpie address was rejected: "+err.Error())
			continue
		}
		if !seen[address] {
			addresses = append(addresses, address)
			seen[address] = true
		}
	}
	return addresses, warnings
}
