package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func appKey(m tea.Model, key string) tea.Model {
	if h, ok := m.(harborModel); ok {
		h.reviewOpenedAt = time.Time{}
		if key == "enter" {
			h.uiFocus = ""
		}
		m = h // appKey models separate, deliberate human key presses.
		if f, ok := h.service.(*fakeApplicationService); ok && f.Sessions == nil {
			f.Sessions = append([]sessionView{}, h.sessions...)
		}
	}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg.Type = tea.KeyEnter
	case "esc":
		msg.Type = tea.KeyEsc
	case "up":
		msg.Type = tea.KeyUp
	case "tab":
		msg.Type = tea.KeyTab
	case "down":
		msg.Type = tea.KeyDown
	}
	next, cmd := m.Update(msg)
	return settleApp(next, cmd)
}
func TestHarborSessionsAtPracticalSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Name: "Harbor API", WorkingDirectory: "/projects/harbor", CLI: "codex", Mode: "magpie", MagpieModel: "relay-a/test-model"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}, Status: "Needs attention", Reason: "gateway offline"}}}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = model.Update(m.Init()())
		text := model.View()
		t.Logf("RENDER %dx%d\n%s\nEND RENDER", size[0], size[1], text)
		for _, want := range []string{"Harbor API", "Needs attention", "gateway offline", "n New session", "? help"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%v missing %q:\n%s", size, want, text)
			}
		}
		if len(strings.Split(text, "\n")) > size[1] {
			t.Fatal("height overflow")
		}
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("width overflow: %q", line)
			}
		}
		model = appKey(model, "/")
		model = appKey(model, "unmatched")
		model = appKey(model, "enter")
		if !strings.Contains(model.View(), "No matching sessions") {
			t.Fatal(model.View())
		}
	}
}

func TestHarborStopAndRemoveRequireReview(t *testing.T) {
	for _, action := range []struct{ key, call, note string }{{"s", "stop one", "conversation stays resumable"}, {"x", "remove one", "Codex history is kept"}} {
		f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Name: "API", WorkingDirectory: "/projects/api", Mode: "local"}, Runtime: runtimeObservation{Presence: "observed", Activity: "working"}, Status: "Running"}}}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(m.Init()())
		model = appKey(model, action.key)
		if !strings.Contains(model.View(), action.note) {
			t.Fatal(model.View())
		}
		if strings.Contains(strings.Join(f.Calls, "|"), action.call) {
			t.Fatal("acted before confirmation")
		}
		model = appKey(model, "esc")
		if strings.Contains(strings.Join(f.Calls, "|"), action.call) {
			t.Fatal("acted on cancel")
		}
		model = appKey(model, action.key)
		model = appKey(model, "enter")
		if !strings.Contains(strings.Join(f.Calls, "|"), action.call) {
			t.Fatal(f.Calls)
		}
	}
}

func TestHarborCreatesLocalOnlyAfterReview(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {

		f := &fakeApplicationService{}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = model.Update(m.Init()())
		for _, k := range []string{"n", "up", "enter", "API", "enter", "down"} {
			model = appKey(model, k)
		}
		if strings.Contains(strings.Join(f.Calls, "|"), "create") {
			t.Fatal("created before review")
		}
		if !strings.Contains(model.View(), "New session") {
			t.Fatal(model.View())
		}
		model = appKey(model, "ctrl+s")
		if !strings.Contains(strings.Join(f.Calls, "|"), "open ") {
			t.Fatal(f.Calls)
		}

	}
}

func TestHarborMagpieManagementExplainsRefusal(t *testing.T) {
	f := &fakeApplicationService{}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "m")
	if !strings.Contains(model.View(), "Magpie instances") {
		t.Fatal(model.View())
	}
	if !strings.Contains(cleanUI(model.View()), "Bind external") {
		t.Fatal("no recovery action", model.View())
	}
}

