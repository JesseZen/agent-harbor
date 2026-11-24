package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type appExtensionsLocalLoaded struct {
	generation uint64
	profile    profileBinding
	snapshot   extensionSnapshot
	err        error
}

type appCodexSkillsLoaded struct {
	generation uint64
	profile    profileBinding
	skills     []extensionItem
	err        error
}

type appSkillsSearchLoaded struct {
	generation uint64
	profile    profileBinding
	results    []skillCatalogItem
	err        error
}

type appExtensionToggled struct {
	profile profileBinding
	item    extensionItem
	enabled bool
	err     error
}

type appExtensionExecDone struct {
	action       string
	catalogIndex int
	err          error
}

type extensionCacheEntry struct {
	snapshot extensionSnapshot
	updated  time.Time
}

type extensionState struct {
	profile            *profileBinding
	generation         uint64
	loading            bool
	snapshot           *extensionSnapshot
	err                string
	filter             string
	searching          bool
	cursor             int
	detailScroll       int
	removeTarget       *extensionItem
	notice             string
	cache              map[profileBinding]extensionCacheEntry
	discoverQuery      string
	discoverSearching  bool
	discoverLoading    bool
	discoverGeneration uint64
	discoverResults    []skillCatalogItem
	discoverCursor     int
	discoverErr        string
	discoverSubmitted  bool
}

func (m harborModel) extensionService() profileExtensionService {
	if svc, ok := m.service.(profileExtensionService); ok {
		return svc
	}
	return nil
}

func (m *harborModel) invalidateExtensionsCache() {
	if m.ext.cache != nil && m.ext.profile != nil {
		delete(m.ext.cache, *m.ext.profile)
	}
	m.ext.loading = false
}

func (m *harborModel) beginExtensionsInspect() tea.Cmd {
	if m.ext.profile == nil && m.activeProfile != nil {
		b := *m.activeProfile
		m.ext.profile = &b
	}
	if m.ext.profile == nil || m.ext.loading {
		return nil
	}
	binding := *m.ext.profile
	if cached, ok := m.ext.cache[binding]; ok {
		snap := cached.snapshot
		m.ext.snapshot = &snap
		m.ext.err = ""
		flat := m.extensionRows()
		m.ext.cursor = max(0, min(m.ext.cursor, max(0, len(flat)-1)))
		if time.Since(cached.updated) < 30*time.Second {
			m.ext.loading = false
			return nil
		}
	}
	svc := m.extensionService()
	if svc == nil {
		m.ext.err = "Extensions service is unavailable."
		return nil
	}
	m.ext.loading = true
	m.ext.err = ""
	m.ext.generation++
	gen := m.ext.generation
	return func() tea.Msg {
		snap, err := svc.InspectExtensionsLocal(context.Background(), binding)
		return appExtensionsLocalLoaded{generation: gen, profile: binding, snapshot: snap, err: err}
	}
}

func (m *harborModel) applyExtensionSnapshot(binding profileBinding, snapshot extensionSnapshot) {
	selected, hasSelection := m.selectedExtension()
	m.ext.snapshot = &snapshot
	if m.ext.cache == nil {
		m.ext.cache = map[profileBinding]extensionCacheEntry{}
	}
	m.ext.cache[binding] = extensionCacheEntry{snapshot: snapshot, updated: time.Now()}
	rows := m.extensionRows()
	m.ext.cursor = min(m.ext.cursor, max(0, len(rows)-1))
	if hasSelection {
		for index, row := range rows {
			if selected.ID == row.ID {
				m.ext.cursor = index
				break
			}
		}
	}
}

func (m *harborModel) beginSkillsSearch() tea.Cmd {
	if m.ext.profile == nil {
		m.ext.discoverErr = "No active Profile."
		return nil
	}
	svc := m.extensionService()
	if svc == nil {
		m.ext.discoverErr = "Skills search is unavailable."
		return nil
	}
	query := strings.TrimSpace(m.ext.discoverQuery)
	m.ext.discoverLoading = true
	m.ext.discoverSubmitted = true
	m.ext.discoverResults = nil
	m.ext.discoverCursor = 0
	m.ext.discoverErr = ""
	m.ext.discoverGeneration++
	gen := m.ext.discoverGeneration
	binding := *m.ext.profile
	return func() tea.Msg {
		results, err := svc.SearchSkills(context.Background(), binding, query)
		return appSkillsSearchLoaded{generation: gen, profile: binding, results: results, err: err}
	}
}

