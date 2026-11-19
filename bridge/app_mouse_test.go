package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"strings"
	"testing"
)

func mouseAt(m harborModel, x, y int) harborModel {
	if f, ok := m.service.(*fakeApplicationService); ok && f.Sessions == nil {
		f.Sessions = append([]sessionView{}, m.sessions...)
	}
	n, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	for cmd != nil {
		n, cmd = n.Update(cmd())
	}
	return n.(harborModel)
}
func TestMouseCurrentRowActivatesAndConfirmationBlocksClickThrough(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "a", Name: "中文 🚢", Mode: "local"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	for _, hit := range m.sessionsLayout().Hits {
		if hit.ID == "session:a" {
			m = mouseAt(m, hit.X, hit.Y)
			break
		}
	}
	if m.screen != "confirm" || m.reviewed.ID != "a" {
		t.Fatal("one click did not open the current row", m.screen)
	}
	for _, hit := range m.reviewLayout().Hits {
		if hit.ID == "review:enter" {
			next, cmd := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if next.(harborModel).screen != "confirm" || cmd != nil {
				t.Fatal("confirmation accepted click-through")
			}
			break
		}
	}
}
func TestMouseFormAndBounds(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {60, 20}, {100, 30}, {160, 45}} {
		m, f := formFixture()
		m.width, m.height = size[0], size[1]
		p, _ := m.newSessionPage()
		for _, r := range p.Hits {
			if r.ID == "new-field:Name" {
				m = mouseAt(m, r.X, r.Y)
				if m.screen != "new-form" || m.formFocus != formName {
					t.Fatal("first click should focus Name")
				}
				m = mouseAt(m, r.X, r.Y)
				break
			}
		}
		if m.screen != "new-name" || len(f.Calls) != 0 {
			t.Fatalf("%v %s %v", size, m.screen, f.Calls)
		}
		m = appKey(m, "esc").(harborModel)
		m = mouseAt(m, 159, 6)
		if m.screen != "new-form" {
			t.Fatal("outside width activated")
		}
	}
	m := newHarborModel(&fakeApplicationService{})
	m.width = 160
	m.sessions = []sessionView{{session: session{ID: "a", Name: "one"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	n, _ := m.Update(tea.MouseMsg{X: 80, Y: 8, Button: tea.MouseButtonWheelDown})
	if n.(harborModel).detailScroll != 1 || n.(harborModel).cursor != 0 {
		t.Fatal("detail wheel scope")
	}
	m.screen = "delete-history"
	m.reviewed = session{Name: "exact"}
	m = mouseAt(m, 2, 6)
	if m.screen != "delete-history" || m.input.Value() != "" {
		t.Fatal("typed deletion bypass")
	}
}
func TestMouseVisual(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	out := os.Getenv("HARBOR_MOUSE_VISUAL_OUT")
	if out == "" {
		t.Skip("opt in")
	}
	original, exists := os.LookupEnv("NO_COLOR")
	defer func() {
		if exists {
			os.Setenv("NO_COLOR", original)
		} else {
			os.Unsetenv("NO_COLOR")
		}
	}()
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		form, _ := formFixture()
		form.width, form.height = size[0], size[1]
		form = mouseAt(form, 3, 9)
		form = appKey(form, "mouse draft").(harborModel)
		form = appKey(form, "enter").(harborModel)
		list := newHarborModel(&fakeApplicationService{})
		list.width, list.height = size[0], size[1]
		list.sessions = []sessionView{{session: session{ID: "a", Name: "Harbor API", CLI: "codex", Mode: "magpie", WorkingDirectory: "/projects/harbor", MagpieModel: "route-a"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Stopped"}, {session: session{ID: "b", Name: "中文 🚢 Review", CLI: "codex", Mode: "local", WorkingDirectory: "/projects/docs"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Stopped"}}
		list = mouseAt(list, 3, 9)
		for _, plain := range []bool{false, true} {
			os.Unsetenv("NO_COLOR")
			if plain {
				os.Setenv("NO_COLOR", "1")
			}
			for name, m := range map[string]harborModel{"form": form, "sessions": list} {
				os.WriteFile(fmt.Sprintf("%s/%s-%v-%d.ansi", out, name, plain, size[0]), []byte(strings.ReplaceAll(m.View(), "\n", "\r\n")), 0600)
			}
		}
	}
}

func TestMouseUnselectedThenCurrentRow(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "first", Name: "First"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}, {session: session{ID: "second", Name: "Second 🚢"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	lines := strings.Split(m.View(), "\n")
	y := -1
	for i, l := range lines {
		if strings.Contains(l, "Second 🚢") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatal("missing rendered row")
	}
	m = mouseAt(m, 3, y)
	if m.cursor != 1 || m.screen != "sessions" {
		t.Fatal("single selection")
	}
	m = mouseAt(m, 3, y)
	if m.screen != "confirm" || m.reviewed.ID != "second" {
		t.Fatal("click on newly selected row")
	}
	m = appKey(m, "esc").(harborModel)
	m = mouseAt(m, 3, y)
	next, _ := m.Update(appLoaded{sessions: []sessionView{{session: session{ID: "third", Name: "Replacement"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}})
	if !next.(harborModel).selectionMissing {
		t.Fatal("async replacement armed a different row")
	}
}

func TestMouseShortPopupVisibleCells(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.width, m.height = 45, 12
	m.sessions = []sessionView{{session: session{ID: "a", Name: "Visible"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	rowY := -1
	for i, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "Visible") {
			rowY = i
			break
		}
	}
	if rowY < 0 {
		t.Fatal("visible row was clipped")
	}
	m = mouseAt(m, 0, m.height-1)
	if m.screen != "sessions" {
		t.Fatal("footer activated row")
	}
	m = mouseAt(m, 3, rowY)
	if m.screen != "confirm" {
		t.Fatal("visible name inert")
	}
}

func TestMousePickerRenderedGeometry(t *testing.T) {
	for _, screen := range []string{"magpie", "new-instance", "new-catalog", "new-directory"} {
		t.Run(screen, func(t *testing.T) {
			m, _ := formFixture()
			m.screen = screen
			m.width, m.height = 60, 20
			reports := []instanceReport{{magpieInstance: magpieInstance{ID: "one", Name: "First"}}, {magpieInstance: magpieInstance{ID: "two", Name: "Second", ConfigDir: strings.Repeat("/long/中文", 15)}}}
			m.reports, m.newInstances = reports, reports
			m.models = []catalogModel{{ID: "first"}, {ID: "second"}}
			m.picker.entries = []directoryEntry{{path: "/first", source: "fixture"}, {path: "/second", source: "fixture"}}
			lines := strings.Split(m.View(), "\n")
			y := -1
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), "second") {
					y = i
					break
				}
			}
			if y < 0 {
				t.Fatal(m.View())
			}
			m = mouseAt(m, 3, y)
			cursor := m.instanceCursor
			if screen == "new-catalog" {
				cursor = m.catalogCursor
			}
			if screen == "new-directory" {
				cursor = m.picker.cursor
			}
			if cursor != 1 {
				t.Fatalf("%s cursor=%d at rendered y%d", screen, cursor, y)
			}
		})
	}
}

func TestMouseEveryFormControl(t *testing.T) {
	for i, want := range []string{"new-directory", "new-cli", "new-connection", "new-form", codexOptionsScreen, "new-name", "busy", "sessions"} {
		for _, height := range []int{12, 24} {
			m, _ := formFixture()
			m.height = height
			m.formFocus = 0
			ids := []string{"new-field:Project", "new-field:Client", "new-field:Connection", "new-field:Model", "new-field:Options", "new-field:Name", "new-create", "new-cancel"}
			p, _ := m.newSessionPage()
			x, y := -1, -1
			for _, r := range p.Hits {
				if r.ID == ids[i] {
					x, y = r.X, r.Y
					break
				}
			}
			if x < 0 {
				t.Fatalf("missing control %s", ids[i])
			}
			n := mouseAt(m, x, y)
			if i > formProject && i <= formName {
				if n.screen != "new-form" || n.formFocus != i {
					t.Fatalf("field %d first click did not only move focus", i)
				}
				n = mouseAt(n, x, y)
			}
			if n.screen != want {
				t.Fatalf("field %d height%d: %s want %s", i, height, n.screen, want)
			}
		}
	}
}
func TestMouseDetailScrollSelectionParity(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "a"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}, {session: session{ID: "b"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	m.detailScroll = 4
	n := appKey(m, "down").(harborModel)
	if n.detailScroll != 0 {
		t.Fatal("keyboard retained old detail offset")
	}
	m.detailScroll = 3
	n = appKey(m, "/").(harborModel)
	n = appKey(n, "nonmatching").(harborModel)
	if n.detailScroll != 0 {
		t.Fatal("search retained old detail offset")
	}
}

func TestMouseScrolledSelectionStaysUnderPointer(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.height = 30
	for i := 0; i < 20; i++ {
		m.sessions = append(m.sessions, sessionView{session: session{ID: fmt.Sprint(i), Name: fmt.Sprintf("Row %02d", i)}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}})
	}
	m.cursor = 10
	lines := strings.Split(m.View(), "\n")
	y := -1
	for i, line := range lines {
		if strings.Contains(line, "Row 08") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatal("fixture")
	}
	m = mouseAt(m, 3, y)
	if !strings.Contains(strings.Split(m.View(), "\n")[y], "Row 08") {
		t.Fatal("clicked row shifted")
	}
	m = mouseAt(m, 3, y)
	if m.screen != "confirm" || m.reviewed.ID != "8" {
		t.Fatal("click on scrolled current row failed")
	}
}

func TestMouseDirectoryPreviewPreservesExplicitSelection(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-directory"
	m.picker.entries = []directoryEntry{{path: "/projects/harbor", source: "fixture"}}
	m.picker.previewPath = "/projects/harbor"
	p := m.directoryPage()
	x, y := -1, -1
	for _, r := range p.Hits {
		if strings.HasPrefix(r.ID, "dir-path:") {
			x, y = r.X, r.Y
			break
		}
	}
	if x < 0 {
		t.Fatal("directory row missing")
	}
	m = mouseAt(m, x, y)
	// Preview completion must preserve selection; only Use commits the edit.
	next, _ := m.Update(directoryPreviewResult{generation: m.picker.previewGeneration, path: m.picker.previewPath, text: "synthetic preview"})
	m = next.(harborModel)
	if m.picker.entries[m.picker.cursor].path != "/projects/harbor" {
		t.Fatal("preview changed selection")
	}
	p = m.directoryPage()
	for _, r := range p.Hits {
		if r.ID == "dir-use" {
			m = mouseAt(m, r.X, r.Y)
			break
		}
	}
	if m.screen != "new-form" {
		t.Fatalf("directory double click: %s", m.screen)
	}
}