func TestHarborMagpieCatalogFiltersBeforeReview(t *testing.T) {
	for _, size := range [][2]int{{60, 24}, {80, 24}, {100, 30}, {160, 45}} {

		f := &fakeApplicationService{KnownInstances: []magpieInstance{{ID: "managed", Name: "Test gateway", Address: "http://127.0.0.1:44001", ConfigDir: "/fixture/magpie"}}, Models: []catalogModel{{ID: "relay-a/test"}, {ID: "relay-b/test"}}}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = model.Update(m.Init()())
		for _, k := range []string{"n", "down", "down", "enter", "down", "enter", "enter", "relay-b"} {
			model = appKey(model, k)
		}
		if view := cleanUI(model.View()); !strings.Contains(view, "relay-b/test") || strings.Contains(view, "relay-a/test") {
			t.Fatal(model.View())
		}

	}
}

func TestHarborResumeShowsServiceRefusals(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {

		for _, reason := range []string{"directory missing", "instance changed", "identity unknown", "model missing", "gateway offline", "ID ambiguous", "conversation open elsewhere"} {
			row := savedReviewFixture()
			row.ID, row.Mode = "one", "local"
			f := &fakeApplicationService{Sessions: []sessionView{row}}
			m := newHarborModel(f)
			var model tea.Model = m
			model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			model, _ = model.Update(m.Init()())
			model = appKey(model, "enter")
			model = appKey(model, "a")
			f.Err = fmt.Errorf("%s", reason)
			model = appKey(model, "enter")
			if !strings.Contains(model.View(), reason) {
				t.Fatal(model.View())
			}
			if !strings.Contains(strings.Join(f.Calls, "|"), "open one") {
				t.Fatal(f.Calls)
			}
		}

	}
}

func TestHarborLongRefusalIsReadable(t *testing.T) {
	row := savedReviewFixture()
	row.ID, row.Mode = "one", "local"
	f := &fakeApplicationService{Sessions: []sessionView{row}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	model, _ = model.Update(m.Init()())
	model = appKey(model, "enter")
	model = appKey(model, "a")
	f.Err = fmt.Errorf("The instance identity differs from the recorded configuration. Nothing was started. Check Magpie providers and bind the intended instance before retrying.")
	model = appKey(model, "enter")
	model = appKey(model, "i")
	for i := 0; i < 20; i++ {
		model = appKey(model, "down")
	}
	if !strings.Contains(cleanUI(model.View()), "retrying.") {
		t.Fatal(model.View())
	}
}

func TestHarborInstanceSelectionStaysVisible(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		f := &fakeApplicationService{}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		reports := []instanceReport{}
		instances := []magpieInstance{}
		for i := 0; i < 30; i++ {
			name := fmt.Sprintf("Gateway-%02d", i)
			reports = append(reports, instanceReport{magpieInstance: magpieInstance{ID: name, Name: name, ConfigDir: "/fixture/" + strings.Repeat("long/", 30)}, Health: instanceVerified})
			instances = append(instances, magpieInstance{ID: name, Name: name})
		}
		for _, wizard := range []bool{false, true} {
			if wizard {
				model = appKey(model, "esc")
				f.KnownInstances = instances
				for _, k := range []string{"n", "down", "down", "enter", "down", "enter"} {
					model = appKey(model, k)
				}
			} else {
				f.Reports = reports
				model = appKey(model, "m")
			}
			for i := 0; i < 29; i++ {
				model = appKey(model, "down")
			}
			view := model.View()
			if !strings.Contains(cleanUI(view), "Gateway-29") {
				t.Fatalf("%v selection invisible:\n%s", size, view)
			}
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("height overflow")
			}
		}
	}
}

func TestHarborFormEditorCancelPreservesDraft(t *testing.T) {
	var model tea.Model = newHarborModel(&fakeApplicationService{})
	for _, k := range []string{"n", "up", "enter", "API", "enter", "enter", "discard", "esc"} {
		model = appKey(model, k)
	}
	m := model.(harborModel)
	if m.screen != "new-form" || m.draft.Name != "API" {
		t.Fatal(m.screen, m.draft)
	}
}

func TestHarborMagpieHelpAndClose(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	var model tea.Model = m
	model = appKey(model, "m")
	model = appKey(model, "?")
	if !strings.Contains(model.View(), "Start Harbor-managed") || strings.Contains(model.View(), "Rename") {
		t.Fatal(model.View())
	}
	model = appKey(model, "esc")
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatalf("q did not close Magpie: %s", next.View())
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}

