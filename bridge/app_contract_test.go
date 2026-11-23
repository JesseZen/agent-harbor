package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
	"time"
)

func TestContextHelpRestoresParentControlAfterMouseBack(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-catalog"
	m.width, m.height = 46, 12
	m.models = []catalogModel{{ID: "group/test"}}
	m.draft.Instance = "fixture"
	m.uiFocus = "selector-apply"
	p0, _ := m.additionalPage()
	m.scroll = min(1, p0.MaxScroll)
	expectedScroll := m.scroll
	n, _ := m.Update(uiKey("?"))
	m = n.(harborModel)
	if m.screen != "help" {
		t.Fatal("help did not open")
	}
	p, _ := m.secondaryPage()
	for _, r := range p.Hits {
		if r.ID == "secondary:esc" {
			n, _ = m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m = n.(harborModel)
			break
		}
	}
	if m.screen != "new-catalog" || m.uiFocus != "selector-apply" || m.scroll != expectedScroll {
		t.Fatalf("parent state lost: %s %s %d", m.screen, m.uiFocus, m.scroll)
	}
}

func TestSecondaryBackReturnsFocusToSessionList(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "detail"
	m.uiFocus = "action:i"
	p, _ := m.secondaryPage()
	for _, r := range p.Hits {
		if r.ID == "secondary:esc" {
			n, _ := m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			m = n.(harborModel)
			break
		}
	}
	if m.screen != "sessions" || m.uiFocus != "list" {
		t.Fatalf("stale control retained: %s %s", m.screen, m.uiFocus)
	}
}

func TestTabLeavesSelectorFocusUnchanged(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-catalog"
	m.draft.Instance = "fixture"
	m.models = []catalogModel{{ID: "test"}}
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyShiftTab} {
		next, cmd := m.Update(tea.KeyMsg{Type: key})
		if cmd != nil || next.(harborModel).uiFocus != m.uiFocus {
			t.Fatal("Tab changed selector focus")
		}
	}
}

func TestMainTabNavigationAndTextOwnership(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	for _, step := range []struct{ key, screen string }{
		{"tab", "sessions"}, {"l", "magpie"}, {"right", "accounts"},
		{"l", "profile-list"}, {"right", "profile-list"},
	} {
		next, _ := m.Update(uiKey(step.key))
		m = next.(harborModel)
		if m.screen != step.screen {
			t.Fatalf("%s selected %s, want %s", step.key, m.screen, step.screen)
		}
	}
	if m.settings == nil {
		t.Fatal("right from Profiles did not open Settings")
	}
	next, _ := m.Update(uiKey("h"))
	m = next.(harborModel)
	if m.settings != nil || m.screen != "profile-list" {
		t.Fatal("h did not go back to Profiles")
	}
	for _, step := range []struct{ key, screen string }{
		{"left", "accounts"}, {"h", "magpie"}, {"left", "sessions"},
		{"/", "sessions"}, {"h", "sessions"}, {"l", "sessions"}, {"j", "sessions"}, {"k", "sessions"},
	} {
		next, _ := m.Update(uiKey(step.key))
		m = next.(harborModel)
		if m.screen != step.screen {
			t.Fatalf("%s left %s", step.key, step.screen)
		}
	}
	if m.filter != "hljk" {
		t.Fatal("vim keys escaped the search field")
	}
}

func TestNewFormRemembersOnlyItsProfileConnection(t *testing.T) {
	m, _ := formFixture()
	a := profileBinding{ProfileID: "A", Client: "codex"}
	b := profileBinding{ProfileID: "B", Client: "codex"}
	m.draft.Profile, m.draft.Connection = &a, "magpie"
	m.rememberConnection()
	for _, test := range []struct {
		binding        *profileBinding
		recorded, want string
	}{
		{&a, "local", "magpie"},
		{&b, "", "local"},
		{&b, "magpie", "magpie"},
	} {
		m.screen = "new-project-loading"
		next, _ := m.Update(appNewReady{generation: m.generation, environment: appEnvironment{
			Profile: test.binding, LastConnection: test.recorded,
			LastMagpieInstance: "saved-instance", LastMagpieModel: "saved-model",
		}})
		if draft := next.(harborModel).draft; draft.Connection != test.want || draft.Instance != "saved-instance" || draft.Model != "saved-model" {
			t.Fatal(draft)
		}
	}
	m.draft = sessionDraft{Profile: &a, Connection: "magpie", Instance: "saved-instance", Model: "saved-model"}
	m.formSaved = m.draft
	m.screen = "new-connection"
	m.connectionCursor = 0
	next, _ := m.Update(uiKey("enter"))
	if draft := next.(harborModel).draft; draft.Connection != "local" || draft.Instance != "saved-instance" || draft.Model != "saved-model" {
		t.Fatalf("connection switch discarded remembered Magpie choice: %+v", draft)
	}
}

func TestProfileEnvironmentRemembersLatestMagpieSessionSelection(t *testing.T) {
	service, _ := profileFixture(t)
	profileA, profileB := mustProfile(t, service, "A"), mustProfile(t, service, "B")
	bindingA, bindingB := bindingFor(profileA), bindingFor(profileB)
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	if err := withSessions(func([]session) ([]session, error) {
		return []session{
			{Profile: &bindingA, Mode: "magpie", InstanceID: "gateway-a", MagpieModel: "model-a", CreatedAt: older},
			{Profile: &bindingA, Mode: "magpie", InstanceID: "gateway-b", MagpieModel: "group-b", CreatedAt: newer},
			{Profile: &bindingA, Mode: "local", CreatedAt: newer.Add(time.Hour)},
			{Profile: &bindingB, Mode: "magpie", InstanceID: "gateway-other", MagpieModel: "model-other", CreatedAt: newer.Add(time.Hour)},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	env, err := service.ProfileEnvironment(bindingA)
	if err != nil || env.LastConnection != "local" || env.LastMagpieInstance != "gateway-b" || env.LastMagpieModel != "group-b" {
		t.Fatalf("last choices: connection=%q Magpie=%q/%q (%v)", env.LastConnection, env.LastMagpieInstance, env.LastMagpieModel, err)
	}
	if err := service.RememberMagpieSelection(bindingA, "gateway-saved", "model-saved"); err != nil {
		t.Fatal(err)
	}
	reopened, err := service.ProfileEnvironment(bindingA)
	if err != nil || reopened.LastMagpieInstance != "gateway-saved" || reopened.LastMagpieModel != "model-saved" {
		t.Fatalf("reopened preference: %q %q (%v)", reopened.LastMagpieInstance, reopened.LastMagpieModel, err)
	}
	other, err := service.ProfileEnvironment(bindingB)
	if err != nil || other.LastMagpieInstance != "gateway-other" || other.LastMagpieModel != "model-other" {
		t.Fatalf("preference crossed Profiles: %q %q (%v)", other.LastMagpieInstance, other.LastMagpieModel, err)
	}
}