func (m harborModel) extensionRows() []extensionItem {
	if m.ext.snapshot == nil {
		return nil
	}
	items := m.ext.snapshot.Skills
	if strings.TrimSpace(m.ext.filter) == "" {
		return items
	}
	texts := make([]string, len(items))
	for index, item := range items {
		texts[index] = item.Name + " " + item.Description + " " + item.Source
	}
	hits := searchMatches(m.ext.filter, texts)
	out := make([]extensionItem, 0, len(hits))
	for _, hit := range hits {
		out = append(out, items[hit.Index])
	}
	return out
}

func (m harborModel) selectedExtension() (extensionItem, bool) {
	flat := m.extensionRows()
	if len(flat) == 0 {
		return extensionItem{}, false
	}
	idx := max(0, min(m.ext.cursor, len(flat)-1))
	return flat[idx], true
}

func (m harborModel) extensionsTitle() string {
	if len(m.profiles) > 1 && m.ext.profile != nil {
		return "⚓ Harbor · " + m.profileShortLabel(m.ext.profile)
	}
	return "⚓ Harbor"
}

func (m harborModel) extensionsActions() []uiAction {
	switch m.screen {
	case "extensions-discover":
		if m.ext.discoverSearching {
			return []uiAction{
				{ID: "ext:discover-search", Key: "enter", Label: "Search", Primary: true},
				{ID: "ext:back", Key: "esc", Label: "Leave search"},
			}
		}
		actions := []uiAction{{ID: "ext:discover-focus", Key: "/", Label: "Search"}}
		if m.ext.discoverCursor < len(m.ext.discoverResults) && !m.ext.discoverResults[m.ext.discoverCursor].Installed {
			actions = append(actions, uiAction{ID: "ext:discover-install", Key: "a", Label: "Install", Primary: true})
		}
		return append(actions, uiAction{ID: "ext:back", Key: "esc", Label: "Back"})
	case "extensions-plugins":
		return []uiAction{
			{ID: "ext:marketplaces", Key: "m", Label: "Marketplaces"},
			{ID: "ext:back", Key: "esc", Label: "Back"},
		}
	case "extensions-marketplaces":
		return []uiAction{{ID: "ext:back", Key: "esc", Label: "Back"}}
	case "extensions-detail":
		out := []uiAction{}
		if it, ok := m.selectedExtension(); ok && it.Toggleable {
			label := "Enable"
			if it.Enabled {
				label = "Disable"
			}
			out = append(out, uiAction{ID: "ext:toggle", Key: "space", Label: label})
		}
		return append(out, uiAction{ID: "ext:back", Key: "esc", Label: "Back"})
	case "extensions-add":
		return []uiAction{
			{ID: "ext:add-confirm", Key: "enter", Label: "Continue", Primary: true},
			{ID: "ext:back", Key: "esc", Label: "Cancel"},
		}
	case "extensions-remove-confirm":
		return []uiAction{
			{ID: "ext:remove-confirm", Key: "y", Label: "Confirm remove"},
			{ID: "ext:back", Key: "esc", Label: "Cancel"},
		}
	}
	actions := []uiAction{
		{ID: "ext:discover", Key: "d", Label: "Discover Skills"},
		{ID: "ext:plugins", Key: "b", Label: "Plugins"},
	}
	if it, ok := m.selectedExtension(); ok {
		actions = append(actions, uiAction{ID: "ext:detail", Key: "enter", Label: "Details", Primary: true})
		if it.Toggleable {
			actions = append(actions, uiAction{ID: "ext:toggle", Key: "space", Label: "Toggle"})
		}
	}
	actions = append(actions,
		uiAction{ID: "ext:add", Key: "a", Label: "Add"},
		uiAction{ID: "ext:update", Key: "u", Label: "Update"},
	)
	if _, ok := m.selectedExtension(); ok {
		actions = append(actions, uiAction{ID: "ext:remove", Key: "x", Label: "Remove"})
	}
	actions = append(actions, uiAction{ID: "ext:refresh", Key: "r", Label: "Refresh"})
	return actions
}

