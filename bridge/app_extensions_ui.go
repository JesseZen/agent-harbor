package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type appExtensionsLoaded struct {
	generation uint64
	profile    profileBinding
	snapshot   extensionSnapshot
	err        error
}

type appExtensionToggled struct {
	generation uint64
	profile    profileBinding
	item       extensionItem
	enabled    bool
	err        error
}

type appExtensionExecDone struct {
	action string
	err    error
}

type appPluginSyncPlanned struct {
	generation uint64
	plan       pluginSyncPlan
	err        error
}

type appPluginSyncDone struct {
	generation uint64
	result     pluginSyncResult
	err        error
}

type extensionState struct {
	profile      *profileBinding
	generation   uint64
	loading      bool
	snapshot     *extensionSnapshot
	err          string
	filter       string
	searching    bool
	cursor       int
	listScroll   int
	detailScroll int
	addKind      extensionKind
	removeTarget *extensionItem
	syncCursor   int
	syncSources  []profile
	syncPlan     *pluginSyncPlan
	syncResult   *pluginSyncResult
	syncBusy     bool
	notice       string
}

func (m harborModel) extensionService() profileExtensionService {
	if svc, ok := m.service.(profileExtensionService); ok {
		return svc
	}
	return nil
}

func (m *harborModel) beginExtensionsInspect() tea.Cmd {
	if m.ext.profile == nil && m.activeProfile != nil {
		b := *m.activeProfile
		m.ext.profile = &b
	}
	if m.ext.profile == nil {
		return nil
	}
	svc := m.extensionService()
	if svc == nil {
		m.ext.err = "Extensions service is unavailable."
		return nil
	}
	m.ext.generation++
	m.ext.loading = true
	m.ext.err = ""
	gen := m.ext.generation
	binding := *m.ext.profile
	return func() tea.Msg {
		snap, err := svc.InspectExtensions(context.Background(), binding)
		return appExtensionsLoaded{generation: gen, profile: binding, snapshot: snap, err: err}
	}
}

func (m harborModel) extensionRows() (skills, plugins []extensionItem) {
	if m.ext.snapshot == nil {
		return nil, nil
	}
	match := func(items []extensionItem) []extensionItem {
		if strings.TrimSpace(m.ext.filter) == "" {
			return append([]extensionItem(nil), items...)
		}
		texts := make([]string, len(items))
		for i, it := range items {
			texts[i] = it.Name + " " + it.Description + " " + it.Source + " " + it.Marketplace
		}
		hits := searchMatches(m.ext.filter, texts)
		out := make([]extensionItem, 0, len(hits))
		for _, h := range hits {
			out = append(out, items[h.Index])
		}
		return out
	}
	return match(m.ext.snapshot.Skills), match(m.ext.snapshot.Plugins)
}

func (m harborModel) extensionFlat() []extensionItem {
	skills, plugins := m.extensionRows()
	out := make([]extensionItem, 0, len(skills)+len(plugins))
	out = append(out, skills...)
	out = append(out, plugins...)
	return out
}

func (m harborModel) selectedExtension() (extensionItem, bool) {
	flat := m.extensionFlat()
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
			{ID: "ext:add-confirm", Key: "enter", Label: "Run CLI", Primary: true},
			{ID: "ext:add-kind", Key: "ctrl+t", Label: "Switch Skill/Plugin"},
			{ID: "ext:back", Key: "esc", Label: "Cancel"},
		}
	case "extensions-remove-confirm":
		return []uiAction{
			{ID: "ext:remove-confirm", Key: "y", Label: "Confirm remove"},
			{ID: "ext:back", Key: "esc", Label: "Cancel"},
		}
	case "extensions-sync-pick":
		return []uiAction{
			{ID: "ext:sync-choose", Key: "enter", Label: "Inspect diff", Primary: true},
			{ID: "ext:back", Key: "esc", Label: "Cancel"},
		}
	case "extensions-sync-confirm":
		actions := []uiAction{}
		if m.ext.syncPlan != nil && (len(m.ext.syncPlan.MarketplacesToAdd) > 0 || len(m.ext.syncPlan.PluginsToAdd) > 0) {
			actions = append(actions, uiAction{ID: "ext:sync-run", Key: "enter", Label: "Sync plugins", Primary: true})
		}
		return append(actions, uiAction{ID: "ext:back", Key: "esc", Label: "Cancel"})
	case "extensions-sync-result":
		return []uiAction{{ID: "ext:back", Key: "esc", Label: "Back to Extensions"}}
	}
	actions := []uiAction{}
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
	if len(m.profiles) > 1 {
		actions = append(actions, uiAction{ID: "ext:sync", Key: "y", Label: "Sync plugins"})
	}
	actions = append(actions, uiAction{ID: "ext:refresh", Key: "r", Label: "Refresh"})
	return actions
}