func TestHarborMagpieWarningCanBeReadAndCancelReturns(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {

		f := &fakeApplicationService{Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "gateway", Name: "Gateway"}, Health: instanceVerified, Warnings: []string{strings.Repeat("Review this configuration. ", 40) + "END WARNING"}}}}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

		model = appKey(model, "m")
		model = appKey(model, "enter")
		for i := 0; i < 50; i++ {
			model = appKey(model, "pgdown")
		}
		if !strings.Contains(model.View(), "END WARNING") {
			t.Fatal(model.View())
		}
		model = appKey(model, "esc")
		if !strings.Contains(model.View(), "Magpie instances") {
			t.Fatal(model.View())
		}

	}
}

func TestHarborRouteBelongsToSelectedSession(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "a", Name: "A", Mode: "magpie"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}, {session: session{ID: "b", Name: "B", Mode: "magpie"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}, Route: observedRoute{Observed: true, Main: "PROVIDER_A"}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "i")
	model = appKey(model, "esc")
	model = appKey(model, "/")
	model = appKey(model, "B")
	model = appKey(model, "enter")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if strings.Contains(model.View(), "PROVIDER_A") {
		t.Fatal(model.View())
	}
}

func TestHarborBusyIgnoresEscapeAndNewActions(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "a", Name: "A"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "s")
	model = appKey(model, "tab")
	h := model.(harborModel)
	h.reviewOpenedAt = time.Time{}
	model = h
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, k := range []string{"esc", "n", "?", "m"} {
		model = appKey(model, k)
	}
	if !strings.Contains(model.View(), "Working") {
		t.Fatal(model.View())
	}
	model, _ = model.Update(cmd())
	if strings.Join(f.Calls, "|") != "list|stop a" {
		t.Fatal(f.Calls)
	}
}

func TestHarborCancelledCatalogResponseCannotReplaceNewInstance(t *testing.T) {
	for _, changeInstance := range []bool{false, true} {
		f := &fakeApplicationService{KnownInstances: []magpieInstance{{ID: "a", Name: "Gateway A"}, {ID: "b", Name: "Gateway B"}}, Models: []catalogModel{{ID: "shared-id", DisplayName: "OLD_CATALOG"}}}
		var model tea.Model = newHarborModel(f)
		for _, k := range []string{"n", "down", "down", "enter", "down", "enter"} {
			model = appKey(model, k)
		}
		model, cmdA := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		lateA := cmdA()
		model = appKey(model, "esc")
		for _, k := range []string{"enter", "down", "enter"} {
			model = appKey(model, k)
		}
		if changeInstance {
			model = appKey(model, "down")
		}
		f.Models = []catalogModel{{ID: "shared-id", DisplayName: "CURRENT_CATALOG"}}
		// Deliver A while the newer request is still pending, including when both
		// requests target the same instance and have the same model ID.
		model, cmdCurrent := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model, _ = model.Update(lateA)
		if strings.Contains(model.View(), "OLD_CATALOG") {
			t.Fatal(model.View())
		}
		model, _ = model.Update(cmdCurrent())
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "CURRENT_CATALOG") || strings.Contains(view, "OLD_CATALOG") {
			t.Fatal(view)
		}
		model = appKey(model, "esc")
		model = appKey(model, "esc")
		model, _ = model.Update(lateA)
		if strings.Contains(model.View(), "Choose a model") {
			t.Fatal("cancelled catalog reopened wizard")
		}
	}
}

func TestHarborFocusesLiveSessionDespiteConnectionWarning(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "a", Name: "A"}, Runtime: runtimeObservation{Presence: "observed", Activity: "working", RunVerified: true, Terminal: "term-1"}, Running: true, Status: "Needs attention", Reason: "gateway offline"}}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "enter")
	if !strings.Contains(strings.Join(f.Calls, "|"), "focus a") || strings.Contains(model.View(), "Review resume") {
		t.Fatal(f.Calls, model.View())
	}
}

