package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestProfilePageCreateRenameSelectAndCapturedDraft(t *testing.T) {
	f := &fakeApplicationService{}
	a, err := f.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
	if err != nil {
		t.Fatal(err)
	}
	ab := bindingFor(a)
	f.SelectedProfile = &ab
	m := newHarborModel(f)
	m.screen = "profile-list"
	result := m.loadProfiles()()
	n, _ := m.Update(result)
	m = n.(harborModel)
	if !strings.Contains(m.View(), "A") || m.activeProfile == nil {
		t.Fatal(m.View())
	}
	m = appKey(m, "n").(harborModel)
	m.input.SetValue("B")
	n, cmd := m.Update(uiKey("enter"))
	m = n.(harborModel)
	if cmd == nil {
		t.Fatal("create did not reload profiles")
	}
	n, _ = m.Update(cmd())
	m = n.(harborModel)
	if len(f.ProfileRecords) != 2 {
		t.Fatal(f.ProfileRecords)
	}
	m.profileCursor = 1
	m = appKey(m, "r").(harborModel)
	m.input.SetValue("Work B")
	n, cmd = m.Update(uiKey("enter"))
	m = n.(harborModel)
	n, _ = m.Update(cmd())
	m = n.(harborModel)
	if f.ProfileRecords[1].Name != "Work B" {
		t.Fatal(f.ProfileRecords)
	}
	m.profileCursor = 1
	m = appKey(m, "enter").(harborModel)
	if m.screen != "sessions" || m.activeProfile.ProfileID != f.ProfileRecords[1].ID {
		t.Fatal("selection not applied", m)
	}
	n, cmd = m.Update(uiKey("n"))
	m = n.(harborModel)
	if cmd == nil {
		t.Fatal("no new-session load")
	}
	n, _ = m.Update(cmd())
	m = n.(harborModel)
	if m.draft.Profile == nil || m.draft.Profile.ProfileID != f.ProfileRecords[1].ID {
		t.Fatal("draft did not capture B")
	}
	// Selecting another popup default cannot redirect the captured B draft.
	f.SelectProfile(ab)
	if m.draft.Profile.ProfileID != f.ProfileRecords[1].ID {
		t.Fatal("draft followed external selection")
	}
}

func TestProfilePageMouseAndKeyboardDefault(t *testing.T) {
	f := &fakeApplicationService{KnownInstances: []magpieInstance{{ID: "shared-api", Name: "Shared API"}}}
	a, _ := f.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
	ab := bindingFor(a)
	f.SelectedProfile = &ab
	m := newHarborModel(f)
	m.screen = "profile-list"
	n, _ := m.Update(m.loadProfiles()())
	m = n.(harborModel)
	page, _ := m.profilePage()
	clicked := false
	for _, hit := range page.Hits {
		if hit.ID == "profile:n" {
			n, cmd := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if cmd != nil {
				t.Fatal("unexpected process")
			}
			m = n.(harborModel)
			clicked = true
			break
		}
	}
	if !clicked || m.screen != "profile-new" {
		t.Fatal("create mouse action inert")
	}
	m = appKey(m, "esc").(harborModel)
	m = appKey(m, "d").(harborModel)
	m = appKey(m, "down").(harborModel)
	n, cmd := m.Update(uiKey("enter"))
	m = n.(harborModel)
	if f.ProfileRecords[0].DefaultInstanceID != "shared-api" || cmd == nil {
		t.Fatal("keyboard default not saved")
	}
}

func TestProfileLegacyBindingRequiresReviewAndExplicitChoice(t *testing.T) {
	f := &fakeApplicationService{}
	p, _ := f.CreateProfile(profileCreateRequest{Name: "Original", Kind: profileExisting})
	f.Sessions = []sessionView{{session: session{ID: "0123456789abcdef0123456789abcdef", Name: "Old Conversation", CLI: "codex"}}}
	m := newHarborModel(f)
	m.screen = "profile-list"
	m.sessions = f.Sessions
	n, _ := m.Update(m.loadProfiles()())
	m = n.(harborModel)
	m = appKey(m, "b").(harborModel)
	if m.screen != "profile-bind" || !strings.Contains(m.View(), "independently verify") || !strings.Contains(m.View(), p.Clients[0].Home) {
		t.Fatal(m.View())
	}
	if f.Sessions[0].Profile != nil {
		t.Fatal("opening review bound the session")
	}
	// The default Enter cancels; it never accepts an unknown original home.
	n, _ = m.Update(uiKey("enter"))
	m = n.(harborModel)
	if m.screen != "profile-list" || f.Sessions[0].Profile != nil {
		t.Fatal("review default accepted binding")
	}
	m = appKey(m, "b").(harborModel)
	m = appKey(m, "tab").(harborModel)
	n, cmd := m.Update(uiKey("enter"))
	m = n.(harborModel)
	if cmd == nil || f.Sessions[0].Profile == nil || f.Sessions[0].Profile.ProfileID != p.ID {
		t.Fatal("explicit review did not bind original environment")
	}
}

func TestAccountPageUsesGlobalProfileSelection(t *testing.T) {
	f := &fakeApplicationService{}
	a, _ := f.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
	b, _ := f.CreateProfile(profileCreateRequest{Name: "B", Kind: profileIsolated})
	ab, bb := bindingFor(a), bindingFor(b)
	m := newHarborModel(f)
	m.activeProfile = &ab
	m.draft.Profile = &bb
	n, _ := m.Update(uiKey("a"))
	m = n.(harborModel)
	if m.accountProfile == nil || *m.accountProfile != ab {
		t.Fatal("inactive draft redirected account page")
	}
	n, _ = m.Update(uiKey("p"))
	m = n.(harborModel)
	if m.screen != "profile-list" {
		t.Fatal("account page did not use global selector")
	}
	m.profiles = f.ProfileRecords
	m.profileCursor = 1
	m.uiFocus = ""
	n, _ = m.Update(uiKey("enter"))
	m = n.(harborModel)
	if m.activeProfile == nil || *m.activeProfile != bb || m.accountProfile == nil || *m.accountProfile != bb {
		t.Fatal("Profile/account selections diverged")
	}
}
