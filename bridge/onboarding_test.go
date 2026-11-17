package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeOnboardingService struct {
	report onboardingReport
	calls  []onboardingChoice
	err    error
}

func (f *fakeOnboardingService) Onboarding() (onboardingReport, error) { return f.report, f.err }
func (f *fakeOnboardingService) FinishOnboarding(c onboardingChoice) error {
	f.calls = append(f.calls, c)
	return f.err
}

func TestDependencyChecksShowOnlyTheActualFailure(t *testing.T) {
	m := onboardingModel{service: &fakeOnboardingService{}, width: 80, height: 24, report: onboardingReport{Herdr: "herdr 0.9.5", Codex: "missing or unsupported executable; install native Codex", Magpie: "magpie fixture"}}
	next := appKey(m, "enter")
	view := next.View()
	for _, unwanted := range []string{"Choose how", "Local Codex only", "Create Harbor-managed", "Install the required Herdr"} {
		if strings.Contains(view, unwanted) {
			t.Fatal(view)
		}
	}
	if !strings.Contains(view, "Codex: missing") || !strings.Contains(view, "Herdr: herdr 0.9.5") {
		t.Fatal(view)
	}
}

func TestFirstRunWithoutTerminalDoesNotStartWorkspace(t *testing.T) {
	b := newTestBridge(t)
	b.env = append(b.env, "BRIDGE_TEST_ONBOARDING=1")
	out, err := b.command("open")
	if err == nil || !strings.Contains(out, "interactive terminal") || !strings.Contains(out, "Herdr:") {
		t.Fatal(out, err)
	}
}

func TestOnboardingFitsPracticalSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		var model tea.Model = onboardingModel{service: &fakeOnboardingService{}, report: onboardingReport{Ready: true, Herdr: "herdr 0.9.5", Codex: "codex-cli 0.167.0"}}
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := model.View()
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("height overflow", size)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatal("width overflow", size)
			}
		}
	}
}

func TestOnboardingLocalPersistsOnlyHarborCompletion(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	t.Setenv("PATH", t.TempDir())
	h := harborService{}
	before, err := h.Onboarding()
	if err != nil {
		t.Fatal(err)
	}
	if before.Completed || before.Ready {
		t.Fatal(before)
	}
	if err := h.FinishOnboarding(onboardingChoice{Kind: "local"}); err != nil {
		t.Fatal(err)
	}
	after, err := h.Onboarding()
	if err != nil {
		t.Fatal(err)
	}
	if !after.Completed || after.Ready {
		t.Fatal("completion must not hide missing dependencies", after)
	}
	if _, err := os.Stat(filepath.Join(root, "magpie.json")); !os.IsNotExist(err) {
		t.Fatal("local-only changed the registry", err)
	}
}

func TestDependencyRefreshOpensWorkspaceWhenReady(t *testing.T) {
	f := &fakeOnboardingService{report: onboardingReport{Ready: true}}
	var model tea.Model = onboardingModel{service: f, report: onboardingReport{Herdr: "herdr 0.9.5", Codex: "missing"}}
	model = appKey(model, "?")
	model = appKey(model, "esc")
	model = appKey(model, "r")
	if !model.(onboardingModel).done || len(f.calls) != 0 {
		t.Fatal("refresh did not continue without connection setup")
	}
}

func TestOnboardingAllowsColdNativeVersionProbe(t *testing.T) {
	b := newTestBridge(t)
	for _, pair := range b.env {
		key, value, _ := strings.Cut(pair, "=")
		t.Setenv(key, value)
	}
	// Missing Herdr deliberately prevents gateway discovery in this fixture.
	t.Setenv("AGENT_HARBOR_HERDR_BIN", filepath.Join(b.root, "missing-herdr"))
	t.Setenv("BRIDGE_STUB_VERSION", "1")
	t.Setenv("BRIDGE_STUB_VERSION_DELAY", "1")
	native, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), native, 0700); err != nil {
		t.Fatal(err)
	}
	r, err := (harborService{}).Onboarding()
	if err != nil {
		t.Fatal(err)
	}
	if r.Codex != "codex-cli 0.167.0" {
		t.Fatal("cold native probe treated as missing", r.Codex)
	}
}

func TestOnboardingVersionTimeoutOffersRetryWithoutClaimingMissingMagpie(t *testing.T) {
	b := newTestBridge(t)
	path := filepath.Join(b.root, "bin", "magpie")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 4\nprintf 'magpie 0.1.3528\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "BRIDGE_TEST_ONBOARDING=1", "AGENT_HARBOR_MAGPIE_BIN="+path)
	out, _ := b.command("open")
	if !strings.Contains(out, "version check timed out") || !strings.Contains(out, "retry") {
		t.Fatal(out)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'magpie 0.1.3528\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	out, _ = b.command("open")
	if !strings.Contains(out, "Magpie: magpie 0.1.3528") {
		t.Fatal(out)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'magpie 0.1.3528-harbor-dev\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	out, _ = b.command("open")
	if !strings.Contains(out, "Magpie: magpie 0.1.3528-harbor-dev") {
		t.Fatal(out)
	}
}
