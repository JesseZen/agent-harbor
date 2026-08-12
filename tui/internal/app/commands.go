package app

import (
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/preferences"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/routes"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/targets"
	"github.com/asheshgoplani/agent-deck/internal/spotlight"
	"github.com/asheshgoplani/agent-deck/internal/ui"
	"github.com/asheshgoplani/agent-deck/internal/upstream/lazygit/commandmenu"
	tea "github.com/charmbracelet/bubbletea"
)

func (model *Model) openCommandOverlay() {
	model.commandStack = nil
	model.commandKind = spotlight.KindRoot
	model.showCommandMenu(spotlight.KindRoot, nil)
}

func (model *Model) openLanguageOverlay() {
	model.showCommandMenu(spotlight.KindLanguage, nil)
}

func (model *Model) openThemeOverlay() {
	model.showCommandMenu(spotlight.KindTheme, nil)
}

func (model *Model) showCommandMenu(kind spotlight.Kind, restore *commandFrame) {
	var opts commandmenu.Options
	switch kind {
	case spotlight.KindLanguage:
		opts = spotlight.MenuOptions(i18n.T("dialog.language.title"), spotlight.LanguageItems(string(i18n.GetLocale())))
	case spotlight.KindTheme:
		opts = spotlight.MenuOptions(i18n.T("dialog.theme.title"), spotlight.ThemeItems(ui.GetCurrentThemeName()))
	case spotlight.KindAdvancedResources:
		opts = spotlight.MenuOptions(i18n.T("command.advanced.title"), spotlight.AdvancedResourceItems())
	default:
		kind = spotlight.KindRoot
		opts = spotlight.MenuOptions(i18n.T("spotlight.title"), spotlight.RootItems())
	}
	model.commandKind = kind
	model.commands = commandmenu.New(opts)
	model.commands.Open()
	if restore != nil {
		model.commands.RestoreNav(restore.filter, restore.cursor, restore.scroll)
	}
	// Nested frames use Esc-back footer (Grok ArgPicker → previous_palette).
	if len(model.commandStack) > 0 {
		model.commands.SetFooterText(i18n.T("spotlight.footer_back"))
	}
}

func (model *Model) pushCommandFrame() {
	if model.commands == nil {
		return
	}
	filter, cursor, scroll := model.commands.NavState()
	model.commandStack = append(model.commandStack, commandFrame{
		kind:   model.commandKind,
		filter: filter,
		cursor: cursor,
		scroll: scroll,
	})
}

func (model *Model) popCommandFrame() bool {
	if len(model.commandStack) == 0 {
		return false
	}
	frame := model.commandStack[len(model.commandStack)-1]
	model.commandStack = model.commandStack[:len(model.commandStack)-1]
	model.showCommandMenu(frame.kind, &frame)
	return true
}

func (model *Model) dismissCommandOverlay() {
	model.commandStack = nil
	model.commandKind = spotlight.KindRoot
	if model.commands != nil {
		model.commands.Close()
	}
}

func (model *Model) persistPrefs() {
	_ = preferences.Save(preferences.Prefs{
		Locale: string(i18n.GetLocale()),
		Theme:  ui.GetCurrentThemeName(),
	})
}