func (m harborModel) extensionsPage() (uiPage, bool) {
	if m.screen != "extensions" && !strings.HasPrefix(m.screen, "extensions-") {
		return uiPage{}, false
	}
	switch m.screen {
	case "extensions-discover":
		return m.extensionsDiscoverPage(), true
	case "extensions-plugins", "extensions-marketplaces":
		p := newUIPage(m.width, m.height)
		if m.screen == "extensions-plugins" {
			p.title("Extensions / Plugins")
			p.emptyState("Plugin support pending.")
		} else {
			p.title("Extensions / Marketplaces")
			p.emptyState("Marketplace support pending.")
		}
		p.shortcuts(m.extensionsActions(), "")
		return p.finish(0), true
	case "extensions-detail":
		return m.extensionsDetailPage(), true
	case "extensions-add":
		return m.extensionsAddPage(), true
	case "extensions-remove-confirm":
		return m.extensionsRemoveConfirmPage(), true
	}

	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), m.extensionsTitle()))
	p.tabs("extensions", m.uiHover, m.uiFocus)
	p.search(m.ext.filter, "Filter installed Skills", m.ext.searching, m.input)

	footer := newUIPage(m.width, m.height)
	if !m.ext.searching {
		footer.shortcuts(m.extensionsActions(), "")
	}
	if m.ext.searching {
		footer.Hint = "Type filter · ↵ list · ⎋ clear/back"
	} else {
		footer.Hint = "↑↓/jk select · " + tabHint + " · / search · ? help"
	}
	footerH := len(footer.shortcutBar().Lines) + 1
	top := len(p.Lines)
	bodyH := max(1, m.height-top-footerH)

	leftW := p.Width
	split := p.Width >= 90
	if split {
		leftW = max(32, p.Width*48/100)
	}
	left := newUIPage(leftW, bodyH)

	type extEntry struct {
		isHeader    bool
		title, meta string
		index       int
		item        extensionItem
		top         int
		first       bool
	}
	skills := m.extensionRows()
	entries := []extEntry{}
	vTop, flatIdx := 0, 0
	skillsMeta := fmt.Sprintf("%d", len(skills))
	if m.ext.snapshot != nil && m.ext.snapshot.SkillsCLI == "unavailable" {
		skillsMeta = fmt.Sprintf("%d · CLI not found", len(skills))
	}
	entries = append(entries, extEntry{isHeader: true, title: "Installed Skills", meta: skillsMeta, top: vTop})
	vTop++
	for i, s := range skills {
		entries = append(entries, extEntry{index: flatIdx, item: s, top: vTop, first: i == 0})
		vTop += 2
		flatIdx++
	}

	if m.ext.loading && m.ext.snapshot == nil {
		left.emptyState("Loading local Skills…")
	} else if m.ext.err != "" && m.ext.snapshot == nil {
		left.line(paint(fg("warn"), cleanUI(m.ext.err)))
	} else {
		cursor := 0
		if flatIdx > 0 {
			cursor = max(0, min(m.ext.cursor, flatIdx-1))
		}
		itemTop, itemBottom, preferredTop := 0, 0, 0
		if flatIdx > 0 {
			for _, e := range entries {
				if !e.isHeader && e.index == cursor {
					itemTop, itemBottom = e.top, e.top+2
					preferredTop = itemTop
					if e.first && bodyH >= 3 {
						preferredTop = itemTop - 1
					}
					break
				}
			}
		}
		scrollTop := 0
		if itemBottom > bodyH {
			scrollTop = itemBottom - bodyH
		}
		if scrollTop > preferredTop && preferredTop+bodyH >= itemBottom {
			scrollTop = preferredTop
		}
		scrollTop = max(0, min(scrollTop, max(0, vTop-bodyH)))

		railReserve := 0
		if vTop > bodyH {
			railReserve = 1
		}
		lineW := max(1, leftW-2-railReserve)
		bandPage := newUIPage(max(3, leftW-railReserve), 10)
		for _, e := range entries {
			h := 2
			if e.isHeader {
				h = 1
			}
			if e.top+h <= scrollTop || e.top >= scrollTop+bodyH {
				continue
			}
			if e.isHeader {
				if e.top >= scrollTop && len(left.Lines) < bodyH {
					bandPage.Lines = nil
					bandPage.groupBand(e.title, e.meta)
					left.line(bandPage.Lines[0])
				}
				continue
			}
			if bodyH >= 2 && (e.top < scrollTop || len(left.Lines)+2 > bodyH) {
				continue
			}
			it := e.item
			id := fmt.Sprintf("ext-row:%d", e.index)
			row := m.rowStyle(id, e.index == cursor && !m.ext.searching, false)
			y := len(left.Lines)
			statusLabel, statusColor := "Enabled", "ok"
			if !it.Enabled {
				statusLabel, statusColor = "Disabled", "muted"
			}
			statusW := ansi.StringWidth(statusLabel)
			nameW := max(1, lineW-statusW-1)
			line1 := row.line(row.marker()+cleanUI(it.Name), "text", nameW) +
				paint(row.style("value"), " ") +
				paint(row.style(statusColor), statusLabel)
			sub := cleanUI(it.Description)
			if sub == "" {
				sub = cleanUI(it.Source)
			}
			line2 := row.line("  "+sub, "muted", lineW)
			rendered := 0
			if e.top >= scrollTop && len(left.Lines) < bodyH {
				left.line(line1)
				rendered++
			}
			if e.top+1 >= scrollTop && len(left.Lines) < bodyH {
				left.line(line2)
				rendered++
			}
			if rendered > 0 {
				left.hit(id, 1, y, lineW, rendered)
			}
		}
		if flatIdx == 0 && len(left.Lines) < bodyH {
			left.emptyState("No matching Skills.")
		}
		if split {
			left.scrollRail(0, min(bodyH, len(left.Lines)), vTop, scrollTop)
		}
	}

	for _, r := range left.Hits {
		p.hit(r.ID, r.X, r.Y+top, r.W, r.H)
	}

	right := newUIPage(max(1, p.Width-leftW-2), 10000)
	if split {
		if it, ok := m.selectedExtension(); ok {
			right.line(paint(fg("text").Bold(true), cleanUI(it.Name)))
			state := "Enabled"
			if !it.Enabled {
				state = "Disabled in this Profile"
			}
			right.field("", "", "Status", state, false, false)
			if it.Source != "" {
				right.field("", "", "Source", cleanUI(it.Source), false, false)
			}
			if it.Path != "" {
				right.field("", "", "Path", cleanUI(it.Path), false, false)
			}
			if it.Description != "" {
				right.line("")
				for _, r := range strings.Split(ansi.Wrap(cleanUI(it.Description), max(1, right.Width-2), ""), "\n") {
					right.line(paint(fg("value"), r))
				}
			}
		} else if m.ext.snapshot != nil {
			right.line(paint(fg("muted"), "Select a Skill to inspect its path and status."))
		}
	}
	rightStart := min(m.ext.detailScroll, max(0, len(right.Lines)-bodyH))
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(left.Lines) {
			l = left.Lines[i]
		}
		if split {
			l += strings.Repeat(" ", max(0, leftW-ansi.StringWidth(l)))
			if i+rightStart < len(right.Lines) {
				l += "  " + right.Lines[i+rightStart]
			}
		}
		p.line(l)
	}
	if !split && vTop > bodyH {
		cursor := 0
		if flatIdx > 0 {
			cursor = max(0, min(m.ext.cursor, flatIdx-1))
		}
		scrollTop := 0
		for _, e := range entries {
			if !e.isHeader && e.index == cursor {
				if e.top+2 > bodyH {
					scrollTop = e.top + 2 - bodyH
				}
				break
			}
		}
		p.scrollRail(top, min(bodyH, len(left.Lines)), vTop, scrollTop)
	}

	notice, noticeRole := m.ext.notice, "value"
	if notice == "" && m.ext.err != "" {
		notice, noticeRole = m.ext.err, "warn"
	} else if notice == "" && m.ext.snapshot != nil && len(m.ext.snapshot.Warnings) > 0 {
		notice, noticeRole = m.ext.snapshot.Warnings[0], "muted"
	}
	p.line(paint(fg(noticeRole), ansi.Truncate(cleanUI(notice), max(1, p.Width-2), "…")))
	p.Actions = footer.Actions
	p.Hint = footer.Hint
	return p.finish(0), true
}

