package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

type profileReviewState struct {
	Session     session
	Environment profileClientEnvironment
}

type appProfilesLoaded struct {
	profiles []profile
	selected profile
	err      error
}

func (m harborModel) loadProfiles() tea.Cmd {
	return func() tea.Msg {
		p, err := m.service.EnsureProfiles()
		if err != nil {
			return appProfilesLoaded{err: err}
		}
		all, err := m.service.Profiles()
		return appProfilesLoaded{profiles: all, selected: p, err: err}
	}
}
func (m harborModel) activeProfileLabel() string {
	if m.activeProfile == nil {
		return "Choose Profile"
	}
	return m.profileDisplayLabel(m.activeProfile)
}
func (m harborModel) profilePage() (uiPage, bool) {
	if !strings.HasPrefix(m.screen, "profile-") {
		return uiPage{}, false
	}
	if m.screen == "profile-bind" && m.profileReview != nil {
		r := m.profileReview
		original := r.Session.CodexHome
		if original == "" {
			original = "Unknown — independently verify the original environment before binding"
		}
		body := fmt.Sprintf("Session: %s\nRecorded original client home: %s\n\nSelected existing environment: %s\nClient home: %s\nSQLite: %s\n\nConfirm only after checking this is the Conversation's original environment. No history or credentials will be moved. External clients may share this environment.\n%s", r.Session.Name, original, r.Environment.ProfileName, r.Environment.Home, r.Environment.SQLite, m.notice)
		return secondaryDocument(m.width, m.height, "Review original environment", body, []uiAction{{ID: "profile:confirm-bind", Label: "Bind original environment", Key: "ctrl+y"}, {ID: "profile:esc", Label: "Cancel", Key: "esc"}}, m.uiFocus, m.scroll), true
	}
	if m.screen == "profile-new" || m.screen == "profile-rename" {
		title := "Create isolated Profile"
		if m.screen == "profile-rename" {
			title = "Rename Profile"
		}
		return secondaryDocument(m.width, m.height, title, textEntry(m.input, m.width, "Profile name")+"\n"+m.notice, []uiAction{{ID: "profile:enter", Label: "Save", Key: "enter", Primary: true}, {ID: "profile:esc", Label: "Cancel", Key: "esc"}}, m.uiFocus, m.scroll), true
	}
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "Harbor · Profiles"))
	p.tabs("profiles", m.uiHover, m.uiFocus, true)
	p.field("", "", "Current", cleanUI(m.activeProfileLabel()), false, false)
	p.line(paint(fg("section"), "Profiles"))
	actions := []uiAction{{ID: "profile:enter", Label: "Select", Key: "enter", Primary: true}, {ID: "profile:n", Label: "Create", Key: "n"}, {ID: "profile:r", Label: "Rename", Key: "r"}, {ID: "profile:d", Label: "Magpie default", Key: "d"}, {ID: "profile:esc", Label: "Back", Key: "esc"}}
	if s, ok := m.selected(); ok && !s.External && s.Profile == nil {
		actions = append(actions, uiAction{ID: "profile:b", Label: "Bind reviewed original environment", Key: "b"})
	}
	p.shortcuts(actions, m.uiFocus)
	p.Hint = "↑↓ choose · Tab focus"
	listTop := len(p.Lines)
	count := max(1, m.height-len(p.Lines)-len(p.shortcutBar().Lines))
	if m.notice != "" {
		count = max(1, count-1)
	}
	start := m.listStart(m.profileCursor, count)
	for i := start; i < min(len(m.profiles), start+count); i++ {
		row := m.profiles[i]
		label := row.Name + " · " + string(row.Kind)
		if m.activeProfile != nil && row.ID == m.activeProfile.ProfileID {
			label += " · selected"
		}
		y := len(p.Lines)
		p.field("", "", cleanUI(label), "", i == m.profileCursor, false)
		p.hit("profile-row:"+row.ID, 1, y, max(1, p.Width-2), 1)
	}
	p.scrollRail(listTop, len(p.Lines)-listTop, len(m.profiles), start)

	if m.screen == "profile-default" {
		p = newUIPage(m.width, m.height)
		p.line(paint(fg("brand"), "Harbor › Profile Magpie default"))
		p.line("For future sessions only; existing sessions keep their instance.")
		count := max(1, m.height-7)
		start := max(0, m.profileDefaultCursor-count+1)
		listTop := len(p.Lines)
		for i := start; i < min(len(m.profileInstances)+1, start+count); i++ {
			id, label := "", "Shared workspace default"
			if i > 0 {
				id, label = m.profileInstances[i-1].ID, m.profileInstances[i-1].Name+" · "+m.profileInstances[i-1].ID
			}
			p.field("profile-instance:"+id, "", label, "", m.profileDefaultCursor == i, false)
		}
		p.scrollRail(listTop, len(p.Lines)-listTop, len(m.profileInstances)+1, start)
		p.shortcuts([]uiAction{{ID: "profile:enter", Label: "Save default", Key: "enter"}, {ID: "profile:esc", Label: "Cancel", Key: "esc"}}, "")
	}
	if m.notice != "" {
		p.line(cleanUI(m.notice))
	}

	p.Hint = "↑↓ choose · Tab focus"
	return p.finish(0), true
}
func (m harborModel) profilePageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if result, ok := msg.(appProfilesLoaded); ok {
		if result.err != nil {
			m.notice = result.err.Error()
			return m, nil, true
		}
		m.profiles = result.profiles
		if m.activeProfile == nil {
			b, err := bindingOf(result.selected, "codex")
			if err == nil {
				m.activeProfile = &b
				m.accountProfile = &b
				m.accountSnapshot = nil
				m.accountGeneration++
			}
		}
		return m, nil, true
	}
	if !strings.HasPrefix(m.screen, "profile-") {
		return m, nil, false
	}
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		return m, nil, false
	}
	key, iskey := msg.(tea.KeyMsg)
	k := key.String()
	if mouse, ok := msg.(tea.MouseMsg); ok && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
		page, _ := m.profilePage()
		id := page.target(mouse.X, mouse.Y)
		if strings.HasPrefix(id, "nav:") {
			n, c := m.activateSessions(id)
			return n, c, true
		}
		if strings.HasPrefix(id, "profile-row:") {
			for i, p := range m.profiles {
				if p.ID == strings.TrimPrefix(id, "profile-row:") {
					m.profileCursor = i
				}
			}
			return m, nil, true
		}
		if strings.HasPrefix(id, "profile-instance:") {
			if m.profileCursor >= len(m.profiles) {
				return m, nil, true
			}
			b, err := bindingOf(m.profiles[m.profileCursor], "codex")
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			if err := m.service.SetProfileDefaultInstance(b, strings.TrimPrefix(id, "profile-instance:")); err != nil {
				m.notice = err.Error()
			} else {
				m.notice = "Default saved; existing sessions keep their recorded instance."
				m.screen = "profile-list"
			}
			return m, m.loadProfiles(), true
		}
		k = strings.TrimPrefix(id, "profile:")
		iskey = true
	}
	if !iskey {
		return m, nil, false
	}
	if k == "tab" || k == "shift+tab" {
		page, _ := m.profilePage()
		ids := []string{}
		for _, hit := range page.Hits {
			if strings.HasPrefix(hit.ID, "nav:") || strings.HasPrefix(hit.ID, "profile:") || strings.HasPrefix(hit.ID, "profile-instance:") {
				ids = append(ids, hit.ID)
			}
		}
		m.uiFocus = uiCycle(m.uiFocus, ids, k == "shift+tab")
		if strings.HasPrefix(m.uiFocus, "profile-instance:") {
			m.profileDefaultCursor = 0
			for i, instance := range m.profileInstances {
				if m.uiFocus == "profile-instance:"+instance.ID {
					m.profileDefaultCursor = i + 1
				}
			}
		}
		return m, nil, true
	}
	if k == "enter" && m.uiFocus != "" {
		if strings.HasPrefix(m.uiFocus, "profile-instance:") {
			m.profileDefaultCursor = 0
			for i, instance := range m.profileInstances {
				if m.uiFocus == "profile-instance:"+instance.ID {
					m.profileDefaultCursor = i + 1
				}
			}
		}
		if strings.HasPrefix(m.uiFocus, "nav:") {
			n, c := m.activateSessions(m.uiFocus)
			return n, c, true
		}
		if strings.HasPrefix(m.uiFocus, "profile:") {
			k = strings.TrimPrefix(m.uiFocus, "profile:")
		}
	}
	if k == "esc" {
		m.profileReview = nil
		m.uiFocus = ""
		if m.screen == "profile-list" {
			m.screen = "sessions"
			if m.profileReturn != "" {
				m.screen, m.scroll, m.uiFocus = m.profileReturn, m.profileReturnScroll, ""
				m.profileReturn = ""
			}
		} else {
			m.screen = "profile-list"
		}
		return m, nil, true
	}
	if m.screen == "profile-bind" {
		if (k == "confirm-bind" || k == "ctrl+y") && m.profileReview != nil {
			r := m.profileReview
			if err := m.service.ReviewSessionProfile(sessionApproval{Session: r.Session, Confirmed: true}, r.Environment.Binding); err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			m.profileReview = nil
			m.uiFocus = ""
			m.screen = "profile-list"
			m.notice = "Original environment binding recorded; conversation and connection unchanged."
			return m, m.load(), true
		}
		return m, nil, true
	}
	if m.screen == "profile-new" || m.screen == "profile-rename" {
		if k == "enter" {
			var err error
			if m.screen == "profile-new" {
				_, err = m.service.CreateProfile(profileCreateRequest{Name: m.input.Value(), Kind: profileIsolated})
			} else if m.profileCursor < len(m.profiles) {
				b, e := bindingOf(m.profiles[m.profileCursor], "codex")
				err = e
				if e == nil {
					err = m.service.RenameProfile(b, m.input.Value())
				}
			}
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			m.screen = "profile-list"
			m.notice = "Profile saved"
			return m, m.loadProfiles(), true
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd, true
	}
	if m.screen == "profile-default" {
		if k == "up" || k == "k" {
			m.uiFocus = ""
			m.profileDefaultCursor = max(0, m.profileDefaultCursor-1)
		}
		if k == "down" || k == "j" {
			m.uiFocus = ""
			m.profileDefaultCursor = min(len(m.profileInstances), m.profileDefaultCursor+1)
		}
		if k == "enter" && m.profileCursor < len(m.profiles) {
			b, err := bindingOf(m.profiles[m.profileCursor], "codex")
			id := ""
			if m.profileDefaultCursor > 0 {
				id = m.profileInstances[m.profileDefaultCursor-1].ID
			}
			if err == nil {
				err = m.service.SetProfileDefaultInstance(b, id)
			}
			if err != nil {
				m.notice = err.Error()
			} else {
				m.screen = "profile-list"
				m.notice = "Default saved; existing sessions unchanged."
				return m, m.loadProfiles(), true
			}
		}
		return m, nil, true
	}
	switch k {
	case "up", "k":
		m.profileCursor = max(0, m.profileCursor-1)
	case "down", "j":
		m.profileCursor = min(max(0, len(m.profiles)-1), m.profileCursor+1)
	case "n":
		m.screen = "profile-new"
		m.uiFocus = ""
		m.notice = ""
		m.input.SetValue("")
		m.input.Focus()
	case "r":
		if m.profileCursor < len(m.profiles) {
			m.screen = "profile-rename"
			m.uiFocus = ""
			m.input.SetValue(m.profiles[m.profileCursor].Name)
			m.input.Focus()
		}
	case "enter":
		if m.profileCursor < len(m.profiles) {
			b, err := bindingOf(m.profiles[m.profileCursor], "codex")
			if err == nil {
				err = m.service.SelectProfile(b)
			}
			if err != nil {
				m.notice = err.Error()
			} else {
				m.activeProfile = &b
				m.accountProfile = &b
				m.accountSnapshot = nil
				m.accountGeneration++
				m.generation++
				m.screen = "sessions"
				m.notice = "Profile selected: " + m.activeProfileLabel()
				if m.profileReturn != "" {
					m.screen, m.scroll, m.uiFocus = m.profileReturn, m.profileReturnScroll, ""
					m.profileReturn = ""
					return m, m.inspectAccounts(b), true
				}
			}
		}
	case "d":
		instances, err := m.service.Instances()
		if err != nil {
			m.notice = err.Error()
		} else {
			m.profileInstances = instances
			m.screen = "profile-default"
		}
	case "b":
		if s, ok := m.selected(); ok && !s.External && s.Profile == nil && m.profileCursor < len(m.profiles) {
			b, err := bindingOf(m.profiles[m.profileCursor], s.CLI)
			var e profileClientEnvironment
			if err == nil {
				e, err = m.service.ResolveProfileClient(profileClientRequest{Binding: b})
			}
			if err != nil {
				m.notice = err.Error()
			} else if e.Kind != profileExisting {
				m.notice = "Choose the original existing environment; moving a Conversation into an isolated Profile is unsupported."
			} else {
				m.profileReview = &profileReviewState{Session: s.session, Environment: e}
				m.screen = "profile-bind"
				m.uiFocus = "profile:esc"
				m.notice = ""
			}
		}
	}
	return m, nil, true
}

func (m harborModel) sessionNativeSummary(s session) string {
	if s.Profile == nil {
		return m.service.NativeCodexConfig()
	}
	e, err := m.service.ResolveProfileClient(profileClientRequest{Binding: *s.Profile})
	if err != nil {
		return err.Error()
	}
	return nativeCodexConfigSummary(e.Environment)
}

func (m harborModel) profileDisplayLabel(b *profileBinding) string {
	if b == nil {
		return "Existing environment · review required"
	}
	for _, p := range m.profiles {
		if p.ID == b.ProfileID && p.WorkspaceID == b.WorkspaceID {
			label := p.Name
			if p.Kind == profileExisting {
				label += " · shared existing environment"
			}
			return label
		}
	}
	return "Profile " + b.ProfileID
}
