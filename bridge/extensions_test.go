package main

import (
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

func TestParseSkillsCatalogResponse(t *testing.T) {
	result, err := parseSkillsCatalogResponse([]byte(`{"skills":[{"id":"vercel-labs/agent-skills/react","source":"vercel-labs/agent-skills","skillId":"react","name":"React","installs":42}]}`))
	if err != nil || len(result) != 1 || result[0].Skill != "react" || result[0].Installs != 42 {
		t.Fatalf("unexpected catalog result: %+v %v", result, err)
	}
}

func TestSkillInputClassifiesKeywordsAndSources(t *testing.T) {
	for _, keyword := range []string{"tdd", "react", "code-review"} {
		if skillInputIsSource(keyword) {
			t.Fatalf("keyword classified as source: %q", keyword)
		}
	}
	for _, source := range []string{"owner/repo", "https://example.test/skills.git", "/tmp/skills", "git@example.test:owner/repo.git"} {
		if !skillInputIsSource(source) {
			t.Fatalf("source classified as keyword: %q", source)
		}
	}
	if got, err := buildSkillsCLIArgs("path", "add", "tdd"); err == nil {
		t.Fatalf("keyword reached skills add: %v", got)
	}
	if got, err := buildSkillsCLIArgs("npx", "add", "owner/repo"); err != nil || strings.Join(got, " ") != "--yes skills add owner/repo -g -a codex" {
		t.Fatalf("repository should run skills add: %v %v", got, err)
	}
}

func TestExtensionsToggleAndPluginPlaceholderNavigation(t *testing.T) {
	service := &fakeApplicationService{}
	profile, _ := service.CreateProfile(profileCreateRequest{Name: "Default", Kind: profileIsolated})
	binding, _ := bindingOf(profile, "codex")
	service.Extensions = map[profileBinding]extensionSnapshot{binding: {
		SkillsCLI: "path",
		Skills:    []extensionItem{{ID: "/s/review", Name: "review", Path: "/s/review", Enabled: true, Toggleable: true}},
	}}
	m := newHarborModel(service)
	m.width, m.height = 60, 24
	m.activeProfile = &binding
	enter, load := m.Update(uiKey("right"))
	local, enrich := enter.Update(load())
	m = local.(harborModel)
	if enrich == nil || !strings.Contains(m.View(), "review") || !strings.Contains(m.View(), "Plugins") {
		t.Fatalf("local Skills and Plugins entry must render before Codex responds:\n%s", m.View())
	}
	m = settleApp(m, enrich).(harborModel)
	next, toggle := m.Update(uiKey(" "))
	m = settleApp(next, toggle).(harborModel)
	if m.ext.snapshot.Skills[0].Enabled {
		t.Fatal("space did not disable the Skill")
	}
	calls := len(service.Calls)
	for _, step := range []struct{ key, screen, text string }{
		{"b", "extensions-plugins", "Plugin support pending."},
		{"enter", "extensions-plugins", "Plugin support pending."},
		{"a", "extensions-plugins", "Plugin support pending."},
		{"m", "extensions-marketplaces", "Marketplace support pending."},
		{"r", "extensions-marketplaces", "Marketplace support pending."},
		{"x", "extensions-marketplaces", "Marketplace support pending."},
		{"esc", "extensions-plugins", "Plugin support pending."},
		{"esc", "extensions", "Installed Skills"},
	} {
		next, cmd := m.Update(uiKey(step.key))
		m = next.(harborModel)
		if cmd != nil || m.screen != step.screen || !strings.Contains(m.View(), step.text) {
			t.Fatalf("%q: screen=%q, called command=%v, view:\n%s", step.key, m.screen, cmd != nil, m.View())
		}
	}
	if len(service.Calls) != calls || len(service.ExecCommands) != 0 {
		t.Fatalf("placeholder invoked the service: %v %v", service.Calls[calls:], service.ExecCommands)
	}
}

func TestSettingsProfilesReturnsToSettings(t *testing.T) {
	service := &fakeApplicationService{}
	m := newHarborModel(service)
	settings := m.openSettingsModel()
	m.settings = &settings
	m.settings.cursor = settingProfiles
	next, cmd := m.Update(uiKey("enter"))
	m = settleApp(next, cmd).(harborModel)
	if m.screen != "profile-list" || m.profileReturn != "settings" {
		t.Fatalf("Settings Profiles opened %q with return page %q", m.screen, m.profileReturn)
	}
	next, _ = m.Update(uiKey("esc"))
	if next.(harborModel).settings == nil {
		t.Fatal("Esc from Profiles did not return to Settings")
	}
}

func TestExtensionsReentryUsesFreshProfileCache(t *testing.T) {
	f := &fakeApplicationService{}
	p, _ := f.CreateProfile(profileCreateRequest{Name: "Default", Kind: profileIsolated})
	b, _ := bindingOf(p, "codex")
	f.Extensions = map[profileBinding]extensionSnapshot{b: {ProfileName: "Default", SkillsCLI: "path"}}
	m := newHarborModel(f)
	m.activeProfile = &b
	m.ext.profile = &b
	m.screen = "sessions"
	n, cmd := m.Update(uiKey("right"))
	m = settleApp(n, cmd).(harborModel)
	first := countCall(f.Calls, "extensions-local-inspect")
	if first != 1 {
		t.Fatalf("expected one initial inspection, calls=%v", f.Calls)
	}
	m.screen = "sessions"
	if cmd := m.beginExtensionsInspect(); cmd != nil {
		t.Fatal("fresh Extensions cache started another inspection")
	}
	if got := countCall(f.Calls, "extensions-local-inspect"); got != first {
		t.Fatalf("fresh Extensions cache did not avoid inspection, calls=%v", f.Calls)
	}
}

func TestExtensionsDiscoverSearchStaysInHarbor(t *testing.T) {
	f := &fakeApplicationService{}
	p, _ := f.CreateProfile(profileCreateRequest{Name: "Default", Kind: profileIsolated})
	b, _ := bindingOf(p, "codex")
	f.Extensions = map[profileBinding]extensionSnapshot{b: {ProfileName: "Default", SkillsCLI: "path"}}
	f.SkillSearch = map[profileBinding][]skillCatalogItem{b: {
		{ID: "owner/repo/react", Name: "React", Source: "owner/repo", Skill: "react"},
	}}
	m := newHarborModel(f)
	m.width, m.height = 100, 30
	m.profiles = []profile{p}
	m.activeProfile = &b
	m.screen = "sessions"
	n, cmd := m.Update(uiKey("right"))
	m = settleApp(n, cmd).(harborModel)
	n, _ = m.Update(uiKey("d"))
	m = n.(harborModel)
	if m.screen != "extensions-discover" || !m.ext.discoverSearching {
		t.Fatalf("d did not open Discover search: screen=%q searching=%v", m.screen, m.ext.discoverSearching)
	}
	for _, letter := range "react" {
		n, _ = m.Update(uiKey(string(letter)))
		m = n.(harborModel)
	}
	n, cmd = m.Update(uiKey("enter"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions-discover" || len(m.ext.discoverResults) != 1 || m.ext.discoverResults[0].Name != "React" {
		t.Fatalf("search result did not stay in Discover: screen=%q results=%+v", m.screen, m.ext.discoverResults)
	}
	n, cmd = m.Update(uiKey("a"))
	m = settleApp(n, cmd).(harborModel)
	if !containsExecCommand(f.ExecCommands, "install-skill:owner/repo\x00react") {
		t.Fatalf("Discover install command missing: %+v", f.ExecCommands)
	}
	if m.screen != "extensions-discover" {
		t.Fatalf("install returned to wrong page: %q", m.screen)
	}
	n, cmd = m.Update(appExtensionExecDone{action: "install-skill"})
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions-discover" || !m.ext.discoverResults[0].Installed {
		t.Fatal("successful installation did not keep the result in Discover")
	}
	n, _ = m.Update(uiKey("esc"))
	n, _ = n.Update(uiKey("a"))
	for _, letter := range "react" {
		n, _ = n.Update(uiKey(string(letter)))
	}
	n, cmd = n.Update(uiKey("enter"))
	m = settleApp(n, cmd).(harborModel)
	if m.screen != "extensions-discover" || len(m.ext.discoverResults) != 1 || len(f.ExecCommands) != 1 {
		t.Fatalf("Add keyword failed to route to Discover: screen=%q results=%v exec=%v", m.screen, m.ext.discoverResults, f.ExecCommands)
	}
}

func TestExtensionsDiscoverSearchOwnsHJKLInput(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "extensions-discover"
	m.ext.discoverSearching = true
	m.input.Focus()
	for _, letter := range "hjklHJKL" {
		next, _ := m.Update(uiKey(string(letter)))
		m = next.(harborModel)
	}
	if m.input.Value() != "hjklHJKL" {
		t.Fatalf("Discover search lost typed characters: %q", m.input.Value())
	}
}

func containsExecCommand(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

func countCall(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
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