func TestHarborFiltersInstancesAndShowsCurrentIdentity(t *testing.T) {
	f := &fakeApplicationService{Reports: []instanceReport{
		{magpieInstance: magpieInstance{ID: "a", Name: "Alpha", ConfigDir: "/fixture/alpha"}, Health: instanceVerified},
		{magpieInstance: magpieInstance{ID: "b", Name: "Beta", ConfigDir: "/fixture/beta"}, Health: "unknown", Detail: "configuration unknown"},
	}}
	for _, wizard := range []bool{false, true} {
		var model tea.Model = newHarborModel(f)
		if wizard {
			for _, k := range []string{"n", "down", "down", "enter", "down", "enter"} {
				model = appKey(model, k)
			}
		} else {
			model = appKey(model, "m")
		}
		for _, k := range []string{"/", "Beta"} {
			model = appKey(model, k)
		}
		model, _ = model.Update(uiKey("enter"))
		view := ansi.Strip(model.View())
		if strings.Contains(view, "Alpha") || !strings.Contains(view, "Beta") || !strings.Contains(view, "unknown") {
			t.Fatal(view)
		}
		if !wizard {
			model = appKey(model, "i")
		}
		if !strings.Contains(ansi.Strip(model.View()), "/fixture/beta") || !strings.Contains(ansi.Strip(model.View()), "configuration unknown") {
			t.Fatal(model.View())
		}
	}
}

func TestHarborInstanceRefreshCannotReopenCancelledScreen(t *testing.T) {
	f := &fakeApplicationService{Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "a", Name: "Alpha"}, Health: instanceVerified}}}
	var model tea.Model = newHarborModel(f)
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	late := cmd()
	model = appKey(model, "esc")
	model, _ = model.Update(late)
	if strings.Contains(model.View(), "Magpie instances") {
		t.Fatal(model.View())
	}
	model = appKey(model, "m")
	for _, k := range []string{"/", "Alpha", "enter"} {
		model = appKey(model, k)
	}
	f.Reports = []instanceReport{{magpieInstance: magpieInstance{ID: "b", Name: "Beta"}, Health: instanceVerified}}
	model = appKey(model, "r")
	if strings.Contains(model.View(), "› Beta") {
		t.Fatal("refresh lost filter", model.View())
	}
	model = appKey(model, "enter")
	if strings.Contains(model.View(), "Review Magpie action") {
		t.Fatal("acted without selection", model.View())
	}
}

func TestHarborFilteredWizardSelectsMatchingInstance(t *testing.T) {
	f := &fakeApplicationService{Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "a", Name: "Alpha"}, Health: instanceVerified}, {magpieInstance: magpieInstance{ID: "b", Name: "Beta"}, Health: instanceUnknown}}, Models: []catalogModel{{ID: "shared-model"}}}
	var model tea.Model = newHarborModel(f)
	for _, k := range []string{"n", "down", "down", "enter", "down", "enter", "/", "Beta", "enter"} {
		model = appKey(model, k)
	}
	if !strings.Contains(strings.Join(f.Calls, "|"), "catalog b") || model.(harborModel).draft.Instance != "b" {
		t.Fatal(f.Calls, model.View())
	}
}

func TestHarborInstanceDetailsFitAndScroll(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		f := &fakeApplicationService{Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "a", Name: "Alpha", ConfigDir: "/fixture/config"}, Health: instanceVerified, Warnings: []string{strings.Repeat("Warning text. ", 300) + "END_WARNING"}}}}
		var model tea.Model = newHarborModel(f)
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model = appKey(model, "m")
		model = appKey(model, "i")
		for i := 0; i < 400; i++ {
			model = appKey(model, "down")
		}
		view := model.View()
		if !strings.Contains(view, "END_WARNING") {
			t.Fatal(size, view)
		}
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("height overflow")
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatal("width overflow")
			}
		}
		model = appKey(model, "esc")
		if !strings.Contains(model.View(), "Magpie instances") {
			t.Fatal(model.View())
		}
	}
}

func TestHarborMagpieStartsAtSelectedSessionsInstance(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Mode: "magpie", InstanceID: "b"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}, Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "a", Name: "Alpha"}, Health: instanceVerified}, {magpieInstance: magpieInstance{ID: "b", Name: "Beta"}, Health: instanceVerified}}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "m")
	model = appKey(model, "enter")
	if !strings.Contains(model.View(), "Instance  b") {
		t.Fatal(model.View())
	}
	// A missing recorded instance must not silently select a different gateway.
	f.Sessions[0].InstanceID = "missing"
	m = newHarborModel(f)
	model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "m")
	model = appKey(model, "enter")
	if strings.Contains(model.View(), "Review Magpie action") {
		t.Fatal(model.View())
	}
	if !strings.Contains(model.View(), "missing") {
		t.Fatal(model.View())
	}
}

