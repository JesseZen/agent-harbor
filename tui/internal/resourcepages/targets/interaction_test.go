package targets

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyboardAndMouseIntents(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetSize(120, 30)
	_ = page.View()

	page.SelectID("cred-1")
	cases := []struct {
		key  tea.KeyMsg
		want resourcepage.Intent
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, resourcepage.IntentCreate},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, resourcepage.IntentEdit},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, resourcepage.IntentDelete},
		{tea.KeyMsg{Type: tea.KeyEnter}, resourcepage.IntentDetails},
	}
	for _, tc := range cases {
		page.CancelOverlay()
		model, _ := page.Update(tc.key)
		page = model.(*Page)
		if page.LastIntent() != tc.want {
			t.Fatalf("key %v => intent=%q want %q", tc.key, page.LastIntent(), tc.want)
		}
	}

	page.CancelOverlay()
	_ = page.View()
	newHit := page.Table().FooterActionHit(resourceview.ActionCreate)
	if newHit.Kind != 0 {
		y := newHit.Y + page.StripHeight() + page.BannerOffset()
		model, _ := page.Update(tea.MouseMsg{X: newHit.X, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		page = model.(*Page)
		if page.LastIntent() != resourcepage.IntentCreate {
			t.Fatalf("footer new => %q", page.LastIntent())
		}
	}

	page.CancelOverlay()
	_ = page.View()
	rowY := page.TableHeaderY() + 1
	click := tea.MouseMsg{X: 2, Y: rowY + page.StripHeight() + page.BannerOffset(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	model, _ := page.Update(click)
	page = model.(*Page)
	model, _ = page.Update(click)
	page = model.(*Page)
	if page.LastIntent() != resourcepage.IntentDetails && !page.ShowingDetails() {
		t.Fatalf("double-click details intent=%q details=%v", page.LastIntent(), page.ShowingDetails())
	}
}

func TestStripSwitchKinds(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	if page.Kind() != KindCredentials {
		t.Fatalf("kind=%q", page.Kind())
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindLimitPolicies {
		t.Fatalf("alt-left kind=%q want limit_policies", page.Kind())
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	page = model.(*Page)
	if page.Kind() != KindUpstreams {
		t.Fatalf("alt-right kind=%q", page.Kind())
	}
	view := page.View()
	for _, label := range []string{"Upstreams", "Limit Policies"} {
		if !strings.Contains(view, label) {
			t.Fatalf("strip missing %s:\n%s", label, view)
		}
	}
}

func TestDetailsNeverShowsSecretOrManagedLocator(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SelectID("cred-1")
	page.OpenDetails()
	view := strings.ToLower(page.View())
	for _, field := range []string{"id", "name", "provider", "generation"} {
		if !strings.Contains(view, field) {
			t.Fatalf("details missing %s:\n%s", field, page.View())
		}
	}
	if strings.Contains(view, "planted") || strings.Contains(view, "locator") && strings.Contains(view, "value") {
		t.Fatalf("suspicious secret content:\n%s", page.View())
	}
	page.CancelOverlay()
	page.SelectID("cred-ext")
	page.OpenDetails()
	view = page.View()
	// External schema fields may appear; managed locators / secret bytes must not.
	if strings.Contains(view, "planted-cred-token") {
		t.Fatal("secret token leaked into details")
	}
}
