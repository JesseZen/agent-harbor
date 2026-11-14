package main

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestOnboardingLocalChoiceRequiresReview(t *testing.T) {
	f := &fakeOnboardingService{}
	var model tea.Model = onboardingModel{service: f, report: onboardingReport{Ready: true, Herdr: "herdr 0.9.5", Codex: "codex-cli 0.167.0"}}
	model = appKey(model, "enter")
	if len(f.calls) != 0 || !strings.Contains(model.View(), "Review: local") {
		t.Fatal(f.calls, model.View())
	}
	model = appKey(model, "esc")
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
	model = appKey(model, "enter")
	model = appKey(model, "enter")
	if len(f.calls) != 1 || f.calls[0].Kind != "local" {
		t.Fatal(f.calls)
	}
}
func TestOnboardingRefusesMissingDependencyOrUnknownIdentity(t *testing.T) {
	for _, ready := range []bool{false, true} {
		f := &fakeOnboardingService{}
		var model tea.Model = onboardingModel{service: f, report: onboardingReport{Ready: ready, Instances: []gatewayProbe{{Address: "http://127.0.0.1:3425", State: gatewayUnknown}}}}
		if ready {
			model = appKey(model, "down")
			model = appKey(model, "down")
		}
		model = appKey(model, "enter")
		model = appKey(model, "enter")
		if len(f.calls) != 0 {
			t.Fatal(f.calls)
		}
		if !strings.Contains(model.View(), "Install") && !strings.Contains(model.View(), "identity is unknown") {
			t.Fatal(model.View())
		}
	}
}
func TestOnboardingFailureRemainsReviewable(t *testing.T) {
	f := &fakeOnboardingService{err: errors.New("fixture failure")}
	var model tea.Model = onboardingModel{service: f, report: onboardingReport{Ready: true}}
	model = appKey(model, "enter")
	model = appKey(model, "enter")
	if !strings.Contains(model.View(), "fixture failure") {
		t.Fatal(model.View())
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
		var model tea.Model = onboardingModel{service: &fakeOnboardingService{}, report: onboardingReport{Ready: true, Herdr: "herdr 0.9.5", Codex: "codex-cli 0.167.0", Instances: []gatewayProbe{{Address: "http://127.0.0.1:41001", State: gatewayVerified, ConfigDir: "/fixture/" + strings.Repeat("long/", 100)}}}}
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

func TestOnboardingDiscoversEnvironmentSettingsAndStandardAddresses(t *testing.T) {
	// Claim the standard port before allowing discovery to probe it. If another
	// process owns it, this test skips rather than contacting that process.
	standard, err := net.Listen("tcp", "127.0.0.1:3425")
	if err != nil {
		t.Skip("standard port is not owned by this test")
	}
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	server.Listener = standard
	server.Start()
	defer server.Close()
	configured := httptest.NewServer(http.NotFoundHandler())
	defer configured.Close()
	environment := httptest.NewServer(http.NotFoundHandler())
	defer environment.Close()
	b := newTestBridge(t)
	for _, pair := range b.env {
		k, v, _ := strings.Cut(pair, "=")
		t.Setenv(k, v)
	}
	t.Setenv("BRIDGE_STUB_VERSION", "1")
	t.Setenv("MAGPIE_ADDR", strings.TrimPrefix(environment.URL, "http://"))
	t.Setenv("AGENT_HARBOR_HERDR_BIN", filepath.Join(b.root, "bin", "herdr"))
	if err := os.WriteFile(filepath.Join(b.root, "bin", "herdr"), []byte("#!/bin/sh\nprintf 'herdr 0.9.5\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	native, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), native, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(b.root, ".config", "magpie")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(configured.URL, "http://"))
	settings := []byte("{\"port\":" + port + "}")
	if err := os.WriteFile(filepath.Join(config, "settings.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := (harborService{}).Onboarding()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, p := range report.Instances {
		found[p.Address] = true
	}
	for _, address := range []string{environment.URL, configured.URL, server.URL} {
		if !found[address] {
			t.Fatalf("missing proposal %s: %+v", address, report)
		}
	}
	after, err := os.ReadFile(filepath.Join(config, "settings.json"))
	if err != nil || string(after) != string(settings) {
		t.Fatal("discovery changed user settings", err)
	}
}

func TestOnboardingRefreshAndHelpDoNotLoseSelectionReview(t *testing.T) {
	f := &fakeOnboardingService{report: onboardingReport{Ready: true, Herdr: "herdr 0.9.5", Codex: "codex-cli 0.167.0"}}
	var model tea.Model = onboardingModel{service: f, report: onboardingReport{Ready: false}}
	model = appKey(model, "r")
	if !strings.Contains(model.View(), "codex-cli 0.167.0") {
		t.Fatal(model.View())
	}
	model = appKey(model, "?")
	if !strings.Contains(model.View(), "Keyboard help") || !strings.Contains(model.View(), "Refresh") {
		t.Fatal(model.View())
	}
	model = appKey(model, "esc")
	model = appKey(model, "enter")
	if !strings.Contains(model.View(), "Review: local") {
		t.Fatal(model.View())
	}
	model = appKey(model, "?")
	model = appKey(model, "esc")
	if !strings.Contains(model.View(), "Review: local") || len(f.calls) > 0 {
		t.Fatal(f.calls, model.View())
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

func TestOnboardingKeepsSelectedInstanceVisibleAtPracticalSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		report := onboardingReport{Ready: true}
		for i := 0; i < 30; i++ {
			report.Instances = append(report.Instances, gatewayProbe{Address: fmt.Sprintf("http://127.0.0.1:%d", 45000+i), State: gatewayVerified, ConfigDir: "/fixture/config"})
		}
		var model tea.Model = onboardingModel{report: report, service: &fakeOnboardingService{}, width: size[0], height: size[1]}
		for i := 0; i < 31; i++ {
			model = appKey(model, "down")
		}
		if !strings.Contains(model.View(), "45029") {
			t.Fatal(model.View())
		}
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
}
