package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestScanLocalSkillsDeduplicatesSymlinkAndReadsProfileToggle(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	sharedDir := filepath.Join(home, ".agents", "skills", "code-review")
	profileA := filepath.Join(root, "profile-a")
	profileB := filepath.Join(root, "profile-b")
	for _, d := range []string{sharedDir, filepath.Join(profileA, "skills"), filepath.Join(profileB, "skills"), filepath.Join(profileA, "skills", "local-only")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "SKILL.md"), []byte("---\nname: code-review\ndescription: Review branch changes\n---\nbody\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedDir, filepath.Join(profileA, "skills", "code-review")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileA, "skills", "local-only", "SKILL.md"), []byte("---\nname: local-only\ndescription: Profile A only\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Disable shared skill in Profile A only; keep unrelated config comments intact.
	if err := os.WriteFile(filepath.Join(profileA, "config.toml"), []byte("# keep comment\nmodel = \"gpt-5\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sharedFile := filepath.Join(sharedDir, "SKILL.md")
	if err := writeCodexSkillConfigFallback(profileA, sharedFile, false); err != nil {
		t.Fatal(err)
	}
	rawCfg, err := os.ReadFile(filepath.Join(profileA, "config.toml"))
	if err != nil || !strings.Contains(string(rawCfg), "# keep comment") || !strings.Contains(string(rawCfg), `model = "gpt-5"`) {
		t.Fatalf("fallback writer dropped unrelated config: %s %v", string(rawCfg), err)
	}

	itemsA, _ := scanLocalSkills(home, profileA)
	if len(itemsA) != 2 {
		t.Fatalf("expected 2 deduplicated skills in Profile A, got %+v", itemsA)
	}
	if itemsA[0].Name != "code-review" || itemsA[0].Enabled || itemsA[0].Scope != "global-shared" {
		t.Fatalf("shared skill should be disabled in Profile A: %+v", itemsA[0])
	}
	if itemsA[1].Name != "local-only" || !itemsA[1].Enabled || itemsA[1].Scope != "profile" {
		t.Fatalf("local skill unexpected: %+v", itemsA[1])
	}

	// Profile B still sees the shared skill as enabled!
	itemsB, _ := scanLocalSkills(home, profileB)
	if len(itemsB) != 1 || itemsB[0].Name != "code-review" || !itemsB[0].Enabled {
		t.Fatalf("disabling in Profile A leaked to Profile B: %+v", itemsB)
	}

	// Re-enabling updates existing [[skills.config]] block without duplicating it.
	if err := writeCodexSkillConfigFallback(profileA, sharedFile, true); err != nil {
		t.Fatal(err)
	}
	rawCfg, _ = os.ReadFile(filepath.Join(profileA, "config.toml"))
	if strings.Count(string(rawCfg), "[[skills.config]]") != 1 {
		t.Fatalf("duplicate [[skills.config]] entries written:\n%s", string(rawCfg))
	}
	itemsA, _ = scanLocalSkills(home, profileA)
	if !itemsA[0].Enabled {
		t.Fatalf("re-enabled skill still disabled: %+v", itemsA[0])
	}
}

func TestResolveSkillsCLIPriority(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	mkExec := func(path string) {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENT_HARBOR_SKILLS_BIN", "")
	if mode, _ := resolveSkillsCLI([]string{"PATH=" + binDir}); mode != "unavailable" {
		t.Fatalf("expected unavailable, got %q", mode)
	}
	npxPath := filepath.Join(binDir, "npx")
	mkExec(npxPath)
	if mode, got := resolveSkillsCLI([]string{"PATH=" + binDir}); mode != "npx" || got != npxPath {
		t.Fatalf("expected npx fallback, got %q %q", mode, got)
	}
	skillsPath := filepath.Join(binDir, "skills")
	mkExec(skillsPath)
	if mode, got := resolveSkillsCLI([]string{"PATH=" + binDir}); mode != "path" || got != skillsPath {
		t.Fatalf("expected PATH skills, got %q %q", mode, got)
	}
	override := filepath.Join(root, "custom-skills")
	mkExec(override)
	t.Setenv("AGENT_HARBOR_SKILLS_BIN", override)
	if mode, got := resolveSkillsCLI([]string{"PATH=" + binDir}); mode != "companion" || got != override {
		t.Fatalf("expected companion override, got %q %q", mode, got)
	}
}

func TestBuildPluginSyncPlanDiffsManifestOnly(t *testing.T) {
	bA := profileBinding{WorkspaceID: "ws", ProfileID: "a", Client: "codex", EnvironmentID: "ea"}
	bB := profileBinding{WorkspaceID: "ws", ProfileID: "b", Client: "codex", EnvironmentID: "eb"}
	src := extensionSnapshot{
		Profile:     bA,
		ProfileName: "Work",
		Marketplaces: []extensionMarketplace{
			{Name: "official", Source: "https://example.test/official"},
			{Name: "team", Source: "https://example.test/team"},
		},
		Plugins: []extensionItem{
			{Kind: extPlugin, ID: "github@official", Name: "github", Marketplace: "official"},
			{Kind: extPlugin, ID: "linear@team", Name: "linear", Marketplace: "team"},
		},
	}
	dst := extensionSnapshot{
		Profile:     bB,
		ProfileName: "Personal",
		Marketplaces: []extensionMarketplace{
			{Name: "official", Source: "https://example.test/official"},
		},
		Plugins: []extensionItem{
			{Kind: extPlugin, ID: "github@official", Name: "github", Marketplace: "official"},
		},
	}
	plan := buildPluginSyncPlan(src, dst)
	if len(plan.MarketplacesToAdd) != 1 || plan.MarketplacesToAdd[0].Name != "team" {
		t.Fatalf("unexpected marketplaces diff: %+v", plan.MarketplacesToAdd)
	}
	if len(plan.PluginsToAdd) != 1 || plan.PluginsToAdd[0].ID != "linear@team" {
		t.Fatalf("unexpected plugins diff: %+v", plan.PluginsToAdd)
	}
	if len(plan.AlreadyInstalled) != 1 || plan.AlreadyInstalled[0] != "github" {
		t.Fatalf("unexpected already installed list: %+v", plan.AlreadyInstalled)
	}
}

func TestExtensionsUIToggleSyncAndNavigation(t *testing.T) {
	f := &fakeApplicationService{}
	pA, _ := f.CreateProfile(profileCreateRequest{Name: "Default", Kind: profileIsolated})
	pB, _ := f.CreateProfile(profileCreateRequest{Name: "Work", Kind: profileIsolated})
	bA, _ := bindingOf(pA, "codex")
	bB, _ := bindingOf(pB, "codex")
	f.Extensions = map[profileBinding]extensionSnapshot{
		bA: {
			Profile:     bA,
			ProfileName: "Default",
			SkillsCLI:   "path",
			Skills: []extensionItem{
				{Kind: extSkill, ID: "/s/code-review", Name: "code-review", Description: "Review code", Source: "shared (~/.agents/skills)", Path: "/s/code-review", Enabled: true, Toggleable: true},
			},
			Plugins: []extensionItem{
				{Kind: extPlugin, ID: "github@official", Name: "github", Marketplace: "official", Enabled: true},
			},
		},
		bB: {
			Profile:     bB,
			ProfileName: "Work",
			SkillsCLI:   "path",
			Plugins: []extensionItem{
				{Kind: extPlugin, ID: "github@official", Name: "github", Marketplace: "official", Enabled: true},
				{Kind: extPlugin, ID: "linear@team", Name: "linear", Marketplace: "team", Enabled: true},
			},
		},
	}

	m := newHarborModel(f)
	m.width, m.height = 100, 30
	m.profiles = []profile{pA, pB}
	m.activeProfile = &bA

	// Navigate from Sessions -> Extensions via Right arrow.
	n, cmd := m.Update(uiKey("right"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions" || m.ext.snapshot == nil || len(m.ext.snapshot.Skills) != 1 {
		t.Fatalf("failed to enter extensions tab: screen=%q snap=%+v", m.screen, m.ext.snapshot)
	}
	view := cleanUI(m.View())
	if !strings.Contains(view, "code-review") || !strings.Contains(view, "github") {
		t.Fatalf("extensions view missing items:\n%s", view)
	}

	// Toggle skill with Space.
	n, cmd = m.Update(uiKey(" "))
	m = settleApp(n, cmd).(harborModel)
	if m.ext.snapshot.Skills[0].Enabled {
		t.Fatal("space did not disable skill")
	}

	// Sync plugins from Work -> Default.
	n, cmd = m.Update(uiKey("y"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions-sync-pick" || len(m.ext.syncSources) != 1 || m.ext.syncSources[0].Name != "Work" {
		t.Fatalf("sync picker unexpected: screen=%q sources=%+v", m.screen, m.ext.syncSources)
	}
	n, cmd = m.Update(uiKey("enter"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions-sync-confirm" || m.ext.syncPlan == nil || len(m.ext.syncPlan.PluginsToAdd) != 1 {
		t.Fatalf("sync confirm unexpected: screen=%q plan=%+v", m.screen, m.ext.syncPlan)
	}
	n, cmd = m.Update(uiKey("enter"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions-sync-result" || m.ext.syncResult == nil || len(m.ext.syncResult.Outcomes) != 2 {
		t.Fatalf("sync result unexpected: screen=%q res=%+v", m.screen, m.ext.syncResult)
	}

	// Test Settings -> Profiles -> Esc returns to Settings.
	m.screen = "sessions"
	settings := m.openSettingsModel()
	m.settings = &settings
	m.settings.cursor = settingProfiles
	n, cmd = m.Update(uiKey("enter"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "profile-list" || m.profileReturn != "settings" {
		t.Fatalf("Settings -> Profiles failed: screen=%q return=%q", m.screen, m.profileReturn)
	}
	n, _ = m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.settings == nil {
		t.Fatal("Esc from Profiles opened via Settings did not return to Settings")
	}
	_ = context.Background()
}

func TestSettingsTwoColumnOmpStyleUnsavedAndNarrowMode(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	m := newSettingsModel()
	m.width, m.height = 100, 30
	m.profileSummary = "2 profiles · Work"
	p := m.layout()
	wideView := p.view()
	plainWide := cleanUI(wideView)
	// Wide mode shows left column divider, both sections, and bottom description.
	if !strings.Contains(plainWide, "│") || !strings.Contains(plainWide, "Workspace shortcuts") || !strings.Contains(plainWide, "Environment & Profiles") {
		t.Fatalf("wide settings missing 2-column sections:\n%s", plainWide)
	}
	if !strings.Contains(plainWide, "Open the Agent Harbor popup") {
		t.Fatalf("missing bottom gray option description:\n%s", plainWide)
	}

	// Clicking left-column section "section:environment" jumps cursor to Profiles and focuses that section.
	clicked := false
	for _, hit := range p.Hits {
		if hit.ID == "section:environment" {
			next, _ := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m = next.(settingsModel)
			clicked = true
			break
		}
	}
	if !clicked || m.cursor != settingProfiles || m.activeSectionID() != "environment" {
		t.Fatalf("clicking left section did not jump to Environment & Profiles: clicked=%v cursor=%d sec=%q", clicked, m.cursor, m.activeSectionID())
	}
	if !strings.Contains(cleanUI(m.View()), "Manage isolated Profiles") {
		t.Fatalf("bottom description did not update for Profiles option:\n%s", cleanUI(m.View()))
	}

	// Narrow width hides the left sidebar column and vertical divider.
	m.width, m.height = 60, 24
	narrowView := m.View()
	for _, hit := range m.layout().Hits {
		if strings.HasPrefix(hit.ID, "section:") {
			t.Fatalf("narrow settings should hide left section column, found hit %+v", hit)
		}
	}
	for _, line := range strings.Split(narrowView, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("narrow settings overflowed width %d: %q", m.width, line)
		}
	}

	// Modified-but-unsaved item renders in yellow (#DAB77E -> 218;183;126).
	m.width, m.height = 100, 30
	m.cursor = shortcutHarbor
	m.values.Harbor = "ctrl+g"
	rawDirty := m.View()
	foundYellow := false
	for _, line := range strings.Split(rawDirty, "\n") {
		if strings.Contains(cleanUI(line), "Harbor") && strings.Contains(cleanUI(line), "ctrl+g") {
			if strings.Contains(line, "218;183;126") {
				foundYellow = true
			}
		}
	}
	if !foundYellow {
		t.Fatalf("modified unsaved option did not render in yellow:\n%s", cleanUI(rawDirty))
	}

	// Attempting to leave with Esc shows red (#E3A09B -> 227;160;155) Unsaved changes warning above gray description; second Esc leaves.
	n, _ := m.Update(uiKey("esc"))
	m = n.(settingsModel)
	if m.closed || !m.unsavedWarn {
		t.Fatalf("first Esc with unsaved changes should warn instead of closing: closed=%v warn=%v", m.closed, m.unsavedWarn)
	}
	rawWarn := m.View()
	lines := strings.Split(rawWarn, "\n")
	redIdx, descIdx := -1, -1
	for i, line := range lines {
		plain := cleanUI(line)
		if strings.Contains(plain, "Unsaved changes") && strings.Contains(line, "227;160;155") {
			redIdx = i
		}
		if strings.Contains(plain, "Open the Agent Harbor popup") {
			descIdx = i
		}
	}
	if redIdx < 0 || descIdx < 0 || redIdx >= descIdx {
		t.Fatalf("expected red Unsaved changes line (%d) above gray description (%d):\n%s", redIdx, descIdx, cleanUI(rawWarn))
	}
	n, _ = m.Update(uiKey("esc"))
	m = n.(settingsModel)
	if !m.closed {
		t.Fatal("second Esc should discard draft and close Settings")
	}
}