func (model *Model) updateCommandOverlay(message tea.Msg) (tea.Model, tea.Cmd) {
	// Snapshot nav before Update: leaf Close() clears filter/cursor.
	priorFilter, priorCursor, priorScroll := model.commands.NavState()

	result := model.commands.Update(message)
	if result.DisabledReason != "" {
		model.status = result.DisabledReason
		return model, nil
	}
	if result.Closed && result.SelectedID == "" {
		// Esc / [x] / outside: pop stack like Grok previous_palette restore.
		if model.popCommandFrame() {
			return model, nil
		}
		model.dismissCommandOverlay()
		return model, nil
	}
	if result.SelectedID == "" {
		return model, nil
	}

	switch model.commandKind {
	case spotlight.KindLanguage:
		i18n.SetLocale(result.SelectedID)
		model.persistPrefs()
		// Refresh radio marks but keep filter/scroll; cursor lands on selection.
		model.showCommandMenu(spotlight.KindLanguage, &commandFrame{
			kind:   spotlight.KindLanguage,
			filter: priorFilter,
			cursor: priorCursor,
			scroll: priorScroll,
		})
		model.commands.SelectID(result.SelectedID)
		return model, model.setTransientStatus(i18n.T("command.language.title") + ": " + result.SelectedID)
	case spotlight.KindTheme:
		ui.InitTheme(result.SelectedID)
		model.persistPrefs()
		model.showCommandMenu(spotlight.KindTheme, &commandFrame{
			kind:   spotlight.KindTheme,
			filter: priorFilter,
			cursor: priorCursor,
			scroll: priorScroll,
		})
		model.commands.SelectID(result.SelectedID)
		return model, model.setTransientStatus(i18n.T("command.theme.title") + ": " + result.SelectedID)
	case spotlight.KindAdvancedResources:
		model.openAdvancedResource(result.SelectedID)
		model.dismissCommandOverlay()
		return model, nil
	}

	switch result.SelectedID {
	case "language":
		model.pushCommandFrame()
		model.openLanguageOverlay()
		return model, nil
	case "theme":
		model.pushCommandFrame()
		model.openThemeOverlay()
		return model, nil
	case "advanced_resources":
		model.pushCommandFrame()
		model.showCommandMenu(spotlight.KindAdvancedResources, nil)
		return model, nil
	case "quit":
		model.dismissCommandOverlay()
		model.cancel()
		return model, tea.Quit
	case "reload":
		model.dismissCommandOverlay()
		return model, model.loadAll(false)
	case "publish":
		model.dismissCommandOverlay()
		return model, model.publish()
	case "discard":
		model.dismissCommandOverlay()
		if model.targets != nil {
			if err := model.targets.DiscardOwnedStages(model.ctx); err != nil {
				model.targets.ApplyCleanupPending()
				model.status = "Discard blocked: secret cleanup pending"
				return model, nil
			}
		}
		if model.draft != nil {
			model.draft.Discard()
			model.refreshPages(false)
		}
		model.status = "Draft discarded"
	case "previous":
		// KeepOpen: do not rebuild — preserve filter/cursor/scroll in place.
		model.shiftTab(-1)
		model.pageFocus = false
		return model, nil
	case "next":
		model.shiftTab(1)
		model.pageFocus = false
		return model, nil
	case "filter":
		model.dismissCommandOverlay()
		_, _, command := model.updateActivePage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		return model, command
	case "sort":
		model.dismissCommandOverlay()
		_, _, command := model.updateActivePage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		return model, command
	}
	model.resizePages()
	return model, nil
}

func (model *Model) openAdvancedResource(id string) {
	model.advancedPage = ""
	switch id {
	case "profiles":
		model.active, model.advancedPage = tabSessions, "profiles"
	case "quotas":
		model.active, model.advancedPage = tabSessions, "quotas"
	case "targets":
		model.active = tabTargets
		model.targets.SetKind(targets.KindTargets)
	case "endpoints":
		model.active = tabTargets
		model.targets.SetKind(targets.KindEndpoints)
	case "credentials":
		model.active = tabTargets
		model.targets.SetKind(targets.KindCredentials)
	case "routes":
		model.active = tabRoutes
		model.routes.SetKind(routes.KindRoutes)
	case "backend_sets":
		model.active = tabRoutes
		model.routes.SetKind(routes.KindBackendSets)
	case "content_policies":
		model.active = tabRoutes
		model.routes.SetKind(routes.KindContentPolicies)
	case "model_policies":
		model.active = tabRoutes
		model.routes.SetKind(routes.KindModelPolicies)
	case "model_projections":
		model.active = tabRoutes
		model.routes.SetKind(routes.KindModelProjections)
	case "transforms":
		model.active = tabRoutes
		model.routes.SetKind(routes.KindTransforms)
	}
	model.pageFocus = false
	model.resizePages()
}