func TestHarborRemembersAuxiliaryNoticePerInstance(t *testing.T) {
	f := &fakeApplicationService{KnownInstances: []magpieInstance{{ID: "a"}, {ID: "b"}}, Models: []catalogModel{{ID: "model"}}}
	m := newHarborModel(f)
	m.screen = "new-catalog"
	m.draft = sessionDraft{Connection: "magpie", Instance: "a"}
	m.models = f.Models
	var model tea.Model = m
	model = appKey(model, "enter")
	if model.(harborModel).screen != "new-notice" || !strings.Contains(model.View(), "Acknowledge & apply") {
		t.Fatal(model.View())
	}
	model = appKey(model, "enter")
	m = model.(harborModel)
	m.screen = "new-catalog"
	model = appKey(m, "enter")
	if !strings.Contains(model.View(), "Session name") {
		t.Fatal("acknowledgement was not remembered:", model.View())
	}
	m = model.(harborModel)
	m.screen = "new-catalog"
	m.draft.Instance = "b"
	model = appKey(m, "enter")
	if model.(harborModel).screen != "new-notice" || !strings.Contains(model.View(), "Acknowledge & apply") {
		t.Fatal("another instance bypassed notice:", model.View())
	}
}

func TestHarborSelectsRecentProjectWithoutTyping(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "new-project"
	m.environment.Projects = []string{"/projects/first", "/projects/second"}
	var model tea.Model = m
	model = appKey(model, "down")
	model = appKey(model, "down")
	model = appKey(model, "enter")
	if got := model.(harborModel).input.Value(); got != "/projects/second" {
		t.Fatalf("selected %q", got)
	}
	model = appKey(model, "enter")
	if model.(harborModel).draft.Directory != "/projects/second" {
		t.Fatal(model.View())
	}
}

func TestHarborDetailsSeparateMainAuxiliaryAndIdentityEvidence(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "a", Name: "API", Mode: "magpie", MagpieModel: "selected-group", HerdrAgentName: "harbor-api", ConversationIDSource: "captured", Candidates: []candidateEvidence{{ID: "candidate-one", Directory: "/projects/api"}}}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}, Route: observedRoute{Observed: true, Main: routeRecord{Model: "provider-a/model-a", Kind: "main", Tries: []routeAttempt{{Model: "provider-a/model-a", Status: 200}}}, Auxiliary: []any{routeRecord{Model: "provider-b/title", Kind: "title"}}}}
	m := newHarborModel(f)
	m.height = 100
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "i")
	for _, want := range []string{"Selected ID  selected-group", "Main request", "provider-a/model-a", "HTTP 200", "Auxiliary request", "provider-b/title", "candidate-one", "/projects/api", "harbor-api", "ID source  captured"} {
		if !strings.Contains(strings.Join(strings.Fields(model.View()), " "), strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("missing %q: %s", want, model.View())
		}
	}
}

func TestHarborDirectoryCompletionChoicesAtPracticalSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		choices := []string{}
		for i := 0; i < 30; i++ {
			choices = append(choices, fmt.Sprintf("/projects/project-%02d/", i))
		}
		m := newHarborModel(&fakeApplicationService{})
		m.environment.Projects = choices
		m.screen = "new-project"
		m.width = size[0]
		m.height = size[1]
		m.input.SetValue("/projects/")
		model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if cmd == nil {
			t.Fatal("preview effect missing")
		}
		model, _ = model.Update(cmd())
		for i := 0; i < 29; i++ {
			model = appKey(model, "down")
		}
		if !strings.Contains(model.View(), "project-29") {
			t.Fatal(model.View())
		}
		if len(strings.Split(model.View(), "\n")) > size[1] {
			t.Fatal("height overflow")
		}
		model = appKey(model, "esc")
		if model.(harborModel).input.Value() != "/projects/" {
			t.Fatal("cancel changed input")
		}
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = appKey(model, "down")
		model = appKey(model, "enter")
		if model.(harborModel).input.Value() != "/projects/project-01" {
			t.Fatal(model.View())
		}
	}
}

