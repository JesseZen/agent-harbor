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
	p.tabs("profiles", m.uiHover, m.uiFocus)
	p.field("", "", "Current", cleanUI(m.activeProfileLabel()), false, false)
	p.line(paint(fg("section"), "Profiles"))
	actions := []uiAction{{ID: "profile:enter", Label: "Select", Key: "enter", Primary: true}, {ID: "profile:n", Label: "Create", Key: "n"}, {ID: "profile:r", Label: "Rename", Key: "r"}, {ID: "profile:d", Label: "Magpie default", Key: "d"}, {ID: "profile:esc", Label: "Back", Key: "esc"}}
	if m.profileCursor < len(m.profiles) {
		actions = append(actions, uiAction{ID: "profile:c", Label: "Codex defaults", Key: "c"})
	}
	if s, ok := m.selected(); ok && !s.External && s.Profile == nil {
		actions = append(actions, uiAction{ID: "profile:b", Label: "Bind reviewed original environment", Key: "b"})
	}
	p.shortcuts(actions, m.uiFocus)
	p.Hint = "↑↓/jk choose"
	p.Hint += " · " + tabHint
	listTop := len(p.Lines)
	count := max(1, m.height-len(p.Lines)-len(p.shortcutBar().Lines))
	if m.notice != "" {
		count = max(1, count-1)
	}
	start := m.listStart(m.profileCursor, count)
	p.ListStart = start
	for i := start; i < min(len(m.profiles), start+count); i++ {
		row := m.profiles[i]
		label := row.Name + " · " + string(row.Kind)
		id := "profile-row:" + row.ID
		selected := m.activeProfile != nil && row.ID == m.activeProfile.ProfileID
		p.listRow(id, selectorMark(selected), cleanUI(label), "", m.rowStyle(id, i == m.profileCursor && !strings.HasPrefix(m.uiFocus, "nav:"), selected))
	}
	p.scrollRail(listTop, len(p.Lines)-listTop, len(m.profiles), start)

	if m.screen == "profile-default" {
		p = newUIPage(m.width, m.height)
		p.line(paint(fg("brand"), "Harbor › Profile Magpie default"))
		p.line("For future sessions only; existing sessions keep their instance.")
		count := max(1, m.height-7)
		start := m.listStart(m.profileDefaultCursor, count)
		p.ListStart = start
		listTop := len(p.Lines)
		for i := start; i < min(len(m.profileInstances)+1, start+count); i++ {
			id, label := "", "Shared workspace default"
			if i > 0 {
				id, label = m.profileInstances[i-1].ID, m.profileInstances[i-1].Name+" · "+m.profileInstances[i-1].ID
			}
			selected := m.profileCursor < len(m.profiles) && id == m.profiles[m.profileCursor].DefaultInstanceID
			target := "profile-instance:" + id
			p.listRow(target, selectorMark(selected), cleanUI(label), "", m.rowStyle(target, m.profileDefaultCursor == i, selected))
		}
		p.scrollRail(listTop, len(p.Lines)-listTop, len(m.profileInstances)+1, start)
		p.shortcuts([]uiAction{{ID: "profile:enter", Label: "Save default", Key: "enter"}, {ID: "profile:esc", Label: "Cancel", Key: "esc"}}, "")
	}
	if m.notice != "" {
		p.line(cleanUI(m.notice))
	}

	p.Hint = "↑↓/jk choose"
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
				if m.screen == "accounts" {
					cmd := m.beginAccountInspect()
					return m, cmd, true
				}
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
			m.uiFocus = ""
			for i, p := range m.profiles {
				if p.ID == strings.TrimPrefix(id, "profile-row:") {
					if m.profileCursor == i {
						return m.profilePageInput(uiKey("enter"))
					}
					m.pinList(page.ListStart)
					m.profileCursor = i
					break
				}
			}
			return m, nil, true
		}
		if strings.HasPrefix(id, "profile-instance:") {
			if m.profileCursor >= len(m.profiles) {
				return m, nil, true
			}
			index := 0
			for i, instance := range m.profileInstances {
				if id == "profile-instance:"+instance.ID {
					index = i + 1
					break
				}
			}
			if m.profileDefaultCursor == index {
				return m.profilePageInput(uiKey("enter"))
			}
			m.pinList(page.ListStart)
			m.profileDefaultCursor = index
			return m, nil, true
		}
		k = strings.TrimPrefix(id, "profile:")
		iskey = true
	}
	if !iskey {
		return m, nil, false
	}
	if k == "esc" || (m.screen == "profile-bind" && k == "enter") {
		m.profileReview = nil
		m.uiFocus = ""
		if m.screen == "profile-list" {
			m.screen = "sessions"
			if m.profileReturn == "settings" {
				m.profileReturn = ""
				settings := m.openSettingsModel()
				m.settings = &settings
				return m, nil, true
			}
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
			r := *m.profileReview
			return m.serviceTask(func() error {
				return m.service.ReviewSessionProfile(sessionApproval{Session: r.Session, Confirmed: true}, r.Environment.Binding)
			}, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
					return nil
				}
				n.profileReview = nil
				n.uiFocus = ""
				n.screen = "profile-list"
				n.notice = "Original environment binding recorded; conversation and connection unchanged."
				return n.load()
			})
		}
		return m, nil, true
	}
	if m.screen == "profile-new" || m.screen == "profile-rename" {
		if k == "enter" {
			name, screen := m.input.Value(), m.screen
			var b profileBinding
			if screen == "profile-rename" {
				if m.profileCursor >= len(m.profiles) {
					return m, nil, true
				}
				var err error
				b, err = bindingOf(m.profiles[m.profileCursor], "codex")
				if err != nil {
					m.notice = err.Error()
					return m, nil, true
				}
			}
			return m.serviceTask(func() error {
				if screen == "profile-new" {
					_, err := m.service.CreateProfile(profileCreateRequest{Name: name, Kind: profileIsolated})
					return err
				}
				return m.service.RenameProfile(b, name)
			}, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
					return nil
				}
				n.screen = "profile-list"
				n.notice = "Profile saved"
				return n.loadProfiles()
			})
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
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			return m.saveProfileDefault(b, id)
		}
		return m, nil, true
	}
	switch k {
	case "up", "k":
		m.uiFocus = ""
		m.profileCursor = max(0, m.profileCursor-1)
	case "down", "j":
		m.uiFocus = ""
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
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			return m.serviceTask(func() error { return m.service.SelectProfile(b) }, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
					return nil
				}
				n.activeProfile = &b
				n.accountProfile = &b
				n.accountSnapshot = nil
				n.accountRow = ""
				n.accountGeneration++
				n.ext.profile = &b
				n.ext.snapshot = nil
				n.ext.notice = ""
				n.ext.cursor = 0
				n.generation++
				n.screen = "sessions"
				n.uiFocus = "list"
				n.notice = "Profile selected: " + n.activeProfileLabel()
				if n.profileReturn == "settings" {
					n.profileReturn = ""
					settings := n.openSettingsModel()
					n.settings = &settings
					return nil
				}
				if n.profileReturn == "extensions" {
					n.screen, n.scroll, n.uiFocus = "extensions", n.profileReturnScroll, ""
					n.profileReturn = ""
					return n.beginExtensionsInspect()
				}
				if n.profileReturn != "" {
					n.screen, n.scroll, n.uiFocus = n.profileReturn, n.profileReturnScroll, ""
					n.profileReturn = ""
					return n.beginAccountInspect()
				}
				return n.load()
			})
		}
	case "d":
		var instances []magpieInstance
		return m.serviceTask(func() error { var err error; instances, err = m.service.Instances(); return err }, func(n *harborModel, err error) tea.Cmd {
			if err != nil {
				n.notice = err.Error()
				return nil
			}
			n.profileInstances = instances
			n.uiFocus = ""
			n.profileDefaultCursor = 0
			if n.profileCursor < len(n.profiles) {
				for i, instance := range instances {
					if instance.ID == n.profiles[n.profileCursor].DefaultInstanceID {
						n.profileDefaultCursor = i + 1
					}
				}
			}
			n.screen = "profile-default"
			return nil
		})
	case "c":
		if m.profileCursor < len(m.profiles) {
			binding, err := bindingOf(m.profiles[m.profileCursor], "codex")
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			return m.loadCodexDefaults(binding, "", false)
		}
	case "b":
		if s, ok := m.selected(); ok && !s.External && s.Profile == nil && m.profileCursor < len(m.profiles) {
			b, err := bindingOf(m.profiles[m.profileCursor], s.CLI)
			if err != nil {
				m.notice = err.Error()
				return m, nil, true
			}
			var e profileClientEnvironment
			return m.serviceTask(func() error {
				var err error
				e, err = m.service.ResolveProfileClient(profileClientRequest{Binding: b})
				return err
			}, func(n *harborModel, err error) tea.Cmd {
				if err != nil {
					n.notice = err.Error()
				} else if e.Kind != profileExisting {
					n.notice = "Choose the original existing environment; moving a Conversation into an isolated Profile is unsupported."
				} else {
					n.profileReview = &profileReviewState{Session: s.session, Environment: e}
					n.screen = "profile-bind"
					n.uiFocus = "profile:esc"
					n.notice = ""
				}
				return nil
			})
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

func (m harborModel) profileShortLabel(b *profileBinding) string {
	if b == nil {
		return "Unbound"
	}
	for _, p := range m.profiles {
		if p.ID == b.ProfileID && p.WorkspaceID == b.WorkspaceID {
			return p.Name
		}
	}
	return "Profile " + b.ProfileID
}

func (m harborModel) saveProfileDefault(b profileBinding, id string) (tea.Model, tea.Cmd, bool) {
	return m.serviceTask(func() error { return m.service.SetProfileDefaultInstance(b, id) }, func(n *harborModel, err error) tea.Cmd {
		if err != nil {
			n.notice = err.Error()
			return nil
		}
		n.screen = "profile-list"
		n.notice = "Default saved; existing sessions keep their recorded instance."
		return n.loadProfiles()
	})
}
