package spotlight

import (
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/ui"
	"github.com/asheshgoplani/agent-deck/internal/upstream/lazygit/commandmenu"
)

type Kind string

const (
	KindRoot              Kind = "root"
	KindLanguage          Kind = "language"
	KindTheme             Kind = "theme"
	KindAdvancedResources Kind = "advanced_resources"
)

// RootItems returns the primary Spotlight command list (localized).
// Right-column Shortcut values follow Grok PaletteEntry.shortcut style
// (slash commands / chord hints), not lazygit single-key bindings.
func RootItems() []commandmenu.Item {
	return []commandmenu.Item{
		{
			ID: "advanced_resources", LabelColumns: []string{i18n.T("command.advanced.title"), i18n.T("command.advanced.desc")},
			Aliases: []string{"advanced", "resources", "settings"}, Shortcut: "/advanced",
			Section: i18n.T("spotlight.category.config"), OpensMenu: true,
		},
		{
			ID: "language", LabelColumns: []string{i18n.T("command.language.title"), i18n.T("command.language.desc")},
			Aliases: []string{"language", "lang"}, Shortcut: "/language",
			Section: i18n.T("spotlight.category.system"), OpensMenu: true,
		},
		{
			ID: "theme", LabelColumns: []string{i18n.T("command.theme.title"), i18n.T("command.theme.desc")},
			Aliases: []string{"theme", "themes"}, Shortcut: "/theme",
			Section: i18n.T("spotlight.category.system"), OpensMenu: true,
		},
		{
			ID: "quit", LabelColumns: []string{i18n.T("command.quit.title"), i18n.T("command.quit.desc")},
			Aliases: []string{"quit", "exit"}, Shortcut: "/quit",
			Section: i18n.T("spotlight.category.system"),
		},
		{
			ID: "reload", LabelColumns: []string{i18n.T("command.reload.title"), i18n.T("command.reload.desc")},
			Aliases: []string{"reload"}, Shortcut: "/reload",
			Section: i18n.T("spotlight.category.runtime"),
		},
		{
			ID: "previous", LabelColumns: []string{i18n.T("command.previous.title"), i18n.T("command.previous.desc")},
			Aliases: []string{"previous", "prev"}, Shortcut: "/prev-tab",
			Section: i18n.T("spotlight.category.navigation"), KeepOpen: true,
		},
		{
			ID: "next", LabelColumns: []string{i18n.T("command.next.title"), i18n.T("command.next.desc")},
			Aliases: []string{"next"}, Shortcut: "/next-tab",
			Section: i18n.T("spotlight.category.navigation"), KeepOpen: true,
		},
		{
			ID: "filter", LabelColumns: []string{i18n.T("command.filter.title"), i18n.T("command.filter.desc")},
			Aliases: []string{"filter"}, Shortcut: "/filter",
			Section: i18n.T("spotlight.category.view"),
		},
		{
			ID: "sort", LabelColumns: []string{i18n.T("command.sort.title"), i18n.T("command.sort.desc")},
			Aliases: []string{"sort"}, Shortcut: "/sort",
			Section: i18n.T("spotlight.category.view"),
		},
	}
}

func AdvancedResourceItems() []commandmenu.Item {
	items := []struct{ id, label, desc string }{
		{"profiles", "Profiles", "CLI launch resources"},
		{"quotas", "Quota Groups", "Raw quota resources"},
		{"targets", "Targets", "Protocol and capability bindings"},
		{"endpoints", "Endpoints", "HTTP transport resources"},
		{"credentials", "Credentials", "Provider secret bindings"},
		{"routes", "Routes", "Raw routing resources"},
		{"backend_sets", "Backend Sets", "Target candidate sets"},
		{"content_policies", "Content Policies", "Request and response policies"},
		{"model_policies", "Model Policies", "Physical model mappings"},
		{"model_projections", "Model Projections", "Client model catalogs"},
		{"transforms", "Transforms", "Compatibility transforms"},
	}
	out := make([]commandmenu.Item, 0, len(items))
	for _, item := range items {
		out = append(out, commandmenu.Item{
			ID: item.id, LabelColumns: []string{item.label, item.desc}, Aliases: []string{item.id, item.label},
			Section: i18n.T("command.advanced.title"),
		})
	}
	return out
}

func LanguageItems(current string) []commandmenu.Item {
	items := make([]commandmenu.Item, 0, len(i18n.Locales()))
	for _, locale := range i18n.Locales() {
		widget := commandmenu.RadioUnselected
		if string(locale.ID) == current {
			widget = commandmenu.RadioSelected
		}
		items = append(items, commandmenu.Item{
			ID:           string(locale.ID),
			LabelColumns: []string{locale.Name, string(locale.ID)},
			Shortcut:     string(locale.ID),
			Widget:       widget,
			Section:      i18n.T("dialog.language.title"),
		})
	}
	return items
}

func ThemeItems(current string) []commandmenu.Item {
	items := make([]commandmenu.Item, 0, len(ui.ListThemes()))
	for _, name := range ui.ListThemes() {
		widget := commandmenu.RadioUnselected
		if name == current {
			widget = commandmenu.RadioSelected
		}
		items = append(items, commandmenu.Item{
			ID:           name,
			LabelColumns: []string{name, ""},
			Shortcut:     name,
			Widget:       widget,
			Section:      i18n.T("dialog.theme.title"),
		})
	}
	return items
}

func MenuOptions(title string, items []commandmenu.Item) commandmenu.Options {
	return commandmenu.Options{
		Title:                     title,
		Items:                     items,
		AllowFilteringKeybindings: true,
		HideCancel:                true,
		EmptyText:                 i18n.T("spotlight.empty"),
		FooterText:                i18n.T("spotlight.footer"),
		Prompt:                    " " + i18n.T("spotlight.filter") + " ",
	}
}