func (m harborModel) extensionsDetailPage() uiPage {
	it, ok := m.selectedExtension()
	if !ok {
		return secondaryDocument(m.width, m.height, "Extension details", "No extension selected.", m.extensionsActions(), "", 0)
	}
	lines := []string{
		cleanUI(it.Name),
		"",
		kv("Status", map[bool]string{true: "Enabled", false: "Disabled"}[it.Enabled]),
		kv("Source", orUnknown(it.Source)),
		kv("Scope", orUnknown(it.Scope)),
		kv("Path", orUnknown(it.Path)),
	}
	if len(it.Clients) > 0 {
		lines = append(lines, kv("Clients", strings.Join(it.Clients, ", ")))
	}
	if it.Description != "" {
		lines = append(lines, "", it.Description)
	}
	if m.ext.snapshot != nil {
		lines = append(lines, "", kv("Profile", m.ext.snapshot.ProfileName), kv("Codex home", m.ext.snapshot.CodexHome), kv("skills CLI", m.ext.snapshot.SkillsCLI))
	}
	return secondaryDocument(m.width, m.height, "Extension details", strings.Join(lines, "\n"), m.extensionsActions(), "", m.scroll)
}

func (m harborModel) extensionsDiscoverPage() uiPage {
	p := newUIPage(m.width, m.height)
	p.title("Extensions / Discover Skills")
	p.search(m.ext.discoverQuery, "Search skills.sh", m.ext.discoverSearching, m.input)
	p.shortcuts(m.extensionsActions(), "")
	if !m.ext.discoverSearching && len(m.ext.discoverResults) > 0 {
		p.Hint = "↑↓/jk select"
	}
	start := len(p.Lines)
	if m.ext.discoverLoading {
		p.emptyState("Searching Skills catalog…")
	} else if m.ext.discoverErr != "" {
		p.line(paint(fg("warn"), cleanUI(m.ext.discoverErr)))
	} else if len(m.ext.discoverResults) == 0 {
		if m.ext.discoverSubmitted {
			p.emptyState("No matching Skills.")
		} else {
			p.emptyState("Enter at least two characters to search.")
		}
	}
	if m.ext.notice != "" {
		p.line(paint(fg("value"), cleanUI(m.ext.notice)))
		start++
	}
	for index, item := range m.ext.discoverResults {
		id := fmt.Sprintf("ext-discover:%d", index)
		row := m.rowStyle(id, index == m.ext.discoverCursor && !m.ext.discoverSearching, false)
		meta := cleanUI(item.Source)
		if item.Installed {
			meta = "Installed · " + meta
		} else if item.Installs > 0 {
			meta += fmt.Sprintf(" · %d installs", item.Installs)
		}
		p.listRow(id, "", cleanUI(item.Name), meta, row)
	}
	scroll := 0
	if !m.ext.discoverSearching && len(m.ext.discoverResults) > 0 {
		bodyHeight := max(1, m.height-len(p.shortcutBar().Lines))
		scroll = max(0, start+m.ext.discoverCursor-bodyHeight+1)
	}
	return p.finish(scroll)
}