func (m harborModel) extensionsPage() (uiPage, bool) {
	if m.screen != "extensions" && !strings.HasPrefix(m.screen, "extensions-") {
		return uiPage{}, false
	}
	switch m.screen {
	case "extensions-detail":
		return m.extensionsDetailPage(), true
	case "extensions-add":
		return m.extensionsAddPage(), true
	case "extensions-remove-confirm":
		return m.extensionsRemoveConfirmPage(), true
	case "extensions-sync-pick":
		return m.extensionsSyncPickPage(), true
	case "extensions-sync-confirm":
		return m.extensionsSyncConfirmPage(), true
	case "extensions-sync-result":
		return m.extensionsSyncResultPage(), true
	}

	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), m.extensionsTitle()))
	p.tabs("extensions", m.uiHover, m.uiFocus)
	p.search(m.ext.filter, "Filter skills and plugins", m.ext.searching, m.input)

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
	skills, plugins := m.extensionRows()
	entries := []extEntry{}
	vTop, flatIdx := 0, 0
	skillsMeta := fmt.Sprintf("%d", len(skills))
	if m.ext.snapshot != nil && m.ext.snapshot.SkillsCLI == "unavailable" {
		skillsMeta = fmt.Sprintf("%d · CLI not found", len(skills))
	}
	entries = append(entries, extEntry{isHeader: true, title: "Skills · shared (~/.agents/skills)", meta: skillsMeta, top: vTop})
	vTop++
	for i, s := range skills {
		entries = append(entries, extEntry{index: flatIdx, item: s, top: vTop, first: i == 0})
		vTop += 2
		flatIdx++
	}
	entries = append(entries, extEntry{isHeader: true, title: "Plugins · this Profile", meta: fmt.Sprintf("%d", len(plugins)), top: vTop})
	vTop++
	for i, pl := range plugins {
		entries = append(entries, extEntry{index: flatIdx, item: pl, top: vTop, first: i == 0})
		vTop += 2
		flatIdx++
	}

	if m.ext.loading && m.ext.snapshot == nil {
		left.emptyState("Loading skills and plugins…")
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
			if it.Kind == extPlugin {
				statusLabel = "Installed"
			}
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
			left.emptyState("No skills or plugins found. Press a to add.")
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
			right.field("", "", "Type", strings.ToUpper(string(it.Kind[:1]))+string(it.Kind[1:]), false, false)
			state := "Enabled"
			if it.Kind == extPlugin {
				state = "Installed"
			}
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
			right.line(paint(fg("muted"), "Select a skill or plugin to inspect its path and status."))
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
		kv("Type", string(it.Kind)),
		kv("Status", map[bool]string{true: "Enabled", false: "Disabled"}[it.Enabled]),
		kv("Source", orUnknown(it.Source)),
		kv("Scope", orUnknown(it.Scope)),
		kv("Path", orUnknown(it.Path)),
	}
	if it.Marketplace != "" {
		lines = append(lines, kv("Marketplace", it.Marketplace))
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

func (m harborModel) extensionsAddPage() uiPage {
	kindLabel := "Skill (via skills CLI → ~/.agents/skills)"
	placeholder := "owner/repo or leave empty to run `skills find`"
	if m.ext.addKind == extPlugin {
		kindLabel = "Codex Plugin (via `codex plugin add` → this Profile)"
		placeholder = "plugin-name or plugin@marketplace"
	}
	body := "Target: " + kindLabel + "\n\n" + textEntry(m.input, m.width, placeholder) + "\n\nHarbor hands off the terminal to the CLI, then reloads the list on exit."
	if m.ext.notice != "" {
		body += "\n\n" + m.ext.notice
	}
	return secondaryDocument(m.width, m.height, "Add extension", body, m.extensionsActions(), "", 0)
}

func (m harborModel) extensionsRemoveConfirmPage() uiPage {
	name, scopeNote := "unknown", ""
	if t := m.ext.removeTarget; t != nil {
		name = t.Name
		if t.Kind == extSkill {
			scopeNote = "Removing a global skill affects shared ~/.agents/skills across clients. Press Space on the list instead if you only want to disable it in this Profile."
		} else {
			scopeNote = "Removes this Codex plugin and its local cache from the current Profile only."
		}
	}
	body := fmt.Sprintf("Remove %s?\n\n%s", cleanUI(name), scopeNote)
	return secondaryDocument(m.width, m.height, "Remove extension", body, m.extensionsActions(), "", 0)
}

func (m harborModel) extensionsSyncPickPage() uiPage {
	p := newUIPage(m.width, m.height)
	p.title("Sync plugins from Profile")
	p.line(paint(fg("muted"), "Choose a source Profile to copy its Codex plugin manifest into "+cleanUI(m.profileShortLabel(m.ext.profile))+"."))
	p.line(paint(fg("muted"), "Only plugin definitions and marketplaces are synced; logins, history, and MCP OAuth tokens are never copied."))
	p.line("")
	for i, src := range m.ext.syncSources {
		id := fmt.Sprintf("ext-sync-src:%d", i)
		row := m.rowStyle(id, i == m.ext.syncCursor, false)
		p.listRow(id, "", cleanUI(src.Name), string(src.Kind), row)
	}
	if len(m.ext.syncSources) == 0 {
		p.emptyState("No other Profiles available to sync from.")
	}
	p.shortcuts(m.extensionsActions(), "")
	p.Hint = "↑↓/jk choose"
	return p.finish(0)
}

func (m harborModel) extensionsSyncConfirmPage() uiPage {
	plan := m.ext.syncPlan
	if plan == nil {
		return secondaryDocument(m.width, m.height, "Sync plugins", "Inspecting Profile plugins…", m.extensionsActions(), "", 0)
	}
	lines := []string{
		kv("Source Profile", cleanUI(plan.SourceName)),
		kv("Target Profile", cleanUI(plan.TargetName)),
		"",
		"Only plugin definitions and marketplace sources will be installed in the target Profile.",
		"Account logins, conversation histories, and MCP OAuth credentials are NOT copied.",
		"",
	}
	if len(plan.MarketplacesToAdd) > 0 {
		lines = append(lines, fmt.Sprintf("Marketplaces to add (%d):", len(plan.MarketplacesToAdd)))
		for _, mk := range plan.MarketplacesToAdd {
			lines = append(lines, note(mk.Name+" · "+mk.Source))
		}
		lines = append(lines, "")
	}
	if len(plan.PluginsToAdd) > 0 {
		lines = append(lines, fmt.Sprintf("Plugins to install (%d):", len(plan.PluginsToAdd)))
		for _, pl := range plan.PluginsToAdd {
			lines = append(lines, note(pl.Name))
		}
		lines = append(lines, "")
	}
	if len(plan.AlreadyInstalled) > 0 {
		lines = append(lines, fmt.Sprintf("Already installed in target (%d skipped): %s", len(plan.AlreadyInstalled), strings.Join(plan.AlreadyInstalled, ", ")))
	}
	if len(plan.MarketplacesToAdd) == 0 && len(plan.PluginsToAdd) == 0 {
		lines = append(lines, "Target Profile already has every plugin from the source Profile.")
	}
	return secondaryDocument(m.width, m.height, "Confirm plugin sync", strings.Join(lines, "\n"), m.extensionsActions(), "", m.scroll)
}

func (m harborModel) extensionsSyncResultPage() uiPage {
	res := m.ext.syncResult
	if res == nil {
		return secondaryDocument(m.width, m.height, "Plugin sync result", "No sync results.", m.extensionsActions(), "", 0)
	}
	lines := []string{
		kv("Source Profile", cleanUI(res.Plan.SourceName)),
		kv("Target Profile", cleanUI(res.Plan.TargetName)),
		"",
	}
	for _, out := range res.Outcomes {
		line := fmt.Sprintf("[%s] %s (%s)", out.State, out.Name, out.Kind)
		if out.Detail != "" {
			line += " — " + out.Detail
		}
		lines = append(lines, line)
	}
	if len(res.Outcomes) == 0 {
		lines = append(lines, "No changes needed.")
	}
	return secondaryDocument(m.width, m.height, "Plugin sync result", strings.Join(lines, "\n"), m.extensionsActions(), "", m.scroll)
}

func (m harborModel) runExtensionExec(action string, kind extensionKind, target string) (tea.Model, tea.Cmd) {
	svc := m.extensionService()
	if svc == nil || m.ext.profile == nil {
		m.ext.notice = "Extensions service or Profile is unavailable."
		m.screen = "extensions"
		return m, nil
	}
	cmd, err := svc.ExtensionExecCommand(context.Background(), *m.ext.profile, action, kind, target)
	if err != nil {
		m.ext.notice = err.Error()
		m.screen = "extensions"
		return m, nil
	}
	m.screen = "extensions"
	m.ext.notice = ""
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return appExtensionExecDone{action: action, err: err}
	})
}

