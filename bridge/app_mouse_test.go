package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"strings"
	"testing"
	"time"
)

func mouseAt(m harborModel, x, y int, at time.Time) harborModel {
	if f, ok := m.service.(*fakeApplicationService); ok && f.Sessions == nil {
		f.Sessions = append([]sessionView{}, m.sessions...)
	}
	n, cmd := m.updateMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, at)
	for cmd != nil {
		n, cmd = n.Update(cmd())
	}
	return n.(harborModel)
}
func TestMouseDoubleClickSafety(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "a", Name: "中文 🚢", Mode: "local"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	at := time.Now()
	for _, tc := range []struct {
		name    string
		delay   time.Duration
		dx      int
		confirm bool
	}{{"double", 100 * time.Millisecond, 0, true}, {"late", time.Second, 0, false}, {"different cell", 100 * time.Millisecond, 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			n := mouseAt(m, 2, 6, at)
			if n.screen != "sessions" {
				t.Fatal(n.screen)
			}
			release, _ := n.updateMouse(tea.MouseMsg{X: 2, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}, at)
			n = release.(harborModel)
			n = mouseAt(n, 2+tc.dx, 6, at.Add(tc.delay))
			if (n.screen == "confirm") != tc.confirm {
				t.Fatal(n.screen)
			}
			if tc.confirm {
				n = mouseAt(n, 2, 6, at.Add(150*time.Millisecond))
				if n.screen != "confirm" {
					t.Fatal("confirmation penetrated")
				}
			}
		})
	}
	n := mouseAt(m, 2, 6, at)
	n.sessions[0].ID = "replacement"
	n = mouseAt(n, 2, 6, at.Add(time.Millisecond))
	if n.screen != "sessions" {
		t.Fatal("stale ID acted")
	}
	n = mouseAt(m, 2, 6, at)
	v, _ := n.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	n = mouseAt(v.(harborModel), 2, 6, at.Add(time.Millisecond))
	if n.screen != "sessions" {
		t.Fatal("resize retained click")
	}
	n = mouseAt(m, 2, 6, at)
	n = appKey(n, "/").(harborModel)
	n = appKey(n, "中").(harborModel)
	if n.mouse.id != "" {
		t.Fatal("query retained click")
	}
}
func TestMouseFormAndBounds(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {60, 20}, {100, 30}, {160, 45}} {
		m, f := formFixture()
		m.width, m.height = size[0], size[1]
		p, _ := m.newSessionPage()
		for _, r := range p.Hits {
			if r.ID == "new-field:Name" {
				m = mouseAt(m, r.X, r.Y, time.Now())
				break
			}
		}
		if m.screen != "new-name" || len(f.Calls) != 0 {
			t.Fatalf("%v %s %v", size, m.screen, f.Calls)
		}
		m = appKey(m, "esc").(harborModel)
		m = mouseAt(m, 159, 6, time.Now())
		if m.screen != "new-form" {
			t.Fatal("outside width activated")
		}
	}
	m := newHarborModel(&fakeApplicationService{})
	m.width = 160
	m.sessions = []sessionView{{session: session{ID: "a", Name: "one"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	n, _ := m.updateMouse(tea.MouseMsg{X: 80, Y: 8, Button: tea.MouseButtonWheelDown}, time.Now())
	if n.(harborModel).detailScroll != 1 || n.(harborModel).cursor != 0 {
		t.Fatal("detail wheel scope")
	}
	m.screen = "delete-history"
	m.reviewed = session{Name: "exact"}
	m = mouseAt(m, 2, 6, time.Now())
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
		form = mouseAt(form, 3, 9, time.Now())
		form = appKey(form, "mouse draft").(harborModel)
		form = appKey(form, "enter").(harborModel)
		list := newHarborModel(&fakeApplicationService{})
		list.width, list.height = size[0], size[1]
		list.sessions = []sessionView{{session: session{ID: "a", Name: "Harbor API", CLI: "codex", Mode: "magpie", WorkingDirectory: "/projects/harbor", MagpieModel: "route-a"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Stopped"}, {session: session{ID: "b", Name: "中文 🚢 Review", CLI: "codex", Mode: "local", WorkingDirectory: "/projects/docs"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Stopped"}}
		list = mouseAt(list, 3, 9, time.Now())
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

func TestMouseUnselectedAndRepaint(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "first", Name: "First"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}, {session: session{ID: "second", Name: "Second 🚢"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	at := time.Now()
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
	m = mouseAt(m, 3, y, at)
	if m.cursor != 1 || m.screen != "sessions" {
		t.Fatal("single selection")
	}
	m = mouseAt(m, 3, y, at.Add(time.Millisecond))
	if m.screen != "confirm" || m.reviewed.ID != "second" {
		t.Fatal("double on newly selected row")
	}
	m = appKey(m, "esc").(harborModel)
	m = mouseAt(m, 3, y, at)
	next, _ := m.Update(appLoaded{sessions: []sessionView{{session: session{ID: "third", Name: "Replacement"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}})
	if next.(harborModel).mouse.id != "" {
		t.Fatal("async replacement retained history")
	}
}

func TestMouseShortPopupVisibleCells(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.width, m.height = 45, 12
	m.sessions = []sessionView{{session: session{ID: "a", Name: "Visible"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}
	at := time.Now()
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
	m = mouseAt(m, 3, rowY, at)
	if m.mouse.id != "a" {
		t.Fatal("visible name inert")
	}
	m = mouseAt(m, 3, m.height-1, at.Add(time.Millisecond))
	if m.mouse.id != "" || m.screen != "sessions" {
		t.Fatal("footer activated row")
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
			m = mouseAt(m, 3, y, time.Now())
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
			n, _ := m.updateMouse(tea.MouseMsg{X: 3, Y: y, Button: tea.MouseButtonWheelUp}, time.Now())
			if n.(harborModel).mouse.id != "" {
				t.Fatal("wheel retained click")
			}
		})
	}
}

func TestMouseEveryFormControl(t *testing.T) {
	for i, want := range []string{"new-directory", "new-cli", "new-connection", "new-form", "new-name", "busy", "sessions"} {
		for _, height := range []int{12, 24} {
			m, _ := formFixture()
			m.height = height
			ids := []string{"new-field:Project", "new-field:Client", "new-field:Connection", "new-field:Model", "new-field:Name", "new-create", "new-cancel"}
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
			n := mouseAt(m, x, y, time.Now())
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
	at := time.Now()
	m = mouseAt(m, 3, y, at)
	if !strings.Contains(strings.Split(m.View(), "\n")[y], "Row 08") {
		t.Fatal("clicked row shifted")
	}
	m = mouseAt(m, 3, y, at.Add(time.Millisecond))
	if m.screen != "confirm" || m.reviewed.ID != "8" {
		t.Fatal("scrolled double click failed")
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
	m = mouseAt(m, x, y, time.Now())
	// Preview completion must preserve selection; only Use commits the edit.
	next, _ := m.Update(directoryPreviewResult{generation: m.picker.previewGeneration, path: m.picker.previewPath, text: "synthetic preview"})
	m = next.(harborModel)
	if m.picker.entries[m.picker.cursor].path != "/projects/harbor" {
		t.Fatal("preview changed selection")
	}
	p = m.directoryPage()
	for _, r := range p.Hits {
		if r.ID == "dir-use" {
			m = mouseAt(m, r.X, r.Y, time.Now())
			break
		}
	}
	if m.screen != "new-form" {
		t.Fatalf("directory double click: %s", m.screen)
	}
}
