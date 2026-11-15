package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func searchFixture() harborModel {
	m := newHarborModel(&fakeApplicationService{})
	for i, n := range []string{"Harbor 日本語 🚢", "Harbor API", "Other project"} {
		m.sessions = append(m.sessions, sessionView{session: session{ID: fmt.Sprint(i), Name: n, WorkingDirectory: "/projects/日本語/harbor-api", CLI: "codex", Mode: "local"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Exited"})
	}
	m.reports = []instanceReport{{magpieInstance: magpieInstance{ID: "alpha", Name: "Alpha gateway", Kind: "external"}, Health: "offline"}, {magpieInstance: magpieInstance{ID: "beta", Name: "Beta gateway", Kind: "external"}, Health: "verified"}}
	m.newInstances = m.reports
	m.models = []catalogModel{{ID: "relay-a/model", DisplayName: "日本語 model"}, {ID: "relay-b/model", DisplayName: "Beta model"}}
	return m
}
func keySearch(m harborModel, k string) harborModel {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg.Type = tea.KeyEnter
	case "tab":
		msg.Type = tea.KeyTab
	case "esc":
		msg.Type = tea.KeyEsc
	case "left":
		msg.Type = tea.KeyLeft
	case "down":
		msg.Type = tea.KeyDown
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	if f, ok := m.service.(*fakeApplicationService); ok && f.Sessions == nil {
		f.Sessions = append([]sessionView{}, m.sessions...)
	}
	n, cmd := m.Update(msg)
	for cmd != nil {
		n, cmd = n.Update(cmd())
	}
	return n.(harborModel)
}
func TestInlineSearchOwnershipAndActions(t *testing.T) {
	for _, screen := range []string{"sessions", "magpie", "new-instance", "new-catalog"} {
		m := searchFixture()
		m.screen = screen
		m = keySearch(m, "/")
		for _, k := range []string{"q", "j", "k", "n", "?", "!"} {
			m = keySearch(m, k)
		}
		if m.searchQuery() != "qjkn?!" || m.screen != screen {
			t.Fatal("typing escaped editor", m.screen, m.searchQuery())
		}
		m = keySearch(m, "enter")
		if m.screen != screen {
			t.Fatal("zero result acted", m.screen)
		}
		m = keySearch(m, "esc")
		if m.searchQuery() != "" || m.searchFocus != "" {
			t.Fatal("clear failed")
		}
		m = keySearch(m, "/")
		m = keySearch(m, "日本")
		if screen == "sessions" && len(m.visible()) != 3 {
			t.Fatal("unicode path matching")
		}
		m = keySearch(m, "tab")
		if m.searchFocus != "" {
			t.Fatal("tab focus")
		}
	}
}
func TestInlineSearchStableRefreshAndReturn(t *testing.T) {
	m := searchFixture()
	m.cursor = 1
	m = keySearch(m, "/")
	m = keySearch(m, "Harbor")
	id := m.searchSelectedID()
	m = keySearch(m, "tab")
	m = keySearch(m, "i")
	m = keySearch(m, "esc")
	if m.searchSelectedID() != id || m.filter != "Harbor" {
		t.Fatal("detail lost context")
	}
	m = keySearch(m, "enter")
	if m.screen != "confirm" || m.reviewed.ID != id {
		t.Fatal("confirmation wrong target")
	}
	m = keySearch(m, "esc")
	n, _ := m.Update(appLoaded{sessions: []sessionView{m.sessions[1], m.sessions[0]}})
	m = n.(harborModel)
	if m.searchSelectedID() != id {
		t.Fatal("refresh changed target")
	}
	m.listGeneration = 3
	n, _ = m.Update(appLoaded{generation: 2})
	if len(n.(harborModel).sessions) != 2 {
		t.Fatal("stale refresh")
	}
}
func TestSearchMatcherAndHighlight(t *testing.T) {
	hits := searchMatches("hapi", []string{"harbor-api", "huge unrelated application item", "other"})
	if len(hits) != 2 || hits[0].Index != 0 {
		t.Fatal(hits)
	}
	hits = searchMatches("日本", []string{"\x1b[31m🚢 日本語\x1b[0m"})
	if len(hits) != 1 || hits[0].MatchedIndexes[0] != 2 {
		t.Fatal(hits)
	}
	t.Setenv("NO_COLOR", "1")
	if highlightSearch("日本語", "日本") != "日本語" {
		t.Fatal("NO_COLOR")
	}
	os.Unsetenv("NO_COLOR")
	s := highlightSearch("\x1b[48;2;29;53;71m日本語\x1b[0m", "日本")
	if ansi.Strip(s) != "日本語" || !strings.Contains(s, "38;2;"+brandRGB()) {
		t.Fatal(s)
	}
}
func TestInlineSearchVisual(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		for _, screen := range []string{"sessions", "magpie", "new-instance", "new-catalog"} {
			m := searchFixture()
			m.screen = screen
			if strings.HasPrefix(screen, "new-") {
				m.formActive = true
			}
			m.width, m.height = size[0], size[1]
			m = keySearch(m, "/")
			q := "Harbor"
			if screen == "magpie" || screen == "new-instance" {
				q = "gateway"
			}
			if screen == "new-catalog" {
				q = "model"
			}
			m = keySearch(m, q)
			view := m.View()
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("height")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("width", line)
				}
			}
			if !strings.Contains(ansi.Strip(view), "tab list") {
				t.Fatal("lost focus hints")
			}
			if dir := os.Getenv("HARBOR_SEARCH_VISUAL"); dir != "" {
				os.MkdirAll(dir, 0700)
				os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.ansi", screen, size[0])), []byte(view), 0600)
			}
		}
	}
}
func TestInlineSearchTerminalProbe(t *testing.T) {
	if os.Getenv("HARBOR_SEARCH_TERMINAL") == "" {
		t.Skip("opt-in isolated terminal")
	}
	m := searchFixture()
	m.service = &fakeApplicationService{Sessions: m.sessions, Reports: m.reports}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSpansAcrossFields(t *testing.T) {
	p := candidatePositions("› Harbor API", "api日本", []string{"Harbor API", "/日本/project", ""})
	if len(p) != 3 {
		t.Fatal(p)
	}
	p = candidatePositions("    📂 /日本/…", "api日本", []string{"Harbor API", "/日本/project", ""})
	if len(p) != 2 {
		t.Fatal(p)
	}
}

func TestSearchLargeCatalogScrolledSpans(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := searchFixture()
	m.screen = "new-catalog"
	m.width, m.height = 60, 20
	m.models = nil
	for i := 0; i < 10000; i++ {
		m.models = append(m.models, catalogModel{ID: fmt.Sprintf("relay-%05d/model", i), DisplayName: "shared model"})
	}
	m.catalogFilter = "model"
	m.catalogCursor = 9999
	m.formActive = true
	plain := m.View()
	if !strings.Contains(plain, m.filteredModels()[9999].ID) || strings.Contains(plain, m.filteredModels()[0].ID) {
		t.Fatal("wrong scrolled candidates")
	}
	os.Unsetenv("NO_COLOR")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	lines := strings.Split(m.View(), "\n")
	if len(lines) > m.height {
		t.Fatal("render unbounded")
	}
	for _, line := range lines[4:] {
		if strings.Contains(ansi.Strip(line), "relay-") && !strings.Contains(line, "38;2;"+brandRGB()) {
			t.Fatal("lost visible row spans", line)
		}
	}
}

func TestInlineSearchHorizontalEdit(t *testing.T) {
	m := searchFixture()
	m = keySearch(m, "/")
	m = keySearch(m, "Harbor")
	m = keySearch(m, "left")
	m = keySearch(m, "X")
	if m.filter != "HarboXr" || !strings.Contains(m.searchLine(0, 3), "HarboX▏r") {
		t.Fatal(m.filter, m.searchLine(0, 3))
	}
}

func TestSearchLongQueryKeepsCountAndCursor(t *testing.T) {
	m := searchFixture()
	m.width = 60
	m = keySearch(m, "/")
	m = keySearch(m, strings.Repeat("日本語", 80))
	line := m.searchLine(0, 3)
	if ansi.StringWidth(line) > 56 || !strings.Contains(line, "0/3") || !strings.Contains(line, "▏") {
		t.Fatal(line)
	}
}

func TestQueryEditSelectsBestMatch(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{
		{session: session{ID: "native", Name: "UI-Native", Mode: "local"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}},
		{session: session{ID: "alpha", Name: "UI-A", Mode: "magpie"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}},
	}
	m = keySearch(m, "/")
	m = keySearch(m, "UI-A")
	if m.searchSelectedID() != "alpha" {
		t.Fatal("query retained a weaker fuzzy match instead of selecting best result", m.searchSelectedID())
	}
}