func (m harborModel) extensionsAddPage() uiPage {
	body := textEntry(m.input, m.width, "Search term, owner/repo, Git URL or path")
	if m.ext.notice != "" {
		body += "\n\n" + m.ext.notice
	}
	return secondaryDocument(m.width, m.height, "Add Skill", body, m.extensionsActions(), "", 0)
}

func (m harborModel) extensionsRemoveConfirmPage() uiPage {
	name, scopeNote := "unknown", ""
	if t := m.ext.removeTarget; t != nil {
		name = t.Name
		scopeNote = "Removing a global skill affects shared ~/.agents/skills across clients. Press Space on the list instead if you only want to disable it in this Profile."
	}
	body := fmt.Sprintf("Remove %s?\n\n%s", cleanUI(name), scopeNote)
	return secondaryDocument(m.width, m.height, "Remove Skill", body, m.extensionsActions(), "", 0)
}

func (m harborModel) runExtensionExec(action, target string) (tea.Model, tea.Cmd) {
	svc := m.extensionService()
	if svc == nil || m.ext.profile == nil {
		m.ext.notice = "Extensions service or Profile is unavailable."
		return m, nil
	}
	cmd, err := svc.ExtensionExecCommand(context.Background(), *m.ext.profile, action, target)
	if err != nil {
		m.ext.notice = err.Error()
		return m, nil
	}
	index := m.ext.discoverCursor
	if action != "install-skill" {
		m.screen = "extensions"
	}
	m.input.Blur()
	m.ext.notice = ""
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return appExtensionExecDone{action: action, catalogIndex: index, err: err}
	})
}

