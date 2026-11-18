package main

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

type walkthroughService struct {
	*fakeApplicationService
	refreshes atomic.Int64
}

func (f *walkthroughService) ProfileEnvironment(b profileBinding) (appEnvironment, error) {
	lastConnection := "local"
	if b.ProfileID == "coding" {
		lastConnection = "magpie"
	}
	return appEnvironment{
		Profile: &b, Directory: "/projects/harbor", CodexVersion: "codex-cli 0.169.0",
		DefaultInstance: managedInstanceID, LastConnection: lastConnection,
		Projects: []string{"/projects/docs", "/projects/港口"},
	}, nil
}

func (f *walkthroughService) ProfileMagpieCatalog(_ context.Context, req profileClientRequest, id string) ([]catalogModel, error) {
	return f.ProfileCatalog(req.Binding, id)
}

func (f *walkthroughService) ProfileCodexAccounts(ctx context.Context, req codexAccountRequest, _ io.Reader, _, _ io.Writer) (profileAccountResult, error) {
	result := walkthroughAccounts(req.Profile)
	switch req.Action {
	case "inspect":
		return result, nil
	case "usage":
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(4 * time.Second):
		}
		shift := float64(f.refreshes.Add(1) % 5)
		result.Accounts[0].Usage = walkthroughUsage(32+shift*5, 13+shift*5)
		result.Accounts[1].Usage = walkthroughUsage(77+shift*3, 52+shift*3)
		return result, nil
	default:
		return result, errors.New("Mock walkthrough · account changes are simulated")
	}
}

func walkthroughAccounts(b profileBinding) profileAccountResult {
	return profileAccountResult{Profile: b, ProfileName: "Coding", State: "selected", Selected: "primary",
		Accounts: []savedCodexAccount{
			{Key: "primary", Email: "work@example.test", Plan: "Pro"},
			{Key: "backup", Email: "backup@example.test", Plan: "Plus"},
		}}
}

func walkthroughUsage(short, weekly float64) *savedAccountUsage {
	usage := &savedAccountUsage{Source: "api",
		Primary:   &savedUsageWindow{UsedPercent: &short, Minutes: 300, ResetsAt: 1795017600},
		Secondary: &savedUsageWindow{UsedPercent: &weekly, Minutes: 10080, ResetsAt: 1795449600},
	}
	usage.Refresh.Status = "ok"
	return usage
}

// Run only in an isolated terminal environment. All account and gateway data
// are synthetic. The interactive controls are the production Harbor model.
func TestTUIWalkthrough(t *testing.T) {
	if os.Getenv("HARBOR_TUI_WALKTHROUGH") != "1" {
		t.Skip("opt-in interactive walkthrough")
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		t.Fatal("run the compiled test binary directly from a terminal")
	}
	p := profile{ID: "coding", Name: "Coding", WorkspaceID: "mock", Kind: profileIsolated,
		Clients: []profileClientHome{{ID: "coding-codex", Client: "codex", Home: "/profiles/coding/codex"}}}
	b, err := bindingOf(p, "codex")
	if err != nil {
		t.Fatal(err)
	}
	f := &walkthroughService{fakeApplicationService: &fakeApplicationService{
		ProfileRecords: []profile{p, {ID: "review", Name: "Review", WorkspaceID: "mock", Kind: profileIsolated,
			Clients: []profileClientHome{{ID: "review-codex", Client: "codex", Home: "/profiles/review/codex"}}}},
		SelectedProfile: &b,
		Reports: []instanceReport{
			{magpieInstance: magpieInstance{ID: managedInstanceID, Name: "Harbor-managed Magpie", Kind: "managed", Address: "http://127.0.0.1:3435"}, Health: instanceVerified},
			{magpieInstance: magpieInstance{ID: "review-gateway", Name: "Review gateway", Kind: "external", Address: "http://127.0.0.1:4444"}, Health: instanceVerified},
		},
		Models:       []catalogModel{{ID: "demo/fast", DisplayName: "Fast"}, {ID: "group/review", DisplayName: "Review"}},
		Acknowledged: map[string]bool{managedInstanceID: true, "review-gateway": true},
		Sessions: []sessionView{
			{session: session{ID: "s1", Name: "Harbor API", Profile: &b, WorkingDirectory: "/projects/harbor", CLI: "codex", Mode: "magpie", MagpieModel: "demo/fast", InstanceID: managedInstanceID}, Runtime: runtimeObservation{Presence: "not_started"}, Status: "Not started"},
			{session: session{ID: "s2", Name: "港口 Docs", Profile: &b, WorkingDirectory: "/projects/docs", CLI: "codex", Mode: "local"}, Runtime: runtimeObservation{Presence: "not_started"}, Status: "Not started"},
		},
	}}
	m := newHarborModel(f)
	defer m.cancelRefresh()
	m.activeProfile, m.accountProfile = &b, &b
	m.profiles = f.ProfileRecords
	cached := walkthroughAccounts(b)
	cached.Accounts[0].Usage = walkthroughUsage(32, 13)
	cached.Accounts[1].Usage = walkthroughUsage(77, 52)
	m.cacheAccountSnapshot(cached)
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run(); err != nil {
		t.Fatal(err)
	}
}