func (m harborModel) extensionsPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case appExtensionsLoaded:
		if msg.generation != m.ext.generation || (m.ext.profile != nil && msg.profile != *m.ext.profile) {
			return m, nil, true
		}
		m.ext.loading = false
		if msg.err != nil {
			m.ext.err = msg.err.Error()
		} else {
			snap := msg.snapshot
			m.ext.snapshot = &snap
			m.ext.err = ""
			flat := m.extensionFlat()
			m.ext.cursor = max(0, min(m.ext.cursor, max(0, len(flat)-1)))
		}
		return m, nil, true
	case appExtensionToggled:
		if msg.generation != m.ext.generation {
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
		return m, m.beginExtensionsInspect(), true
	case appExtensionExecDone:
		if msg.err != nil {
			m.ext.notice = fmt.Sprintf("Extension %s finished with error: %v", msg.action, msg.err)
		} else {
			m.ext.notice = fmt.Sprintf("Extension %s completed.", msg.action)
		}
		return m, tea.Batch(tea.EnableMouseAllMotion, m.beginExtensionsInspect()), true
	case appPluginSyncPlanned:
		if msg.generation != m.ext.generation {
			return m, nil, true
		}
		m.ext.syncBusy = false
		if msg.err != nil {
			m.ext.notice = msg.err.Error()
			m.screen = "extensions"
			return m, nil, true
		}
		plan := msg.plan
		m.ext.syncPlan = &plan
		m.screen = "extensions-sync-confirm"
		m.scroll = 0
		return m, nil, true
	case appPluginSyncDone:
		if msg.generation != m.ext.generation {
			return m, nil, true
		}
		m.ext.syncBusy = false
		if msg.err != nil {
			m.ext.notice = msg.err.Error()
			m.screen = "extensions"
			return m, nil, true
		}
		res := msg.result
		m.ext.syncResult = &res
		m.screen = "extensions-sync-result"
		m.scroll = 0
		return m, m.beginExtensionsInspect(), true
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
				flat := m.extensionFlat()
				if len(flat) > 0 {
					m.ext.cursor = max(0, min(len(flat)-1, m.ext.cursor+delta))
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
			next, cmd := m.activateSessions(target)
			return next, cmd, true
		}
		if target == "search" && m.screen == "extensions" {
			m.ext.searching = true
			m.input.SetValue(m.ext.filter)
			m.input.CursorEnd()
			return m, m.input.Focus(), true
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
		if idxStr, ok := strings.CutPrefix(target, "ext-sync-src:"); ok {
			var idx int
			if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil {
				if m.ext.syncCursor == idx {
					return m.extensionsActivate("ext:sync-choose")
				}
				m.ext.syncCursor = idx
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
		case "ctrl+t":
			if m.ext.addKind == extSkill {
				m.ext.addKind = extPlugin
			} else {
				m.ext.addKind = extSkill
			}
			return m, nil, true
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.input.Blur()
			next, cmd := m.runExtensionExec("add", m.ext.addKind, val)
			return next, cmd, true
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd, true
		}
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
	case "extensions-detail", "extensions-sync-result":
		switch k {
		case "esc", "q":
			m.screen = "extensions"
			m.scroll = 0
		case " ", "space":
			if m.screen == "extensions-detail" {
				return m.extensionsActivate("ext:toggle")
			}
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
	case "extensions-sync-pick":
		switch k {
		case "esc", "q":
			m.screen = "extensions"
		case "down", "j":
			if len(m.ext.syncSources) > 0 {
				m.ext.syncCursor = min(len(m.ext.syncSources)-1, m.ext.syncCursor+1)
			}
		case "up", "k":
			m.ext.syncCursor = max(0, m.ext.syncCursor-1)
		case "enter":
			return m.extensionsActivate("ext:sync-choose")
		}
		return m, nil, true
	case "extensions-sync-confirm":
		switch k {
		case "esc", "q":
			m.ext.syncPlan = nil
			m.screen = "extensions"
		case "enter":
			return m.extensionsActivate("ext:sync-run")
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
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
		if flat := m.extensionFlat(); len(flat) > 0 {
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
	case "y":
		return m.extensionsActivate("ext:sync")
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
	case "ext:back":
		m.ext.removeTarget = nil
		m.ext.syncPlan = nil
		m.screen = "extensions"
		m.scroll = 0
		return m, nil, true
	case "ext:detail":
		if _, ok := m.selectedExtension(); ok {
			m.screen = "extensions-detail"
			m.scroll = 0
		}
		return m, nil, true
	case "ext:refresh":
		m.ext.notice = ""
		return m, m.beginExtensionsInspect(), true
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
		gen := m.ext.generation
		return m, func() tea.Msg {
			err := svc.ToggleExtension(context.Background(), binding, it, nextEnabled)
			return appExtensionToggled{generation: gen, profile: binding, item: it, enabled: nextEnabled, err: err}
		}, true
	case "ext:add":
		m.ext.addKind = extSkill
		if it, ok := m.selectedExtension(); ok && it.Kind == extPlugin {
			m.ext.addKind = extPlugin
		}
		m.ext.notice = ""
		m.input.SetValue("")
		m.screen = "extensions-add"
		return m, m.input.Focus(), true
	case "ext:add-kind":
		if m.ext.addKind == extSkill {
			m.ext.addKind = extPlugin
		} else {
			m.ext.addKind = extSkill
		}
		return m, nil, true
	case "ext:add-confirm":
		val := strings.TrimSpace(m.input.Value())
		m.input.Blur()
		next, cmd := m.runExtensionExec("add", m.ext.addKind, val)
		return next, cmd, true
	case "ext:update":
		target := ""
		if it, ok := m.selectedExtension(); ok && it.Kind == extSkill {
			target = it.Name
		}
		next, cmd := m.runExtensionExec("update", extSkill, target)
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
		t := *m.ext.removeTarget
		m.ext.removeTarget = nil
		target := t.Name
		if t.Kind == extPlugin && t.ID != "" {
			target = t.ID
		}
		next, cmd := m.runExtensionExec("remove", t.Kind, target)
		return next, cmd, true
	case "ext:sync":
		if len(m.profiles) < 2 || m.ext.profile == nil {
			m.ext.notice = "Plugin sync requires at least two Profiles."
			return m, nil, true
		}
		sources := []profile{}
		for _, p := range m.profiles {
			if p.ID != m.ext.profile.ProfileID {
				sources = append(sources, p)
			}
		}
		m.ext.syncSources = sources
		m.ext.syncCursor = 0
		m.screen = "extensions-sync-pick"
		return m, nil, true
	case "ext:sync-choose":
		if m.ext.syncCursor < 0 || m.ext.syncCursor >= len(m.ext.syncSources) || m.ext.profile == nil {
			return m, nil, true
		}
		srcProfile := m.ext.syncSources[m.ext.syncCursor]
		srcBinding, err := bindingOf(srcProfile, "codex")
		if err != nil {
			m.ext.notice = err.Error()
			m.screen = "extensions"
			return m, nil, true
		}
		svc := m.extensionService()
		if svc == nil {
			return m, nil, true
		}
		dstBinding := *m.ext.profile
		m.ext.syncBusy = true
		gen := m.ext.generation
		return m, func() tea.Msg {
			plan, err := svc.PlanPluginSync(context.Background(), srcBinding, dstBinding)
			return appPluginSyncPlanned{generation: gen, plan: plan, err: err}
		}, true
	case "ext:sync-run":
		if m.ext.syncPlan == nil {
			return m, nil, true
		}
		svc := m.extensionService()
		if svc == nil {
			return m, nil, true
		}
		plan := *m.ext.syncPlan
		m.ext.syncBusy = true
		gen := m.ext.generation
		return m, func() tea.Msg {
			res, err := svc.ExecutePluginSync(context.Background(), plan)
			return appPluginSyncDone{generation: gen, result: res, err: err}
		}, true
	}
	return m, nil, true
}

func (m harborModel) extensionsHelp() string {
	return strings.Join([]string{
		"🧩 Extensions",
		"",
		"Skills are shared from ~/.agents/skills and toggled per Profile in $CODEX_HOME/config.toml.",
		"Plugins belong to each Profile's $CODEX_HOME/plugins; use Sync to install definitions across Profiles without copying credentials or history.",
		"",
		kv("↑↓/jk", "Select skill or plugin"),
		kv("Enter / i", "Inspect full path and details"),
		kv("Space / t", "Enable or disable selected skill for this Profile"),
		kv("a", "Add skill (via skills CLI) or plugin (via codex plugin add)"),
		kv("u", "Update skills via skills CLI"),
		kv("x", "Remove selected skill or plugin"),
		kv("y", "Sync Codex plugin manifest from another Profile"),
		kv("/", "Filter skills and plugins"),
		kv("p", "Switch Profile"),
		kv("r", "Refresh"),
	}, "\n")
}