func TestMagpieCatalogShowsGatewayLabelsWithoutInferringFromIDs(t *testing.T) {
	f := &fakeApplicationService{Models: []catalogModel{{ID: "arbitrary-id", DisplayName: "Fast", MagpieLabel: "Fast · routing group", OwnedBy: "relay-a"}}}
	m := newHarborModel(f)
	m.screen = "new-catalog"
	m.models = f.Models
	if !strings.Contains(m.View(), "Fast · routing group") {
		t.Fatal(m.View())
	}
}

func TestMagpiePopupUsesVerifiedCurrentPaneContext(t *testing.T) {
	f := &fakeApplicationService{ContextInstance: "b", Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "a", Name: "A"}}, {magpieInstance: magpieInstance{ID: "b", Name: "B"}, Health: instanceVerified}}}
	m := newMagpieModel(f)
	var model tea.Model = m
	model, cmd := model.Update(m.Init()())
	for cmd != nil {
		model, cmd = model.Update(cmd())
	}
	model = appKey(model, "enter")
	if !strings.Contains(model.View(), "Instance  b") {
		t.Fatal(model.View())
	}
}

func TestPreviousShellCloseRequiresReviewAndCancelDoesNotClose(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Name: "Previous API", HerdrPane: "w1:p2"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Exited"}}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "c")
	if !strings.Contains(model.View(), "Close inspected shell") {
		t.Fatal(model.View())
	}
	if strings.Contains(strings.Join(f.Calls, "|"), "close-previous-pane") {
		t.Fatal(f.Calls)
	}
	model = appKey(model, "esc")
	if strings.Contains(strings.Join(f.Calls, "|"), "close-previous-pane") {
		t.Fatal(f.Calls)
	}
	model = appKey(model, "c")
	model = appKey(model, "enter")
	if !strings.Contains(strings.Join(f.Calls, "|"), "close-previous-pane one") {
		t.Fatal(f.Calls)
	}
}

func TestRetiredHistoryShortcutExplainsNativeTools(t *testing.T) {
	f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Name: "API"}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}, Status: "Exited"}}}
	m := newHarborModel(f)
	var model tea.Model = m
	model, _ = model.Update(m.Init()())
	model = appKey(model, "d")
	if !strings.Contains(strings.Join(strings.Fields(ansi.Strip(model.View())), " "), "client’s own tools") {
		t.Fatal(model.View())
	}
	model = appKey(model, "API")
	model = appKey(model, "enter")
	if strings.Contains(strings.Join(f.Calls, "|"), "delete-history") {
		t.Fatal(f.Calls)
	}
}

func TestDeletionReviewAndManagementRefusalAtPracticalSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Name: "API", CodexSessionID: "01912345-6789-7abc-8def-0123456789ab"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}, Reports: []instanceReport{{magpieInstance: magpieInstance{ID: "external", Name: "External"}, Health: instanceUnknown, Detail: "configuration identity unknown"}}}
		m := newHarborModel(f)
		var model tea.Model = m
		model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = model.Update(m.Init()())
		model = appKey(model, "d")
		model = appKey(model, "TYPE_ME")
		if !strings.Contains(strings.Join(strings.Fields(model.View()), " "), "client’s own tools") {
			t.Fatal("history explanation hidden:", model.View())
		}
		model = appKey(model, "esc")
		model = appKey(model, "m")
		model = appKey(model, "enter")
		if !strings.Contains(model.View(), "Cannot open management") {
			t.Fatal(model.View())
		}
		if strings.Contains(strings.Join(f.Calls, "|"), "magpie manage") {
			t.Fatal(f.Calls)
		}
	}
}

func TestWizardChoicesCanCloseWithQ(t *testing.T) {
	for _, screen := range []string{"new-cli", "new-connection", "new-instance", "new-catalog", "new-notice", "new-review", "new-project-choices"} {
		m := newHarborModel(&fakeApplicationService{})
		m.screen = screen
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		if cmd == nil {
			t.Fatalf("%s did not close", screen)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not quit", screen)
		}
	}
}