func (m harborModel) extensionsPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case appExtensionsLocalLoaded:
		if msg.generation != m.ext.generation || m.ext.profile == nil || msg.profile != *m.ext.profile {
			return m, nil, true
		}
		m.ext.loading = false
		if msg.err != nil {
			m.ext.err = msg.err.Error()
			return m, nil, true
		}
		m.applyExtensionSnapshot(msg.profile, msg.snapshot)
		svc := m.extensionService()
		return m, func() tea.Msg {
			skills, err := svc.InspectCodexSkills(context.Background(), msg.profile)
			return appCodexSkillsLoaded{generation: msg.generation, profile: msg.profile, skills: skills, err: err}
		}, true
	case appCodexSkillsLoaded:
		if msg.generation != m.ext.generation || m.ext.profile == nil || msg.profile != *m.ext.profile || m.ext.snapshot == nil {
			return m, nil, true
		}
		snap := *m.ext.snapshot
		if msg.err == nil {
			snap.Skills = mergeSkillItems(snap.Skills, msg.skills)
		} else {
			snap.Warnings = append(append([]string(nil), snap.Warnings...), "Codex skills could not be loaded. Showing local Skills.")
		}
		m.applyExtensionSnapshot(msg.profile, snap)
		return m, nil, true
	case appSkillsSearchLoaded:
		if msg.generation != m.ext.discoverGeneration || m.ext.profile == nil || msg.profile != *m.ext.profile {
			return m, nil, true
		}
		m.ext.discoverLoading = false
		if msg.err != nil {
			m.ext.discoverErr = msg.err.Error()
		} else {
			m.ext.discoverResults = msg.results
		}
		return m, nil, true
	case appExtensionToggled:
		if m.ext.profile == nil || msg.profile != *m.ext.profile {
			return m, nil, true
		}
		if msg.err != nil {
			m.ext.notice = msg.err.Error()
			return m, nil, true
		}
		state := "disabled"
		if msg.enabled {
			state = "enabled"
		}
		m.ext.notice = fmt.Sprintf("%s %s for this Profile.", msg.item.Name, state)
		m.invalidateExtensionsCache()
		return m, m.beginExtensionsInspect(), true
	case appExtensionExecDone:
		if msg.err != nil {
			m.ext.notice = fmt.Sprintf("Extension %s finished with error: %v", msg.action, msg.err)
		} else {
			m.ext.notice = fmt.Sprintf("Extension %s completed.", msg.action)
		}
		m.invalidateExtensionsCache()
		if msg.action == "install-skill" && msg.err == nil && msg.catalogIndex < len(m.ext.discoverResults) {
			m.ext.discoverResults = append([]skillCatalogItem(nil), m.ext.discoverResults...)
			m.ext.discoverResults[msg.catalogIndex].Installed = true
		}
		return m, tea.Batch(tea.EnableMouseAllMotion, m.beginExtensionsInspect()), true
	}

	if m.screen != "extensions" && !strings.HasPrefix(m.screen, "extensions-") {
		return m, nil, false
	}
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		return m, nil, false
	}

	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Button == tea.MouseButtonWheelDown || mouse.Button == tea.MouseButtonWheelUp {
			delta := 1
			if mouse.Button == tea.MouseButtonWheelUp {
				delta = -1
			}
			if m.screen == "extensions" {
				flat := m.extensionRows()
				if len(flat) > 0 {
					m.ext.cursor = max(0, min(len(flat)-1, m.ext.cursor+delta))
				}
			} else if m.screen == "extensions-discover" {
				if len(m.ext.discoverResults) > 0 {
					m.ext.discoverCursor = max(0, min(len(m.ext.discoverResults)-1, m.ext.discoverCursor+delta))
				}
			} else {
				m.scroll = max(0, m.scroll+delta)
			}
			return m, nil, true
		}
		p, _ := m.extensionsPage()
		target := p.target(mouse.X, mouse.Y)
		if mouse.Action == tea.MouseActionMotion {
			m.uiHover = strings.TrimPrefix(target, "nav:")
			return m, nil, true
		}
		if mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
			return m, nil, true
		}
		if strings.HasPrefix(target, "nav:") {
			m.ext.searching = false
			next, cmd := m.activateSessions(target)
			return next, cmd, true
		}
		if target == "search" && m.screen == "extensions" {
			m.ext.searching = true
			m.input.SetValue(m.ext.filter)
			m.input.CursorEnd()
			return m, m.input.Focus(), true
		}
		if target == "search" && m.screen == "extensions-discover" {
			return m.extensionsActivate("ext:discover-focus")
		}
		if idxStr, ok := strings.CutPrefix(target, "ext-row:"); ok {
			var idx int
			if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil {
				if m.ext.cursor == idx && !m.ext.searching {
					return m.extensionsActivate("ext:detail")
				}
				m.ext.cursor = idx
				m.ext.searching = false
				m.input.Blur()
				m.ext.detailScroll = 0
			}
			return m, nil, true
		}
		if idxStr, ok := strings.CutPrefix(target, "ext-discover:"); ok {
			var idx int
			if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(m.ext.discoverResults) {
				m.ext.discoverCursor = idx
				m.ext.discoverSearching = false
				m.input.Blur()
			}
			return m, nil, true
		}
		if strings.HasPrefix(target, "ext:") {
			return m.extensionsActivate(target)
		}
		return m, nil, true
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	k := key.String()

	if m.screen == "extensions-add" {
		switch k {
		case "esc":
			m.screen = "extensions"
			m.input.Blur()
			return m, nil, true
		case "enter":
			return m.extensionsActivate("ext:add-confirm")
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd, true
		}
	}

	if m.screen == "extensions-discover" {
		if m.ext.discoverSearching {
			switch k {
			case "esc":
				m.ext.discoverSearching = false
				m.input.Blur()
			case "enter":
				return m.extensionsActivate("ext:discover-search")
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.ext.discoverQuery = m.input.Value()
				return m, cmd, true
			}
			return m, nil, true
		}
		switch k {
		case "esc", "q":
			m.screen = "extensions"
		case "/":
			return m.extensionsActivate("ext:discover-focus")
		case "down", "j":
			m.ext.discoverCursor = min(max(0, len(m.ext.discoverResults)-1), m.ext.discoverCursor+1)
		case "up", "k":
			m.ext.discoverCursor = max(0, m.ext.discoverCursor-1)
		case "a":
			return m.extensionsActivate("ext:discover-install")
		}
		return m, nil, true
	}

	if m.screen == "extensions" && m.ext.searching {
		switch k {
		case "esc":
			if m.ext.filter != "" {
				m.ext.filter = ""
				m.input.SetValue("")
				m.ext.cursor = 0
			} else {
				m.ext.searching = false
				m.input.Blur()
			}
			return m, nil, true
		case "enter":
			m.ext.searching = false
			m.input.Blur()
			return m, nil, true
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.ext.filter = m.input.Value()
			m.ext.cursor = 0
			return m, cmd, true
		}
	}

	switch m.screen {
	case "extensions-plugins", "extensions-marketplaces":
		switch k {
		case "esc", "q":
			return m.extensionsActivate("ext:back")
		case "m":
			if m.screen == "extensions-plugins" {
				return m.extensionsActivate("ext:marketplaces")
			}
		}
		return m, nil, true
	case "extensions-detail":
		switch k {
		case "esc", "q":
			m.screen = "extensions"
			m.scroll = 0
		case " ", "space":
			return m.extensionsActivate("ext:toggle")
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		}
		return m, nil, true
	case "extensions-remove-confirm":
		switch k {
		case "y":
			return m.extensionsActivate("ext:remove-confirm")
		case "esc", "enter", "q":
			m.ext.removeTarget = nil
			m.screen = "extensions"
		}
		return m, nil, true
	}

	// Main "extensions" screen keys.
	switch k {
	case "?":
		return m, nil, false
	case "esc":
		m.screen = "sessions"
		return m, nil, true
	case "q":
		if m.cancelRefresh != nil {
			m.cancelRefresh()
		}
		return m, tea.Quit, true
	case "/":
		m.ext.searching = true
		m.input.SetValue(m.ext.filter)
		m.input.CursorEnd()
		return m, m.input.Focus(), true
	case "down", "j":
		if flat := m.extensionRows(); len(flat) > 0 {
			m.ext.cursor = min(len(flat)-1, m.ext.cursor+1)
			m.ext.detailScroll = 0
		}
	case "up", "k":
		m.ext.cursor = max(0, m.ext.cursor-1)
		m.ext.detailScroll = 0
	case "enter", "i":
		return m.extensionsActivate("ext:detail")
	case " ", "space", "t":
		return m.extensionsActivate("ext:toggle")
	case "a":
		return m.extensionsActivate("ext:add")
	case "u":
		return m.extensionsActivate("ext:update")
	case "x":
		return m.extensionsActivate("ext:remove")
	case "d":
		return m.extensionsActivate("ext:discover")
	case "b":
		return m.extensionsActivate("ext:plugins")
	case "r":
		return m.extensionsActivate("ext:refresh")
	case "p":
		m.profileReturn, m.profileReturnScroll = "extensions", m.scroll
		m.screen = "profile-list"
		m.scroll = 0
		return m, m.loadProfiles(), true
	}
	return m, nil, true
}