func TestProjectTypingValidatesBeforeEnter(t *testing.T) {
	f := &fakeApplicationService{Err: fmt.Errorf("directory unavailable; edit the path")}
	m := newHarborModel(f)
	m.screen = "new-project"
	m.input.Focus()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/missing")})
	// Run asynchronous effects, excluding cursor timer messages.
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				run(child)
			}
			return
		}
		if _, ok := msg.(tea.KeyMsg); ok {
			return
		}
		next, _ = next.Update(msg)
	}
	run(cmd)
	if !strings.Contains(next.View(), "directory unavailable") {
		t.Fatal(next.View())
	}
	if !strings.Contains(strings.Join(f.Calls, "|"), "validate /missing") {
		t.Fatal(f.Calls)
	}
}
func TestLaterMagpieReviewRetainsGroupSwitchingReminder(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "new-review"
	m.draft = sessionDraft{Connection: "magpie", Model: "group-id"}
	if !strings.Contains(m.View(), "Routing groups may switch members") {
		t.Fatal(m.View())
	}
}

func TestDirectoryValidationCannotReplaceEditedOrCancelledWizard(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "new-project"
	m.generation = 2
	m.input.SetValue("/current")
	for _, msg := range []appDirectoryValidated{{generation: 1, path: "/old", err: fmt.Errorf("stale error")}, {generation: 2, path: "/old", err: fmt.Errorf("stale error")}} {
		next, _ := m.Update(msg)
		if strings.Contains(next.View(), "stale error") {
			t.Fatal(next.View())
		}
	}
	m.screen = "sessions"
	next, _ := m.Update(appDirectoryValidated{generation: 2, path: "/current", err: fmt.Errorf("stale error")})
	if strings.Contains(next.View(), "stale error") {
		t.Fatal(next.View())
	}
}

func TestCatalogFilteringShowsMatchesWhileTyping(t *testing.T) {
	f := &fakeApplicationService{}
	m := newHarborModel(f)
	m.screen = "new-catalog-filter"
	m.input.Focus()
	m.models = []catalogModel{{ID: "alpha"}, {ID: "beta"}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("beta")})
	if !strings.Contains(next.View(), "beta") || strings.Contains(next.View(), "alpha") || !strings.Contains(next.View(), "› beta") {
		t.Fatal(next.View())
	}
}

func TestEveryHarborScreenFitsPracticalSizes(t *testing.T) {
	screens := []string{"sessions", "detail", "filter", "rename", "delete-history", "confirm", "error", "help", "busy", "new-project", "new-project-choices", "new-cli", "new-connection", "new-instance", "new-instance-detail", "new-instance-filter", "new-catalog", "new-catalog-filter", "new-notice", "new-name", "new-review", "magpie", "magpie-detail", "magpie-filter", "magpie-bind", "magpie-confirm", "magpie-context", "magpie-busy"}
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		for _, screen := range screens {
			t.Run(fmt.Sprintf("%s/%d", screen, size[0]), func(t *testing.T) {
				m := newHarborModel(&fakeApplicationService{})
				m.width = size[0]
				m.height = size[1]
				m.screen = screen
				m.previous = "sessions"
				m.action = "resume"
				m.reviewed = session{ID: "one", Name: "API", WorkingDirectory: "/project", CLI: "codex", Mode: "magpie", MagpieModel: "group"}
				m.sessions = []sessionView{{session: m.reviewed, Status: "Needs attention", Reason: strings.Repeat("gateway offline; refresh. ", 30)}}
				m.draft = sessionDraft{Name: "API", Directory: "/project", Connection: "magpie", Instance: "managed", Model: "group"}
				m.models = []catalogModel{{ID: "group", MagpieLabel: "routing group"}}
				m.input.SetValue("group")
				m.directoryChoices = []string{"/project"}
				m.notice = strings.Repeat("Long recovery explanation. ", 40)
				view := m.View()
				lines := strings.Split(view, "\n")
				if len(lines) > size[1] {
					t.Fatal("height", len(lines), view)
				}
				for _, line := range lines {
					if lipgloss.Width(line) > size[0] {
						t.Fatal("width", view)
					}
				}
				if strings.TrimSpace(ansi.Strip(lines[0])) == "" {
					t.Fatal("missing frame", view)
				}
				if strings.TrimSpace(lines[len(lines)-1]) == "" {
					t.Fatal("missing key footer", view)
				}
			})
		}
	}
}

func TestSessionConfirmationsAtThreeSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		for _, action := range []struct{ key, call, note string }{{"s", "stop one", "conversation stays resumable"}, {"x", "remove one", "Codex history is kept"}} {
			f := &fakeApplicationService{Sessions: []sessionView{{session: session{ID: "one", Name: "API", WorkingDirectory: "/project", CLI: "codex"}, Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}}}}
			m := newHarborModel(f)
			var model tea.Model = m
			model, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			model, _ = model.Update(m.Init()())
			model = appKey(model, action.key)
			if !strings.Contains(model.View(), action.note) {
				t.Fatal(size, action.key, model.View())
			}
			model = appKey(model, "esc")
			if strings.Contains(strings.Join(f.Calls, "|"), action.call) {
				t.Fatal("acted on cancel", f.Calls)
			}
			model = appKey(model, action.key)
			if action.key == "d" {
				model = appKey(model, "API")
			}
			model = appKey(model, "enter")
			if !strings.Contains(strings.Join(f.Calls, "|"), action.call) {
				t.Fatal(size, f.Calls)
			}
		}
	}
}

func TestHelpPreservesLoadingResponses(t *testing.T) {
	for _, tc := range []struct {
		screen, want string
		response     tea.Msg
	}{
		{"new-project-loading", "new-form", appNewReady{generation: 4, environment: appEnvironment{Directory: "/project"}}},
		{"new-instance-loading", "new-instance", appNewInstances{generation: 4, instances: []instanceReport{{magpieInstance: magpieInstance{ID: "a"}}}}},
		{"new-catalog-loading", "new-catalog", appCatalog{generation: 4, instance: "a", models: []catalogModel{{ID: "model"}}}},
		{"magpie-context-loading", "magpie", appContextReady{id: "a"}},
		{"magpie", "magpie", appInstances{generation: 4, reports: []instanceReport{{magpieInstance: magpieInstance{ID: "a"}}}}},
	} {
		t.Run(tc.screen, func(t *testing.T) {
			m := newHarborModel(&fakeApplicationService{})
			m.screen = tc.screen
			m.generation = 4
			m.draft.Instance = "a"
			var model tea.Model = appKey(m, "?")
			model, _ = model.Update(tc.response)
			if model.(harborModel).screen != "help" {
				t.Fatal("help closed unexpectedly")
			}
			model = appKey(model, "esc")
			if model.(harborModel).screen != tc.want {
				t.Fatalf("got %s want %s", model.(harborModel).screen, tc.want)
			}
		})
	}
}

func TestWizardPreselectsDefaultInstance(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "new-instance-loading"
	m.environment.DefaultInstance = "b"
	model, _ := m.Update(appNewInstances{instances: []instanceReport{{magpieInstance: magpieInstance{ID: "a"}}, {magpieInstance: magpieInstance{ID: "b"}}}})
	if model.(harborModel).instanceCursor != 1 {
		t.Fatal("default b not selected")
	}
}

func TestHelpLoadingErrorReturnsToRecoveryScreen(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "new-catalog-loading"
	m.generation = 3
	m.draft.Instance = "a"
	model := appKey(m, "?")
	model, _ = model.Update(appCatalog{generation: 3, instance: "a", err: fmt.Errorf("gateway offline")})
	model = appKey(model, "esc")
	if model.(harborModel).screen != "new-instance" || !strings.Contains(model.View(), "gateway offline") {
		t.Fatal(model.View())
	}
	// A truly cancelled request remains ignored after help closes.
	model = appKey(model, "esc")
	model, _ = model.Update(appCatalog{generation: 3, instance: "a", models: []catalogModel{{ID: "stale"}}})
	if model.(harborModel).screen == "new-catalog" {
		t.Fatal("cancelled response reopened catalog")
	}
}

func TestResumeReviewExplainsMagpieIdentityAndMutableGroups(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		m := newHarborModel(&fakeApplicationService{})
		m.width, m.height = size[0], size[1]
		m.screen, m.action = "confirm", "resume"
		m.reviewed = session{Name: "API", Mode: "magpie", MagpieModel: "group", InstanceID: "managed", MagpieURL: "http://127.0.0.1:3435"}
		view := m.confirmView()
		for _, want := range []string{"managed", "127.0.0.1:3435", "Groups may change members", "Review in Magpie"} {
			if !strings.Contains(view, want) {
				t.Fatalf("size %v missing %q: %s", size, want, view)
			}
		}
	}
}