func (m harborModel) extensionsActivate(id string) (tea.Model, tea.Cmd, bool) {
	switch id {
	case "ext:discover":
		m.screen = "extensions-discover"
		m.ext.notice = ""
		return m.extensionsActivate("ext:discover-focus")
	case "ext:discover-focus":
		m.ext.discoverSearching = true
		m.input.SetValue(m.ext.discoverQuery)
		m.input.CursorEnd()
		return m, m.input.Focus(), true
	case "ext:discover-search":
		m.ext.discoverSearching = false
		m.ext.notice = ""
		m.input.Blur()
		return m, m.beginSkillsSearch(), true
	case "ext:discover-install":
		if m.ext.discoverSearching || m.ext.discoverCursor >= len(m.ext.discoverResults) {
			return m, nil, true
		}
		item := m.ext.discoverResults[m.ext.discoverCursor]
		if item.Installed {
			return m, nil, true
		}
		next, cmd := m.runExtensionExec("install-skill", item.Source+"\x00"+item.Skill)
		return next, cmd, true
	case "ext:back":
		m.ext.removeTarget = nil
		m.input.Blur()
		m.scroll = 0
		switch {
		case m.screen == "extensions-discover" && m.ext.discoverSearching:
			m.ext.discoverSearching = false
		case m.screen == "extensions-marketplaces":
			m.screen = "extensions-plugins"
		default:
			m.screen = "extensions"
		}
		return m, nil, true
	case "ext:detail":
		if _, ok := m.selectedExtension(); ok {
			m.screen = "extensions-detail"
			m.scroll = 0
		}
		return m, nil, true
	case "ext:refresh":
		m.ext.notice = ""
		m.invalidateExtensionsCache()
		return m, m.beginExtensionsInspect(), true
	case "ext:plugins":
		m.screen = "extensions-plugins"
		return m, nil, true
	case "ext:marketplaces":
		m.screen = "extensions-marketplaces"
		return m, nil, true
	case "ext:toggle":
		it, ok := m.selectedExtension()
		if !ok || m.ext.profile == nil {
			return m, nil, true
		}
		if !it.Toggleable {
			m.ext.notice = "This item does not support enable/disable toggling."
			return m, nil, true
		}
		svc := m.extensionService()
		if svc == nil {
			return m, nil, true
		}
		nextEnabled := !it.Enabled
		binding := *m.ext.profile
		return m, func() tea.Msg {
			err := svc.ToggleExtension(context.Background(), binding, it, nextEnabled)
			return appExtensionToggled{profile: binding, item: it, enabled: nextEnabled, err: err}
		}, true
	case "ext:add":
		m.ext.notice = ""
		m.input.SetValue("")
		m.screen = "extensions-add"
		return m, m.input.Focus(), true
	case "ext:add-confirm":
		value := strings.TrimSpace(m.input.Value())
		if !skillInputIsSource(value) {
			m.ext.discoverQuery = value
			if value == "" {
				return m.extensionsActivate("ext:discover")
			}
			m.screen = "extensions-discover"
			return m.extensionsActivate("ext:discover-search")
		}
		next, cmd := m.runExtensionExec("add", value)
		return next, cmd, true
	case "ext:update":
		target := ""
		if item, ok := m.selectedExtension(); ok {
			target = item.Name
		}
		next, cmd := m.runExtensionExec("update", target)
		return next, cmd, true
	case "ext:remove":
		if it, ok := m.selectedExtension(); ok {
			copy := it
			m.ext.removeTarget = &copy
			m.screen = "extensions-remove-confirm"
		}
		return m, nil, true
	case "ext:remove-confirm":
		if m.ext.removeTarget == nil {
			m.screen = "extensions"
			return m, nil, true
		}
		name := m.ext.removeTarget.Name
		m.ext.removeTarget = nil
		m.screen = "extensions"
		next, cmd := m.runExtensionExec("remove", name)
		return next, cmd, true
	}
	return m, nil, true
}
